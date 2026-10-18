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

	"papermark/internal/models"

	"github.com/google/uuid"
)

// ConnectorTool is one tool a connector exposes, named <connector>_<tool>.
type ConnectorTool struct {
	Name        string
	Description string
	InputSchema json.RawMessage // a JSON Schema object
}

// ConnectorHandler runs one connector tool. isError results and errors are
// both shown to the model as error results.
type ConnectorHandler func(ctx context.Context, input json.RawMessage) (text string, isError bool, err error)

// ConnectorToolset is the connectors one run uses. The agent takes a
// snapshot at the start of every run (a waiting task resumes in a new run)
// and calls Close, when set, at its end.
type ConnectorToolset struct {
	Tools         []ConnectorTool // sorted by name
	Handlers      map[string]ConnectorHandler
	NeedsApproval map[string]bool // every tool not annotated readOnlyHint
	NoAlways      map[string]bool // tools whose approval cannot be remembered for the task
	// Resolve rewrites a call's input before it is shown for approval or run,
	// so the user approves exactly what executes (gmail fills in the account).
	Resolve map[string]func(input json.RawMessage) (json.RawMessage, error)
	Close   func()
}

type workspaceKey struct{}

// WorkspaceFrom returns the task workspace directory a connector handler runs
// in, or "" outside a task.
func WorkspaceFrom(ctx context.Context) string {
	dir, _ := ctx.Value(workspaceKey{}).(string)
	return dir
}

// ConnectorProvider returns a user's connectors that are connected right now.
type ConnectorProvider interface {
	Snapshot(ctx context.Context, userID uuid.UUID) (*ConnectorToolset, error)
}

// Notifier delivers a notification (APNs, ntfy) when a task needs the user or has
// finished: eventType is ask, confirm, idle or error; text is the question,
// the tool name to approve, the result or the error. It is called after the
// event commits, on the run's goroutine; errors are logged.
type Notifier interface {
	Send(ctx context.Context, taskID, eventType, text string) error
}

// Preparer builds what the model gets for a task's prompt (skill
// instructions, project and memory context) and names the skills it picked.
// It runs in the worker at the first turn, so creating a task never waits on
// it, and may call a model; it must be safe to repeat after a crash.
type Preparer interface {
	Prepare(ctx context.Context, task *models.Task) (text string, skills []string, err error)
}

type noConnectors struct{}

func (noConnectors) Snapshot(context.Context, uuid.UUID) (*ConnectorToolset, error) {
	return &ConnectorToolset{}, nil
}

type noNotifier struct{}

func (noNotifier) Send(context.Context, string, string, string) error { return nil }
