/*
 * Copyright (c) 2026. Papermark - All Rights Reserved
 * Unauthorized copying of this file, via any medium is strictly prohibited
 * Proprietary and confidential
 * Written by Ebot Tabi <ebot.tabi@gmail.com>, 16/10/2026
 */
package services

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"sync"
	"testing"

	"papermark/internal/config"
	"papermark/internal/models"
	"papermark/internal/models/queries"
	"papermark/internal/secretbox"

	"github.com/google/uuid"
)

// google stands in for accounts.google.com, oauth2.googleapis.com and the Gmail API.
type google struct {
	srv      *httptest.Server
	mu       sync.Mutex
	revoked  []string
	apiCode  int             // status of Gmail API calls other than profile
	bad      map[string]bool // access tokens whose API calls answer 401
	labelHit int
}

// emailOf maps the fake's tokens to the address that signs in: "good-code" is
// me@example.com, "code:<email>" is <email>.
func emailOf(token string) string {
	if token == "at-secret" {
		return "me@example.com"
	}
	return strings.TrimPrefix(token, "at-")
}

func newGoogle(t *testing.T) *google {
	g := &google{apiCode: http.StatusOK, bad: map[string]bool{}}
	mux := http.NewServeMux()
	mux.HandleFunc("/token", func(w http.ResponseWriter, r *http.Request) {
		r.ParseForm()
		code := r.Form.Get("code")
		if r.Form.Get("grant_type") != "authorization_code" || (code != "good-code" && !strings.HasPrefix(code, "code:")) || r.Form.Get("code_verifier") == "" || r.Form.Get("resource") != "" {
			http.Error(w, `{"error":"invalid_grant"}`, http.StatusBadRequest)
			return
		}
		at, rt := "at-secret", "rt-secret"
		if email, ok := strings.CutPrefix(code, "code:"); ok {
			at, rt = "at-"+email, "rt-"+email
		}
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(map[string]any{"access_token": at, "refresh_token": rt, "token_type": "Bearer", "expires_in": 3600})
	})
	mux.HandleFunc("/revoke", func(w http.ResponseWriter, r *http.Request) {
		r.ParseForm()
		g.mu.Lock()
		g.revoked = append(g.revoked, r.Form.Get("token"))
		g.mu.Unlock()
	})
	mux.HandleFunc("/gmail/v1/users/me/profile", func(w http.ResponseWriter, r *http.Request) {
		tok, ok := strings.CutPrefix(r.Header.Get("Authorization"), "Bearer ")
		if !ok || !strings.HasPrefix(tok, "at-") {
			http.Error(w, "unauthorized", http.StatusUnauthorized)
			return
		}
		json.NewEncoder(w).Encode(map[string]string{"emailAddress": emailOf(tok), "historyId": "777"})
	})
	mux.HandleFunc("/gmail/v1/users/me/labels", func(w http.ResponseWriter, r *http.Request) {
		g.mu.Lock()
		g.labelHit++
		code := g.apiCode
		if g.bad[strings.TrimPrefix(r.Header.Get("Authorization"), "Bearer ")] {
			code = http.StatusUnauthorized
		}
		g.mu.Unlock()
		if code != http.StatusOK {
			http.Error(w, `{"error":{"code":401,"message":"Invalid Credentials"}}`, code)
			return
		}
		json.NewEncoder(w).Encode(map[string]any{"labels": []map[string]string{{"id": "INBOX", "name": "INBOX", "type": "system"}}})
	})
	mux.HandleFunc("/calendar/v3/users/me/calendarList/primary", func(w http.ResponseWriter, r *http.Request) {
		tok, ok := strings.CutPrefix(r.Header.Get("Authorization"), "Bearer ")
		if !ok || !strings.HasPrefix(tok, "at-") {
			http.Error(w, `{"error":{"code":401,"message":"Invalid Credentials"}}`, http.StatusUnauthorized)
			return
		}
		json.NewEncoder(w).Encode(map[string]any{"id": emailOf(tok), "primary": true, "accessRole": "owner"})
	})
	mux.HandleFunc("/calendar/v3/users/me/calendarList", func(w http.ResponseWriter, r *http.Request) {
		tok := strings.TrimPrefix(r.Header.Get("Authorization"), "Bearer ")
		g.mu.Lock()
		bad := g.bad[tok]
		g.mu.Unlock()
		if bad {
			http.Error(w, `{"error":{"code":401,"message":"Invalid Credentials"}}`, http.StatusUnauthorized)
			return
		}
		json.NewEncoder(w).Encode(map[string]any{"items": []map[string]any{{"id": emailOf(tok), "summary": emailOf(tok), "primary": true, "accessRole": "owner", "selected": true}}})
	})
	g.srv = httptest.NewServer(mux)
	t.Cleanup(g.srv.Close)
	return g
}

