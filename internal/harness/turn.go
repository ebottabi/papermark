/*
 * Copyright (c) 2026. Papermark - All Rights Reserved
 * Unauthorized copying of this file, via any medium is strictly prohibited
 * Proprietary and confidential
 * Written by Ebot Tabi <ebot.tabi@gmail.com>, 15/10/2026
 */
package harness

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log"
	"os"
	"slices"
	"strings"
	"time"

	"papermark/internal/cache"
	"papermark/internal/integrations/fireworks"
	"papermark/internal/models"
	"papermark/internal/models/queries"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
)

const (
	interruptedText = "Not run: the task was interrupted."
	stuckText       = "The agent was stuck repeating the same step."
	wrapUpText      = "You have used every step allowed for this task. Do not call any tool. Reply with a summary of what you did, what is left, and the workspace paths of any files the user should open."
	// trimKeep is how many recent messages an over-budget request keeps whole.
	trimKeep = 6
	trimCap  = 800
	trimNote = "\n[shortened; run the tool again if you need the full result]"
)

// turn is one run of the agent loop for a task: call the model with the
// tools, run the calls it makes, store both, and repeat until it answers
// without one, parks on the user, or runs out of steps.
type turn struct {
	h          *Harness
	id         uuid.UUID
	userID     uuid.UUID
	scheduleID *uuid.UUID // set when a routine started the task
	mode       string
	system     string
	maxSteps   int
	tools      *tools
	conn       *ConnectorToolset
	specs      []json.RawMessage
}

// runTurn builds the task's turn: its workspace, its connectors and the
// tool specs the model is offered.
func (h *Harness) runTurn(ctx context.Context, task *models.Task) error {
	if !h.apiKeySet {
		return errors.New("FIREWORKS_API_KEY is not set.")
	}
	workspace := h.Workspace(task.ID)
	if err := os.MkdirAll(workspace, 0o755); err != nil {
		return err
	}
	conn, err := h.Connectors.Snapshot(ctx, task.UserID)
	if err != nil {
		log.Printf("agent: task %s runs without connectors: %v", task.ID, err)
		conn = &ConnectorToolset{}
	}
	if conn.Close != nil {
		defer conn.Close()
	}

	tn := &turn{h: h, id: task.ID, userID: task.UserID, scheduleID: task.ScheduleID, mode: task.Mode, conn: conn,
		system: systemPrompt, maxSteps: h.maxSteps}
	if task.ScheduleID != nil {
		prefs, err := queries.GetPreferences(ctx, h.pool, task.UserID)
		if err != nil {
			return err
		}
		tn.system += routineGuidance(prefs, time.Now())
		tn.maxSteps = min(h.maxSteps, h.routineMaxSteps)
	}
	tn.tools = &tools{workspace: workspace, web: h.web, notify: func(text string) error {
		return h.TaskTx(ctx, task.ID, func(_ pgx.Tx, _ *models.Task, emit Emit) error {
			return emit(TextEvent{"notify", text})
		})
	}}
	tn.specs = slices.Clone(builtinTools)
	if h.web.tavilyKey != "" {
		tn.specs = append(tn.specs, webSearchTool)
	}
	tn.specs = append(tn.specs, assistantTools...)
	if h.Routines != nil && task.ScheduleID == nil {
		tn.specs = append(tn.specs, routineCreateTool)
	}
	for _, t := range conn.Tools {
		if externalRef(t.InputSchema) {
			log.Printf("agent: skipping connector tool %s: its schema has an external $ref", t.Name)
			continue
		}
		tn.specs = append(tn.specs, fn(t.Name, t.Description, t.InputSchema))
	}

	return tn.run(ctx)
}

