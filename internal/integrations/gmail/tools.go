/*
 * Copyright (c) 2026. Papermark - All Rights Reserved
 * Unauthorized copying of this file, via any medium is strictly prohibited
 * Proprietary and confidential
 * Written by Ebot Tabi <ebot.tabi@gmail.com>, 15/10/2026
 */
package gmail

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/mail"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"unicode"

	"papermark/internal/storage"
)

const (
	messageCap    = 12_000 // characters of body per gmail_get_message
	threadMsgCap  = 4_000  // per message in gmail_get_thread
	threadCap     = 40_000 // whole gmail_get_thread result
	previewLen    = 500
	fetchParallel = 8
	maxAttachment = 18 << 20 // Gmail's 25 MB message limit after base64
)

// Cursor stores the Gmail history id the last gmail_digest(sinceLast) reached.
type Cursor interface {
	Get(ctx context.Context) (string, error)
	Set(ctx context.Context, historyID string) error
}

// Tool is one agent tool. Name has no connector prefix. Run gets the task
// workspace directory ("" when the caller has none) and the model's JSON input.
type Tool struct {
	Name        string
	Description string
	Schema      json.RawMessage
	Approval    bool // the user approves each call
	NoAlways    bool // and cannot approve it once for the whole task
	Run         func(ctx context.Context, workspace string, input json.RawMessage) (string, error)
	// Resolve returns input with the account made explicit; nil when the tool has no confirm step.
	Resolve func(input json.RawMessage) (json.RawMessage, error)
}

type toolset struct {
	c     *Client
	cur   Cursor
	email string
}

// accountTools returns the Gmail tools for one mailbox, in a stable order.
func accountTools(c *Client, cur Cursor, email string) []Tool {
	t := &toolset{c, cur, email}
	return []Tool{
		{Name: "search", Run: t.search, Description: "Search the user's Gmail with Gmail search syntax (from:, to:, subject:, is:unread, has:attachment, newer_than:7d, label:...). Returns one page of messages with id, threadId, from, to, subject, date, snippet, unread, labels, and nextPageToken when there are more. Empty query lists the newest mail.",
			Schema: schema(`"query":{"type":"string","description":"Gmail search query"},"maxResults":{"type":"integer","minimum":1,"maximum":100,"description":"Default 20"},"pageToken":{"type":"string","description":"nextPageToken from the previous page"}`)},
		{Name: "get_message", Run: t.getMessage, Description: "Read one message: headers, attachment list (id, filename, mimeType, size) and the plain-text body with quoted reply history removed. Long bodies are truncated.",
			Schema: schema(`"id":{"type":"string","description":"Message id"}`, "id")},
		{Name: "get_thread", Run: t.getThread, Description: "Read every message of a conversation, oldest first, each with headers, attachment list and a shortened plain-text body without quoted history.",
			Schema: schema(`"threadId":{"type":"string"}`, "threadId")},
		{Name: "digest", Run: t.digest, Description: "Summarize new mail: each message has headers, snippet and the first 500 characters of its body. With sinceLast the digest covers mail that arrived since the previous sinceLast digest (the first one covers the last day); the position advances only on those calls. Use it for inbox briefings; use gmail_search to look for something specific.",
			Schema: schema(`"query":{"type":"string","description":"Gmail search query to filter the digest. Default: the last day"},"sinceLast":{"type":"boolean","description":"Only mail since the previous sinceLast digest"},"max":{"type":"integer","minimum":1,"maximum":100,"description":"Default 50"}`)},
		{Name: "list_labels", Run: t.listLabels, Description: "List the mailbox's labels with id, name and type (system or user). Use ids or names with gmail_modify.",
			Schema: schema(``)},
		{Name: "get_attachment", Run: t.getAttachment, Description: "Download one attachment into the task workspace and return its path, which file_read and shell_exec can open. Get messageId and attachmentId from gmail_get_message.",
			Schema: schema(`"messageId":{"type":"string"},"attachmentId":{"type":"string"},"filename":{"type":"string","description":"Name to save as; defaults to the attachment's own name"}`, "messageId", "attachmentId")},
		{Name: "create_draft", Run: t.createDraft, Description: "Save a draft in Gmail without sending it. To reply, pass replyToMessageId: the draft joins that thread with the right headers and a Re: subject, and to defaults to the sender (replyAll adds the other recipients). Attachments are workspace file paths.",
			Schema: composeSchema},
		{Name: "send", Run: t.send, Approval: true, NoAlways: true, Description: "Send an email now. The user approves every send. Same input as gmail_create_draft: to reply, pass replyToMessageId; attachments are workspace file paths.",
			Schema: composeSchema},
		{Name: "send_draft", Run: t.sendDraft, Approval: true, NoAlways: true, Description: "Send an existing Gmail draft. The user approves every send.",
			Schema: schema(`"draftId":{"type":"string"}`, "draftId")},
		{Name: "forward", Run: t.forward, Approval: true, NoAlways: true, Description: "Forward a message, with its original attachments, to new recipients. The user approves every forward.",
			Schema: schema(`"messageId":{"type":"string"},"to":`+addrList+`,"cc":`+addrList+`,"note":{"type":"string","description":"Text placed above the forwarded message"}`, "messageId", "to")},
		{Name: "modify", Run: t.modify, Description: "Change labels on messages: add or remove labels (names or ids), mark read or unread, archive (remove from inbox), star or unstar. Up to 1000 ids.",
			Schema: schema(`"ids":{"type":"array","items":{"type":"string"},"minItems":1,"maxItems":1000},"addLabels":{"type":"array","items":{"type":"string"}},"removeLabels":{"type":"array","items":{"type":"string"}},"markRead":{"type":"boolean","description":"true marks read, false marks unread"},"archive":{"type":"boolean","description":"true removes the INBOX label"},"star":{"type":"boolean","description":"true stars, false unstars"}`, "ids")},
		{Name: "trash", Run: t.trash, Approval: true, Description: "Move messages to Trash (recoverable for 30 days). The user approves the call. Up to 100 ids.",
			Schema: schema(`"ids":{"type":"array","items":{"type":"string"},"minItems":1,"maxItems":100}`, "ids")},
	}
}

