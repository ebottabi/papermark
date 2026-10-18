/*
 * Copyright (c) 2026. Papermark - All Rights Reserved
 * Unauthorized copying of this file, via any medium is strictly prohibited
 * Proprietary and confidential
 * Written by Ebot Tabi <ebot.tabi@gmail.com>, 17/10/2026
 */
package services

import (
	"context"
	"encoding/json"
	"errors"
	"net/url"
	"slices"
	"strings"
	"testing"

	"papermark/internal/config"
	"papermark/internal/models"
	"papermark/internal/models/queries"

	"github.com/google/uuid"
)

var calendarScopes = []string{"https://www.googleapis.com/auth/calendar.events", "https://www.googleapis.com/auth/calendar.calendarlist.readonly"}

// googleService serves both Google connectors from one fake, sharing one OAuth client as production does.
func googleService(t *testing.T, g *google) *ConnectorService {
	entry := func(kind string, scopes []string) config.ConnectorConfig {
		return config.ConnectorConfig{Kind: kind, URL: g.srv.URL, AuthURL: "https://accounts.google.com/o/oauth2/v2/auth", TokenURL: g.srv.URL + "/token",
			RevokeURL: g.srv.URL + "/revoke", ClientID: "cid", ClientSecret: "csecret", Scopes: scopes,
			AuthParams: map[string]string{"access_type": "offline", "prompt": "consent"}}
	}
	return NewConnectorService(testPool, "https://keen.example", map[string]config.ConnectorConfig{
		"gmail":    entry("gmail", []string{"https://www.googleapis.com/auth/gmail.modify"}),
		"calendar": entry("calendar", calendarScopes),
	}, nil)
}

func signInTo(t *testing.T, s *ConnectorService, user uuid.UUID, name, code string) (*models.Connector, string) {
	t.Helper()
	authURL, err := s.Authorize(context.Background(), user, name)
	if err != nil {
		t.Fatal(err)
	}
	return finish(t, s, authURL, code)
}

func TestCalendarConnectAccountsAndPrimary(t *testing.T) {
	g := newGoogle(t)
	s := googleService(t, g)
	ctx := context.Background()
	user := testUser(t, testPool)

	authURL, err := s.Authorize(ctx, user, "calendar")
	if err != nil {
		t.Fatal(err)
	}
	u, _ := url.Parse(authURL)
	if q := u.Query(); q.Get("scope") != strings.Join(calendarScopes, " ") || q.Get("access_type") != "offline" || q.Get("client_id") != "cid" {
		t.Fatalf("authorize url: %s", authURL)
	}
	c, email := finish(t, s, authURL, "code:a@example.com")
	if email != "a@example.com" || c.Kind != models.ConnectorCalendar {
		t.Fatalf("callback: %q %+v", email, c)
	}
	signInTo(t, s, user, "calendar", "code:b@example.com")
	signInTo(t, s, user, "calendar", "code:b@example.com") // the same address reconnects
	accts := gmailAccountsOf(t, c)
	if len(accts) != 2 || !accts[0].Primary || accts[1].Primary || accts[0].HistoryID != "" {
		t.Fatalf("accounts: %+v", accts)
	}

	list, _ := s.List(ctx, user)
	var cal *models.Connector
	for _, l := range list {
		if l.Name == "calendar" {
			cal = l
		}
	}
	if cal == nil || cal.Status != models.ConnectorConnected || *cal.Account != "a@example.com" || len(cal.Accounts) != 2 || cal.Tools != 8 {
		t.Fatalf("list: %+v", cal)
	}

	if err := s.SetPrimary(ctx, user, "calendar", accts[1].ID); err != nil {
		t.Fatal(err)
	}
	if got, _ := queries.GetConnector(ctx, testPool, c.ID); *got.Account != "b@example.com" {
		t.Fatalf("primary: %v", *got.Account)
	}
	// A calendar account is not reachable under gmail's routes, and the reverse.
	if err := s.SetPrimary(ctx, user, "gmail", accts[0].ID); !IsNotFound(err) {
		t.Fatalf("kind mismatch: %v", err)
	}
	if _, err := s.AuthorizeAccount(ctx, user, "gmail", accts[0].ID); !IsNotFound(err) {
		t.Fatalf("kind mismatch: %v", err)
	}
	if err := s.DeleteAccount(ctx, user, "notes", accts[0].ID); !IsNotFound(err) {
		t.Fatalf("kind mismatch: %v", err)
	}

	if err := s.DeleteAccount(ctx, user, "calendar", accts[1].ID); err != nil {
		t.Fatal(err)
	}
	if len(g.revoked) != 1 || g.revoked[0] != "rt-b@example.com" {
		t.Fatalf("revoked: %v", g.revoked)
	}
	if rest := gmailAccountsOf(t, c); len(rest) != 1 || !rest[0].Primary || rest[0].Email != "a@example.com" {
		t.Fatalf("after delete: %+v", rest)
	}
	if err := s.Delete(ctx, user, "calendar"); err != nil {
		t.Fatal(err)
	}
	if len(g.revoked) != 2 || g.revoked[1] != "rt-a@example.com" {
		t.Fatalf("revoked on delete: %v", g.revoked)
	}
}

