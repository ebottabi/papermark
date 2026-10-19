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
	"strings"
)

// FileWrite writes or appends to a text file in the workspace.
var FileWrite = Tool{
	Name:        "file_write",
	Description: "Write or append to a text file in the workspace. Write .md files as plain GitHub-flavoured Markdown (headings, lists, tables), never wrapped in a code fence; the app renders them formatted.",
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
	content := *in.Content
	if !in.Append && isMarkdown(full) {
		content = tidyMarkdown(content)
	}
	if _, err := f.WriteString(content); err != nil {
		f.Close()
		return Output{}, err
	}
	if err := f.Close(); err != nil {
		return Output{}, err
	}
	return Output{Text: fmt.Sprintf("wrote %d chars to %s", jsLen(*in.Content), *in.Path)}, nil
}

func isMarkdown(path string) bool {
	ext := strings.ToLower(filepath.Ext(path))
	return ext == ".md" || ext == ".markdown"
}

// tidyMarkdown repairs the layout slips models make in Markdown files without
// changing their words: a whole-document code fence, escaped newlines, and a
// table glued to the paragraph above it.
func tidyMarkdown(s string) string {
	s = strings.ReplaceAll(s, "\r\n", "\n")
	if !strings.Contains(s, "\n") && strings.Contains(s, `\n`) {
		s = strings.ReplaceAll(s, `\n`, "\n")
	}
	if t := strings.TrimSpace(s); strings.HasPrefix(t, "```") && strings.HasSuffix(t, "```") {
		first, body, _ := strings.Cut(t, "\n")
		if lang := strings.ToLower(strings.TrimSpace(strings.TrimLeft(first, "`"))); lang == "md" || lang == "markdown" || lang == "" && strings.HasPrefix(body, "#") {
			if inner := strings.TrimSuffix(body, "```"); !strings.Contains(inner, "```") {
				s = inner
			}
		}
	}
	lines := strings.Split(s, "\n")
	out := make([]string, 0, len(lines))
	fenced := false
	for i, line := range lines {
		t := strings.TrimSpace(line)
		if strings.HasPrefix(t, "```") || strings.HasPrefix(t, "~~~") {
			fenced = !fenced
		}
		if !fenced && i > 0 && i+1 < len(lines) && strings.Contains(t, "|") && isTableRule(lines[i+1]) {
			if prev := strings.TrimSpace(lines[i-1]); prev != "" && !strings.Contains(prev, "|") {
				out = append(out, "")
			}
		}
		out = append(out, line)
	}
	return strings.Trim(strings.Join(out, "\n"), "\n") + "\n"
}

// isTableRule reports whether line is a table's header separator, like |---|:--:|.
func isTableRule(line string) bool {
	t := strings.Trim(strings.TrimSpace(line), "|")
	if !strings.Contains(t, "-") {
		return false
	}
	for _, c := range strings.Split(t, "|") {
		c = strings.Trim(strings.TrimSpace(c), ":")
		if c == "" || strings.Trim(c, "-") != "" {
			return false
		}
	}
	return true
}
