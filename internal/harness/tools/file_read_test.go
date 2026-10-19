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
	"os"
	"path/filepath"
	"strings"
	"testing"
)

type fakeMedia struct{ question string }

func (f *fakeMedia) Image(_ context.Context, _, q string) (string, error) {
	f.question = q
	return "a cat", nil
}
func (f *fakeMedia) Audio(context.Context, string) (string, error) { return "", nil }
func (f *fakeMedia) Video(context.Context, string, string) (string, error) {
	return "", nil
}

func TestFileReadImage(t *testing.T) {
	dir := t.TempDir()
	png := append([]byte("\x89PNG\r\n\x1a\n"), make([]byte, 32)...)
	if err := os.WriteFile(filepath.Join(dir, "a.png"), png, 0o644); err != nil {
		t.Fatal(err)
	}
	in := json.RawMessage(`{"path":"a.png","question":"what animal?"}`)
	env := &Env{Workspace: dir}
	if _, err := fileRead(context.Background(), env, in); err == nil || !strings.Contains(err.Error(), "not set up") {
		t.Fatalf("nil media: %v", err)
	}
	m := &fakeMedia{}
	env.Media = m
	out, err := fileRead(context.Background(), env, in)
	if err != nil || out.Text != "a cat" || m.question != "what animal?" {
		t.Fatalf("got %q %q %v", out.Text, m.question, err)
	}
}
