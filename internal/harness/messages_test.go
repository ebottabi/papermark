/*
 * Copyright (c) 2026. Papermark - All Rights Reserved
 * Unauthorized copying of this file, via any medium is strictly prohibited
 * Proprietary and confidential
 * Written by Ebot Tabi <ebot.tabi@gmail.com>, 21/10/2026
 */
package harness

import (
	"encoding/json"
	"testing"

	"papermark/internal/integrations/fireworks"
)

func call(name, args string) fireworks.ToolCall {
	return fireworks.ToolCall{ID: "c_" + name, Function: fireworks.Function{Name: name, Arguments: args}}
}

// A length-truncated turn leaves a half-written tool call and a large
// reasoning_content. replayable must drop the broken call and clear the
// reasoning so the stored turn is safe to resend; Fireworks 400s otherwise.
func TestReplayableDropsTruncatedCallAndReasoning(t *testing.T) {
	m := fireworks.Message{
		Role:             "assistant",
		ReasoningContent: "...56KB of thinking...",
		ToolCalls: []fireworks.ToolCall{
			call("file_write", `{"path":"todo.md","text":"ok"}`), // complete
			call("idle", ``),                                     // empty args are valid ("{}")
			call("file_write", `{"path":"big.md","text":"half`),  // truncated: invalid JSON
		},
	}

	got := replayable(m)

	if got.ReasoningContent != "" {
		t.Error("reasoning_content must be cleared before replay")
	}
	if len(got.ToolCalls) != 2 {
		t.Fatalf("got %d tool calls, want 2 (truncated one dropped)", len(got.ToolCalls))
	}
	if got.ToolCalls[0].Function.Name != "file_write" || got.ToolCalls[1].Function.Name != "idle" {
		t.Errorf("kept the wrong calls: %q, %q", got.ToolCalls[0].Function.Name, got.ToolCalls[1].Function.Name)
	}
	if m.ReasoningContent == "" || len(m.ToolCalls) != 3 {
		t.Error("replayable mutated the caller's message; it must leave the response intact")
	}
}

// The call replayable keeps is exactly the call callsOf can run, so a stored
// turn never carries a tool call the model cannot answer.
func TestReplayableAgreesWithCallsOf(t *testing.T) {
	m := fireworks.Message{Role: "assistant", ToolCalls: []fireworks.ToolCall{
		call("good", `{"a":1}`),
		call("truncated", `{"a":`),
	}}

	kept := replayable(m).ToolCalls
	if len(kept) != 1 || kept[0].Function.Name != "good" {
		t.Fatalf("replayable kept %d calls, want just the runnable one", len(kept))
	}
	raw, err := json.Marshal(m)
	if err != nil {
		t.Fatal(err)
	}
	for _, c := range callsOf(raw) {
		if c.Name == "truncated" && c.Input != nil {
			t.Error("callsOf would run a call replayable dropped; predicates disagree")
		}
	}
}
