/*
 * Copyright (c) 2026. Papermark - All Rights Reserved
 * Unauthorized copying of this file, via any medium is strictly prohibited
 * Proprietary and confidential
 * Written by Ebot Tabi <ebot.tabi@gmail.com>, 15/10/2026
 */
package services

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log"
	"os"
	"path/filepath"
	"slices"
	"strings"

	"papermark/internal/cache"
	"papermark/internal/integrations/anthropic"
	"papermark/internal/models"
	"papermark/internal/models/queries"

	sdk "github.com/anthropics/anthropic-sdk-go"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
)

// RunTaskJob is the job type that runs a task; its key is the task id, so a
// task has at most one active run.
const RunTaskJob = "task.run"

// RunTaskPayload is the payload of a RunTaskJob.
type RunTaskPayload struct {
	TaskID uuid.UUID `json:"taskId"`
}

const interruptedText = "Not run: the task was interrupted."

// AgentService runs the agent loop for one task as a durable job. A run is
// idempotent: it rebuilds everything from Postgres, so a retried or
// re-enqueued job continues where the last one stopped.
type AgentService struct {
	pool       *pgxpool.Pool
	ev         *events
	bus        cache.Bus
	s          *Services
	client     *anthropic.Client
	workspaces string
	model      string
	maxTokens  int64
	maxSteps   int
}

// Workspace returns a task's workspace directory.
func (a *AgentService) Workspace(id uuid.UUID) string {
	return filepath.Join(a.workspaces, id.String())
}

// Run advances a running task until it finishes, parks on the user, is
// stopped, or ctx ends (worker shutdown; the job is then retried).
func (a *AgentService) Run(ctx context.Context, taskID uuid.UUID) error {
	ctx, cancel := context.WithCancelCause(ctx)
	defer cancel(nil)
	frames, unsubscribe, err := a.bus.Subscribe(ctx, taskID.String())
	if err != nil {
		return err
	}
	defer unsubscribe()
	go func() {
		for f := range frames {
			if f.Kind == cache.FrameStop {
				cancel(errStopped)
			}
		}
	}()
	// Read after subscribing, so a stop is seen either here or on the bus.
	task, err := queries.GetTask(ctx, a.pool, taskID)
	if errors.Is(err, queries.ErrNotFound) {
		return nil
	}
	if err != nil {
		return err
	}
	if task.Status != models.TaskRunning {
		return nil
	}
	if task.StopRequested {
		cancel(errStopped)
	}

	err = a.loop(ctx, task)
	if errors.Is(context.Cause(ctx), errStopped) {
		return a.stopped(context.WithoutCancel(ctx), taskID)
	}
	if ctx.Err() != nil {
		return ctx.Err()
	}
	if err != nil {
		log.Printf("agent: task %s failed: %v", taskID, err)
		return a.ev.taskTx(ctx, taskID, func(_ pgx.Tx, t *models.Task, emit Emit) error {
			if t.Status != models.TaskRunning {
				return nil
			}
			return emit(textEvent{"error", err.Error()})
		})
	}
	return nil
}

func (a *AgentService) stopped(ctx context.Context, taskID uuid.UUID) error {
	return a.ev.taskTx(ctx, taskID, func(tx pgx.Tx, t *models.Task, emit Emit) error {
		if err := queries.SetTaskStopRequested(ctx, tx, taskID, false); err != nil {
			return err
		}
		if t.Status != models.TaskRunning {
			return nil
		}
		return emit(textEvent{"stopped", "Stopped by the user."})
	})
}

// run is the state of one Run call.
type run struct {
	a         *AgentService
	id        uuid.UUID
	mode      string
	tools     *tools
	conn      *ConnectorToolset
	toolsJSON json.RawMessage
}

