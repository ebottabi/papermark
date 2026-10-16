/*
 * Copyright (c) 2026. Papermark - All Rights Reserved
 * Unauthorized copying of this file, via any medium is strictly prohibited
 * Proprietary and confidential
 * Written by Ebot Tabi <ebot.tabi@gmail.com>, 15/10/2026
 */
package services

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path"
	"path/filepath"
	"regexp"
	"strings"
	"time"

	"papermark/internal/cache"
	"papermark/internal/models"
	"papermark/internal/models/queries"
	"papermark/internal/storage"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
)

// TaskService handles everything a client does to a task. The agent loop
// runs elsewhere (AgentService, as a job); this service records the user's
// side and enqueues runs.
type TaskService struct {
	pool *pgxpool.Pool
	ev   *events
	s    *Services
}

// NewTask is a task to create.
type NewTask struct {
	Prompt      string
	Mode        string // "" means quality
	ProjectID   *uuid.UUID
	ScheduleID  *uuid.UUID
	Attachments json.RawMessage // [{name, base64}] or empty
}

// Attachment is one uploaded file.
type Attachment struct {
	Name   string `json:"name"`
	Base64 string `json:"base64"`
}

// Create records a task and its first message, and enqueues its first run.
func (s *TaskService) Create(ctx context.Context, in NewTask) (uuid.UUID, error) {
	id, err := s.create(ctx, s.pool, in)
	if err == nil {
		s.s.Jobs.Notify()
	}
	return id, err
}

// CreateTask is Create for the scheduler: it writes through db, so the task,
// its first message and its run job commit with the caller's transaction. db
// must be *pgxpool.Pool or pgx.Tx. Call Services.Jobs.Notify after commit.
func (s *TaskService) CreateTask(ctx context.Context, db queries.DBTX, prompt, mode string, projectID *uuid.UUID, scheduleID uuid.UUID) (string, error) {
	b, ok := db.(beginner)
	if !ok {
		return "", fmt.Errorf("CreateTask: %T cannot begin a transaction", db)
	}
	id, err := s.create(ctx, b, NewTask{Prompt: prompt, Mode: mode, ProjectID: projectID, ScheduleID: &scheduleID})
	if err != nil {
		return "", err
	}
	return id.String(), nil
}

func (s *TaskService) create(ctx context.Context, db beginner, in NewTask) (uuid.UUID, error) {
	if strings.TrimSpace(in.Prompt) == "" {
		return uuid.Nil, badRequest("task is required")
	}
	if in.Mode == "" {
		in.Mode = "quality"
	}
	if in.Mode != "speed" && in.Mode != "quality" {
		return uuid.Nil, badRequest("mode must be speed or quality")
	}
	var project *models.Project
	if in.ProjectID != nil {
		var err error
		if project, err = queries.GetProject(ctx, s.pool, *in.ProjectID); IsNotFound(err) {
			return uuid.Nil, badRequest("no such project")
		} else if err != nil {
			return uuid.Nil, err
		}
	}
	text, err := s.expandSkill(ctx, in.Prompt)
	if err != nil {
		return uuid.Nil, err
	}
	if text, err = s.withContext(ctx, project, text); err != nil {
		return uuid.Nil, err
	}
	t := &models.Task{Base: models.Base{ID: uuid.New()}, Prompt: in.Prompt, Status: models.TaskRunning, Mode: in.Mode, ProjectID: in.ProjectID, ScheduleID: in.ScheduleID}
	files, err := s.saveAttachments(t.ID, in.Attachments)
	if err != nil {
		return uuid.Nil, err
	}
	err = s.ev.createTx(ctx, db, t, func(tx pgx.Tx, t *models.Task, emit Emit) error {
		return s.start(ctx, tx, emit, t.ID, in.Prompt, text, files)
	})
	if err != nil {
		return uuid.Nil, err
	}
	return t.ID, nil
}

// start records a user message and enqueues a run that delivers it. text is
// what the model gets (skill and context expanded); shown is what the user typed.
func (s *TaskService) start(ctx context.Context, tx pgx.Tx, emit Emit, id uuid.UUID, shown, text string, files []string) error {
	if err := s.queue(ctx, tx, emit, id, shown, text, files); err != nil {
		return err
	}
	return s.s.Jobs.Enqueue(ctx, tx, RunTaskJob, id.String(), RunTaskPayload{TaskID: id})
}

func (s *TaskService) queue(ctx context.Context, tx pgx.Tx, emit Emit, id uuid.UUID, shown, text string, files []string) error {
	if err := emit(messageEvent{"message", shown, files}); err != nil {
		return err
	}
	if len(files) > 0 {
		text += "\n\nAttached files (workspace paths): " + strings.Join(files, ", ")
	}
	return queries.InsertTaskInbox(ctx, tx, id, text)
}