func TestCalendarReconnectAsDifferentAccount(t *testing.T) {
	s := googleService(t, newGoogle(t))
	ctx := context.Background()
	user := testUser(t, testPool)
	c, _ := signInTo(t, s, user, "calendar", "code:a@example.com")
	a := gmailAccountsOf(t, c)[0]
	authURL, err := s.AuthorizeAccount(ctx, user, "calendar", a.ID)
	if err != nil {
		t.Fatal(err)
	}
	q, _ := url.Parse(authURL)
	if _, _, err := s.Callback(ctx, q.Query().Get("state"), "code:other@example.com", ""); !errors.Is(err, ErrDifferentAccount) {
		t.Fatalf("callback: %v", err)
	}
	got := gmailAccountsOf(t, c)
	if len(got) != 1 || got[0].Email != "a@example.com" || got[0].Status != models.ConnectorError || *got[0].Error != ErrDifferentAccount.Error() {
		t.Fatalf("accounts: %+v", got)
	}
	if conn, _ := queries.GetConnector(ctx, testPool, c.ID); conn.Status != models.ConnectorError {
		t.Fatalf("connector status %s", conn.Status)
	}
	// Signing in as the right address again recovers it.
	authURL, _ = s.AuthorizeAccount(ctx, user, "calendar", a.ID)
	finish(t, s, authURL, "code:a@example.com")
	if got := gmailAccountsOf(t, c); got[0].Status != models.ConnectorConnected || got[0].Error != nil {
		t.Fatalf("after reconnect: %+v", got[0])
	}
}

// One address signed in to both connectors is two accounts; removing one
// keeps the Google grant the other still uses.
func TestCalendarAndGmailShareAnAddress(t *testing.T) {
	g := newGoogle(t)
	s := googleService(t, g)
	ctx := context.Background()
	user := testUser(t, testPool)
	gm, _ := signInTo(t, s, user, "gmail", "code:a@example.com")
	cal, _ := signInTo(t, s, user, "calendar", "code:a@example.com")
	if len(gmailAccountsOf(t, gm)) != 1 || len(gmailAccountsOf(t, cal)) != 1 {
		t.Fatal("expected one account per connector")
	}
	if err := s.DeleteAccount(ctx, user, "calendar", gmailAccountsOf(t, cal)[0].ID); err != nil {
		t.Fatal(err)
	}
	if len(g.revoked) != 0 {
		t.Fatalf("revoked a grant gmail still uses: %v", g.revoked)
	}
	if a := gmailAccountsOf(t, gm); len(a) != 1 || a[0].Status != models.ConnectorConnected {
		t.Fatalf("gmail account: %+v", a)
	}
}

