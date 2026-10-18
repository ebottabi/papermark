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
	"path/filepath"
)

// FileWrite writes or appends to a text file in the workspace.
var FileWrite = Tool{
	Name:        "file_write",
	Description: "Write or append to a text file in the workspace.",
	Schema:      json.RawMessage(`{"type":"object","properties":{"path":{"type":"string","description":"Path relative to the workspace"},"content":{"type":"string","description":"File content"},"append":{"type":"boolean"}},"required":["path","content"],"additionalProperties":false}`),
	Run:         fileWrite,
}

func fileWrite(_ context.Context, env *Env, input json.RawMessage) (Output, error) {
	var in struct {
		Path    *string `json:"path"`
		Content *string `json:"content"`
		Append  bool    `json:"append"`
	}
	if err := json.Unmarshal(input, &in); err != nil {
		return Output{}, err
	}
	if in.Path == nil || in.Content == nil {
		return Output{}, errors.New("path and content must be strings")
	}
	full, err := env.inside(*in.Path)
	if err != nil {
		return Output{}, err
	}
	if err := os.MkdirAll(filepath.Dir(full), 0o755); err != nil {
		return Output{}, err
	}
	flags := os.O_WRONLY | os.O_CREATE | os.O_TRUNC
	if in.Append {
		flags = os.O_WRONLY | os.O_CREATE | os.O_APPEND
	}
	f, err := os.OpenFile(full, flags, 0o644)
	if err != nil {
		return Output{}, err
	}
	if _, err := f.WriteString(*in.Content); err != nil {
		f.Close()
		return Output{}, err
	}
	if err := f.Close(); err != nil {
		return Output{}, err
	}
	return Output{Text: fmt.Sprintf("wrote %d chars to %s", jsLen(*in.Content), *in.Path)}, nil
}