var skillCall = regexp.MustCompile(`^/([a-z0-9-]{1,40})(\s|$)`)

// expandSkill replaces a leading /<skill-name> with that skill's instructions.
func (s *TaskService) expandSkill(ctx context.Context, text string) (string, error) {
	m := skillCall.FindStringSubmatch(text)
	if m == nil {
		return text, nil
	}
	skill, err := queries.GetSkillByName(ctx, s.pool, m[1])
	if IsNotFound(err) {
		return "", badRequest("no such skill: /%s", m[1])
	}
	if err != nil {
		return "", err
	}
	rest := strings.TrimSpace(text[len(m[0]):])
	out := fmt.Sprintf("Use the skill %q:\n%s", skill.Name, skill.Instructions)
	if rest != "" {
		out += "\n\n" + rest
	}
	return out, nil
}

// withContext puts the project's instructions and the enabled knowledge in
// front of a task's first message (the system prompt stays byte-stable).
func (s *TaskService) withContext(ctx context.Context, project *models.Project, text string) (string, error) {
	var parts []string
	if project != nil && strings.TrimSpace(project.Instructions) != "" {
		parts = append(parts, fmt.Sprintf("Project %q instructions:\n%s", project.Name, project.Instructions))
	}
	notes, err := queries.ListKnowledge(ctx, s.pool, true)
	if err != nil {
		return "", err
	}
	for _, k := range notes {
		parts = append(parts, fmt.Sprintf("Knowledge %q:\n%s", k.Title, k.Content))
	}
	if len(parts) == 0 {
		return text, nil
	}
	return strings.Join(parts, "\n\n") + "\n\nTask:\n" + text, nil
}

// saveAttachments writes base64 attachments under the workspace's uploads/,
// never overwriting, and returns their workspace paths.
func (s *TaskService) saveAttachments(id uuid.UUID, raw json.RawMessage) ([]string, error) {
	saved := []string{}
	if len(raw) == 0 || string(raw) == "null" {
		return saved, nil
	}
	invalid := badRequest("attachments must be [{name, base64}]")
	var list []map[string]any
	if json.Unmarshal(raw, &list) != nil {
		return nil, invalid
	}
	var files []Attachment
	for _, a := range list {
		name, ok1 := a["name"].(string)
		b64, ok2 := a["base64"].(string)
		base := path.Base(strings.ReplaceAll(name, "\\", "/"))
		if !ok1 || !ok2 || name == "" || base == "." || base == ".." || base == "/" {
			return nil, invalid
		}
		files = append(files, Attachment{base, b64})
	}
	dir := filepath.Join(s.s.Agent.Workspace(id), "uploads")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return nil, err
	}
	for _, a := range files {
		data, err := base64.StdEncoding.DecodeString(a.Base64)
		if err != nil {
			return nil, invalid
		}
		ext := path.Ext(a.Name)
		stem := strings.TrimSuffix(a.Name, ext)
		for i := 0; ; i++ {
			name := a.Name
			if i > 0 {
				name = fmt.Sprintf("%s-%d%s", stem, i, ext)
			}
			f, err := os.OpenFile(filepath.Join(dir, name), os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0o644)
			if errors.Is(err, fs.ErrExist) {
				continue
			}
			if err != nil {
				return nil, err
			}
			_, werr := f.Write(data)
			if cerr := f.Close(); werr == nil {
				werr = cerr
			}
			if werr != nil {
				return nil, werr
			}
			saved = append(saved, "uploads/"+name)
			break
		}
	}
	return saved, nil
}

// List returns tasks newest activity first.
func (s *TaskService) List(ctx context.Context, archived bool, projectID *uuid.UUID) ([]*models.Task, error) {
	return queries.ListTasks(ctx, s.pool, archived, projectID)
}

// Get returns a task.
func (s *TaskService) Get(ctx context.Context, id uuid.UUID) (*models.Task, error) {
	return queries.GetTask(ctx, s.pool, id)
}

// Events returns a task's events with seq > after.
func (s *TaskService) Events(ctx context.Context, id uuid.UUID, after int) ([]*models.TaskEvent, error) {
	return queries.ListTaskEvents(ctx, s.pool, id, after)
}

// Patch sets the favorite and archived flags.
func (s *TaskService) Patch(ctx context.Context, id uuid.UUID, favorite, archived *bool) (*models.Task, error) {
	return queries.UpdateTaskFlags(ctx, s.pool, id, favorite, archived)
}