// Calendar accounts never reach the mail poller or the Gmail tools.
func TestCalendarAccountsStayOutOfMail(t *testing.T) {
	g := newGoogle(t)
	s := googleService(t, g)
	ctx := context.Background()
	user := testUser(t, testPool)
	signInTo(t, s, user, "calendar", "code:cal@example.com")

	set, err := s.Snapshot(ctx, user)
	if err != nil {
		t.Fatal(err)
	}
	for _, tool := range set.Tools {
		if strings.HasPrefix(tool.Name, "gmail_") {
			t.Fatalf("gmail tool %s with only calendar connected", tool.Name)
		}
	}
	set.Close()

	signInTo(t, s, user, "gmail", "code:mail@example.com")
	all, err := queries.ListUserGmailAccounts(ctx, testPool, user)
	if err != nil || len(all) != 1 || all[0].Email != "mail@example.com" {
		t.Fatalf("mail accounts: %+v %v", all, err)
	}
	sched := NewScheduleService(testPool)
	if _, err := sched.Create(ctx, user, ScheduleFields{Title: "Mail", Prompt: "p", Mode: "quality", Enabled: true, Trigger: "email", Rule: json.RawMessage(`{"from":"boss@example.com"}`)}); err != nil {
		t.Fatal(err)
	}
	if err := syncMailboxPolls(ctx, testPool, user); err != nil {
		t.Fatal(err)
	}
	var polled []string
	rows, _ := testPool.Query(ctx, `SELECT a.email FROM mailbox_polls p JOIN gmail_accounts a ON a.id = p.account_id WHERE p.user_id = $1`, user)
	for rows.Next() {
		var e string
		rows.Scan(&e)
		polled = append(polled, e)
	}
	rows.Close()
	if !slices.Equal(polled, []string{"mail@example.com"}) {
		t.Fatalf("polled mailboxes: %v", polled)
	}

	set, _ = s.Snapshot(ctx, user)
	defer set.Close()
	text, isErr, err := set.Handlers["gmail_list_labels"](ctx, json.RawMessage(`{"account":"cal@example.com"}`))
	if err == nil || !strings.Contains(err.Error(), `no connected Gmail account "cal@example.com"`) || strings.Contains(text, "INBOX") {
		t.Fatalf("gmail tool reached a calendar account: %q %v %v", text, isErr, err)
	}
}

func TestCalendarSnapshotToolsApprovalAndNeedsAuth(t *testing.T) {
	g := newGoogle(t)
	s := googleService(t, g)
	ctx := context.Background()
	user := testUser(t, testPool)
	prefs, _ := queries.GetPreferences(ctx, testPool, user)
	prefs.Timezone = "Europe/Berlin"
	if err := queries.SetPreferences(ctx, testPool, user, prefs); err != nil {
		t.Fatal(err)
	}
	c, _ := signInTo(t, s, user, "calendar", "code:a@example.com")
	signInTo(t, s, user, "calendar", "code:b@example.com")

	set, err := s.Snapshot(ctx, user)
	if err != nil {
		t.Fatal(err)
	}
	defer set.Close()
	var names []string
	for _, tool := range set.Tools {
		names = append(names, tool.Name)
	}
	want := []string{"calendar_list_calendars", "calendar_list_events", "calendar_get_event", "calendar_free_busy",
		"calendar_create_event", "calendar_update_event", "calendar_delete_event", "calendar_respond"}
	slices.Sort(names)
	if !slices.Equal(names, slices.Sorted(slices.Values(want))) {
		t.Fatalf("tools: %v", names)
	}
	for _, n := range want {
		write := strings.Contains(n, "create") || strings.Contains(n, "update") || strings.Contains(n, "delete") || strings.Contains(n, "respond")
		if set.NeedsApproval[n] != write || set.NoAlways[n] || (set.Resolve[n] != nil) != write {
			t.Errorf("%s: approval=%v noAlways=%v resolve=%v", n, set.NeedsApproval[n], set.NoAlways[n], set.Resolve[n] != nil)
		}
	}
	in, err := set.Resolve["calendar_create_event"](json.RawMessage(`{"summary":"S","start":"2026-10-20T09:00","end":"2026-10-20T10:00"}`))
	if err != nil || !strings.Contains(string(in), `"timeZone":"Europe/Berlin"`) || !strings.Contains(string(in), `"account":"a@example.com"`) {
		t.Fatalf("resolve: %s %v", in, err)
	}

	text, isErr, err := set.Handlers["calendar_list_calendars"](ctx, json.RawMessage(`{}`))
	if err != nil || isErr || !strings.Contains(text, "a@example.com") || !strings.Contains(text, "b@example.com") {
		t.Fatalf("list_calendars: %q %v %v", text, isErr, err)
	}
	g.mu.Lock()
	g.bad["at-b@example.com"] = true
	g.mu.Unlock()
	text, isErr, err = set.Handlers["calendar_list_calendars"](ctx, json.RawMessage(`{"account":"b@example.com"}`))
	if err != nil || !isErr || text != "Calendar account b@example.com needs reconnecting in Connectors" {
		t.Fatalf("401: %q %v %v", text, isErr, err)
	}
	status := map[string]string{}
	for _, a := range gmailAccountsOf(t, c) {
		status[a.Email] = a.Status
	}
	if status["a@example.com"] != models.ConnectorConnected || status["b@example.com"] != models.ConnectorNeedsAuth {
		t.Fatalf("statuses: %v", status)
	}
}