const addrList = `{"type":"array","items":{"type":"string"},"description":"Email addresses, optionally Name <addr>"}`

var composeSchema = schema(`"to":` + addrList + `,"cc":` + addrList + `,"bcc":` + addrList + `,"subject":{"type":"string"},"body":{"type":"string","description":"Plain-text body"},"replyToMessageId":{"type":"string","description":"Message to reply to; keeps the thread, sets the reply headers and the Re: subject"},"replyAll":{"type":"boolean","description":"With replyToMessageId, also address the original To and Cc"},"attachments":{"type":"array","items":{"type":"string"},"description":"Workspace file paths to attach"}`)

// schema builds an object JSON Schema from its properties and required names.
func schema(props string, required ...string) json.RawMessage {
	req := ""
	if len(required) > 0 {
		b, _ := json.Marshal(required)
		req = `,"required":` + string(b)
	}
	return json.RawMessage(`{"type":"object","properties":{` + props + `}` + req + `,"additionalProperties":false}`)
}

func reply(v any) (string, error) {
	b, err := json.Marshal(v)
	return string(b), err
}

func clamp(n, def, max int) int {
	if n <= 0 {
		return def
	}
	return min(n, max)
}

func parse(input json.RawMessage, v any) error {
	if len(input) == 0 {
		input = json.RawMessage(`{}`)
	}
	if err := json.Unmarshal(input, v); err != nil {
		return fmt.Errorf("invalid input: %w", err)
	}
	return nil
}

// summary is the header view of a message.
type summary struct {
	ID       string   `json:"id"`
	ThreadID string   `json:"threadId"`
	From     string   `json:"from"`
	To       string   `json:"to"`
	Subject  string   `json:"subject"`
	Date     string   `json:"date"`
	Snippet  string   `json:"snippet"`
	Unread   bool     `json:"unread"`
	Labels   []string `json:"labels"`
	Preview  string   `json:"preview,omitempty"`
	Account  string   `json:"account,omitempty"`
}

func summarize(m *Message) summary {
	return summary{m.ID, m.ThreadID, m.Payload.header("From"), m.Payload.header("To"), m.Payload.header("Subject"),
		m.Payload.header("Date"), m.Snippet, m.has("UNREAD"), m.LabelIDs, "", ""}
}

