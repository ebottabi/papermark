/*
 * Copyright (c) 2026. Papermark - All Rights Reserved
 * Unauthorized copying of this file, via any medium is strictly prohibited
 * Proprietary and confidential
 * Written by Ebot Tabi <ebot.tabi@gmail.com>, 21/10/2026
 */
package tools

import (
	"strings"
	"testing"
)

func TestSubAgentToolsetDropsEscalatingTools(t *testing.T) {
	names := func(sub bool) string {
		var n []string
		for _, tool := range Offered(&Env{}, false, sub) {
			n = append(n, tool.Name)
		}
		return "," + strings.Join(n, ",") + ","
	}
	sub := names(true)
	// A sub-agent cannot ask the user, run the shell, publish, recurse, or create routines.
	for _, banned := range []string{"message_ask_user", "shell_exec", "publish_update", "agent", "routine_create"} {
		if strings.Contains(sub, ","+banned+",") {
			t.Errorf("sub-agent was offered %q: %s", banned, sub)
		}
	}
	// It keeps the self-contained work tools.
	for _, want := range []string{"message_notify_user", "file_read", "file_write", "web_fetch", "idle"} {
		if !strings.Contains(sub, ","+want+",") {
			t.Errorf("sub-agent missing %q: %s", want, sub)
		}
	}
	// The agent tool is offered at top level only when the harness can spawn.
	if top := func() string {
		var n []string
		for _, tool := range Offered(&Env{CanSpawn: true}, false, false) {
			n = append(n, tool.Name)
		}
		return "," + strings.Join(n, ",") + ","
	}(); !strings.Contains(top, ",agent,") {
		t.Errorf("agent tool not offered at top level with CanSpawn: %s", top)
	}
	if strings.Contains(names(false), ",agent,") {
		t.Error("agent tool offered without CanSpawn")
	}
}