// Reply answers the question the task waits on.
func (s *TaskService) Reply(ctx context.Context, id uuid.UUID, text string) error {
	return s.answer(ctx, id, "ask", text, textEvent{"reply", text}, "task is not waiting for a reply")
}

// Confirm answers the approval the task waits on.
func (s *TaskService) Confirm(ctx context.Context, id uuid.UUID, decision string) error {
	if decision != "allow" && decision != "always" && decision != "deny" {
		return badRequest("decision must be allow, always or deny")
	}
	return s.answer(ctx, id, "confirm", decision, nil, "task is not waiting for approval")
}

func (s *TaskService) answer(ctx context.Context, id uuid.UUID, kind, value string, event any, notWaiting string) error {
	err := s.ev.taskTx(ctx, id, func(tx pgx.Tx, t *models.Task, emit Emit) error {
		p := t.Pending
		if t.Status != models.TaskWaiting || p == nil || p.Kind != kind || p.Answer != nil {
			return conflict("%s", notWaiting)
		}
		if event == nil {
			event = decisionEvent{"decision", p.Name, value}
		}
		if err := emit(event); err != nil {
			return err
		}
		if value == "always" {
			if err := queries.SetTaskAlwaysAllowed(ctx, tx, id, append(t.AlwaysAllowed, p.Name)); err != nil {
				return err
			}
		}
		p.Answer = &value
		if err := queries.SetTaskRunState(ctx, tx, id, p, t.PartialResults); err != nil {
			return err
		}
		return s.s.Jobs.Enqueue(ctx, tx, RunTaskJob, id.String(), RunTaskPayload{TaskID: id})
	})
	if err == nil {
		s.s.Jobs.Notify()
	}
	return err
}

// Message sends the task a user message: queued while it runs, a
// continuation once it has finished.
func (s *TaskService) Message(ctx context.Context, id uuid.UUID, text string, attachments json.RawMessage) error {
	if strings.TrimSpace(text) == "" {
		return badRequest("text is required")
	}
	t, err := queries.GetTask(ctx, s.pool, id)
	if err != nil {
		return err
	}
	if t.Status == models.TaskWaiting {
		return conflict("task is waiting; use reply or confirm")
	}
	expanded, err := s.expandSkill(ctx, text)
	if err != nil {
		return err
	}
	files, err := s.saveAttachments(id, attachments)
	if err != nil {
		return err
	}
	// Status is read again under the row lock, so a message racing the run's
	// last step either rides along with it or starts the next run.
	err = s.ev.taskTx(ctx, id, func(tx pgx.Tx, t *models.Task, emit Emit) error {
		switch t.Status {
		case models.TaskWaiting:
			return conflict("task is waiting; use reply or confirm")
		case models.TaskRunning:
			return s.queue(ctx, tx, emit, id, text, expanded, files)
		}
		if err := queries.SetTaskStopRequested(ctx, tx, id, false); err != nil {
			return err
		}
		return s.start(ctx, tx, emit, id, text, expanded, files)
	})
	if err == nil {
		s.s.Jobs.Notify()
	}
	return err
}

// Stop stops a running or waiting task.
func (s *TaskService) Stop(ctx context.Context, id uuid.UUID) error {
	signal := false
	err := s.ev.taskTx(ctx, id, func(tx pgx.Tx, t *models.Task, emit Emit) error {
		switch t.Status {
		case models.TaskRunning:
			signal = true
			return queries.SetTaskStopRequested(ctx, tx, id, true)
		case models.TaskWaiting:
			if err := queries.SetTaskRunState(ctx, tx, id, nil, t.PartialResults); err != nil {
				return err
			}
			return emit(textEvent{"stopped", "Stopped by the user."})
		}
		return conflict("task is %s", t.Status)
	})
	if err == nil && signal {
		s.s.Bus.Publish(ctx, id.String(), cache.Frame{Kind: cache.FrameStop})
	}
	return err
}

// Fork starts a new task whose history and workspace are copies of id's.
func (s *TaskService) Fork(ctx context.Context, id uuid.UUID, prompt string) (uuid.UUID, error) {
	if strings.TrimSpace(prompt) == "" {
		return uuid.Nil, badRequest("task is required")
	}
	src, err := queries.GetTask(ctx, s.pool, id)
	if err != nil {
		return uuid.Nil, err
	}
	text, err := s.expandSkill(ctx, prompt)
	if err != nil {
		return uuid.Nil, err
	}
	fork := &models.Task{Base: models.Base{ID: uuid.New()}, Prompt: prompt, Status: models.TaskRunning, Mode: src.Mode, ProjectID: src.ProjectID}
	if err := storage.CopyDir(s.s.Agent.Workspace(id), s.s.Agent.Workspace(fork.ID)); err != nil {
		return uuid.Nil, err
	}
	err = s.ev.createTx(ctx, s.pool, fork, func(tx pgx.Tx, t *models.Task, emit Emit) error {
		if err := queries.CopyTaskMessages(ctx, tx, id, t.ID); err != nil {
			return err
		}
		return s.start(ctx, tx, emit, t.ID, prompt, text, []string{})
	})
	if err != nil {
		return uuid.Nil, err
	}
	s.s.Jobs.Notify()
	return fork.ID, nil
}