func gmailService(t *testing.T, g *google, box *secretbox.Box) *ConnectorService {
	catalog := map[string]config.ConnectorConfig{"gmail": {
		Kind: "gmail", URL: g.srv.URL, AuthURL: "https://accounts.google.com/o/oauth2/v2/auth", TokenURL: g.srv.URL + "/token", RevokeURL: g.srv.URL + "/revoke",
		ClientID: "cid", ClientSecret: "csecret", Scopes: []string{"https://www.googleapis.com/auth/gmail.modify", "https://www.googleapis.com/auth/gmail.compose"},
		AuthParams: map[string]string{"access_type": "offline", "prompt": "consent"},
	}}
	return NewConnectorService(testPool, "https://keen.example", catalog, box)
}

func connectGmail(t *testing.T, s *ConnectorService, user uuid.UUID) *models.Connector {
	t.Helper()
	c, _ := signIn(t, s, user, "good-code")
	return c
}

// signIn adds (or reconnects) the account that code signs in as.
func signIn(t *testing.T, s *ConnectorService, user uuid.UUID, code string) (*models.Connector, string) {
	t.Helper()
	authURL, err := s.Authorize(context.Background(), user, "gmail")
	if err != nil {
		t.Fatal(err)
	}
	return finish(t, s, authURL, code)
}

func finish(t *testing.T, s *ConnectorService, authURL, code string) (*models.Connector, string) {
	t.Helper()
	q, _ := url.Parse(authURL)
	c, email, err := s.Callback(context.Background(), q.Query().Get("state"), code, "")
	if err != nil {
		t.Fatalf("callback: %v", err)
	}
	return c, email
}

func TestGmailAuthorizeURL(t *testing.T) {
	s := gmailService(t, newGoogle(t), nil)
	authURL, err := s.Authorize(context.Background(), testUser(t, testPool), "gmail")
	if err != nil {
		t.Fatal(err)
	}
	u, _ := url.Parse(authURL)
	q := u.Query()
	if u.Host != "accounts.google.com" || u.Path != "/o/oauth2/v2/auth" {
		t.Fatalf("endpoint %s", u)
	}
	if q.Get("client_id") != "cid" || q.Get("access_type") != "offline" || q.Get("prompt") != "consent" ||
		q.Get("code_challenge_method") != "S256" || q.Get("code_challenge") == "" || q.Get("state") == "" ||
		q.Get("redirect_uri") != "https://keen.example/v1/oauth/callback" || q.Has("resource") ||
		q.Get("scope") != "https://www.googleapis.com/auth/gmail.modify https://www.googleapis.com/auth/gmail.compose" {
		t.Fatalf("authorization URL: %s", authURL)
	}
}

