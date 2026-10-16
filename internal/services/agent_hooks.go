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
)

// ToolOutput is what a tool call returns to the model.
type ToolOutput struct {
	Text    string
	IsError bool
}

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
	Close         func()
}

// ConnectorProvider returns the connectors that are connected right now.
type ConnectorProvider interface {
	Snapshot(ctx context.Context) (*ConnectorToolset, error)
}

// Notifier delivers a notification (APNs) when a task needs the user or has
// finished: eventType is ask, confirm, idle or error; text is the question,
// the tool name to approve, the result or the error. It is called after the
// event commits, on the run's goroutine; errors are logged.
type Notifier interface {
	Send(ctx context.Context, taskID, eventType, text string) error
}

type noConnectors struct{}

func (noConnectors) Snapshot(context.Context) (*ConnectorToolset, error) {
	return &ConnectorToolset{}, nil
}

type noNotifier struct{}

func (noNotifier) Send(context.Context, string, string, string) error { return nil }
