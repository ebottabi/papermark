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
	"strings"
	"testing"
	"time"

	"papermark/internal/cache"
	"papermark/internal/integrations/fireworks"

	"github.com/google/uuid"
)

func assistant(calls ...[2]string) json.RawMessage {
	m := fireworks.Message{Role: "assistant"}
	for i, c := range calls {
		m.ToolCalls = append(m.ToolCalls, fireworks.ToolCall{ID: "call_" + string(rune('a'+i)), Type: "function",
			Function: fireworks.Function{Name: c[0], Arguments: c[1]}})
	}
	raw, _ := json.Marshal(m)
	return raw
}

func TestStuckNeedsThreeIdenticalCallTurnsInARow(t *testing.T) {
	same := assistant([2]string{"file_read", `{"path":"x"}`})
	other := assistant([2]string{"file_read", `{"path":"y"}`})
	result, _ := toolMessage("call_a", "Error: no such file")
	for _, c := range []struct {
		msgs []json.RawMessage
		want bool
	}{
		{[]json.RawMessage{same, result, same, result, same, result}, true},
		{[]json.RawMessage{same, result, other, result, same, result}, false},
		{[]json.RawMessage{same, result, same, result}, false},
		{[]json.RawMessage{same, result, same, result, assistant(), same, result}, false},
	} {
		if got := stuck(c.msgs); got != c.want {
			t.Errorf("stuck = %v, want %v", got, c.want)
		}
	}
}

func TestCallsOfRejectsArgumentsThatAreNotAnObject(t *testing.T) {
	calls := callsOf(assistant([2]string{"a", `{"x":1}`}, [2]string{"b", ""}, [2]string{"c", `"str"`}, [2]string{"d", `{"x":`}))
	want := []string{`{"x":1}`, `{}`, "", ""}
	for i, c := range calls {
		if string(c.Input) != want[i] {
			t.Errorf("call %s input %q, want %q", c.Name, c.Input, want[i])
		}
	}
	if string(calls[2].eventInput()) != "{}" {
		t.Error("events must show {} for bad arguments")
	}
}

func TestFallbackOnlyForRetryableErrorsBeforeStreaming(t *testing.T) {
	for _, c := range []struct {
		err  error
		want bool
	}{
		{&fireworks.Error{Kind: fireworks.KindUnavailable}, true},
		{&fireworks.Error{Kind: fireworks.KindRateLimited}, true},
		{&fireworks.Error{Kind: fireworks.KindTimeout, Streamed: true}, false},
		{&fireworks.Error{Kind: fireworks.KindAuth}, false},
		{&fireworks.Error{Kind: fireworks.KindBadRequest}, false},
		{context.Canceled, false},
	} {
		if got := fireworks.Fallback(c.err); got != c.want {
			t.Errorf("Fallback(%v) = %v, want %v", c.err, got, c.want)
		}
	}
}

func TestPartialString(t *testing.T) {
	cases := []struct {
		raw, want string
		ok        bool
	}{
		{`{"text":"Hel`, "Hel", true},
		{`{"text":"a\"b\n`, "a\"b\n", true},
		{`{"text":"caf\u00e`, "caf", true},
		{`{"text":"x\`, "x", true},
		{`{"te`, "", false},
	}
	for _, c := range cases {
		if got, ok := partialString(c.raw, "text"); got != c.want || ok != c.ok {
			t.Errorf("partialString(%q) = %q, %v", c.raw, got, ok)
		}
	}
}

func TestTrimmedShortensOnlyOldLongTextInTheRequestCopy(t *testing.T) {
	long := strings.Repeat("x", trimCap+100)
	msg := func(role, content string) json.RawMessage {
		raw, _ := json.Marshal(map[string]string{"role": role, "content": content})
		return raw
	}
	in := []json.RawMessage{msg("user", long), msg("tool", long), msg("assistant", "short")}
	for range trimKeep {
		in = append(in, msg("tool", long))
	}
	out := trimmed(in)
	if string(out[0]) != string(in[0]) || string(out[2]) != string(in[2]) {
		t.Fatal("user and short messages must be left alone")
	}
	if want := msg("tool", long[:trimCap]+trimNote); string(out[1]) != string(want) {
		t.Fatalf("old tool result %s", out[1])
	}
	if string(out[len(out)-1]) != string(in[len(in)-1]) {
		t.Fatal("recent messages must stay whole")
	}
	if !strings.Contains(string(in[1]), long) {
		t.Fatal("the input slice must not change")
	}
}

func TestExternalRef(t *testing.T) {
	for schema, want := range map[string]bool{
		`{"type":"object"}`:                       false,
		`{"$ref":"#/$defs/a"}`:                    false,
		`{"properties":{"a":{"$ref": "x.json"}}}`: true,
	} {
		if externalRef(json.RawMessage(schema)) != want {
			t.Errorf("externalRef(%s) != %v", schema, want)
		}
	}
}

func TestDeltasPublishActivityOncePerCallAndTool(t *testing.T) {
	bus := cache.NewBus(nil)
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	id := uuid.New()
	frames, stop, err := bus.Subscribe(ctx, id.String())
	if err != nil {
		t.Fatal(err)
	}
	defer stop()
	on := (&turn{h: &Harness{bus: bus}, id: id}).deltas(ctx)
	on(fireworks.Delta{Thinking: true})
	on(fireworks.Delta{Thinking: true})
	on(fireworks.Delta{ToolIndex: 0, ToolName: "file_write", ArgsFragment: `{"path":`})
	on(fireworks.Delta{ToolIndex: 0, ToolName: "file_write", ArgsFragment: `"a.md"}`})
	on(fireworks.Delta{ToolIndex: 1, ToolName: "message_notify_user", ArgsFragment: `{"text":"hi`})
	want := []cache.Frame{
		{Kind: cache.FrameActivity, Text: "thinking"},
		{Kind: cache.FrameActivity, Text: "file_write"},
		{Kind: cache.FrameDelta, Text: "hi"},
	}
	for _, w := range want {
		select {
		case f := <-frames:
			if f.Kind != w.Kind || f.Text != w.Text {
				t.Fatalf("got %+v, want %+v", f, w)
			}
		case <-time.After(time.Second):
			t.Fatalf("missing frame %+v", w)
		}
	}
	select {
	case f := <-frames:
		t.Fatalf("unexpected frame %+v", f)
	case <-time.After(50 * time.Millisecond):
	}
}

func TestStampedPutsTheUsersLocalTimeAboveTheirMessages(t *testing.T) {
	lagos, err := time.LoadLocation("Africa/Lagos")
	if err != nil {
		t.Skip("no zone data")
	}
	now := time.Date(2026, 10, 17, 1, 45, 0, 0, time.UTC).In(lagos)
	got := stamped(now, []string{"what day is it?", "and the time?"})
	want := "[Saturday 17 October 2026, 02:45 WAT]\nwhat day is it?\n\nand the time?"
	if got != want {
		t.Fatalf("got %q, want %q", got, want)
	}
}
