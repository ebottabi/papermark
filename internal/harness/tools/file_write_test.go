/*
 * Copyright (c) 2026. Papermark - All Rights Reserved
 * Unauthorized copying of this file, via any medium is strictly prohibited
 * Proprietary and confidential
 * Written by Ebot Tabi <ebot.tabi@gmail.com>, 18/10/2026
 */
package tools

import "testing"

func TestTidyMarkdown(t *testing.T) {
	for _, c := range []struct{ name, in, want string }{
		{"good markdown is untouched", "# Trip\n\nLine one  \nline two\n\n- a\n  - b\n", "# Trip\n\nLine one  \nline two\n\n- a\n  - b\n"},
		{"whole-document fence is unwrapped", "```markdown\n# Report\n\n| A | B |\n|---|---|\n| 1 | 2 |\n```", "# Report\n\n| A | B |\n|---|---|\n| 1 | 2 |\n"},
		{"a code block inside stays", "```md\n# R\n```\n\n```go\nx\n```", "```md\n# R\n```\n\n```go\nx\n```\n"},
		{"escaped newlines", `# Title\n\n- one\n- two`, "# Title\n\n- one\n- two\n"},
		{"table glued to a paragraph", "Totals:\n| A | B |\n|:--|--:|\n| 1 | 2 |", "Totals:\n\n| A | B |\n|:--|--:|\n| 1 | 2 |\n"},
		{"tables in code are left alone", "```\nx\n| A |\n|---|\n```", "```\nx\n| A |\n|---|\n```\n"},
		{"bare fence around a document", "```\n# Notes\n- a\n```", "# Notes\n- a\n"},
		{"crlf", "a\r\nb\r\n", "a\nb\n"},
	} {
		if got := tidyMarkdown(c.in); got != c.want {
			t.Errorf("%s:\n got %q\nwant %q", c.name, got, c.want)
		}
	}
}