func (a *AgentService) loop(ctx context.Context, task *models.Task) error {
	workspace := a.Workspace(task.ID)
	if err := os.MkdirAll(workspace, 0o755); err != nil {
		return err
	}
	conn, err := a.s.Connectors.Snapshot(ctx)
	if err != nil {
		log.Printf("agent: task %s runs without connectors: %v", task.ID, err)
		conn = &ConnectorToolset{}
	}
	if conn.Close != nil {
		defer conn.Close()
	}

	r := &run{a: a, id: task.ID, mode: task.Mode, conn: conn}
	r.tools = &tools{workspace: workspace, notify: func(text string) error {
		return a.ev.taskTx(ctx, task.ID, func(_ pgx.Tx, _ *models.Task, emit Emit) error {
			return emit(textEvent{"notify", text})
		})
	}}
	all := slices.Clone(builtinTools)
	for _, t := range conn.Tools {
		raw, err := json.Marshal(struct {
			Name        string          `json:"name"`
			Description string          `json:"description,omitempty"`
			InputSchema json.RawMessage `json:"input_schema"`
		}{t.Name, t.Description, t.InputSchema})
		if err != nil {
			return err
		}
		all = append(all, raw)
	}
	if r.toolsJSON, err = json.Marshal(all); err != nil {
		return err
	}

	messages, err := queries.ListTaskMessages(ctx, a.pool, task.ID)
	if err != nil {
		return err
	}
	if n := len(messages); n > 0 && roleOf(messages[n-1]) == "assistant" {
		if done, err := r.afterAssistant(ctx, messages[n-1], true); err != nil || done {
			return err
		}
	} else if err := r.deliverInbox(ctx); err != nil {
		return err
	}

	for step := 0; step < a.maxSteps; step++ {
		if ctx.Err() != nil {
			return ctx.Err()
		}
		if messages, err = queries.ListTaskMessages(ctx, a.pool, task.ID); err != nil {
			return err
		}
		effort := "high"
		if r.mode == "speed" {
			effort = "low"
		}
		res, err := a.client.Stream(ctx, anthropic.Request{
			Model: a.model, MaxTokens: a.maxTokens, System: systemPrompt,
			Tools: r.toolsJSON, Messages: messages, Effort: effort,
		}, r.deltas(ctx))
		if err != nil {
			return err
		}
		assistant, err := json.Marshal(struct {
			Role    string          `json:"role"`
			Content json.RawMessage `json:"content"`
		}{"assistant", res.Content})
		if err != nil {
			return err
		}
		err = a.ev.taskTx(ctx, task.ID, func(tx pgx.Tx, _ *models.Task, emit Emit) error {
			if err := queries.AppendTaskMessage(ctx, tx, task.ID, assistant); err != nil {
				return err
			}
			u := res.Usage
			if err := queries.AddTaskUsage(ctx, tx, task.ID, u.InputTokens, u.OutputTokens, u.CacheReadTokens, u.CacheWriteTokens); err != nil {
				return err
			}
			for _, e := range serverToolEvents(res.Content) {
				if err := emit(e); err != nil {
					return err
				}
			}
			if res.StopReason == "refusal" {
				return emit(textEvent{"error", "The model declined this task."})
			}
			return nil
		})
		if err != nil || res.StopReason == "refusal" {
			return err
		}
		if res.StopReason == "pause_turn" {
			continue
		}
		if done, err := r.afterAssistant(ctx, assistant, false); err != nil || done {
			return err
		}
	}
	return fmt.Errorf("Stopped after %d steps.", a.maxSteps)
}

// deltas streams text, and the growing text field of user-facing tool calls,
// to the task's live channel.
func (r *run) deltas(ctx context.Context) anthropic.OnEvent {
	raw := map[int64]string{}
	sent := map[int64]int{}
	publish := func(text string) {
		r.a.bus.Publish(ctx, r.id.String(), cache.Frame{Kind: cache.FrameDelta, Text: text})
	}
	return func(ev sdk.BetaRawMessageStreamEventUnion, snap *sdk.BetaMessage) {
		if ev.Type != "content_block_delta" {
			return
		}
		switch ev.Delta.Type {
		case "text_delta":
			publish(ev.Delta.Text)
		case "input_json_delta":
			if ev.Index >= int64(len(snap.Content)) || snap.Content[ev.Index].Type != "tool_use" {
				return
			}
			field, ok := previewField[snap.Content[ev.Index].Name]
			if !ok {
				return
			}
			raw[ev.Index] += ev.Delta.PartialJSON
			text, _ := partialString(raw[ev.Index], field)
			if len(text) > sent[ev.Index] {
				publish(text[sent[ev.Index]:])
				sent[ev.Index] = len(text)
			}
		}
	}
}

func roleOf(message json.RawMessage) string {
	var m struct {
		Role string `json:"role"`
	}
	json.Unmarshal(message, &m)
	return m.Role
}

type toolCall struct {
	Type  string          `json:"type"`
	ID    string          `json:"id"`
	Name  string          `json:"name"`
	Input json.RawMessage `json:"input"`
	Text  string          `json:"text"`
}

func blocksOf(message json.RawMessage) []toolCall {
	var m struct {
		Content []toolCall `json:"content"`
	}
	json.Unmarshal(message, &m)
	return m.Content
}