// run resumes the turn from its stored messages, then loops: call the
// model, store its message, run its tool calls. It ends when the model
// answers without a call, a call parks on the user, or the steps run out
// (one last tools-free call then writes the summary).
func (tn *turn) run(ctx context.Context) error {
	messages, err := queries.ListTaskMessages(ctx, tn.h.pool, tn.id)
	if err != nil {
		return err
	}
	if n := len(messages); n > 0 && roleOf(messages[n-1]) == "assistant" {
		if done, err := tn.runTools(ctx, messages[n-1], true); err != nil || done {
			return err
		}
	} else if err := tn.deliverInbox(ctx); err != nil {
		return err
	}

	for step := 0; step < tn.maxSteps; step++ {
		if ctx.Err() != nil {
			return ctx.Err()
		}
		if messages, err = queries.ListTaskMessages(ctx, tn.h.pool, tn.id); err != nil {
			return err
		}
		if stuck(messages) {
			return errors.New(stuckText)
		}
		res, err := tn.call(ctx, messages, tn.specs)
		if err != nil {
			return err
		}
		assistant, err := json.Marshal(res.Message)
		if err != nil {
			return err
		}
		badFinish := res.FinishReason != "stop" && res.FinishReason != "tool_calls" && res.FinishReason != "length"
		err = tn.h.TaskTx(ctx, tn.id, func(tx pgx.Tx, _ *models.Task, emit Emit) error {
			if err := tn.store(ctx, tx, res, assistant); err != nil {
				return err
			}
			if badFinish {
				return emit(TextEvent{"error", "The model stopped early (" + res.FinishReason + ")."})
			}
			// Text written alongside tool calls was only streamed; keep it in the timeline.
			if c := res.Message.Content; c != nil && len(res.Message.ToolCalls) > 0 && strings.TrimSpace(*c) != "" {
				return emit(TextEvent{"notify", strings.TrimSpace(*c)})
			}
			return nil
		})
		if err != nil || badFinish {
			return err
		}
		if done, err := tn.runTools(ctx, assistant, false); err != nil || done {
			return err
		}
	}
	return tn.wrapUp(ctx)
}

// call streams one model call on the quality model, moving to the fallback
// model when it fails before streaming anything. Mode only sets the
// reasoning effort.
func (tn *turn) call(ctx context.Context, messages, specs []json.RawMessage) (*fireworks.Response, error) {
	models, effort := []string{tn.h.qualityModel, tn.h.speedModel}, "medium"
	if tn.mode == "speed" {
		effort = "low"
	}
	req := fireworks.Request{System: tn.system, Messages: messages, Tools: specs, MaxTokens: tn.h.maxTokens, Effort: effort}
	if fireworks.EstimateTokens(req) > tn.h.contextBudget {
		req.Messages = trimmed(messages)
	}
	var err error
	for _, m := range models {
		req.Model = m
		var res *fireworks.Response
		if res, err = tn.h.client.Stream(ctx, req, tn.deltas(ctx)); err == nil || !fireworks.Fallback(err) {
			return res, err
		}
	}
	return nil, err
}

// store appends the assistant message and its usage; run it in a task tx.
func (tn *turn) store(ctx context.Context, tx pgx.Tx, res *fireworks.Response, assistant json.RawMessage) error {
	if err := queries.AppendTaskMessage(ctx, tx, tn.id, assistant); err != nil {
		return err
	}
	u := res.Usage
	return queries.AddTaskUsage(ctx, tx, tn.id, u.PromptTokens-u.CachedTokens, u.CompletionTokens, u.CachedTokens, 0)
}

// wrapUp spends one tools-free call on a summary once the step budget is
// gone, and finishes the task with it.
func (tn *turn) wrapUp(ctx context.Context) error {
	messages, err := queries.ListTaskMessages(ctx, tn.h.pool, tn.id)
	if err != nil {
		return err
	}
	prompt, err := userMessage(wrapUpText)
	if err != nil {
		return err
	}
	res, err := tn.call(ctx, append(messages, prompt), nil)
	if err != nil {
		return err
	}
	assistant, err := json.Marshal(res.Message)
	if err != nil {
		return err
	}
	return tn.h.TaskTx(ctx, tn.id, func(tx pgx.Tx, _ *models.Task, emit Emit) error {
		if err := queries.AppendTaskMessage(ctx, tx, tn.id, prompt); err != nil {
			return err
		}
		if err := tn.store(ctx, tx, res, assistant); err != nil {
			return err
		}
		if len(res.Message.ToolCalls) > 0 || res.Message.Content == nil {
			return emit(TextEvent{"error", fmt.Sprintf("Stopped after %d steps.", tn.maxSteps)})
		}
		return emit(IdleEvent{"idle", *res.Message.Content, []string{}})
	})
}

// deltas streams answer text, and the growing text field of user-facing
// tool calls, to the task's live channel.
func (tn *turn) deltas(ctx context.Context) func(fireworks.Delta) {
	raw := map[int]string{}
	sent := map[int]int{}
	thinking, writing := false, map[int]bool{}
	publish := func(kind, text string) {
		tn.h.bus.Publish(ctx, tn.id.String(), cache.Frame{Kind: kind, Text: text})
	}
	return func(d fireworks.Delta) {
		switch {
		case d.Thinking:
			if !thinking {
				thinking = true
				publish(cache.FrameActivity, "thinking")
			}
			return
		case d.Content != "":
			publish(cache.FrameDelta, d.Content)
			return
		}
		field, ok := previewField[d.ToolName]
		if !ok {
			if !writing[d.ToolIndex] && d.ToolName != "" {
				writing[d.ToolIndex] = true
				publish(cache.FrameActivity, d.ToolName)
			}
			return
		}
		raw[d.ToolIndex] += d.ArgsFragment
		text, _ := partialString(raw[d.ToolIndex], field)
		if len(text) > sent[d.ToolIndex] {
			publish(cache.FrameDelta, text[sent[d.ToolIndex]:])
			sent[d.ToolIndex] = len(text)
		}
	}
}

