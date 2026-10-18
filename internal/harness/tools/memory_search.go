/*
 * Copyright (c) 2026. Papermark - All Rights Reserved
 * Unauthorized copying of this file, via any medium is strictly prohibited
 * Proprietary and confidential
 * Written by Ebot Tabi <ebot.tabi@gmail.com>, 17/10/2026
 */
package tools

import (
	"context"
	"encoding/json"
	"strings"

	"papermark/internal/models/queries"
)

const memoryHits = 10

// MemorySearch looks up what the agent remembers about the user.
var MemorySearch = Tool{
	Name:        "memory_search",
	Description: "Search what you remember about the user. Returns up to 10 matches.",
	Schema:      json.RawMessage(`{"type":"object","properties":{"query":{"type":"string"}},"required":["query"],"additionalProperties":false}`),
	Run:         memorySearch,
}

func memorySearch(ctx context.Context, env *Env, input json.RawMessage) (Output, error) {
	var in struct{ Query string }
	if err := json.Unmarshal(input, &in); err != nil {
		return Output{}, err
	}
	found, err := queries.SearchMemories(ctx, env.DB, env.UserID, in.Query, memoryHits)
	if err != nil {
		return Output{}, err
	}
	if len(found) == 0 {
		return Output{Text: "no matching memories"}, nil
	}
	lines := make([]string, len(found))
	for i, m := range found {
		lines[i] = "- " + m.Text
	}
	return Output{Text: strings.Join(lines, "\n")}, nil
}
