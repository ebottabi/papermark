/*
 * Copyright (c) 2026. Papermark - All Rights Reserved
 * Unauthorized copying of this file, via any medium is strictly prohibited
 * Proprietary and confidential
 * Written by Ebot Tabi <ebot.tabi@gmail.com>, 15/10/2026
 */
package mcp

import (
	"strings"
	"testing"
)

func TestToolName(t *testing.T) {
	for _, c := range []struct{ connector, tool, want string }{
		{"notion", "search", "notion_search"},
		{"my-docs", "pages.create v2", "my-docs_pages_create_v2"},
		{"gh", "créer", "gh_cr_er"},
		{"c", strings.Repeat("x", 80), "c_" + strings.Repeat("x", 62)},
	} {
		if got := ToolName(c.connector, c.tool); got != c.want || len(got) > 64 {
			t.Errorf("ToolName(%q, %q) = %q, want %q", c.connector, c.tool, got, c.want)
		}
	}
}
