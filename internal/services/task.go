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
	"papermark/internal/harness"
	"papermark/internal/harness/skills"
	"papermark/internal/models"
	"papermark/internal/models/queries"
	"papermark/internal/storage"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
)

// TaskService handles everything a client does to a task. The agent loop
// runs elsewhere (harness.ChatTurn, as a job); this service records the user's
// side and enqueues runs.
type TaskService struct {
	pool *pgxpool.Pool
	s    *Services
}

// NewTask is a task to create.
type NewTask struct {
	UserID      uuid.UUID
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
func (s *TaskService) CreateTask(ctx context.Context, db queries.DBTX, userID uuid.UUID, prompt, mode string, projectID *uuid.UUID, scheduleID uuid.UUID) (string, error) {
	b, ok := db.(harness.Beginner)
	if !ok {
		return "", fmt.Errorf("CreateTask: %T cannot begin a transaction", db)
	}
	id, err := s.create(ctx, b, NewTask{UserID: userID, Prompt: prompt, Mode: mode, ProjectID: projectID, ScheduleID: &scheduleID})
	if err != nil {
		return "", err
	}
	return id.String(), nil
}

func (s *TaskService) create(ctx context.Context, db harness.Beginner, in NewTask) (uuid.UUID, error) {
	if strings.TrimSpace(in.Prompt) == "" {
		return uuid.Nil, badRequest("task is required")
	}
	if in.Mode == "" {
		in.Mode = "quality"
	}
	if in.Mode != "speed" && in.Mode != "quality" {
		return uuid.Nil, badRequest("mode must be speed or quality")
	}
	if in.ProjectID != nil {
		if _, err := queries.GetProject(ctx, s.pool, in.UserID, *in.ProjectID); IsNotFound(err) {
			return uuid.Nil, badRequest("no such project")
		} else if err != nil {
			return uuid.Nil, err
		}
	}
	if _, err := s.expandSkill(ctx, in.UserID, in.Prompt); err != nil {
		return uuid.Nil, err // an unknown /name is refused now; the worker expands it
	}
	t := &models.Task{Base: models.Base{ID: uuid.New()}, UserID: in.UserID, Prompt: in.Prompt, Status: models.TaskRunning, Mode: in.Mode, ProjectID: in.ProjectID, ScheduleID: in.ScheduleID}
	files, err := s.saveAttachments(t.ID, in.Attachments)
	if err != nil {
		return uuid.Nil, err
	}
	err = s.s.Harness.CreateTx(ctx, db, t, func(tx pgx.Tx, t *models.Task, emit harness.Emit) error {
		return s.start(ctx, tx, emit, t.ID, in.Prompt, in.Prompt, files)
	})
	if err != nil {
		return uuid.Nil, err
	}
	return t.ID, nil
}

// start records a user message and enqueues a run that delivers it. text is
// what the model gets (skill and context expanded); shown is what the user typed.
func (s *TaskService) start(ctx context.Context, tx pgx.Tx, emit harness.Emit, id uuid.UUID, shown, text string, files []string) error {
	if err := s.queue(ctx, tx, emit, id, shown, text, files); err != nil {
		return err
	}
	return s.s.Jobs.Enqueue(ctx, tx, harness.TurnJob, id.String(), harness.TurnPayload{TaskID: id})
}

func (s *TaskService) queue(ctx context.Context, tx pgx.Tx, emit harness.Emit, id uuid.UUID, shown, text string, files []string) error {
	if err := emit(harness.MessageEvent{Type: "message", Text: shown, Attachments: files}); err != nil {
		return err
	}
	if len(files) > 0 {
		text += "\n\nAttached files (workspace paths): " + strings.Join(s.describe(id, files), ", ")
	}
	return queries.InsertTaskInbox(ctx, tx, id, text)
}

// Prepare builds a task's first message for the model: the /name skill or
// the picked skills, then the project, knowledge and memory context. The
// harness calls it at the task's first turn (harness.Preparer).
func (s *TaskService) Prepare(ctx context.Context, t *models.Task) (string, []string, error) {
	var project *models.Project
	if t.ProjectID != nil {
		var err error
		if project, err = queries.GetProject(ctx, s.pool, t.UserID, *t.ProjectID); err != nil && !IsNotFound(err) {
			return "", nil, err
		}
	}
	text, err := s.expandSkill(ctx, t.UserID, t.Prompt)
	if se := (*StatusError)(nil); errors.As(err, &se) {
		text, err = t.Prompt, nil // the skill was deleted since the task was created
	}
	if err != nil {
		return "", nil, err
	}
	var picked []skills.Skill
	if !skillCall.MatchString(t.Prompt) {
		if picked, err = s.pickSkills(ctx, t); err != nil {
			return "", nil, err
		}
		text = skills.Inject(picked, text)
	}
	if text, err = s.withContext(ctx, t.UserID, project, text); err != nil {
		return "", nil, err
	}
	return text, skills.Names(picked), nil
}

// pickSkills chooses the skills a new task follows. A routine's run reuses
// the skills picked at its first run; any other task asks the model.
func (s *TaskService) pickSkills(ctx context.Context, t *models.Task) ([]skills.Skill, error) {
	list, err := skills.For(ctx, s.pool, t.UserID)
	if err != nil {
		return nil, err
	}
	var complete skills.Completer
	if s.s.Harness != nil {
		complete = s.s.Harness.Complete
	}
	if t.ScheduleID == nil {
		picked, _ := skills.Pick(ctx, complete, t.Prompt, list)
		return picked, nil
	}
	sc, err := queries.GetSchedule(ctx, s.pool, t.UserID, *t.ScheduleID)
	if IsNotFound(err) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	if sc.Skills != nil {
		return skills.Select(list, sc.Skills), nil
	}
	picked, ok := skills.Pick(ctx, complete, sc.Prompt, list)
	if !ok {
		return nil, nil // picked again next run
	}
	return picked, queries.SetScheduleSkills(ctx, s.pool, t.UserID, sc.ID, sc.Prompt, skills.Names(picked))
}

var skillCall = regexp.MustCompile(`^/([a-z0-9-]{1,40})(\s|$)`)

// expandSkill replaces a leading /<skill-name> with that skill's instructions:
// the user's own skill, or else the built-in one.
func (s *TaskService) expandSkill(ctx context.Context, user uuid.UUID, text string) (string, error) {
	m := skillCall.FindStringSubmatch(text)
	if m == nil {
		return text, nil
	}
	name, instructions := m[1], ""
	own, err := queries.GetSkillByName(ctx, s.pool, user, name)
	switch {
	case err == nil:
		instructions = own.Instructions
	case !IsNotFound(err):
		return "", err
	default:
		builtin, ok := skills.Find(name)
		if !ok {
			return "", badRequest("no such skill: /%s", name)
		}
		instructions = builtin.Instructions
	}
	rest := strings.TrimSpace(text[len(m[0]):])
	out := fmt.Sprintf("Use the skill %q:\n%s", name, instructions)
	if rest != "" {
		out += "\n\n" + rest
	}
	return out, nil
}

// maxInjectedMemories is how many memories go into a task's first message.
const maxInjectedMemories = 15

// withContext puts the project's instructions, the enabled knowledge and the
// user's most relevant memories in front of a task's first message (the system prompt stays byte-stable).
func (s *TaskService) withContext(ctx context.Context, user uuid.UUID, project *models.Project, text string) (string, error) {
	var parts []string
	if project != nil && strings.TrimSpace(project.Instructions) != "" {
		parts = append(parts, fmt.Sprintf("Project %q instructions:\n%s", project.Name, project.Instructions))
	}
	notes, err := queries.ListKnowledge(ctx, s.pool, user, true)
	if err != nil {
		return "", err
	}
	for _, k := range notes {
		parts = append(parts, fmt.Sprintf("Knowledge %q:\n%s", k.Title, k.Content))
	}
	remembered, err := queries.RelevantMemories(ctx, s.pool, user, text, maxInjectedMemories)
	if err != nil {
		return "", err
	}
	if len(remembered) > 0 {
		lines := make([]string, len(remembered))
		for i, m := range remembered {
			lines[i] = "- " + m.Text
		}
		parts = append(parts, "What you remember about the user:\n"+strings.Join(lines, "\n"))
	}
	if len(parts) == 0 {
		return text, nil
	}
	return strings.Join(parts, "\n\n") + "\n\nTask:\n" + text, nil
}

var unsafeExts = map[string]bool{".exe": true, ".dll": true, ".so": true, ".dylib": true, ".sh": true, ".bat": true, ".cmd": true, ".com": true, ".msi": true, ".apk": true, ".app": true, ".scpt": true, ".jar": true}

var kinds = map[string]string{
	".xlsx": "spreadsheet", ".xls": "spreadsheet", ".csv": "table", ".tsv": "table", ".pdf": "PDF", ".docx": "Word document", ".pptx": "presentation",
	".png": "image", ".jpg": "image", ".jpeg": "image", ".gif": "image", ".webp": "image", ".heic": "image",
	".mp3": "audio", ".m4a": "audio", ".wav": "audio", ".aac": "audio", ".ogg": "audio", ".flac": "audio",
	".mp4": "video", ".mov": "video", ".m4v": "video", ".webm": "video",
	".txt": "text", ".md": "text", ".json": "text",
}

// describe adds each upload's type and size to its workspace path.
func (s *TaskService) describe(id uuid.UUID, files []string) []string {
	out := make([]string, len(files))
	for i, f := range files {
		out[i] = f
		info, err := os.Stat(filepath.Join(s.s.Harness.Workspace(id), f))
		if err != nil {
			continue
		}
		kind, ok := kinds[strings.ToLower(path.Ext(f))]
		if !ok {
			kind = "file"
		}
		size := fmt.Sprintf("%d B", info.Size())
		switch n := float64(info.Size()); {
		case n >= 1<<20:
			size = fmt.Sprintf("%.1f MB", n/(1<<20))
		case n >= 1<<10:
			size = fmt.Sprintf("%.1f KB", n/(1<<10))
		}
		out[i] = fmt.Sprintf("%s (%s, %s)", f, kind, size)
	}
	return out
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
		if unsafeExts[strings.ToLower(path.Ext(base))] {
			return nil, badRequest("%s: executable files can't be attached", base)
		}
		files = append(files, Attachment{base, b64})
	}
	dir := filepath.Join(s.s.Harness.Workspace(id), "uploads")
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

// List returns the user's tasks newest activity first.
func (s *TaskService) List(ctx context.Context, user uuid.UUID, archived bool, projectID *uuid.UUID) ([]*models.Task, error) {
	return queries.ListTasks(ctx, s.pool, user, archived, projectID)
}

// Get returns one of the user's tasks; another user's is not found.
func (s *TaskService) Get(ctx context.Context, user, id uuid.UUID) (*models.Task, error) {
	return queries.GetUserTask(ctx, s.pool, user, id)
}

// Events returns a task's events with seq > after. Callers check ownership with Get first.
func (s *TaskService) Events(ctx context.Context, id uuid.UUID, after int) ([]*models.TaskEvent, error) {
	return queries.ListTaskEvents(ctx, s.pool, id, after)
}

// Patch sets the favorite and archived flags.
func (s *TaskService) Patch(ctx context.Context, user, id uuid.UUID, favorite, archived *bool) (*models.Task, error) {
	return queries.UpdateTaskFlags(ctx, s.pool, user, id, favorite, archived)
}

// ownedTx is Harness.TaskTx for one of the user's tasks; another user's is
// ErrNotFound. A task's owner never changes, so checking under the lock is enough.
func (s *TaskService) ownedTx(ctx context.Context, user, id uuid.UUID, fn func(tx pgx.Tx, t *models.Task, emit harness.Emit) error) error {
	return s.s.Harness.TaskTx(ctx, id, func(tx pgx.Tx, t *models.Task, emit harness.Emit) error {
		if t.UserID != user {
			return queries.ErrNotFound
		}
		return fn(tx, t, emit)
	})
}

// Reply answers the question the task waits on.
func (s *TaskService) Reply(ctx context.Context, user, id uuid.UUID, text string) error {
	return s.answer(ctx, user, id, "ask", text, harness.TextEvent{Type: "reply", Text: text}, "task is not waiting for a reply")
}

// Confirm answers the approval the task waits on.
func (s *TaskService) Confirm(ctx context.Context, user, id uuid.UUID, decision string) error {
	if decision != "allow" && decision != "always" && decision != "deny" {
		return badRequest("decision must be allow, always or deny")
	}
	return s.answer(ctx, user, id, "confirm", decision, nil, "task is not waiting for approval")
}

func (s *TaskService) answer(ctx context.Context, user, id uuid.UUID, kind, value string, event any, notWaiting string) error {
	err := s.ownedTx(ctx, user, id, func(tx pgx.Tx, t *models.Task, emit harness.Emit) error {
		p := t.Pending
		if t.Status != models.TaskWaiting || p == nil || p.Kind != kind || p.Answer != nil {
			return conflict("%s", notWaiting)
		}
		if value == "always" && p.NoAlways {
			return badRequest("this action needs approval every time")
		}
		if event == nil {
			event = harness.DecisionEvent{Type: "decision", Name: p.Name, Decision: value}
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
		return s.s.Jobs.Enqueue(ctx, tx, harness.TurnJob, id.String(), harness.TurnPayload{TaskID: id})
	})
	if err == nil {
		s.s.Jobs.Notify()
	}
	return err
}

// Message sends the task a user message: queued while it runs, a
// continuation once it has finished.
func (s *TaskService) Message(ctx context.Context, user, id uuid.UUID, text string, attachments json.RawMessage) error {
	if strings.TrimSpace(text) == "" {
		return badRequest("text is required")
	}
	t, err := queries.GetUserTask(ctx, s.pool, user, id)
	if err != nil {
		return err
	}
	if t.Status == models.TaskWaiting {
		return conflict("task is waiting; use reply or confirm")
	}
	expanded, err := s.expandSkill(ctx, user, text)
	if err != nil {
		return err
	}
	files, err := s.saveAttachments(id, attachments)
	if err != nil {
		return err
	}
	// Status is read again under the row lock, so a message racing the run's
	// last step either rides along with it or starts the next run.
	err = s.ownedTx(ctx, user, id, func(tx pgx.Tx, t *models.Task, emit harness.Emit) error {
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

// Note tells the task's agent something the user did outside the chat. A
// running or waiting task reads it silently; a finished one continues, with
// shown as the short message in the chat.
func (s *TaskService) Note(ctx context.Context, user, id uuid.UUID, shown, text string) error {
	err := s.ownedTx(ctx, user, id, func(tx pgx.Tx, t *models.Task, emit harness.Emit) error {
		if t.Status == models.TaskRunning || t.Status == models.TaskWaiting {
			return queries.InsertTaskInbox(ctx, tx, id, text)
		}
		if err := queries.SetTaskStopRequested(ctx, tx, id, false); err != nil {
			return err
		}
		return s.start(ctx, tx, emit, id, shown, text, []string{})
	})
	if err == nil {
		s.s.Jobs.Notify()
	}
	return err
}

// Stop stops a running or waiting task.
func (s *TaskService) Stop(ctx context.Context, user, id uuid.UUID) error {
	signal := false
	err := s.ownedTx(ctx, user, id, func(tx pgx.Tx, t *models.Task, emit harness.Emit) error {
		switch t.Status {
		case models.TaskRunning:
			signal = true
			return queries.SetTaskStopRequested(ctx, tx, id, true)
		case models.TaskWaiting:
			if err := queries.SetTaskRunState(ctx, tx, id, nil, t.PartialResults); err != nil {
				return err
			}
			return emit(harness.TextEvent{Type: "stopped", Text: "Stopped by the user."})
		}
		return conflict("task is %s", t.Status)
	})
	if err == nil && signal {
		s.s.Bus.Publish(ctx, id.String(), cache.Frame{Kind: cache.FrameStop})
	}
	return err
}

// Fork starts a new task, owned by the same user, whose history and
// workspace are copies of id's.
func (s *TaskService) Fork(ctx context.Context, user, id uuid.UUID, prompt string) (uuid.UUID, error) {
	if strings.TrimSpace(prompt) == "" {
		return uuid.Nil, badRequest("task is required")
	}
	src, err := queries.GetUserTask(ctx, s.pool, user, id)
	if err != nil {
		return uuid.Nil, err
	}
	text, err := s.expandSkill(ctx, user, prompt)
	if err != nil {
		return uuid.Nil, err
	}
	fork := &models.Task{Base: models.Base{ID: uuid.New()}, UserID: src.UserID, Prompt: prompt, Status: models.TaskRunning, Mode: src.Mode, ProjectID: src.ProjectID}
	if err := storage.CopyDir(s.s.Harness.Workspace(id), s.s.Harness.Workspace(fork.ID)); err != nil {
		return uuid.Nil, err
	}
	err = s.s.Harness.CreateTx(ctx, s.pool, fork, func(tx pgx.Tx, t *models.Task, emit harness.Emit) error {
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
func (s *TaskService) Files(ctx context.Context, user, id uuid.UUID) ([]storage.FileInfo, error) {
	if _, err := queries.GetUserTask(ctx, s.pool, user, id); err != nil {
		return nil, err
	}
	return storage.ListFiles(s.s.Harness.Workspace(id))
}

// OpenFile opens one workspace file. Errors: storage.ErrEscape, fs.ErrNotExist.
func (s *TaskService) OpenFile(ctx context.Context, user, id uuid.UUID, rel string) (*os.File, fs.FileInfo, error) {
	if _, err := queries.GetUserTask(ctx, s.pool, user, id); err != nil {
		return nil, nil, err
	}
	return storage.OpenFile(s.s.Harness.Workspace(id), rel)
}

// Transcript renders a task as markdown for sharing.
func (s *TaskService) Transcript(ctx context.Context, user, id uuid.UUID) (string, error) {
	t, err := queries.GetUserTask(ctx, s.pool, user, id)
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
func (s *TaskService) UsageByDay(ctx context.Context, user uuid.UUID, days int) ([]UsageDay, Usage, error) {
	today := time.Now().UTC().Truncate(24 * time.Hour)
	first := today.AddDate(0, 0, -(days - 1))
	tasks, err := queries.ListTasksCreatedSince(ctx, s.pool, user, first)
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
