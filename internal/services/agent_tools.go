/*
 * Copyright (c) 2026. Papermark - All Rights Reserved
 * Unauthorized copying of this file, via any medium is strictly prohibited
 * Proprietary and confidential
 * Written by Ebot Tabi <ebot.tabi@gmail.com>, 15/10/2026
 */
package services

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
	"syscall"
	"time"
	"unicode/utf16"

	"papermark/internal/storage"
)

const (
	maxRead        = 50_000
	maxShellOutput = 30_000
)

// systemPrompt is kept byte-stable (no dates, no per-task data) so the
// prompt-cache prefix survives every step.
const systemPrompt = `You are an autonomous agent working in a loop: read the latest events, choose the next tool, observe its result, and repeat until the task is done. The user may close the app while you work, so keep going without them unless you are truly blocked.

- Every response must call at least one tool.
- First write your plan to todo.md as a markdown checklist. After finishing each step, rewrite todo.md with that step checked, so your objectives stay fresh.
- Save large intermediate results to files in the workspace and refer to them by path instead of repeating them.
- Use message_notify_user for progress updates; it does not wait for a reply. Use message_ask_user only for a decision or credential only the user can give; a reply may take hours.
- When a tool fails, read the error and change approach; do not repeat the same failing call.
- Tools prefixed with a connector name (for example notion_ or gmail_) act on the user's connected accounts. The system asks the user to approve changes there before they run; if a call comes back declined, do not retry it, find another way or ask why.
- shell_exec runs a command in the workspace. The user approves every call, so combine steps into few commands.
- Use web_search and web_fetch for anything current or outside your knowledge, and cite the pages you used.
- The user may send messages while you work; they arrive next to tool results. Follow the latest instructions.
- When everything is done, call idle with a summary of the result and the workspace paths of the files the user should open.`

// builtinTools are the agent's own tools. Names share prefixes (message_,
// file_) the way Manus groups tools.
var builtinTools = []json.RawMessage{
	json.RawMessage(`{"name":"message_notify_user","description":"Send the user a progress message without waiting for a reply.","input_schema":{"type":"object","properties":{"text":{"type":"string","description":"Message"}},"required":["text"],"additionalProperties":false}}`),
	json.RawMessage(`{"name":"message_ask_user","description":"Ask the user a question and wait for their reply. Pauses the task.","input_schema":{"type":"object","properties":{"text":{"type":"string","description":"Question"}},"required":["text"],"additionalProperties":false}}`),
	json.RawMessage(`{"name":"file_read","description":"Read a text file from the workspace.","input_schema":{"type":"object","properties":{"path":{"type":"string","description":"Path relative to the workspace"}},"required":["path"],"additionalProperties":false}}`),
	json.RawMessage(`{"name":"file_write","description":"Write or append to a text file in the workspace.","input_schema":{"type":"object","properties":{"path":{"type":"string","description":"Path relative to the workspace"},"content":{"type":"string","description":"File content"},"append":{"type":"boolean"}},"required":["path","content"],"additionalProperties":false}}`),
	json.RawMessage(`{"name":"shell_exec","description":"Run a shell command with the workspace as the working directory. Returns stdout and stderr. Needs the user's approval.","input_schema":{"type":"object","properties":{"command":{"type":"string","description":"Shell command"},"timeoutSec":{"type":"number","description":"Kill the command after this many seconds (default 120)"}},"required":["command"],"additionalProperties":false}}`),
	json.RawMessage(`{"type":"web_search_20260318","name":"web_search"}`),
	json.RawMessage(`{"type":"web_fetch_20260318","name":"web_fetch"}`),
	json.RawMessage(`{"name":"idle","description":"Call when all work is complete. Ends the task.","input_schema":{"type":"object","properties":{"result":{"type":"string","description":"Summary of the result"},"files":{"type":"array","items":{"type":"string"},"description":"Workspace paths of the deliverable files"}},"required":["result"],"additionalProperties":false}}`),
}

// previewField is the input field each user-facing tool shows, so its text
// can stream as a delta before the call completes.
var previewField = map[string]string{"message_notify_user": "text", "message_ask_user": "text", "idle": "result"}

