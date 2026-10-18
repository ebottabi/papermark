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
	"io"
	"mime"
	"mime/multipart"
	"mime/quotedprintable"
	"net/http"
	"net/http/httptest"
	"net/mail"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
)

func b64(s string) string { return base64.URLEncoding.EncodeToString([]byte(s)) }

func TestBuildMessageReplyWithAttachment(t *testing.T) {
	raw, err := BuildMessage(Draft{
		To: []string{"Ann <ann@example.com>"}, Cc: []string{"bob@example.com"},
		Subject: "Re: Quarté", Body: "Héllo\nworld",
		InReplyTo: "<a@x>", Refs: "<0@x> <a@x>",
		Attachments: []File{{Name: "report 1.pdf", Data: []byte("%PDF-data")}},
	})
	if err != nil {
		t.Fatal(err)
	}
	msg, err := mail.ReadMessage(strings.NewReader(string(raw)))
	if err != nil {
		t.Fatal(err)
	}
	if msg.Header.Get("In-Reply-To") != "<a@x>" || msg.Header.Get("References") != "<0@x> <a@x>" || msg.Header.Get("MIME-Version") != "1.0" {
		t.Fatalf("headers: %v", msg.Header)
	}
	dec := new(mime.WordDecoder)
	if subj, _ := dec.DecodeHeader(msg.Header.Get("Subject")); subj != "Re: Quarté" {
		t.Fatalf("subject %q", subj)
	}
	if to, _ := msg.Header.AddressList("To"); len(to) != 1 || to[0].Address != "ann@example.com" {
		t.Fatalf("to: %v", to)
	}
	mt, params, err := mime.ParseMediaType(msg.Header.Get("Content-Type"))
	if err != nil || mt != "multipart/mixed" {
		t.Fatalf("content type %q %v", mt, err)
	}
	mr := multipart.NewReader(msg.Body, params["boundary"])
	text, err := mr.NextPart()
	if err != nil {
		t.Fatal(err)
	}
	body, _ := io.ReadAll(quotedprintable.NewReader(text))
	if strings.ReplaceAll(string(body), "\r\n", "\n") != "Héllo\nworld" {
		t.Fatalf("body %q", body)
	}
	att, err := mr.NextPart()
	if err != nil {
		t.Fatal(err)
	}
	if att.FileName() != "report 1.pdf" || att.Header.Get("Content-Type") == "" {
		t.Fatalf("attachment headers: %v", att.Header)
	}
	data, _ := io.ReadAll(base64.NewDecoder(base64.StdEncoding, att))
	if string(data) != "%PDF-data" {
		t.Fatalf("attachment data %q", data)
	}
}

func TestBuildMessageRejects(t *testing.T) {
	for name, d := range map[string]Draft{
		"no recipient":    {Subject: "x"},
		"header injected": {To: []string{"a@x.com"}, Subject: "hi\r\nBcc: evil@x.com"},
		"bad address":     {To: []string{"not an address"}},
	} {
		if _, err := BuildMessage(d); err == nil {
			t.Errorf("%s: accepted", name)
		}
	}
	raw, err := BuildMessage(Draft{To: []string{"a@x.com"}, Subject: "s", Body: "b"})
	if err != nil || strings.Contains(string(raw), "multipart") {
		t.Fatalf("plain message: %v %s", err, raw)
	}
}

func TestExtractBody(t *testing.T) {
	html := Part{MimeType: "text/html"}
	html.Body.Data = b64(`<html><head><style>p{}</style></head><body><p>Hi <b>there</b></p><div class="gmail_quote">old stuff</div></body></html>`)
	got, _ := extract(&html)
	if got != "Hi there" {
		t.Fatalf("html only: %q", got)
	}

	var plain, htmlPart, att Part
	plain.MimeType, plain.Body.Data = "text/plain", b64("Thanks!\n\nOn Mon, 5 Jan 2026 at 10:00, Ann <ann@example.com> wrote:\n> earlier text\n> more")
	htmlPart.MimeType, htmlPart.Body.Data = "text/html", b64("<p>ignored</p>")
	att.MimeType, att.Filename, att.Body.AttachmentID, att.Body.Size = "application/pdf", "a.pdf", "att1", 42
	alt := Part{MimeType: "multipart/alternative", Parts: []Part{plain, htmlPart}}
	root := Part{MimeType: "multipart/mixed", Parts: []Part{alt, att}}
	body, atts := extract(&root)
	if trimQuoted(body) != "Thanks!" || !strings.Contains(body, "earlier text") {
		t.Fatalf("multipart body %q", body)
	}
	if len(atts) != 1 || atts[0].ID != "att1" || atts[0].Filename != "a.pdf" || atts[0].Size != 42 {
		t.Fatalf("attachments: %+v", atts)
	}
}