func (t *toolset) list(ctx context.Context, q string, n int, pageToken string) (ids []string, next string, err error) {
	v := url.Values{"maxResults": {fmt.Sprint(n)}}
	if q != "" {
		v.Set("q", q)
	}
	if pageToken != "" {
		v.Set("pageToken", pageToken)
	}
	var out struct {
		Messages      []struct{ ID string }
		NextPageToken string
	}
	if err := t.c.call(ctx, http.MethodGet, "messages", v, nil, &out); err != nil {
		return nil, "", err
	}
	for _, m := range out.Messages {
		ids = append(ids, m.ID)
	}
	return ids, out.NextPageToken, nil
}

// fetch gets messages concurrently, keeping ids' order. A message deleted in the meantime is skipped.
func (t *toolset) fetch(ctx context.Context, ids []string, format string) ([]*Message, error) {
	got := make([]*Message, len(ids))
	err := each(len(ids), fetchParallel, func(i int) error {
		m, err := t.c.getMessage(ctx, ids[i], format, "From", "To", "Subject", "Date")
		var ae *APIError
		if errors.As(err, &ae) && ae.Status == http.StatusNotFound {
			return nil
		}
		got[i] = m
		return err
	})
	if err != nil {
		return nil, err
	}
	out := got[:0]
	for _, m := range got {
		if m != nil {
			out = append(out, m)
		}
	}
	return out, nil
}

func (t *toolset) search(ctx context.Context, _ string, in json.RawMessage) (string, error) {
	var a struct {
		Query      string
		MaxResults int
		PageToken  string
	}
	if err := parse(in, &a); err != nil {
		return "", err
	}
	ids, next, err := t.list(ctx, a.Query, clamp(a.MaxResults, 20, 100), a.PageToken)
	if err != nil {
		return "", err
	}
	msgs, err := t.fetch(ctx, ids, "metadata")
	if err != nil {
		return "", err
	}
	out := make([]summary, len(msgs))
	for i, m := range msgs {
		out[i] = summarize(m)
		out[i].Account = t.email
	}
	return reply(map[string]any{"messages": out, "nextPageToken": next})
}

type fullMessage struct {
	summary
	Cc          string       `json:"cc,omitempty"`
	Attachments []Attachment `json:"attachments"`
	Body        string       `json:"body"`
}

func render(m *Message, cap int) fullMessage {
	body, atts := extract(&m.Payload)
	if atts == nil {
		atts = []Attachment{}
	}
	s := summarize(m)
	return fullMessage{s, m.Payload.header("Cc"), atts, clip(trimQuoted(body), cap)}
}

func (t *toolset) getMessage(ctx context.Context, _ string, in json.RawMessage) (string, error) {
	var a struct{ ID string }
	if err := parse(in, &a); err != nil || a.ID == "" {
		return "", errors.New("id is required")
	}
	m, err := t.c.getMessage(ctx, a.ID, "full")
	if err != nil {
		return "", err
	}
	return reply(render(m, messageCap))
}

func (t *toolset) getThread(ctx context.Context, _ string, in json.RawMessage) (string, error) {
	var a struct{ ThreadID string }
	if err := parse(in, &a); err != nil || a.ThreadID == "" {
		return "", errors.New("threadId is required")
	}
	var th struct{ Messages []Message }
	if err := t.c.call(ctx, http.MethodGet, "threads/"+url.PathEscape(a.ThreadID), url.Values{"format": {"full"}}, nil, &th); err != nil {
		return "", err
	}
	msgs := make([]fullMessage, len(th.Messages))
	size := 0
	for i := range th.Messages {
		msgs[i] = render(&th.Messages[i], threadMsgCap)
		size += len(msgs[i].Body)
	}
	omitted := 0
	for size > threadCap && len(msgs) > 1 {
		size -= len(msgs[0].Body)
		msgs, omitted = msgs[1:], omitted+1
	}
	out := map[string]any{"threadId": a.ThreadID, "messages": msgs}
	if omitted > 0 {
		out["omittedOlderMessages"] = omitted
	}
	return reply(out)
}

func (t *toolset) listLabels(ctx context.Context, _ string, _ json.RawMessage) (string, error) {
	labels, err := t.labels(ctx)
	if err != nil {
		return "", err
	}
	return reply(map[string]any{"labels": labels})
}

