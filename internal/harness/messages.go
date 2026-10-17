/*
 * Copyright (c) 2026. Papermark - All Rights Reserved
 * Unauthorized copying of this file, via any medium is strictly prohibited
 * Proprietary and confidential
 * Written by Ebot Tabi <ebot.tabi@gmail.com>, 16/10/2026
 */
package harness

import (
	"bytes"
	"encoding/json"
	"regexp"
	"slices"
	"strings"

	"papermark/internal/integrations/fireworks"
)

// stuck reports whether the last three assistant turns made the identical
// tool calls.
func stuck(messages []json.RawMessage) bool {
	var turns []string
	for i := len(messages) - 1; i >= 0 && len(turns) < 3; i-- {
		if roleOf(messages[i]) != "assistant" {
			continue
		}
		calls := callsOf(messages[i])
		if len(calls) == 0 {
			return false
		}
		for j := range calls {
			calls[j].ID = ""
		}
		key, _ := json.Marshal(calls)
		turns = append(turns, string(key))
	}
	return len(turns) == 3 && turns[0] == turns[1] && turns[1] == turns[2]
}

// trimmed is the request copy of an over-budget conversation: long text in
// tool results and assistant turns older than the last trimKeep messages is
// cut. Stored rows are never changed.
func trimmed(messages []json.RawMessage) []json.RawMessage {
	out := slices.Clone(messages)
	for i := range max(len(out)-trimKeep, 0) {
		var m map[string]any
		if json.Unmarshal(out[i], &m) != nil || (m["role"] != "tool" && m["role"] != "assistant") {
			continue
		}
		for _, k := range []string{"content", "reasoning_content"} {
			if s, ok := m[k].(string); ok && len(s) > trimCap {
				m[k] = strings.ToValidUTF8(s[:trimCap], "") + trimNote
			}
		}
		if raw, err := json.Marshal(m); err == nil {
			out[i] = raw
		}
	}
	return out
}

var refPattern = regexp.MustCompile(`"\$ref"\s*:\s*"([^"]*)"`)

// externalRef reports a $ref outside the schema, which Fireworks rejects.
func externalRef(schema json.RawMessage) bool {
	for _, m := range refPattern.FindAllSubmatch(schema, -1) {
		if !bytes.HasPrefix(m[1], []byte("#")) {
			return true
		}
	}
	return false
}

func roleOf(message json.RawMessage) string {
	var m struct {
		Role string `json:"role"`
	}
	json.Unmarshal(message, &m)
	return m.Role
}

// toolCall is one call of an assistant message. Input is nil when the
// model's arguments are not a JSON object.
type toolCall struct {
	ID    string          `json:"id"`
	Name  string          `json:"name"`
	Input json.RawMessage `json:"input"`
}

func callsOf(message json.RawMessage) []toolCall {
	var m fireworks.Message
	json.Unmarshal(message, &m)
	calls := make([]toolCall, len(m.ToolCalls))
	for i, tc := range m.ToolCalls {
		calls[i] = toolCall{ID: tc.ID, Name: tc.Function.Name}
		args := strings.TrimSpace(tc.Function.Arguments)
		if args == "" {
			args = "{}"
		}
		if strings.HasPrefix(args, "{") && json.Valid([]byte(args)) {
			calls[i].Input = json.RawMessage(args)
		}
	}
	return calls
}

// eventInput is a call's input as an event shows it.
func (c toolCall) eventInput() json.RawMessage {
	if c.Input == nil {
		return json.RawMessage(`{}`)
	}
	return c.Input
}

func contentOf(message json.RawMessage) string {
	var m struct {
		Content *string `json:"content"`
	}
	json.Unmarshal(message, &m)
	if m.Content == nil {
		return ""
	}
	return *m.Content
}

func toolMessage(id, content string) (json.RawMessage, error) {
	return json.Marshal(struct {
		Role       string `json:"role"`
		ToolCallID string `json:"tool_call_id"`
		Content    string `json:"content"`
	}{"tool", id, content})
}

func userMessage(text string) (json.RawMessage, error) {
	return json.Marshal(struct {
		Role    string `json:"role"`
		Content string `json:"content"`
	}{"user", text})
}