// partialString reads a string field from incomplete JSON, escapes included.
// The SDK only parses a string once its closing quote arrives, so the growing
// value is read from the raw JSON.
func partialString(raw, field string) (string, bool) {
	m := regexp.MustCompile(`"` + regexp.QuoteMeta(field) + `"\s*:\s*"((?:[^"\\]|\\.)*)`).FindStringSubmatch(raw)
	if m == nil {
		return "", false
	}
	var s string
	if json.Unmarshal([]byte(`"`+trailingEscape.ReplaceAllString(m[1], "")+`"`), &s) != nil {
		return "", false
	}
	return s, true
}

var trailingEscape = regexp.MustCompile(`\\u[0-9a-fA-F]{0,3}$`)

// jsLen is a string's length in UTF-16 code units, as the tool results
// promised by the TypeScript server counted it.
func jsLen(s string) int { return len(utf16.Encode([]rune(s))) }

// truncateRunes keeps the first n characters of s.
func truncateRunes(s string, n int) string {
	r := []rune(s)
	if len(r) <= n {
		return s
	}
	return string(r[:n])
}

// tools runs the built-in tools for one task's workspace.
type tools struct {
	workspace string
	notify    func(text string) error
}

func (t *tools) inside(p string) (string, error) {
	root, err := filepath.Abs(t.workspace)
	if err != nil {
		return "", err
	}
	full, err := storage.Resolve(root, p)
	if err != nil || full == root {
		return "", fmt.Errorf("Path escapes the workspace: %s", p)
	}
	return full, nil
}

func (t *tools) has(name string) bool {
	switch name {
	case "message_notify_user", "file_read", "file_write", "shell_exec":
		return true
	}
	return false
}

func (t *tools) call(ctx context.Context, name string, input json.RawMessage) (ToolOutput, error) {
	var in struct {
		Text       *string  `json:"text"`
		Path       *string  `json:"path"`
		Content    *string  `json:"content"`
		Append     bool     `json:"append"`
		Command    *string  `json:"command"`
		TimeoutSec *float64 `json:"timeoutSec"`
	}
	if err := json.Unmarshal(input, &in); err != nil {
		return ToolOutput{}, err
	}
	switch name {
	case "message_notify_user":
		if in.Text == nil {
			return ToolOutput{}, errors.New("text must be a string")
		}
		if err := t.notify(*in.Text); err != nil {
			return ToolOutput{}, err
		}
		return ToolOutput{Text: "delivered"}, nil
	case "file_read":
		if in.Path == nil {
			return ToolOutput{}, errors.New("path must be a string")
		}
		full, err := t.inside(*in.Path)
		if err != nil {
			return ToolOutput{}, err
		}
		raw, err := os.ReadFile(full)
		if err != nil {
			return ToolOutput{}, err
		}
		text := string(raw)
		if n := jsLen(text); n > maxRead {
			text = fmt.Sprintf("%s\n[truncated: %d chars total, file kept at %s]", truncateRunes(text, maxRead), n, *in.Path)
		}
		return ToolOutput{Text: text}, nil
	case "file_write":
		if in.Path == nil || in.Content == nil {
			return ToolOutput{}, errors.New("path and content must be strings")
		}
		full, err := t.inside(*in.Path)
		if err != nil {
			return ToolOutput{}, err
		}
		if err := os.MkdirAll(filepath.Dir(full), 0o755); err != nil {
			return ToolOutput{}, err
		}
		flags := os.O_WRONLY | os.O_CREATE | os.O_TRUNC
		if in.Append {
			flags = os.O_WRONLY | os.O_CREATE | os.O_APPEND
		}
		f, err := os.OpenFile(full, flags, 0o644)
		if err != nil {
			return ToolOutput{}, err
		}
		if _, err := f.WriteString(*in.Content); err != nil {
			f.Close()
			return ToolOutput{}, err
		}
		if err := f.Close(); err != nil {
			return ToolOutput{}, err
		}
		return ToolOutput{Text: fmt.Sprintf("wrote %d chars to %s", jsLen(*in.Content), *in.Path)}, nil
	case "shell_exec":
		if in.Command == nil {
			return ToolOutput{}, errors.New("command must be a string")
		}
		timeout := 120.0
		if in.TimeoutSec != nil {
			timeout = *in.TimeoutSec
		}
		return shellExec(ctx, t.workspace, *in.Command, timeout), nil
	}
	return ToolOutput{Text: "Unknown tool: " + name, IsError: true}, nil
}

