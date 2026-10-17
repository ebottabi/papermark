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
	"strings"
)

const accountParallel = 4

// Account is one signed-in mailbox the tools can act on.
type Account struct {
	Email   string
	Primary bool
	Client  *Client
	Cursor  Cursor
}

// AccountAuthError means one account's sign-in stopped working.
type AccountAuthError struct{ Email string }

func (e *AccountAuthError) Error() string {
	return "Gmail account " + e.Email + " needs reconnecting in Connectors"
}

func (e *AccountAuthError) Is(target error) bool { return target == ErrNeedsAuth }

type routing int

const (
	routePrimary routing = iota // the primary when account is omitted
	routeRequire                // account is required when several are connected
	routeFan                    // every account when account is omitted
)

var routes = map[string]routing{
	"search": routeFan, "digest": routeFan,
	"get_message": routeRequire, "get_thread": routeRequire, "get_attachment": routeRequire,
	"modify": routeRequire, "trash": routeRequire, "send_draft": routeRequire,
	"list_labels": routePrimary, "create_draft": routePrimary, "send": routePrimary, "forward": routePrimary,
}

type mailboxes struct {
	accounts []Account
	onAuth   func(email string)
}

// Tools returns one set of Gmail tools over accounts, each tool taking an
// optional account. onAuth is told when an account's sign-in is refused.
func Tools(accounts []Account, onAuth func(email string)) []Tool {
	if len(accounts) == 0 {
		return nil
	}
	m := &mailboxes{accounts, onAuth}
	per := make([][]Tool, len(accounts))
	for i, a := range accounts {
		per[i] = accountTools(a.Client, a.Cursor, a.Email)
	}
	tools := make([]Tool, len(per[0]))
	for i, base := range per[0] {
		i, base := i, base
		mode := routes[base.Name]
		pick := func(email string) (int, error) { return m.pick(email, mode == routeRequire) }
		t := base
		t.Schema = withAccount(base.Schema, mode)
		if mode == routeFan {
			t.Description += " Without account it covers every connected account and each message carries account."
		}
		if base.Approval || base.Name == "create_draft" {
			t.Resolve = func(in json.RawMessage) (json.RawMessage, error) {
				idx, err := pick(accountOf(in))
				if err != nil {
					return nil, err
				}
				return setAccount(in, m.accounts[idx].Email)
			}
		}
		t.Run = func(ctx context.Context, ws string, in json.RawMessage) (string, error) {
			email := accountOf(in)
			if mode == routeFan && email == "" && len(m.accounts) > 1 {
				return m.fan(ctx, per, i, ws, in)
			}
			idx, err := pick(email)
			if err != nil {
				return "", err
			}
			out, err := per[idx][i].Run(ctx, ws, in)
			return out, m.mark(idx, err)
		}
		tools[i] = t
	}
	return tools
}

func accountOf(in json.RawMessage) string {
	var a struct{ Account string }
	json.Unmarshal(in, &a)
	return strings.TrimSpace(a.Account)
}

func setAccount(in json.RawMessage, email string) (json.RawMessage, error) {
	fields := map[string]json.RawMessage{}
	if len(in) > 0 {
		if err := json.Unmarshal(in, &fields); err != nil {
			return nil, fmt.Errorf("invalid input: %w", err)
		}
	}
	fields["account"], _ = json.Marshal(email)
	return json.Marshal(fields)
}

// withAccount adds the optional account property to a tool schema.
func withAccount(sch json.RawMessage, mode routing) json.RawMessage {
	desc := map[routing]string{
		routePrimary: "Mailbox to use (email address). Default: the primary account",
		routeRequire: "Mailbox the ids belong to (email address). Required when more than one account is connected",
		routeFan:     "Limit to one mailbox (email address). Default: every connected account",
	}[mode]
	var doc map[string]any
	json.Unmarshal(sch, &doc)
	props, _ := doc["properties"].(map[string]any)
	props["account"] = map[string]any{"type": "string", "description": desc}
	out, _ := json.Marshal(doc)
	return out
}

func (m *mailboxes) list() string {
	names := make([]string, len(m.accounts))
	for i, a := range m.accounts {
		names[i] = a.Email
		if a.Primary {
			names[i] += " (primary)"
		}
	}
	return strings.Join(names, ", ")
}

// pick finds the account named by email. Without one it takes the only
// account, the primary, or fails when require is set and several exist.
func (m *mailboxes) pick(email string, require bool) (int, error) {
	if email != "" {
		for i, a := range m.accounts {
			if strings.EqualFold(a.Email, email) {
				return i, nil
			}
		}
		return 0, fmt.Errorf("no connected Gmail account %q; connected accounts: %s", email, m.list())
	}
	if require && len(m.accounts) > 1 {
		return 0, fmt.Errorf("account is required because several Gmail accounts are connected: %s", m.list())
	}
	for i, a := range m.accounts {
		if a.Primary {
			return i, nil
		}
	}
	return 0, nil
}

// mark reports a refused sign-in for account idx and names the account in the error.
func (m *mailboxes) mark(idx int, err error) error {
	if errors.Is(err, ErrNeedsAuth) {
		if m.onAuth != nil {
			m.onAuth(m.accounts[idx].Email)
		}
		return &AccountAuthError{m.accounts[idx].Email}
	}
	return err
}

// fan runs tool i on every account at once and merges the message lists.
// An account that fails is reported in errors and does not hide the rest.
func (m *mailboxes) fan(ctx context.Context, per [][]Tool, i int, ws string, in json.RawMessage) (string, error) {
	type result struct {
		Messages      []json.RawMessage `json:"messages"`
		NextPageToken string            `json:"nextPageToken"`
		More          bool              `json:"more"`
	}
	res := make([]result, len(m.accounts))
	errs := make([]error, len(m.accounts))
	each(len(m.accounts), accountParallel, func(k int) error {
		out, err := per[k][i].Run(ctx, ws, in)
		if err == nil {
			err = json.Unmarshal([]byte(out), &res[k])
		}
		errs[k] = m.mark(k, err)
		return nil
	})
	merged := map[string]any{}
	messages := []json.RawMessage{}
	tokens := map[string]string{}
	var failed []map[string]string
	more := false
	for k, a := range m.accounts {
		if errs[k] != nil {
			failed = append(failed, map[string]string{"account": a.Email, "error": errs[k].Error()})
			continue
		}
		messages = append(messages, res[k].Messages...)
		more = more || res[k].More
		if res[k].NextPageToken != "" {
			tokens[a.Email] = res[k].NextPageToken
		}
	}
	if len(failed) == len(m.accounts) {
		return "", errors.New(failed[0]["error"])
	}
	merged["messages"] = messages
	merged["count"] = len(messages)
	if more {
		merged["more"] = true
	}
	if len(tokens) > 0 {
		merged["nextPageTokens"] = tokens
		merged["note"] = "Pass account with pageToken to continue one mailbox."
	}
	if len(failed) > 0 {
		merged["errors"] = failed
	}
	return reply(merged)
}
