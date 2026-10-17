/*
 * Copyright (c) 2026. Papermark - All Rights Reserved
 * Unauthorized copying of this file, via any medium is strictly prohibited
 * Proprietary and confidential
 * Written by Ebot Tabi <ebot.tabi@gmail.com>, 15/10/2026
 */
package gmail

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"
	"net/http"
	"strings"
	"sync"
	"testing"
)

// mailbox serves a tiny Gmail for one account, unless status is set.
func mailbox(t *testing.T, ids []string, status int, sent *string) *Client {
	return newServer(t, func(w http.ResponseWriter, r *http.Request) {
		if status != 0 {
			http.Error(w, `{"error":{"message":"nope"}}`, status)
			return
		}
		switch {
		case r.URL.Path == "/gmail/v1/users/me/messages":
			var ms []map[string]string
			for _, id := range ids {
				ms = append(ms, map[string]string{"id": id})
			}
			json.NewEncoder(w).Encode(map[string]any{"messages": ms})
		case r.URL.Path == "/gmail/v1/users/me/messages/send":
			var b map[string]string
			json.NewDecoder(r.Body).Decode(&b)
			if sent != nil {
				raw, _ := base64.URLEncoding.DecodeString(b["raw"])
				*sent = string(raw)
			}
			json.NewEncoder(w).Encode(map[string]string{"id": "new", "threadId": "t"})
		case strings.HasPrefix(r.URL.Path, "/gmail/v1/users/me/messages/"):
			id := strings.TrimPrefix(r.URL.Path, "/gmail/v1/users/me/messages/")
			json.NewEncoder(w).Encode(msgJSON(id, "Ann <ann@example.com>", "Subject "+id, "INBOX"))
		default:
			t.Errorf("unexpected %s", r.URL)
		}
	})
}

func twoAccounts(t *testing.T, bad int, sent *string, onAuth func(string)) []Tool {
	return Tools([]Account{
		{Email: "a@example.com", Primary: true, Client: mailbox(t, []string{"a1"}, 0, sent), Cursor: &memCursor{}},
		{Email: "b@example.com", Client: mailbox(t, []string{"b1"}, bad, nil), Cursor: &memCursor{}},
	}, onAuth)
}

func TestSearchFansOutAndTagsAccounts(t *testing.T) {
	out, err := tool(t, twoAccounts(t, 0, nil, nil), "search").Run(context.Background(), "", json.RawMessage(`{}`))
	if err != nil {
		t.Fatal(err)
	}
	var res struct {
		Messages []struct{ ID, Account string }
	}
	json.Unmarshal([]byte(out), &res)
	got := map[string]string{}
	for _, m := range res.Messages {
		got[m.ID] = m.Account
	}
	if len(got) != 2 || got["a1"] != "a@example.com" || got["b1"] != "b@example.com" {
		t.Fatalf("result: %s", out)
	}
	out, _ = tool(t, twoAccounts(t, 0, nil, nil), "search").Run(context.Background(), "", json.RawMessage(`{"account":"B@Example.com"}`))
	if strings.Contains(out, "a1") || !strings.Contains(out, `"account":"b@example.com"`) {
		t.Fatalf("scoped search: %s", out)
	}
}

func TestIdsNeedAnAccountWhenSeveralAreConnected(t *testing.T) {
	ts := twoAccounts(t, 0, nil, nil)
	_, err := tool(t, ts, "get_message").Run(context.Background(), "", json.RawMessage(`{"id":"a1"}`))
	if err == nil || !strings.Contains(err.Error(), "a@example.com (primary), b@example.com") {
		t.Fatalf("err = %v", err)
	}
	if _, err := tool(t, ts, "get_message").Run(context.Background(), "", json.RawMessage(`{"id":"a1","account":"a@example.com"}`)); err != nil {
		t.Fatal(err)
	}
	if _, err := tool(t, ts, "get_message").Run(context.Background(), "", json.RawMessage(`{"id":"a1","account":"x@example.com"}`)); err == nil {
		t.Fatal("unknown account accepted")
	}
	single := one(mailbox(t, nil, 0, nil), &memCursor{})
	if _, err := tool(t, single, "get_message").Run(context.Background(), "", json.RawMessage(`{"id":"m"}`)); err != nil {
		t.Fatalf("one account should not need account: %v", err)
	}
}

func TestSendDefaultsToPrimaryAndResolveCarriesAccount(t *testing.T) {
	var sent string
	ts := twoAccounts(t, 0, &sent, nil)
	send := tool(t, ts, "send")
	in, err := send.Resolve(json.RawMessage(`{"to":["x@example.com"],"body":"hi"}`))
	if err != nil || accountOf(in) != "a@example.com" {
		t.Fatalf("resolve: %s %v", in, err)
	}
	if _, err := send.Run(context.Background(), "", in); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(sent, "X-Keen-Sent: 1") {
		t.Fatalf("message lacks the Keen header: %q", sent)
	}
	if _, err := tool(t, ts, "send_draft").Resolve(json.RawMessage(`{"draftId":"d"}`)); err == nil {
		t.Fatal("send_draft resolved without an account")
	}
}

func TestUnauthorizedAffectsOneAccount(t *testing.T) {
	var mu sync.Mutex
	var marked []string
	ts := twoAccounts(t, http.StatusUnauthorized, nil, func(email string) {
		mu.Lock()
		marked = append(marked, email)
		mu.Unlock()
	})
	out, err := tool(t, ts, "search").Run(context.Background(), "", json.RawMessage(`{}`))
	if err != nil || !strings.Contains(out, "a1") || !strings.Contains(out, "b@example.com needs reconnecting") {
		t.Fatalf("fan-out: %q %v", out, err)
	}
	if len(marked) != 1 || marked[0] != "b@example.com" {
		t.Fatalf("marked %v", marked)
	}
	_, err = tool(t, ts, "get_message").Run(context.Background(), "", json.RawMessage(`{"id":"b1","account":"b@example.com"}`))
	var ae *AccountAuthError
	if !errors.As(err, &ae) || !errors.Is(err, ErrNeedsAuth) || ae.Email != "b@example.com" {
		t.Fatalf("err = %v", err)
	}
}
