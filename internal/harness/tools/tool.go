/*
 * Copyright (c) 2026. Papermark - All Rights Reserved
 * Unauthorized copying of this file, via any medium is strictly prohibited
 * Proprietary and confidential
 * Written by Ebot Tabi <ebot.tabi@gmail.com>, 17/10/2026
 */

// Package tools holds the agent's built-in tools, one file per tool.
package tools

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"path/filepath"
	"time"
	"unicode/utf16"

	"papermark/internal/harness/skills"
	"papermark/internal/models/queries"
	"papermark/internal/storage"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
)

const maxRead = 50_000

// Output is what a tool call returns to the model.
type Output struct {
	Text    string
	IsError bool
}

// Tool is one built-in tool. A nil Run means the agent loop handles the
// call itself (message_ask_user and idle).
type Tool struct {
	Name          string
	Description   string
	Schema        json.RawMessage // a JSON Schema object
	NeedsApproval bool
	Run           func(ctx context.Context, env *Env, input json.RawMessage) (Output, error)
}

// Env is what one task's tools run against.
type Env struct {
	Workspace  string
	TaskID     uuid.UUID
	UserID     uuid.UUID
	ScheduleID *uuid.UUID // set when a routine started the task
	DB         queries.DBTX
	Web        *Web
	Routines   RoutineCreator // nil: no routine_create
	Skills     []skills.Skill // what skill_read offers, sorted by name
	// Notify adds a notify event to the task.
	Notify func(ctx context.Context, text string) error
	// TaskTx runs fn in the task's locked transaction; events emitted in fn
	// commit with its other writes.
	TaskTx func(ctx context.Context, fn func(tx pgx.Tx, emit func(event any) error) error) error
}

// Offered returns the tools one run offers, in the order the model sees
// them. The order is fixed so the prompt-cache prefix stays stable.
func Offered(env *Env, routineRun bool) []Tool {
	out := []Tool{NotifyUser, AskUser, FileRead, FileWrite, ShellExec, WebFetch, Idle}
	if env.Web.CanSearch() {
		out = append(out, WebSearch)
	}
	out = append(out, PublishUpdate, MemorySave, MemorySearch, ConversationSearch)
	if len(env.Skills) > 0 {
		out = append(out, SkillRead(env.Skills))
	}
	if env.Routines != nil && !routineRun {
		out = append(out, RoutineCreate)
	}
	return out
}

// Function wraps a JSON Schema as a function tool.
func Function(name, description string, parameters json.RawMessage) json.RawMessage {
	raw, err := json.Marshal(map[string]any{"type": "function",
		"function": map[string]any{"name": name, "description": description, "parameters": parameters}})
	if err != nil {
		panic(err)
	}
	return raw
}

// ErrStopped is the cancel cause when the user stops a running task.
var ErrStopped = errors.New("stopped by the user")

// Location loads an IANA zone; the preferences API only stores valid ones,
// so UTC here means the server lacks the zone data.
func Location(name string) *time.Location {
	if loc, err := time.LoadLocation(name); err == nil {
		return loc
	}
	return time.UTC
}

func (e *Env) inside(p string) (string, error) {
	root, err := filepath.Abs(e.Workspace)
	if err != nil {
		return "", err
	}
	full, err := storage.Resolve(root, p)
	if err != nil || full == root {
		return "", fmt.Errorf("Path escapes the workspace: %s", p)
	}
	return full, nil
}

// jsLen is a string's length in UTF-16 code units, as the tool results
// promised by the TypeScript server counted it.
func jsLen(s string) int { return len(utf16.Encode([]rune(s))) }

// truncateRunes keeps the first n characters of s.
func truncateRunes(s string, n int) string {
	r := []rune(s)
	if len(r) <= n {
		return s
	}
	return string(r[:n])
}
