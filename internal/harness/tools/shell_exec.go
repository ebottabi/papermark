/*
 * Copyright (c) 2026. Papermark - All Rights Reserved
 * Unauthorized copying of this file, via any medium is strictly prohibited
 * Proprietary and confidential
 * Written by Ebot Tabi <ebot.tabi@gmail.com>, 15/10/2026
 */
package tools

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os/exec"
	"strconv"
	"strings"
	"syscall"
	"time"
)

const maxShellOutput = 30_000

// ShellExec runs a shell command in the workspace, once the user approves.
var ShellExec = Tool{
	Name:          "shell_exec",
	Description:   "Run a shell command with the workspace as the working directory. Returns stdout and stderr. Needs the user's approval.",
	Schema:        json.RawMessage(`{"type":"object","properties":{"command":{"type":"string","description":"Shell command"},"timeoutSec":{"type":"number","description":"Kill the command after this many seconds (default 120)"}},"required":["command"],"additionalProperties":false}`),
	NeedsApproval: true,
	Run:           shellExec,
}

func shellExec(ctx context.Context, env *Env, input json.RawMessage) (Output, error) {
	var in struct {
		Command    *string  `json:"command"`
		TimeoutSec *float64 `json:"timeoutSec"`
	}
	if err := json.Unmarshal(input, &in); err != nil {
		return Output{}, err
	}
	if in.Command == nil {
		return Output{}, errors.New("command must be a string")
	}
	timeout := 120.0
	if in.TimeoutSec != nil {
		timeout = *in.TimeoutSec
	}
	return runShell(ctx, env.Workspace, *in.Command, timeout), nil
}

// capped keeps output until it reaches the cap; later writes are dropped.
type capped struct{ bytes.Buffer }

func (c *capped) Write(p []byte) (int, error) {
	if c.Len() < maxShellOutput {
		c.Buffer.Write(p)
	}
	return len(p), nil
}

// runShell runs command with /bin/sh in the workspace. The shell leads its
// own process group, so a kill reaches everything it started.
func runShell(ctx context.Context, workspace, command string, timeoutSec float64) Output {
	cmd := exec.Command("/bin/sh", "-c", command)
	cmd.Dir = workspace
	cmd.SysProcAttr = &syscall.SysProcAttr{Setpgid: true}
	var out capped
	cmd.Stdout, cmd.Stderr = &out, &out
	if err := cmd.Start(); err != nil {
		return Output{Text: err.Error(), IsError: true}
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
		if errors.Is(context.Cause(ctx), ErrStopped) {
			why = ErrStopped.Error()
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
	return Output{Text: fmt.Sprintf("%s\n[%s]", body, status), IsError: code != 0 || killedBy != ""}
}