func TestGmailConnectStoresAccountAndSealedTokens(t *testing.T) {
	box, _ := secretbox.New(make([]byte, 32))
	g := newGoogle(t)
	s := gmailService(t, g, box)
	user := testUser(t, testPool)
	c := connectGmail(t, s, user)
	if c.Status != models.ConnectorConnected || c.Kind != models.ConnectorGmail || c.Account == nil || *c.Account != "me@example.com" || c.Tools != 12 {
		t.Fatalf("connector: %+v", c)
	}
	got, _ := queries.GetConnector(context.Background(), testPool, c.ID)
	if got.Account == nil || *got.Account != "me@example.com" {
		t.Fatalf("stored connector: %+v", got)
	}
	accts, _ := queries.ListGmailAccounts(context.Background(), testPool, c.ID)
	if len(accts) != 1 || !accts[0].Primary || accts[0].Email != "me@example.com" || accts[0].HistoryID != "777" {
		t.Fatalf("accounts: %+v", accts)
	}
	cred, err := queries.GetAccountOAuthCredential(context.Background(), testPool, accts[0].ID)
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(cred.AccessToken+cred.RefreshToken+cred.ClientSecret, "secret") {
		t.Fatalf("tokens stored in plaintext: %q %q %q", cred.AccessToken, cred.RefreshToken, cred.ClientSecret)
	}
	if err := s.unseal(cred); err != nil || cred.AccessToken != "at-secret" || cred.RefreshToken != "rt-secret" {
		t.Fatalf("unseal: %v %q %q", err, cred.AccessToken, cred.RefreshToken)
	}

	// A row written before encryption was enabled still reads.
	if err := queries.SaveAccountOAuthToken(context.Background(), testPool, accts[0].ID, "at-secret", "rt-secret", "Bearer", cred.Expiry); err != nil {
		t.Fatal(err)
	}
	set, err := s.Snapshot(context.Background(), user)
	if err != nil || len(set.Tools) != 12 {
		t.Fatalf("snapshot with plaintext row: %v tools=%d", err, len(set.Tools))
	}
	set.Close()
}

func TestGmailSnapshotToolsAndNeedsAuthOn401(t *testing.T) {
	g := newGoogle(t)
	s := gmailService(t, g, nil)
	user := testUser(t, testPool)
	c := connectGmail(t, s, user)

	set, err := s.Snapshot(context.Background(), user)
	if err != nil {
		t.Fatal(err)
	}
	defer set.Close()
	for _, name := range []string{"gmail_send", "gmail_send_draft", "gmail_forward"} {
		if !set.NeedsApproval[name] || !set.NoAlways[name] {
			t.Errorf("%s: approval=%v noAlways=%v", name, set.NeedsApproval[name], set.NoAlways[name])
		}
	}
	if !set.NeedsApproval["gmail_trash"] || set.NoAlways["gmail_trash"] || set.NeedsApproval["gmail_search"] || set.NoAlways["gmail_modify"] {
		t.Fatalf("approval maps: %v %v", set.NeedsApproval, set.NoAlways)
	}
	text, isErr, err := set.Handlers["gmail_list_labels"](context.Background(), json.RawMessage(`{}`))
	if err != nil || isErr || !strings.Contains(text, "INBOX") {
		t.Fatalf("list_labels: %q %v %v", text, isErr, err)
	}

	g.mu.Lock()
	g.apiCode = http.StatusUnauthorized
	g.mu.Unlock()
	text, isErr, err = set.Handlers["gmail_list_labels"](context.Background(), json.RawMessage(`{}`))
	if err != nil || !isErr || text != "Gmail account me@example.com needs reconnecting in Connectors" {
		t.Fatalf("401 result: %q %v %v", text, isErr, err)
	}
	if got, _ := queries.GetConnector(context.Background(), testPool, c.ID); got.Status != models.ConnectorNeedsAuth {
		t.Fatalf("status after 401: %s", got.Status)
	}
	again, _ := s.Snapshot(context.Background(), user)
	defer again.Close()
	if len(again.Tools) != 0 {
		t.Fatalf("needs_auth connector still offers %d tools", len(again.Tools))
	}
}

