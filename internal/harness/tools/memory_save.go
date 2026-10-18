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

	"papermark/internal/models"
	"papermark/internal/models/queries"
)

// MemorySave remembers a lasting fact or preference about the user.
var MemorySave = Tool{
	Name:        "memory_save",
	Description: "Remember a lasting fact or preference about the user (not task details). It is shown to you at the start of future tasks. Saving the same fact twice is harmless.",
	Schema:      json.RawMessage(`{"type":"object","properties":{"text":{"type":"string","description":"One self-contained sentence"}},"required":["text"],"additionalProperties":false}`),
	Run:         memorySave,
}

func memorySave(ctx context.Context, env *Env, input json.RawMessage) (Output, error) {
	var in struct{ Text string }
	if err := json.Unmarshal(input, &in); err != nil {
		return Output{}, err
	}
	text, err := models.CleanMemoryText(in.Text)
	if err != nil {
		return Output{}, err
	}
	_, created, err := queries.SaveMemory(ctx, env.DB, env.UserID, text, &env.TaskID)
	if err != nil {
		return Output{}, err
	}
	if !created {
		return Output{Text: "already remembered"}, nil
	}
	return Output{Text: "saved"}, nil
}