type toolResult struct {
	Type      string `json:"type"`
	ToolUseID string `json:"tool_use_id"`
	Content   string `json:"content"`
	IsError   bool   `json:"is_error,omitempty"`
}

type textBlock struct {
	Type string `json:"type"`
	Text string `json:"text"`
}

// userMessage builds a user turn from raw blocks followed by text blocks.
func userMessage(blocks []json.RawMessage, texts []string) (json.RawMessage, error) {
	content := slices.Clone(blocks)
	for _, t := range texts {
		raw, err := json.Marshal(textBlock{"text", t})
		if err != nil {
			return nil, err
		}
		content = append(content, raw)
	}
	return json.Marshal(struct {
		Role    string            `json:"role"`
		Content []json.RawMessage `json:"content"`
	}{"user", content})
}

// deliverInbox turns queued messages into the next user turn.
func (r *run) deliverInbox(ctx context.Context) error {
	return r.a.ev.taskTx(ctx, r.id, func(tx pgx.Tx, _ *models.Task, _ Emit) error {
		texts, err := queries.DrainTaskInbox(ctx, tx, r.id)
		if err != nil || len(texts) == 0 {
			return err
		}
		msg, err := userMessage(nil, []string{strings.Join(texts, "\n\n")})
		if err != nil {
			return err
		}
		return queries.AppendTaskMessage(ctx, tx, r.id, msg)
	})
}

// afterAssistant runs the tool calls of an assistant message, each result
// recorded as it lands, then writes the user turn and decides whether the
// task is done. On resume it continues from the recorded results: the call
// the task parked on gets the user's answer; a call a dead worker left
// unrecorded gets an interrupted result. done means the run is over.
func (r *run) afterAssistant(ctx context.Context, assistant json.RawMessage, resume bool) (done bool, err error) {
	var calls []toolCall
	for _, b := range blocksOf(assistant) {
		if b.Type == "tool_use" {
			calls = append(calls, b)
		}
	}
	task, err := queries.GetTask(ctx, r.a.pool, r.id)
	if err != nil {
		return false, err
	}
	i := len(task.PartialResults)
	if resume && i < len(calls) {
		if p := task.Pending; p != nil && p.Answer != nil && p.ToolUseID == calls[i].ID {
			// Consume the approval before the call starts, so a worker that dies
			// mid-call leaves it interrupted instead of running it twice.
			if err := queries.SetTaskRunState(ctx, r.a.pool, r.id, nil, task.PartialResults); err != nil {
				return false, err
			}
			out, ev := r.answered(ctx, calls[i], p.Kind, *p.Answer)
			if err := r.record(ctx, calls[i], out, ev); err != nil {
				return false, err
			}
			i++
		} else {
			for ; i < len(calls); i++ {
				if err := r.record(ctx, calls[i], ToolOutput{Text: interruptedText, IsError: true}, nil); err != nil {
					return false, err
				}
			}
		}
	}
	for ; i < len(calls); i++ {
		c := calls[i]
		switch {
		case c.Name == "message_ask_user":
			var in struct {
				Text string `json:"text"`
			}
			json.Unmarshal(c.Input, &in)
			return true, r.park(ctx, c, textEvent{"ask", in.Text})
		case c.Name == "idle":
			if err := r.record(ctx, c, ToolOutput{Text: "ok"}, nil); err != nil {
				return false, err
			}
		case r.needsApproval(c.Name) && !slices.Contains(task.AlwaysAllowed, c.Name):
			return true, r.park(ctx, c, confirmEvent{"confirm", c.Name, c.Input})
		default:
			out := r.execute(ctx, c)
			if err := r.record(ctx, c, out, toolEvent{"tool", c.Name, c.Input, out.Text, out.IsError}); err != nil {
				return false, err
			}
		}
	}
	return r.finish(ctx, calls, assistant)
}

// answered applies the user's reply or decision to the call the task parked on.
func (r *run) answered(ctx context.Context, c toolCall, kind, answer string) (ToolOutput, any) {
	if kind == "ask" {
		return ToolOutput{Text: answer}, nil
	}
	out := ToolOutput{Text: "The user declined this action.", IsError: true}
	if answer != "deny" {
		out = r.execute(ctx, c)
	}
	return out, toolEvent{"tool", c.Name, c.Input, out.Text, out.IsError}
}

