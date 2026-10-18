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
	"strings"
	"testing"

	"papermark/internal/database"
	"papermark/internal/models"
	"papermark/internal/models/queries"

	"github.com/google/uuid"
)

func emailFields(rule string) ScheduleFields {
	return ScheduleFields{Title: "Mail", Prompt: "handle it", Mode: "quality", Enabled: true, Trigger: "email", Rule: json.RawMessage(rule)}
}

func TestRuleValidation(t *testing.T) {
	e := newWatchEnv(t)
	user := testUser(t, e.pool)
	ctx := context.Background()
	cases := []struct {
		name string
		f    ScheduleFields
		want string
	}{
		{"email without rule", emailFields(``), "rule is required"},
		{"email null rule", emailFields(`null`), "rule is required"},
		{"email rule with only account", emailFields(`{"account":"me@example.com"}`), "at least one"},
		{"email rule blank fields", emailFields(`{"from":"  "}`), "at least one"},
		{"email rule not json", emailFields(`[1]`), "not valid"},
		{"unknown trigger", ScheduleFields{Title: "x", Prompt: "p", Mode: "quality", Trigger: "sms", Rule: json.RawMessage(`{}`)}, "trigger must be"},
		{"watch page without url", watchFields(`{"kind":"page","condition":"changed"}`), "needs a url"},
		{"watch search without query", watchFields(`{"kind":"search","condition":"newItems"}`), "needs a query"},
		{"watch bad kind", watchFields(`{"kind":"rss","url":"https://example.com","condition":"changed"}`), "kind must be"},
		{"watch bad condition", watchFields(`{"kind":"page","url":"https://example.com","condition":"gone"}`), "condition must be"},
		{"below without threshold", watchFields(`{"kind":"page","url":"https://example.com","condition":"below"}`), "needs a threshold"},
		{"above without threshold", watchFields(`{"kind":"page","url":"https://example.com","condition":"above"}`), "needs a threshold"},
		{"contains without text", watchFields(`{"kind":"page","url":"https://example.com","condition":"contains"}`), "needs text"},
		{"interval too long", watchFields(`{"kind":"page","url":"https://example.com","condition":"changed","intervalMinutes":1441}`), "intervalMinutes"},
		{"interval negative", watchFields(`{"kind":"page","url":"https://example.com","condition":"changed","intervalMinutes":-1}`), "intervalMinutes"},
		{"loopback url", watchFields(`{"kind":"page","url":"http://127.0.0.1/","condition":"changed"}`), "Keen can only watch public web pages. 127.0.0.1 is a private address."},
		{"localhost url", watchFields(`{"kind":"page","url":"http://localhost:8080/x","condition":"changed"}`), "Keen can only watch public web pages. localhost is a private address."},
		{"metadata url", watchFields(`{"kind":"page","url":"http://169.254.169.254/latest","condition":"changed"}`), "public web pages"},
		{"file url", watchFields(`{"kind":"page","url":"file:///etc/passwd","condition":"changed"}`), "starts with http:// or https://"},
	}
	for _, c := range cases {
		_, err := e.schedule.Create(ctx, user, c.f)
		if !errors.Is(err, ErrInvalidSchedule) || !strings.Contains(err.Error(), c.want) {
			t.Errorf("%s: %v", c.name, err)
		}
	}
	if e.count(t, "schedules") != 0 || e.count(t, "watch_targets") != 0 {
		t.Fatal("a refused routine left rows behind")
	}
}

func watchFields(rule string) ScheduleFields {
	return ScheduleFields{Title: "W", Prompt: "p", Mode: "quality", Enabled: true, Trigger: "watch", Rule: json.RawMessage(rule)}
}

func TestRuleDefaultsAndMaxRuns(t *testing.T) {
	e := newWatchEnv(t)
	user := testUser(t, e.pool)
	ctx := context.Background()
	sc, err := e.schedule.Create(ctx, user, watchFields(`{"kind":"page","url":"https://example.com","condition":"changed"}`))
	if err != nil {
		t.Fatal(err)
	}
	var r WatchRule
	json.Unmarshal(sc.Rule, &r)
	if r.IntervalMinutes != 1 || sc.MaxRunsPerHour != 10 || sc.Repeat != "" || sc.TimeOfDay != "" || sc.NextRunAt != nil {
		t.Fatalf("rule=%+v max=%d repeat=%v time=%v next=%v", r, sc.MaxRunsPerHour, sc.Repeat, sc.TimeOfDay, sc.NextRunAt)
	}
	for _, n := range []int{-1, 101} {
		f := watchFields(`{"kind":"page","url":"https://example.com","condition":"changed"}`)
		f.MaxRunsPerHour = n
		if _, err := e.schedule.Create(ctx, user, f); !errors.Is(err, ErrInvalidSchedule) {
			t.Errorf("maxRunsPerHour %d: %v", n, err)
		}
	}
}

