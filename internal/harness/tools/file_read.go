/*
 * Copyright (c) 2026. Papermark - All Rights Reserved
 * Unauthorized copying of this file, via any medium is strictly prohibited
 * Proprietary and confidential
 * Written by Ebot Tabi <ebot.tabi@gmail.com>, 15/10/2026
 */
package tools

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
)

// FileRead reads a text file from the workspace.
var FileRead = Tool{
	Name:        "file_read",
	Description: "Read a text file from the workspace.",
	Schema:      json.RawMessage(`{"type":"object","properties":{"path":{"type":"string","description":"Path relative to the workspace"}},"required":["path"],"additionalProperties":false}`),
	Run:         fileRead,
}

func fileRead(_ context.Context, env *Env, input json.RawMessage) (Output, error) {
	var in struct {
		Path *string `json:"path"`
	}
	if err := json.Unmarshal(input, &in); err != nil {
		return Output{}, err
	}
	if in.Path == nil {
		return Output{}, errors.New("path must be a string")
	}
	full, err := env.inside(*in.Path)
	if err != nil {
		return Output{}, err
	}
	raw, err := os.ReadFile(full)
	if err != nil {
		return Output{}, err
	}
	text := string(raw)
	if n := jsLen(text); n > maxRead {
		text = fmt.Sprintf("%s\n[truncated: %d chars total, file kept at %s]", truncateRunes(text, maxRead), n, *in.Path)
	}
	return Output{Text: text}, nil
}
