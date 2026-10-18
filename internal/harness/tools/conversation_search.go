/*
 * Copyright (c) 2026. Papermark - All Rights Reserved
 * Unauthorized copying of this file, via any medium is strictly prohibited
 * Proprietary and confidential
 * Written by Ebot Tabi <ebot.tabi@gmail.com>, 18/10/2026
 */

package tools

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"

	"papermark/internal/models/queries"
)

const conversationHits = 8

// speaker names who wrote each searched event type.
var speaker = map[string]string{"message": "user", "reply": "user", "ask": "you asked", "notify": "you", "idle": "result"}

// ConversationSearch looks through the user's past tasks.
var ConversationSearch = Tool{
	Name:        "conversation_search",
	Description: "Search the user's past tasks with you: what they asked, what you told them and the results. Use it when the user refers to earlier work (\"like last time\", \"what did we decide about X\"). Returns up to 8 tasks, best match first, each with its date, its first message, the matching lines and its final result.",
	Schema:      json.RawMessage(`{"type":"object","properties":{"query":{"type":"string","description":"Words to look for"}},"required":["query"],"additionalProperties":false}`),
	Run:         conversationSearch,
}

func conversationSearch(ctx context.Context, env *Env, input json.RawMessage) (Output, error) {
	var in struct{ Query string }
	if err := json.Unmarshal(input, &in); err != nil {
		return Output{}, err
	}
	hits, err := queries.SearchConversations(ctx, env.DB, env.UserID, env.TaskID, in.Query, conversationHits)
	if err != nil {
		return Output{}, err
	}
	if len(hits) == 0 {
		return Output{Text: "no matching past tasks"}, nil
	}
	var b strings.Builder
	for i, h := range hits {
		if i > 0 {
			b.WriteString("\n\n")
		}
		first, _, _ := strings.Cut(h.Prompt, "\n")
		fmt.Fprintf(&b, "Task %s, %s: %q", h.TaskID, h.CreatedAt.UTC().Format("2 Jan 2006"), truncateRunes(first, 120))
		for j, text := range h.Texts {
			fmt.Fprintf(&b, "\n- %s: %s", speaker[h.Types[j]], strings.Join(strings.Fields(text), " "))
		}
		if h.Result != nil && !strings.Contains(strings.Join(h.Types, " "), "idle") {
			fmt.Fprintf(&b, "\n- result: %s", strings.Join(strings.Fields(*h.Result), " "))
		}
	}
	return Output{Text: b.String()}, nil
}