func TestTrimQuotedAndClip(t *testing.T) {
	in := "Sounds good.\n\nOn Tue, Jan 6, 2026 at 9:15 AM Bob Smith\n<bob@example.com> wrote:\n> hi\n"
	if got := trimQuoted(in); got != "Sounds good." {
		t.Fatalf("trim: %q", got)
	}
	long := strings.Repeat("x", 100)
	if got := clip(long, 10); !strings.HasPrefix(got, "xxxxxxxxxx") || !strings.Contains(got, "truncated") {
		t.Fatalf("clip: %q", got)
	}
	if clip("short", 10) != "short" {
		t.Fatal("clip changed a short string")
	}
}

type memCursor struct {
	mu sync.Mutex
	id string
}

func (m *memCursor) Get(context.Context) (string, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	return m.id, nil
}
func (m *memCursor) Set(_ context.Context, id string) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.id = id
	return nil
}

func one(c *Client, cur Cursor) []Tool {
	return Tools([]Account{{Email: "me@example.com", Primary: true, Client: c, Cursor: cur}}, nil)
}

func tool(t *testing.T, ts []Tool, name string) Tool {
	for _, x := range ts {
		if x.Name == name {
			return x
		}
	}
	t.Fatalf("no tool %s", name)
	return Tool{}
}

func msgJSON(id, from, subject string, labels ...string) map[string]any {
	return map[string]any{
		"id": id, "threadId": "t-" + id, "snippet": "snip " + id, "labelIds": labels,
		"payload": map[string]any{"headers": []map[string]string{
			{"name": "From", "value": from}, {"name": "To", "value": "me@example.com"},
			{"name": "Subject", "value": subject}, {"name": "Date", "value": "Mon, 5 Jan 2026 10:00:00 +0000"},
		}},
	}
}

func newServer(t *testing.T, h http.HandlerFunc) *Client {
	srv := httptest.NewServer(h)
	t.Cleanup(srv.Close)
	return &Client{HTTP: srv.Client(), BaseURL: srv.URL}
}

func TestSearchSummarisesMessages(t *testing.T) {
	c := newServer(t, func(w http.ResponseWriter, r *http.Request) {
		switch {
		case r.URL.Path == "/gmail/v1/users/me/messages":
			if r.URL.Query().Get("q") != "is:unread" || r.URL.Query().Get("maxResults") != "20" {
				t.Errorf("list query: %v", r.URL.Query())
			}
			json.NewEncoder(w).Encode(map[string]any{"messages": []map[string]string{{"id": "m1"}, {"id": "m2"}}, "nextPageToken": "tok"})
		case strings.HasPrefix(r.URL.Path, "/gmail/v1/users/me/messages/"):
			id := strings.TrimPrefix(r.URL.Path, "/gmail/v1/users/me/messages/")
			if r.URL.Query().Get("format") != "metadata" {
				t.Errorf("format %q", r.URL.Query().Get("format"))
			}
			json.NewEncoder(w).Encode(msgJSON(id, "Ann <ann@example.com>", "Subject "+id, "UNREAD", "INBOX"))
		default:
			t.Errorf("unexpected %s", r.URL)
		}
	})
	out, err := tool(t, one(c, &memCursor{}), "search").Run(context.Background(), "", json.RawMessage(`{"query":"is:unread"}`))
	if err != nil {
		t.Fatal(err)
	}
	var res struct {
		Messages []struct {
			ID, ThreadID, From, Subject string
			Unread                      bool
		}
		NextPageToken string
	}
	if err := json.Unmarshal([]byte(out), &res); err != nil {
		t.Fatalf("%v: %s", err, out)
	}
	if len(res.Messages) != 2 || res.Messages[0].ID != "m1" || res.Messages[1].Subject != "Subject m2" || !res.Messages[0].Unread || res.NextPageToken != "tok" {
		t.Fatalf("result: %s", out)
	}
}