type label struct {
	ID   string `json:"id"`
	Name string `json:"name"`
	Type string `json:"type"`
}

func (t *toolset) labels(ctx context.Context) ([]label, error) {
	var out struct{ Labels []label }
	if err := t.c.call(ctx, http.MethodGet, "labels", nil, nil, &out); err != nil {
		return nil, err
	}
	return out.Labels, nil
}

func (t *toolset) getAttachment(ctx context.Context, ws string, in json.RawMessage) (string, error) {
	var a struct{ MessageID, AttachmentID, Filename string }
	if err := parse(in, &a); err != nil || a.MessageID == "" || a.AttachmentID == "" {
		return "", errors.New("messageId and attachmentId are required")
	}
	if ws == "" {
		return "", errors.New("no workspace to save into")
	}
	name := a.Filename
	if name == "" {
		m, err := t.c.getMessage(ctx, a.MessageID, "full")
		if err != nil {
			return "", err
		}
		_, atts := extract(&m.Payload)
		for _, at := range atts {
			if at.ID == a.AttachmentID {
				name = at.Filename
			}
		}
	}
	data, err := t.c.attachment(ctx, a.MessageID, a.AttachmentID)
	if err != nil {
		return "", err
	}
	// The message id prefix keeps two messages' "invoice.pdf" apart and makes a repeat call write the same file.
	rel := filepath.Join("attachments", safePrefix(a.MessageID)+"-"+safeName(name))
	full, err := storage.Resolve(ws, rel)
	if err != nil {
		return "", err
	}
	if err := os.MkdirAll(filepath.Dir(full), 0o755); err != nil {
		return "", err
	}
	if err := os.WriteFile(full, data, 0o644); err != nil {
		return "", err
	}
	return reply(map[string]any{"path": rel, "size": len(data)})
}

func safePrefix(id string) string {
	return safeName(id[:min(len(id), 8)])
}

// safeName reduces a remote filename to a single path element.
func safeName(name string) string {
	name = strings.Map(func(r rune) rune {
		if unicode.IsControl(r) || r == '/' || r == '\\' {
			return '_'
		}
		return r
	}, name)
	name = strings.TrimLeft(name, ".")
	if name == "" {
		return "attachment"
	}
	return name
}

// composeInput is the input of create_draft and send.
type composeInput struct {
	To, Cc, Bcc      []string
	Subject, Body    string
	ReplyToMessageID string
	ReplyAll         bool
	Attachments      []string
}

// prepare builds the base64url message and its thread id.
func (t *toolset) prepare(ctx context.Context, ws string, a composeInput) (raw, threadID string, err error) {
	d := Draft{To: a.To, Cc: a.Cc, Bcc: a.Bcc, Subject: a.Subject, Body: a.Body}
	if a.ReplyToMessageID != "" {
		orig, err := t.c.getMessage(ctx, a.ReplyToMessageID, "metadata", "Message-ID", "References", "Subject", "From", "To", "Cc", "Reply-To")
		if err != nil {
			return "", "", fmt.Errorf("message to reply to: %w", err)
		}
		threadID = orig.ThreadID
		id := orig.Payload.header("Message-ID")
		d.InReplyTo, d.Refs = id, strings.TrimSpace(orig.Payload.header("References")+" "+id)
		if d.Subject == "" {
			d.Subject = orig.Payload.header("Subject")
		}
		if !strings.HasPrefix(strings.ToLower(strings.TrimSpace(d.Subject)), "re:") {
			d.Subject = "Re: " + d.Subject
		}
		if len(d.To) == 0 {
			to := orig.Payload.header("Reply-To")
			if to == "" {
				to = orig.Payload.header("From")
			}
			d.To = addresses(to)
		}
		if a.ReplyAll {
			me, err := t.c.Profile(ctx)
			if err != nil {
				return "", "", err
			}
			d.To, d.Cc = replyAllRecipients(d.To, d.Cc, addresses(orig.Payload.header("To")+","+orig.Payload.header("Cc")), me.Email)
		}
	}
	if d.Attachments, err = readFiles(ws, a.Attachments); err != nil {
		return "", "", err
	}
	msg, err := BuildMessage(d)
	if err != nil {
		return "", "", err
	}
	return encodeRaw(msg), threadID, nil
}