func (r *run) needsApproval(name string) bool {
	return name == "shell_exec" || (r.conn.Handlers[name] != nil && r.conn.NeedsApproval[name])
}

func (r *run) execute(ctx context.Context, c toolCall) ToolOutput {
	var out ToolOutput
	var err error
	switch {
	case r.tools.has(c.Name):
		out, err = r.tools.call(ctx, c.Name, c.Input)
	case r.conn.Handlers[c.Name] != nil:
		out.Text, out.IsError, err = r.conn.Handlers[c.Name](ctx, c.Input)
	default:
		out = ToolOutput{Text: "Unknown tool: " + c.Name, IsError: true}
	}
	if err != nil {
		out = ToolOutput{Text: err.Error(), IsError: true}
	}
	return out
}

// record stores one call's result (and its event) so a retried run skips it.
// A call cut short by shutdown is not recorded: the retry reports it as interrupted.
func (r *run) record(ctx context.Context, c toolCall, out ToolOutput, event any) error {
	if ctx.Err() != nil && !errors.Is(context.Cause(ctx), errStopped) {
		return ctx.Err()
	}
	result, err := json.Marshal(toolResult{"tool_result", c.ID, out.Text, out.IsError})
	if err != nil {
		return err
	}
	// A stopped run still records the killed call's result.
	bg := context.WithoutCancel(ctx)
	err = r.a.ev.taskTx(bg, r.id, func(tx pgx.Tx, t *models.Task, emit Emit) error {
		if event != nil {
			if err := emit(event); err != nil {
				return err
			}
		}
		return queries.SetTaskRunState(bg, tx, r.id, nil, append(t.PartialResults, result))
	})
	if err != nil {
		return err
	}
	return ctx.Err()
}

// park records the ask or confirm the task now waits on; the job ends here
// and /reply or /confirm enqueues the next run.
func (r *run) park(ctx context.Context, c toolCall, event any) error {
	kind := "confirm"
	if c.Name == "message_ask_user" {
		kind = "ask"
	}
	return r.a.ev.taskTx(ctx, r.id, func(tx pgx.Tx, t *models.Task, emit Emit) error {
		if err := emit(event); err != nil {
			return err
		}
		return queries.SetTaskRunState(ctx, tx, r.id, &models.PendingAction{Kind: kind, Name: c.Name, ToolUseID: c.ID}, t.PartialResults)
	})
}

// finish writes the user turn (tool results plus any queued messages) and
// goes idle when the model said it is done and nothing new arrived.
func (r *run) finish(ctx context.Context, calls []toolCall, assistant json.RawMessage) (done bool, err error) {
	err = r.a.ev.taskTx(ctx, r.id, func(tx pgx.Tx, t *models.Task, emit Emit) error {
		texts, err := queries.DrainTaskInbox(ctx, tx, r.id)
		if err != nil {
			return err
		}
		if len(t.PartialResults)+len(texts) > 0 {
			msg, err := userMessage(t.PartialResults, texts)
			if err != nil {
				return err
			}
			if err := queries.AppendTaskMessage(ctx, tx, r.id, msg); err != nil {
				return err
			}
		}
		if err := queries.SetTaskRunState(ctx, tx, r.id, nil, nil); err != nil {
			return err
		}
		idle := idleResult(calls, t.PartialResults, assistant)
		if idle == nil || len(texts) > 0 {
			return nil
		}
		done = true
		return emit(*idle)
	})
	return done, err
}

// idleResult is the task's result when this turn finished it: the idle
// call's input, or the text of a turn with no tool calls.
func idleResult(calls []toolCall, results []json.RawMessage, assistant json.RawMessage) *idleEvent {
	if len(calls) == 0 {
		var texts []string
		for _, b := range blocksOf(assistant) {
			if b.Type == "text" {
				texts = append(texts, b.Text)
			}
		}
		return &idleEvent{"idle", strings.Join(texts, "\n"), []string{}}
	}
	failed := map[string]bool{}
	for _, raw := range results {
		var res toolResult
		json.Unmarshal(raw, &res)
		failed[res.ToolUseID] = res.IsError
	}
	for _, c := range calls {
		if c.Name != "idle" || failed[c.ID] {
			continue
		}
		var in struct {
			Result string          `json:"result"`
			Files  json.RawMessage `json:"files"`
		}
		json.Unmarshal(c.Input, &in)
		files := []string{}
		json.Unmarshal(in.Files, &files)
		if files == nil {
			files = []string{}
		}
		return &idleEvent{"idle", in.Result, files}
	}
	return nil
}