func TestUnauthorizedMapsToNeedsAuth(t *testing.T) {
	c := newServer(t, func(w http.ResponseWriter, r *http.Request) {
		http.Error(w, `{"error":{"code":401,"message":"Invalid Credentials"}}`, http.StatusUnauthorized)
	})
	for _, tl := range one(c, &memCursor{}) {
		if tl.Name != "list_labels" {
			continue
		}
		if _, err := tl.Run(context.Background(), "", json.RawMessage(`{}`)); !errors.Is(err, ErrNeedsAuth) {
			t.Fatalf("err = %v", err)
		}
	}
	if _, err := c.Profile(context.Background()); !errors.Is(err, ErrNeedsAuth) {
		t.Fatalf("profile err = %v", err)
	}
}

func TestApprovalFlags(t *testing.T) {
	c := &Client{}
	for _, tl := range one(c, &memCursor{}) {
		wantApproval := map[string]bool{"send": true, "send_draft": true, "forward": true, "trash": true}[tl.Name]
		wantNoAlways := map[string]bool{"send": true, "send_draft": true, "forward": true}[tl.Name]
		if tl.Approval != wantApproval || tl.NoAlways != wantNoAlways {
			t.Errorf("%s: approval=%v noAlways=%v", tl.Name, tl.Approval, tl.NoAlways)
		}
		if !json.Valid(tl.Schema) || tl.Description == "" {
			t.Errorf("%s: schema or description invalid", tl.Name)
		}
	}
}

func TestSendReplyBuildsThreadedMessage(t *testing.T) {
	ws := t.TempDir()
	if err := os.WriteFile(filepath.Join(ws, "note.txt"), []byte("attached"), 0o644); err != nil {
		t.Fatal(err)
	}
	var sent map[string]string
	c := newServer(t, func(w http.ResponseWriter, r *http.Request) {
		switch {
		case r.URL.Path == "/gmail/v1/users/me/messages/orig":
			m := msgJSON("orig", "Ann <ann@example.com>", "Lunch?")
			hs := m["payload"].(map[string]any)["headers"].([]map[string]string)
			m["payload"].(map[string]any)["headers"] = append(hs, map[string]string{"name": "Message-ID", "value": "<orig@x>"}, map[string]string{"name": "References", "value": "<root@x>"})
			json.NewEncoder(w).Encode(m)
		case r.URL.Path == "/gmail/v1/users/me/messages/send":
			json.NewDecoder(r.Body).Decode(&sent)
			json.NewEncoder(w).Encode(map[string]string{"id": "new", "threadId": "t-orig"})
		default:
			t.Errorf("unexpected %s", r.URL)
		}
	})
	out, err := tool(t, one(c, &memCursor{}), "send").Run(context.Background(), ws,
		json.RawMessage(`{"body":"Yes!","replyToMessageId":"orig","attachments":["note.txt"]}`))
	if err != nil || !strings.Contains(out, `"sent":true`) {
		t.Fatalf("send: %q %v", out, err)
	}
	if sent["threadId"] != "t-orig" {
		t.Fatalf("threadId %q", sent["threadId"])
	}
	raw, _ := base64.URLEncoding.DecodeString(sent["raw"])
	msg, err := mail.ReadMessage(strings.NewReader(string(raw)))
	if err != nil {
		t.Fatal(err)
	}
	if msg.Header.Get("In-Reply-To") != "<orig@x>" || msg.Header.Get("References") != "<root@x> <orig@x>" || msg.Header.Get("Subject") != "Re: Lunch?" || !strings.Contains(msg.Header.Get("To"), "ann@example.com") {
		t.Fatalf("reply headers: %v", msg.Header)
	}
	if !strings.Contains(string(raw), "note.txt") {
		t.Fatal("attachment missing")
	}
	if _, err := tool(t, one(c, &memCursor{}), "send").Run(context.Background(), ws, json.RawMessage(`{"to":["a@x.com"],"body":"x","attachments":["../../etc/passwd"]}`)); err == nil {
		t.Fatal("attachment outside the workspace accepted")
	}
	outside := filepath.Join(t.TempDir(), "secret.txt")
	os.WriteFile(outside, []byte("secret"), 0o644)
	if err := os.Symlink(outside, filepath.Join(ws, "link.txt")); err != nil {
		t.Fatal(err)
	}
	if _, err := tool(t, one(c, &memCursor{}), "send").Run(context.Background(), ws, json.RawMessage(`{"to":["a@x.com"],"body":"x","attachments":["link.txt"]}`)); err == nil || !strings.Contains(err.Error(), "escapes") {
		t.Fatalf("symlink out of the workspace accepted: %v", err)
	}
}