// Files lists a task's workspace.
func (s *TaskService) Files(ctx context.Context, id uuid.UUID) ([]storage.FileInfo, error) {
	if _, err := queries.GetTask(ctx, s.pool, id); err != nil {
		return nil, err
	}
	return storage.ListFiles(s.s.Agent.Workspace(id))
}

// OpenFile opens one workspace file. Errors: storage.ErrEscape, fs.ErrNotExist.
func (s *TaskService) OpenFile(ctx context.Context, id uuid.UUID, rel string) (*os.File, fs.FileInfo, error) {
	if _, err := queries.GetTask(ctx, s.pool, id); err != nil {
		return nil, nil, err
	}
	return storage.OpenFile(s.s.Agent.Workspace(id), rel)
}

// Transcript renders a task as markdown for sharing.
func (s *TaskService) Transcript(ctx context.Context, id uuid.UUID) (string, error) {
	t, err := queries.GetTask(ctx, s.pool, id)
	if err != nil {
		return "", err
	}
	events, err := queries.ListTaskEvents(ctx, s.pool, id, -1)
	if err != nil {
		return "", err
	}
	parts := []string{"# " + strings.SplitN(t.Prompt, "\n", 2)[0], "_" + t.CreatedAt.UTC().Format("2006-01-02T15:04:05.000Z07:00") + "_"}
	for _, e := range events {
		var v struct {
			Text, Name, Decision string
			IsError              bool
			Attachments, Files   []string
		}
		json.Unmarshal(e.Data, &v)
		var line string
		switch e.Type {
		case "message":
			line = "**You:** " + v.Text + list("Attached", v.Attachments)
		case "notify":
			line = v.Text
		case "ask":
			line = "**Question:** " + v.Text
		case "reply":
			line = "**You:** " + v.Text
		case "tool":
			line = "- Used `" + v.Name + "`"
			if v.IsError {
				line += " (failed)"
			}
		case "confirm":
			line = "- Asked to approve `" + v.Name + "`"
		case "decision":
			line = "- You chose **" + v.Decision + "** for `" + v.Name + "`"
		case "idle":
			line = "**Result:** " + v.Text + list("Files", v.Files)
		case "stopped":
			line = "_" + v.Text + "_"
		case "error":
			line = "_Error: " + v.Text + "_"
		default:
			continue
		}
		parts = append(parts, line)
	}
	return strings.Join(parts, "\n\n") + "\n", nil
}

func list(label string, items []string) string {
	if len(items) == 0 {
		return ""
	}
	return "\n\n" + label + ": " + strings.Join(items, ", ")
}

// UsageDay is one UTC day of token usage.
type UsageDay struct {
	Date  string
	Usage Usage
	Tasks int
}

// Usage is token counts.
type Usage struct{ InputTokens, OutputTokens, CacheReadTokens, CacheWriteTokens int64 }

func (u *Usage) add(t *models.Task) {
	u.InputTokens += t.InputTokens
	u.OutputTokens += t.OutputTokens
	u.CacheReadTokens += t.CacheReadTokens
	u.CacheWriteTokens += t.CacheWriteTokens
}

// UsageByDay sums usage over the last days UTC days (today included).
// shortcut: a task's usage counts on the UTC day it was created; record usage per day if long tasks skew the chart.
func (s *TaskService) UsageByDay(ctx context.Context, days int) ([]UsageDay, Usage, error) {
	today := time.Now().UTC().Truncate(24 * time.Hour)
	first := today.AddDate(0, 0, -(days - 1))
	tasks, err := queries.ListTasksCreatedSince(ctx, s.pool, first)
	if err != nil {
		return nil, Usage{}, err
	}
	out := make([]UsageDay, days)
	index := map[string]int{}
	for i := range out {
		out[i].Date = first.AddDate(0, 0, i).Format("2006-01-02")
		index[out[i].Date] = i
	}
	var total Usage
	for _, t := range tasks {
		i, ok := index[t.CreatedAt.UTC().Format("2006-01-02")]
		if !ok {
			continue
		}
		out[i].Usage.add(t)
		out[i].Tasks++
		total.add(t)
	}
	return out, total, nil
}