func encodeRaw(msg []byte) string { return base64URL(msg) }

// replyAllRecipients adds others to cc, leaving out me and anyone already addressed.
func replyAllRecipients(to, cc, others []string, me string) ([]string, []string) {
	seen := map[string]bool{strings.ToLower(me): true}
	for _, l := range [][]string{to, cc} {
		for _, s := range l {
			seen[bare(s)] = true
		}
	}
	for _, s := range others {
		if b := bare(s); !seen[b] {
			seen[b] = true
			cc = append(cc, s)
		}
	}
	return to, cc
}

func bare(s string) string {
	if a, err := mail.ParseAddress(s); err == nil {
		return strings.ToLower(a.Address)
	}
	return strings.ToLower(s)
}

// addresses splits an address header, dropping anything unparsable.
func addresses(header string) []string {
	list, err := mail.ParseAddressList(header)
	if err != nil {
		return nil
	}
	out := make([]string, len(list))
	for i, a := range list {
		out[i] = a.String()
	}
	return out
}

func readFiles(ws string, paths []string) ([]File, error) {
	var files []File
	total := 0
	for _, p := range paths {
		if ws == "" {
			return nil, errors.New("no workspace to read attachments from")
		}
		full, err := storage.Resolve(ws, p)
		if err != nil {
			return nil, fmt.Errorf("attachment %s: path escapes the workspace", p)
		}
		st, err := os.Stat(full)
		if err != nil || !st.Mode().IsRegular() {
			return nil, fmt.Errorf("attachment %s: not a file in the workspace", p)
		}
		if total += int(st.Size()); total > maxAttachment {
			return nil, errors.New("attachments are over the 18 MB limit")
		}
		data, err := os.ReadFile(full)
		if err != nil {
			return nil, err
		}
		files = append(files, File{filepath.Base(full), data})
	}
	return files, nil
}

func (t *toolset) createDraft(ctx context.Context, ws string, in json.RawMessage) (string, error) {
	var a composeInput
	if err := parse(in, &a); err != nil {
		return "", err
	}
	raw, thread, err := t.prepare(ctx, ws, a)
	if err != nil {
		return "", err
	}
	var out struct {
		ID      string
		Message struct{ ID, ThreadID string }
	}
	if err := t.c.call(ctx, http.MethodPost, "drafts", nil, map[string]any{"message": messageBody(raw, thread)}, &out); err != nil {
		return "", err
	}
	return reply(map[string]any{"draftId": out.ID, "messageId": out.Message.ID, "threadId": out.Message.ThreadID})
}

func messageBody(raw, thread string) map[string]string {
	m := map[string]string{"raw": raw}
	if thread != "" {
		m["threadId"] = thread
	}
	return m
}

func (t *toolset) send(ctx context.Context, ws string, in json.RawMessage) (string, error) {
	var a composeInput
	if err := parse(in, &a); err != nil {
		return "", err
	}
	raw, thread, err := t.prepare(ctx, ws, a)
	if err != nil {
		return "", err
	}
	return t.deliver(ctx, messageBody(raw, thread))
}

func (t *toolset) deliver(ctx context.Context, body map[string]string) (string, error) {
	var out struct{ ID, ThreadID string }
	if err := t.c.call(ctx, http.MethodPost, "messages/send", nil, body, &out); err != nil {
		return "", err
	}
	return reply(map[string]any{"sent": true, "messageId": out.ID, "threadId": out.ThreadID})
}

func (t *toolset) sendDraft(ctx context.Context, _ string, in json.RawMessage) (string, error) {
	var a struct{ DraftID string }
	if err := parse(in, &a); err != nil || a.DraftID == "" {
		return "", errors.New("draftId is required")
	}
	var out struct{ ID, ThreadID string }
	if err := t.c.call(ctx, http.MethodPost, "drafts/send", nil, map[string]string{"id": a.DraftID}, &out); err != nil {
		return "", err
	}
	return reply(map[string]any{"sent": true, "messageId": out.ID, "threadId": out.ThreadID})
}