// deliverInbox turns queued messages into the next user turn.
func (tn *turn) deliverInbox(ctx context.Context) error {
	return tn.h.TaskTx(ctx, tn.id, func(tx pgx.Tx, _ *models.Task, _ Emit) error {
		texts, err := queries.DrainTaskInbox(ctx, tx, tn.id)
		if err != nil || len(texts) == 0 {
			return err
		}
		msg, err := userMessage(strings.Join(texts, "\n\n"))
		if err != nil {
			return err
		}
		return queries.AppendTaskMessage(ctx, tx, tn.id, msg)
	})
}

// runTools runs the tool calls of an assistant message, each result
// recorded as it lands, then writes the tool and user turns and decides
// whether the task is done. On resume it continues from the recorded
// results: the call the task parked on gets the user's answer; a call a dead
// worker left unrecorded gets an interrupted result. done means the run is over.
func (tn *turn) runTools(ctx context.Context, assistant json.RawMessage, resume bool) (done bool, err error) {
	calls := callsOf(assistant)
	invalid := make([]error, len(calls))
	for i, c := range calls {
		if resolve := tn.conn.Resolve[c.Name]; resolve != nil && c.Input != nil {
			if in, err := resolve(c.Input); err != nil {
				invalid[i] = err
			} else {
				calls[i].Input = in
			}
		}
	}
	task, err := queries.GetTask(ctx, tn.h.pool, tn.id)
	if err != nil {
		return false, err
	}
	i := len(task.PartialResults)
	if resume && i < len(calls) {
		if p := task.Pending; p != nil && p.Answer != nil && p.ToolUseID == calls[i].ID {
			// Consume the approval before the call starts, so a worker that dies
			// mid-call leaves it interrupted instead of running it twice.
			if err := queries.SetTaskRunState(ctx, tn.h.pool, tn.id, nil, task.PartialResults); err != nil {
				return false, err
			}
			out, ev := tn.answered(ctx, calls[i], p.Kind, *p.Answer)
			if err := tn.record(ctx, calls[i], out, ev); err != nil {
				return false, err
			}
			i++
		} else {
			for ; i < len(calls); i++ {
				if err := tn.record(ctx, calls[i], ToolOutput{Text: interruptedText, IsError: true}, nil); err != nil {
					return false, err
				}
			}
		}
	}
	for ; i < len(calls); i++ {
		c := calls[i]
		switch {
		case invalid[i] != nil:
			out := ToolOutput{Text: "Error: " + invalid[i].Error(), IsError: true}
			if err := tn.record(ctx, c, out, ToolEvent{"tool", c.Name, c.Input, out.Text, true}); err != nil {
				return false, err
			}
		case c.Input == nil:
			out := ToolOutput{Text: "Error: the arguments must be a JSON object.", IsError: true}
			if err := tn.record(ctx, c, out, ToolEvent{"tool", c.Name, c.eventInput(), out.Text, true}); err != nil {
				return false, err
			}
		case c.Name == "message_ask_user":
			var in struct {
				Text string `json:"text"`
			}
			json.Unmarshal(c.Input, &in)
			return true, tn.park(ctx, c, TextEvent{"ask", in.Text})
		case c.Name == "idle":
			if err := tn.record(ctx, c, ToolOutput{Text: "ok"}, nil); err != nil {
				return false, err
			}
		case tn.needsApproval(c.Name) && !slices.Contains(task.AlwaysAllowed, c.Name):
			return true, tn.park(ctx, c, ConfirmEvent{"confirm", c.Name, c.Input, !tn.conn.NoAlways[c.Name]})
		default:
			out := tn.execute(ctx, c)
			if err := tn.record(ctx, c, out, ToolEvent{"tool", c.Name, c.Input, out.Text, out.IsError}); err != nil {
				return false, err
			}
		}
	}
	return tn.finish(ctx, calls, assistant)
}