func TestRoutineLimitsPerUser(t *testing.T) {
	e := newWatchEnv(t)
	ctx := context.Background()
	a, b := testUser(t, e.pool), testUser(t, e.pool)
	for i := 0; i < MaxEmailRoutines; i++ {
		if _, err := e.schedule.Create(ctx, a, emailFields(`{"from":"boss@example.com"}`)); err != nil {
			t.Fatalf("email %d: %v", i, err)
		}
	}
	if _, err := e.schedule.Create(ctx, a, emailFields(`{"from":"boss@example.com"}`)); !errors.Is(err, ErrInvalidSchedule) || !strings.Contains(err.Error(), "at most 10 email") {
		t.Fatalf("11th email routine: %v", err)
	}
	if _, err := e.schedule.Create(ctx, b, emailFields(`{"from":"boss@example.com"}`)); err != nil {
		t.Fatalf("another user is limited by the first: %v", err)
	}
	var last *models.Schedule
	for i := 0; i < MaxWatchRoutines; i++ {
		rule := `{"kind":"search","query":"topic ` + uuid.NewString() + `","condition":"newItems"}`
		sc, err := e.schedule.Create(ctx, a, watchFields(rule))
		if err != nil {
			t.Fatalf("watch %d: %v", i, err)
		}
		last = sc
	}
	if _, err := e.schedule.Create(ctx, a, watchFields(`{"kind":"search","query":"one more","condition":"newItems"}`)); !errors.Is(err, ErrInvalidSchedule) {
		t.Fatalf("21st watch routine: %v", err)
	}
	if err := e.schedule.Delete(ctx, a, last.ID); err != nil {
		t.Fatal(err)
	}
	if _, err := e.schedule.Create(ctx, a, watchFields(`{"kind":"search","query":"one more","condition":"newItems"}`)); err != nil {
		t.Fatalf("a deleted routine still counts: %v", err)
	}
	// editing a routine at the limit does not count it twice
	first, _ := queries.ListEmailRoutines(ctx, e.pool, a)
	if _, err := e.schedule.Update(ctx, a, first[0].ID, emailFields(`{"from":"other@example.com"}`)); err != nil {
		t.Fatalf("editing at the limit: %v", err)
	}
}

func TestConcurrentCreatesRespectTheLimit(t *testing.T) {
	e := newWatchEnv(t)
	ctx := context.Background()
	user := testUser(t, e.pool)
	done := make(chan error, 25)
	for i := 0; i < 25; i++ {
		go func() {
			_, err := e.schedule.Create(ctx, user, emailFields(`{"from":"boss@example.com"}`))
			done <- err
		}()
	}
	ok := 0
	for i := 0; i < 25; i++ {
		if err := <-done; err == nil {
			ok++
		}
	}
	if ok != MaxEmailRoutines {
		t.Fatalf("%d created, want %d", ok, MaxEmailRoutines)
	}
}

func TestTriggersAreOwnedByTheRoutinesUser(t *testing.T) {
	e := newWatchEnv(t)
	ctx := context.Background()
	a, b := testUser(t, e.pool), testUser(t, e.pool)
	r := e.price(t, a, "changed", 0, "")
	e.poll(t)
	e.site.price("€1")
	e.poll(t)
	got, err := e.schedule.Triggers(ctx, a, r.ID, 30)
	if err != nil || len(got) != 1 {
		t.Fatalf("owner: %v %v", got, err)
	}
	if _, err := e.schedule.Triggers(ctx, b, r.ID, 30); !errors.Is(err, queries.ErrNotFound) {
		t.Fatalf("stranger: %v", err)
	}
}

func TestGmailMigrationIsIdempotent(t *testing.T) {
	pool, _ := database.NewTestDatabase(t)
	ctx := context.Background()
	user := testUser(t, pool)
	cid, cred := uuid.New(), uuid.New()
	if _, err := pool.Exec(ctx, `INSERT INTO connectors (id, user_id, name, kind, url, status, account, history_id, created_at, updated_at)
		VALUES ($1, $2, 'gmail', 'gmail', 'https://gmail.googleapis.com', 'connected', 'Me@Example.com', '777', now(), now())`, cid, user); err != nil {
		t.Fatal(err)
	}
	if _, err := pool.Exec(ctx, `INSERT INTO oauth_credentials (id, user_id, connector_id, resource, auth_url, token_url, client_id, created_at, updated_at)
		VALUES ($1, $2, $3, 'r', 'a', 't', 'c', now(), now())`, cred, user, cid); err != nil {
		t.Fatal(err)
	}
	for _, stmt := range models.PostMigrations() {
		if _, err := pool.Exec(ctx, stmt); err != nil {
			t.Fatal(err)
		}
	}
	snapshot := func() (n int, email, status, hist string, primary bool, linked int) {
		pool.QueryRow(ctx, `SELECT count(*) FROM gmail_accounts WHERE user_id = $1`, user).Scan(&n)
		pool.QueryRow(ctx, `SELECT email, status, history_id, is_primary FROM gmail_accounts WHERE user_id = $1`, user).Scan(&email, &status, &hist, &primary)
		pool.QueryRow(ctx, `SELECT count(*) FROM oauth_credentials o JOIN gmail_accounts a ON a.id = o.account_id WHERE o.id = $1`, cred).Scan(&linked)
		return
	}
	n, email, status, hist, primary, linked := snapshot()
	if n != 1 || email != "Me@Example.com" || status != "connected" || hist != "777" || !primary || linked != 1 {
		t.Fatalf("after first run: %d %s %s %s %v %d", n, email, status, hist, primary, linked)
	}
	for _, stmt := range models.PostMigrations() {
		if _, err := pool.Exec(ctx, stmt); err != nil {
			t.Fatal(err)
		}
	}
	if n2, _, _, _, _, l2 := snapshot(); n2 != 1 || l2 != 1 {
		t.Fatalf("second run changed rows: %d %d", n2, l2)
	}
}