func (t *toolset) forward(ctx context.Context, _ string, in json.RawMessage) (string, error) {
	var a struct {
		MessageID string
		To, Cc    []string
		Note      string
	}
	if err := parse(in, &a); err != nil || a.MessageID == "" {
		return "", errors.New("messageId and to are required")
	}
	orig, err := t.c.getMessage(ctx, a.MessageID, "full")
	if err != nil {
		return "", err
	}
	text, atts := extract(&orig.Payload)
	p := &orig.Payload
	body := fmt.Sprintf("%s\n\n---------- Forwarded message ---------\nFrom: %s\nDate: %s\nSubject: %s\nTo: %s\n\n%s",
		a.Note, p.header("From"), p.header("Date"), p.header("Subject"), p.header("To"), text)
	d := Draft{To: a.To, Cc: a.Cc, Body: strings.TrimLeft(body, "\n"), Subject: "Fwd: " + strings.TrimPrefix(p.header("Subject"), "Fwd: ")}
	total := 0
	for _, at := range atts {
		if total += at.Size; total > maxAttachment {
			return "", errors.New("the original attachments are over the 18 MB limit")
		}
		data, err := t.c.attachment(ctx, a.MessageID, at.ID)
		if err != nil {
			return "", err
		}
		d.Attachments = append(d.Attachments, File{at.Filename, data})
	}
	msg, err := BuildMessage(d)
	if err != nil {
		return "", err
	}
	return t.deliver(ctx, messageBody(encodeRaw(msg), ""))
}

func (t *toolset) modify(ctx context.Context, _ string, in json.RawMessage) (string, error) {
	var a struct {
		IDs                     []string
		AddLabels, RemoveLabels []string
		MarkRead, Archive, Star *bool
	}
	if err := parse(in, &a); err != nil {
		return "", err
	}
	if len(a.IDs) == 0 || len(a.IDs) > 1000 {
		return "", errors.New("ids must hold 1 to 1000 message ids")
	}
	add, remove := a.AddLabels, a.RemoveLabels
	if len(add)+len(remove) > 0 {
		labels, err := t.labels(ctx)
		if err != nil {
			return "", err
		}
		if add, err = resolveLabels(labels, add); err != nil {
			return "", err
		}
		if remove, err = resolveLabels(labels, remove); err != nil {
			return "", err
		}
	}
	flag := func(on *bool, label string, invert bool) {
		if on == nil {
			return
		}
		if *on != invert {
			add = append(add, label)
		} else {
			remove = append(remove, label)
		}
	}
	flag(a.MarkRead, "UNREAD", true)
	if a.Archive != nil && *a.Archive {
		remove = append(remove, "INBOX")
	}
	flag(a.Star, "STARRED", false)
	if len(add)+len(remove) == 0 {
		return "", errors.New("nothing to change: give addLabels, removeLabels, markRead, archive or star")
	}
	body := map[string]any{"ids": a.IDs, "addLabelIds": add, "removeLabelIds": remove}
	if err := t.c.call(ctx, http.MethodPost, "messages/batchModify", nil, body, nil); err != nil {
		return "", err
	}
	return fmt.Sprintf("modified %d messages", len(a.IDs)), nil
}

// resolveLabels maps label names or ids to ids.
func resolveLabels(all []label, names []string) ([]string, error) {
	out := make([]string, 0, len(names))
	for _, n := range names {
		id := ""
		for _, l := range all {
			if l.ID == n {
				id = l.ID
				break
			}
			if strings.EqualFold(l.Name, n) {
				id = l.ID
			}
		}
		if id == "" {
			return nil, fmt.Errorf("unknown label %q; see gmail_list_labels", n)
		}
		out = append(out, id)
	}
	return out, nil
}

func (t *toolset) trash(ctx context.Context, _ string, in json.RawMessage) (string, error) {
	var a struct{ IDs []string }
	if err := parse(in, &a); err != nil {
		return "", err
	}
	if len(a.IDs) == 0 || len(a.IDs) > 100 {
		return "", errors.New("ids must hold 1 to 100 message ids")
	}
	err := each(len(a.IDs), 4, func(i int) error {
		return t.c.call(ctx, http.MethodPost, "messages/"+url.PathEscape(a.IDs[i])+"/trash", nil, nil, nil)
	})
	if err != nil {
		return "", err
	}
	return fmt.Sprintf("moved %d messages to Trash", len(a.IDs)), nil
}

