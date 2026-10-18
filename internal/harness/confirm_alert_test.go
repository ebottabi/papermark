/*
 * Copyright (c) 2026. Papermark - All Rights Reserved
 * Unauthorized copying of this file, via any medium is strictly prohibited
 * Proprietary and confidential
 * Written by Ebot Tabi <ebot.tabi@gmail.com>, 17/10/2026
 */

package harness

import (
	"encoding/json"
	"testing"
)

func TestConfirmAlertSaysWhatIsApproved(t *testing.T) {
	for _, c := range []struct{ name, input, want string }{
		{"gmail_send", `{"to":["ana@x.com","bo@x.com"],"subject":"Hi"}`, "Approve: Send an email · to ana@x.com +1"},
		{"gmail_forward", `{"to":"ana@x.com"}`, "Approve: Forward an email · to ana@x.com"},
		{"calendar_create_event", `{"summary":"Lunch","start":"2026-10-20T12:00"}`, "Approve: Add an event · Lunch"},
		{"calendar_delete_event", `{"eventId":"ev1"}`, "Approve: Delete an event"},
		{"github_create_issue", `null`, "Approve: github create issue"},
	} {
		if got := confirmAlert(c.name, json.RawMessage(c.input)); got != c.want {
			t.Errorf("%s: got %q, want %q", c.name, got, c.want)
		}
	}
}