func TestDigestSinceLastAdvancesCursorAndFallsBackWhenExpired(t *testing.T) {
	expired := false
	var queries []string
	c := newServer(t, func(w http.ResponseWriter, r *http.Request) {
		switch {
		case r.URL.Path == "/gmail/v1/users/me/history":
			if expired {
				http.Error(w, `{"error":{"code":404,"message":"Requested entity was not found."}}`, http.StatusNotFound)
				return
			}
			if r.URL.Query().Get("startHistoryId") != "100" {
				t.Errorf("startHistoryId %q", r.URL.Query().Get("startHistoryId"))
			}
			json.NewEncoder(w).Encode(map[string]any{"history": []map[string]any{{"id": "101", "messagesAdded": []map[string]any{{"message": map[string]string{"id": "m9"}}}}}})
		case r.URL.Path == "/gmail/v1/users/me/profile":
			json.NewEncoder(w).Encode(map[string]string{"emailAddress": "me@example.com", "historyId": "200"})
		case r.URL.Path == "/gmail/v1/users/me/messages":
			queries = append(queries, r.URL.Query().Get("q"))
			json.NewEncoder(w).Encode(map[string]any{"messages": []map[string]string{{"id": "m1"}}})
		case strings.HasPrefix(r.URL.Path, "/gmail/v1/users/me/messages/"):
			id := strings.TrimPrefix(r.URL.Path, "/gmail/v1/users/me/messages/")
			m := msgJSON(id, "Ann <ann@example.com>", "Hello")
			m["payload"].(map[string]any)["mimeType"] = "text/plain"
			m["payload"].(map[string]any)["body"] = map[string]any{"data": b64(strings.Repeat("word ", 300))}
			json.NewEncoder(w).Encode(m)
		default:
			t.Errorf("unexpected %s", r.URL)
		}
	})
	cur := &memCursor{id: "100"}
	digest := tool(t, one(c, cur), "digest")
	out, err := digest.Run(context.Background(), "", json.RawMessage(`{"sinceLast":true}`))
	if err != nil || !strings.Contains(out, "m9") || cur.id != "200" {
		t.Fatalf("history digest: %q %v cursor=%s", out, err, cur.id)
	}
	if strings.Contains(out, strings.Repeat("word ", 150)) {
		t.Fatal("digest body is not capped near 500 characters")
	}

	expired, cur.id = true, "100"
	out, err = digest.Run(context.Background(), "", json.RawMessage(`{"sinceLast":true}`))
	if err != nil || !strings.Contains(out, "m1") || len(queries) != 1 || queries[0] != "newer_than:1d" || cur.id != "200" {
		t.Fatalf("expired digest: %q %v queries=%v cursor=%s", out, err, queries, cur.id)
	}

	cur.id = "150"
	if _, err := digest.Run(context.Background(), "", json.RawMessage(`{"query":"from:ann"}`)); err != nil || cur.id != "150" {
		t.Fatalf("non-sinceLast digest moved the cursor: %v %s", err, cur.id)
	}
}
