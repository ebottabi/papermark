/*
 * Copyright (c) 2026. Papermark - All Rights Reserved
 * Unauthorized copying of this file, via any medium is strictly prohibited
 * Proprietary and confidential
 * Written by Ebot Tabi <ebot.tabi@gmail.com>, 15/10/2026
 */
package harness

import (
	"encoding/json"
	"regexp"
)

// previewField is the input field each user-facing tool shows, so its text
// can stream as a delta before the call completes.
var previewField = map[string]string{"message_notify_user": "text", "message_ask_user": "text", "idle": "result"}

// partialString reads a string field from incomplete JSON, escapes included.
// Tool arguments stream as JSON fragments, so the growing value is read from
// the raw text.
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