func TestGmailDeleteRevokesToken(t *testing.T) {
	g := newGoogle(t)
	s := gmailService(t, g, nil)
	user := testUser(t, testPool)
	connectGmail(t, s, user)
	if err := s.Delete(context.Background(), user, "gmail"); err != nil {
		t.Fatal(err)
	}
	if len(g.revoked) != 1 || g.revoked[0] != "rt-secret" {
		t.Fatalf("revoked: %v", g.revoked)
	}
	if _, err := queries.GetConnectorByName(context.Background(), testPool, user, "gmail"); !IsNotFound(err) {
		t.Fatalf("connector survives delete: %v", err)
	}
}

func TestConfirmAlwaysRefusedForSendTools(t *testing.T) {
	cfg := config.Load()
	cfg.WorkspaceDir = t.TempDir()
	s := New(cfg, testPool, nil)
	user := testUser(t, testPool)
	for _, tc := range []struct {
		tool     string
		noAlways bool
	}{{"gmail_send", true}, {"gmail_trash", false}} {
		id, err := s.Tasks.Create(context.Background(), NewTask{UserID: user, Prompt: "x"})
		if err != nil {
			t.Fatal(err)
		}
		if _, err := testPool.Exec(context.Background(), `UPDATE tasks SET status = 'waiting' WHERE id = $1`, id); err != nil {
			t.Fatal(err)
		}
		pending := &models.PendingAction{Kind: "confirm", Name: tc.tool, ToolUseID: "u1", NoAlways: tc.noAlways}
		if err := queries.SetTaskRunState(context.Background(), testPool, id, pending, nil); err != nil {
			t.Fatal(err)
		}
		err = s.Tasks.Confirm(context.Background(), user, id, "always")
		var se *StatusError
		if tc.noAlways {
			if !asStatus(err, &se) || se.Status != 400 || se.Message != "this action needs approval every time" {
				t.Fatalf("%s: err = %v", tc.tool, err)
			}
			if err := s.Tasks.Confirm(context.Background(), user, id, "allow"); err != nil {
				t.Fatalf("%s: allow refused: %v", tc.tool, err)
			}
		} else if err != nil {
			t.Fatalf("%s: always refused: %v", tc.tool, err)
		}
	}
}

func asStatus(err error, target **StatusError) bool {
	se, ok := err.(*StatusError)
	*target = se
	return ok
}

func gmailAccountsOf(t *testing.T, c *models.Connector) []*models.GmailAccount {
	t.Helper()
	a, err := queries.ListGmailAccounts(context.Background(), testPool, c.ID)
	if err != nil {
		t.Fatal(err)
	}
	return a
}

func TestGmailAddReconnectAndPromote(t *testing.T) {
	g := newGoogle(t)
	s := gmailService(t, g, nil)
	ctx := context.Background()
	user := testUser(t, testPool)

	c, email := signIn(t, s, user, "code:a@example.com")
	if email != "a@example.com" {
		t.Fatalf("callback email %q", email)
	}
	signIn(t, s, user, "code:b@example.com")
	signIn(t, s, user, "code:c@example.com")
	signIn(t, s, user, "code:b@example.com") // the same address again reconnects
	accts := gmailAccountsOf(t, c)
	if len(accts) != 3 || !accts[0].Primary || accts[1].Primary || accts[2].Primary {
		t.Fatalf("accounts: %+v", accts)
	}

	list, _ := s.List(ctx, user)
	if len(list) != 1 || list[0].Account == nil || *list[0].Account != "a@example.com" || len(list[0].Accounts) != 3 || list[0].Status != models.ConnectorConnected {
		t.Fatalf("list: %+v", list)
	}

	if err := s.SetPrimary(ctx, user, "gmail", accts[2].ID); err != nil {
		t.Fatal(err)
	}
	if err := s.SetPrimary(ctx, testUser(t, testPool), "gmail", accts[1].ID); !IsNotFound(err) {
		t.Fatalf("another user's account: %v", err)
	}
	list, _ = s.List(ctx, user)
	if *list[0].Account != "c@example.com" {
		t.Fatalf("primary after switch: %v", *list[0].Account)
	}

	if err := s.DeleteAccount(ctx, user, "gmail", accts[2].ID); err != nil {
		t.Fatal(err)
	}
	if len(g.revoked) != 1 || g.revoked[0] != "rt-c@example.com" {
		t.Fatalf("revoked: %v", g.revoked)
	}
	rest := gmailAccountsOf(t, c)
	if len(rest) != 2 || !rest[0].Primary || rest[0].Email != "a@example.com" || rest[1].Primary {
		t.Fatalf("after removing the primary: %+v", rest)
	}

	if err := s.DeleteAccount(ctx, user, "gmail", rest[0].ID); err != nil {
		t.Fatal(err)
	}
	if err := s.DeleteAccount(ctx, user, "gmail", rest[1].ID); err != nil {
		t.Fatal(err)
	}
	if _, err := queries.GetConnectorByName(ctx, testPool, user, "gmail"); !IsNotFound(err) {
		t.Fatalf("connector survives its last account: %v", err)
	}
}