type hit struct{ id, record string }

// sinceHistory lists messages added after history id start, oldest first, with
// the history record each arrived in. expired is true when Gmail no longer
// keeps start (it keeps about a week).
func (t *toolset) sinceHistory(ctx context.Context, start string, limit int) (hits []hit, more, expired bool, err error) {
	seen := map[string]bool{}
	token := ""
	for page := 0; page < 20; page++ {
		v := url.Values{"startHistoryId": {start}, "historyTypes": {"messageAdded"}, "maxResults": {"500"}}
		if token != "" {
			v.Set("pageToken", token)
		}
		var out struct {
			History []struct {
				ID            string
				MessagesAdded []struct{ Message struct{ ID string } }
			}
			NextPageToken string
		}
		if err := t.c.call(ctx, http.MethodGet, "history", v, nil, &out); err != nil {
			var ae *APIError
			if errors.As(err, &ae) && ae.Status == http.StatusNotFound {
				return nil, false, true, nil
			}
			return nil, false, false, err
		}
		for _, h := range out.History {
			for _, m := range h.MessagesAdded {
				if seen[m.Message.ID] {
					continue
				}
				seen[m.Message.ID] = true
				if len(hits) == limit {
					return hits, true, false, nil
				}
				hits = append(hits, hit{m.Message.ID, h.ID})
			}
		}
		if token = out.NextPageToken; token == "" {
			return hits, false, false, nil
		}
	}
	return hits, true, false, nil
}

func (t *toolset) digest(ctx context.Context, _ string, in json.RawMessage) (string, error) {
	var a struct {
		Query     string
		SinceLast bool
		Max       int
	}
	if err := parse(in, &a); err != nil {
		return "", err
	}
	limit := clamp(a.Max, 50, 100)
	prof, err := t.c.Profile(ctx)
	if err != nil {
		return "", err
	}

	var hits []hit
	more, viaHistory := false, false
	cursor := prof.HistoryID
	start := ""
	if a.SinceLast {
		if start, err = t.cur.Get(ctx); err != nil {
			return "", err
		}
	}
	if start != "" {
		var expired bool
		if hits, more, expired, err = t.sinceHistory(ctx, start, limit); err != nil {
			return "", err
		}
		viaHistory = !expired
	}
	if viaHistory {
		if a.Query != "" { // history cannot filter, so keep what the query also matches
			ids, _, err := t.list(ctx, a.Query, 500, "")
			if err != nil {
				return "", err
			}
			match := map[string]bool{}
			for _, id := range ids {
				match[id] = true
			}
			kept := hits[:0]
			for _, h := range hits {
				if match[h.id] {
					kept = append(kept, h)
				}
			}
			hits = kept
		}
		if more && len(hits) > 0 {
			cursor = hits[len(hits)-1].record
		}
	} else {
		q := strings.TrimSpace(a.Query + " newer_than:1d")
		if !a.SinceLast && a.Query != "" {
			q = a.Query
		}
		ids, next, err := t.list(ctx, q, limit, "")
		if err != nil {
			return "", err
		}
		more = next != ""
		for _, id := range ids {
			hits = append(hits, hit{id: id})
		}
	}

	ids := make([]string, len(hits))
	for i, h := range hits {
		ids[i] = h.id
	}
	msgs, err := t.fetch(ctx, ids, "full")
	if err != nil {
		return "", err
	}
	out := make([]summary, 0, len(msgs))
	for _, m := range msgs {
		if viaHistory && (m.has("SENT") || m.has("DRAFT")) {
			continue
		}
		s := summarize(m)
		s.Account = t.email
		body, _ := extract(&m.Payload)
		s.Preview = truncate(strings.Join(strings.Fields(trimQuoted(body)), " "), previewLen)
		out = append(out, s)
	}
	if a.SinceLast {
		if err := t.cur.Set(ctx, cursor); err != nil {
			return "", err
		}
	}
	return reply(map[string]any{"messages": out, "count": len(out), "more": more})
}

func truncate(s string, n int) string {
	r := []rune(s)
	if len(r) <= n {
		return s
	}
	return string(r[:n]) + "…"
}