// answered applies the user's reply or decision to the call the task parked on.
func (tn *turn) answered(ctx context.Context, c toolCall, kind, answer string) (ToolOutput, any) {
	if kind == "ask" {
		return ToolOutput{Text: answer}, nil
	}
	out := ToolOutput{Text: "The user declined this action.", IsError: true}
	if answer != "deny" {
		out = tn.execute(ctx, c)
	}
	return out, ToolEvent{"tool", c.Name, c.Input, out.Text, out.IsError}
}

func (tn *turn) needsApproval(name string) bool {
	return name == "shell_exec" || name == "routine_create" || (tn.conn.Handlers[name] != nil && tn.conn.NeedsApproval[name])
}

func (tn *turn) execute(ctx context.Context, c toolCall) ToolOutput {
	var out ToolOutput
	var err error
	switch {
	case isAssistantTool(c.Name):
		out, err = tn.assistant(ctx, c)
	case tn.tools.has(c.Name):
		out, err = tn.tools.call(ctx, c.Name, c.Input)
	case tn.conn.Handlers[c.Name] != nil:
		out.Text, out.IsError, err = tn.conn.Handlers[c.Name](context.WithValue(ctx, workspaceKey{}, tn.tools.workspace), c.Input)
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
func (tn *turn) record(ctx context.Context, c toolCall, out ToolOutput, event any) error {
	if ctx.Err() != nil && !errors.Is(context.Cause(ctx), errStopped) {
		return ctx.Err()
	}
	text := out.Text
	if out.IsError && !strings.HasPrefix(text, "Error") && text != interruptedText {
		text = "Error: " + text
	}
	result, err := toolMessage(c.ID, text)
	if err != nil {
		return err
	}
	// A stopped run still records the killed call's result.
	bg := context.WithoutCancel(ctx)
	err = tn.h.TaskTx(bg, tn.id, func(tx pgx.Tx, t *models.Task, emit Emit) error {
		if event != nil {
			if err := emit(event); err != nil {
				return err
			}
		}
		return queries.SetTaskRunState(bg, tx, tn.id, nil, append(t.PartialResults, result))
	})
	if err != nil {
		return err
	}
	return ctx.Err()
}

// park records the ask or confirm the task now waits on; the job ends here
// and /reply or /confirm enqueues the next run.
func (tn *turn) park(ctx context.Context, c toolCall, event any) error {
	kind := "confirm"
	if c.Name == "message_ask_user" {
		kind = "ask"
	}
	return tn.h.TaskTx(ctx, tn.id, func(tx pgx.Tx, t *models.Task, emit Emit) error {
		if err := emit(event); err != nil {
			return err
		}
		return queries.SetTaskRunState(ctx, tx, tn.id, &models.PendingAction{Kind: kind, Name: c.Name, ToolUseID: c.ID, NoAlways: tn.conn.NoAlways[c.Name]}, t.PartialResults)
	})
}

// finish writes one tool message per call, then any queued messages as a
// user turn, and goes idle when the model said it is done and nothing new
// arrived.
func (tn *turn) finish(ctx context.Context, calls []toolCall, assistant json.RawMessage) (done bool, err error) {
	err = tn.h.TaskTx(ctx, tn.id, func(tx pgx.Tx, t *models.Task, emit Emit) error {
		texts, err := queries.DrainTaskInbox(ctx, tx, tn.id)
		if err != nil {
			return err
		}
		for _, res := range t.PartialResults {
			if err := queries.AppendTaskMessage(ctx, tx, tn.id, res); err != nil {
				return err
			}
		}
		if len(texts) > 0 {
			msg, err := userMessage(strings.Join(texts, "\n\n"))
			if err != nil {
				return err
			}
			if err := queries.AppendTaskMessage(ctx, tx, tn.id, msg); err != nil {
				return err
			}
		}
		if err := queries.SetTaskRunState(ctx, tx, tn.id, nil, nil); err != nil {
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
func idleResult(calls []toolCall, results []json.RawMessage, assistant json.RawMessage) *IdleEvent {
	if len(calls) == 0 {
		return &IdleEvent{"idle", contentOf(assistant), []string{}}
	}
	ok := map[string]bool{}
	for _, raw := range results {
		var res struct {
			ToolCallID string `json:"tool_call_id"`
			Content    string `json:"content"`
		}
		json.Unmarshal(raw, &res)
		ok[res.ToolCallID] = res.Content == "ok"
	}
	for _, c := range calls {
		if c.Name != "idle" || !ok[c.ID] {
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
		return &IdleEvent{"idle", in.Result, files}
	}
	return nil
}