// capped keeps output until it reaches the cap; later writes are dropped.
type capped struct{ bytes.Buffer }

func (c *capped) Write(p []byte) (int, error) {
	if c.Len() < maxShellOutput {
		c.Buffer.Write(p)
	}
	return len(p), nil
}

// errStopped is the cancel cause when the user stops a running task.
var errStopped = errors.New("stopped by the user")

// shellExec runs command with /bin/sh in the workspace. The shell leads its
// own process group, so a kill reaches everything it started.
func shellExec(ctx context.Context, workspace, command string, timeoutSec float64) ToolOutput {
	cmd := exec.Command("/bin/sh", "-c", command)
	cmd.Dir = workspace
	cmd.SysProcAttr = &syscall.SysProcAttr{Setpgid: true}
	var out capped
	cmd.Stdout, cmd.Stderr = &out, &out
	if err := cmd.Start(); err != nil {
		return ToolOutput{Text: err.Error(), IsError: true}
	}
	done := make(chan struct{})
	go func() { cmd.Wait(); close(done) }()
	timer := time.NewTimer(time.Duration(timeoutSec * float64(time.Second)))
	defer timer.Stop()
	killedBy := ""
	kill := func(why string) {
		killedBy = why
		syscall.Kill(-cmd.Process.Pid, syscall.SIGKILL)
		<-done
	}
	select {
	case <-done:
	case <-timer.C:
		kill("timed out after " + strconv.FormatFloat(timeoutSec, 'f', -1, 64) + " s")
	case <-ctx.Done():
		why := "interrupted"
		if errors.Is(context.Cause(ctx), errStopped) {
			why = errStopped.Error()
		}
		kill(why)
	}
	body := out.String()
	if len(body) > maxShellOutput {
		body = strings.ToValidUTF8(body[:maxShellOutput], "") + "\n[output truncated]"
	}
	code := cmd.ProcessState.ExitCode()
	status := fmt.Sprintf("exit code %d", code)
	if killedBy != "" {
		status = "killed: " + killedBy
	}
	return ToolOutput{Text: fmt.Sprintf("%s\n[%s]", body, status), IsError: code != 0 || killedBy != ""}
}

// serverToolEvents turns each web_search/web_fetch use in content and its
// result into a tool event (server tools run inside the API call).
func serverToolEvents(content json.RawMessage) []toolEvent {
	type block struct {
		Type      string          `json:"type"`
		ID        string          `json:"id"`
		Name      string          `json:"name"`
		Input     json.RawMessage `json:"input"`
		ToolUseID string          `json:"tool_use_id"`
		Content   json.RawMessage `json:"content"`
	}
	var blocks []block
	json.Unmarshal(content, &blocks)
	var out []toolEvent
	for _, b := range blocks {
		if b.Type != "server_tool_use" || (b.Name != "web_search" && b.Name != "web_fetch") {
			continue
		}
		// shortcut: a use whose result lands in the next response (after pause_turn) gets no event; pair across responses if that shows up.
		for _, r := range blocks {
			if (r.Type != "web_search_tool_result" && r.Type != "web_fetch_tool_result") || r.ToolUseID != b.ID {
				continue
			}
			ev := toolEvent{Type: "tool", Name: b.Name, Input: b.Input}
			var list []struct{ Title, URL string }
			var one struct {
				Type      string `json:"type"`
				ErrorCode string `json:"error_code"`
				URL       string `json:"url"`
			}
			if json.Unmarshal(r.Content, &list) == nil {
				lines := make([]string, len(list))
				for i, x := range list {
					lines[i] = x.Title + " " + x.URL
				}
				ev.Output = strings.Join(lines, "\n")
			} else if json.Unmarshal(r.Content, &one); strings.HasSuffix(one.Type, "_error") {
				ev.Output, ev.IsError = "error: "+one.ErrorCode, true
			} else {
				ev.Output = "fetched " + one.URL
			}
			out = append(out, ev)
			break
		}
	}
	return out
}