func TestGmailReconnectAsDifferentAccountFails(t *testing.T) {
	s := gmailService(t, newGoogle(t), nil)
	user := testUser(t, testPool)
	c, _ := signIn(t, s, user, "code:a@example.com")
	a := gmailAccountsOf(t, c)[0]
	authURL, err := s.AuthorizeAccount(context.Background(), user, "gmail", a.ID)
	if err != nil {
		t.Fatal(err)
	}
	q, _ := url.Parse(authURL)
	if _, _, err := s.Callback(context.Background(), q.Query().Get("state"), "code:other@example.com", ""); !errors.Is(err, ErrDifferentAccount) {
		t.Fatalf("callback: %v", err)
	}
	if got := gmailAccountsOf(t, c); len(got) != 1 || got[0].Email != "a@example.com" || got[0].Status != models.ConnectorError ||
		got[0].Error == nil || *got[0].Error != ErrDifferentAccount.Error() {
		t.Fatalf("accounts: %+v", got)
	}
	if _, err := s.AuthorizeAccount(context.Background(), testUser(t, testPool), "gmail", a.ID); !IsNotFound(err) {
		t.Fatalf("another user's account: %v", err)
	}
}

func TestGmailUnauthorizedMarksOnlyThatAccount(t *testing.T) {
	g := newGoogle(t)
	s := gmailService(t, g, nil)
	ctx := context.Background()
	user := testUser(t, testPool)
	c, _ := signIn(t, s, user, "code:a@example.com")
	signIn(t, s, user, "code:b@example.com")
	g.mu.Lock()
	g.bad["at-b@example.com"] = true
	g.mu.Unlock()

	set, err := s.Snapshot(ctx, user)
	if err != nil {
		t.Fatal(err)
	}
	defer set.Close()
	text, isErr, err := set.Handlers["gmail_list_labels"](ctx, json.RawMessage(`{"account":"b@example.com"}`))
	if err != nil || !isErr || !strings.Contains(text, "b@example.com needs reconnecting") {
		t.Fatalf("b: %q %v %v", text, isErr, err)
	}
	text, isErr, err = set.Handlers["gmail_list_labels"](ctx, json.RawMessage(`{}`))
	if err != nil || isErr || !strings.Contains(text, "INBOX") {
		t.Fatalf("primary a: %q %v %v", text, isErr, err)
	}
	status := map[string]string{}
	for _, a := range gmailAccountsOf(t, c) {
		status[a.Email] = a.Status
	}
	if status["a@example.com"] != models.ConnectorConnected || status["b@example.com"] != models.ConnectorNeedsAuth {
		t.Fatalf("statuses: %v", status)
	}
	if got, _ := queries.GetConnector(ctx, testPool, c.ID); got.Status != models.ConnectorConnected {
		t.Fatalf("connector status %s", got.Status)
	}
}
