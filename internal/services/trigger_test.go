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
	"github.com/jackc/pgx/v5/pgxpool"
)

func emailFields(rule string) ScheduleFields {
	return ScheduleFields{Title: "Mail", Prompt: "handle it", Enabled: true, Trigger: "email", Rule: json.RawMessage(rule)}
}

type ruleEnv struct {
	pool     *pgxpool.Pool
	schedule *ScheduleService
}

func newRuleEnv(t *testing.T) *ruleEnv {
	t.Helper()
	pool, _ := database.NewTestDatabase(t)
	return &ruleEnv{pool: pool, schedule: NewScheduleService(pool)}
}

func (e *ruleEnv) count(t *testing.T, table string) (n int) {
	t.Helper()
	if err := e.pool.QueryRow(context.Background(), `SELECT count(*) FROM `+table+` WHERE deleted_at IS NULL`).Scan(&n); err != nil {
		t.Fatal(err)
	}
	return n
}

func TestRuleValidation(t *testing.T) {
	e := newRuleEnv(t)
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
		{"unknown trigger", ScheduleFields{Title: "x", Prompt: "p", Trigger: "sms", Rule: json.RawMessage(`{}`)}, "trigger must be"},
		{"watch is gone", ScheduleFields{Title: "W", Prompt: "p", Trigger: "watch", Rule: json.RawMessage(`{"kind":"page","url":"https://example.com","condition":"changed"}`)}, "trigger must be time or email"},
	}
	for _, c := range cases {
		_, err := e.schedule.Create(ctx, user, c.f)
		if !errors.Is(err, ErrInvalidSchedule) || !strings.Contains(err.Error(), c.want) {
			t.Errorf("%s: %v", c.name, err)
		}
	}
	if e.count(t, "schedules") != 0 {
		t.Fatal("a refused routine left rows behind")
	}
}

func TestEmailRuleDefaults(t *testing.T) {
	e := newRuleEnv(t)
	sc, err := e.schedule.Create(context.Background(), testUser(t, e.pool), emailFields(`{"smart":"  from my bank about a failed payment "}`))
	if err != nil {
		t.Fatal(err)
	}
	var r EmailRule
	json.Unmarshal(sc.Rule, &r)
	if r.Smart != "from my bank about a failed payment" || sc.Mode != "quality" || sc.MaxRunsPerHour != models.DefaultMaxRunsPerHour ||
		sc.Repeat != "" || sc.TimeOfDay != "" || sc.NextRunAt != nil {
		t.Fatalf("rule=%+v mode=%s max=%d repeat=%q time=%q next=%v", r, sc.Mode, sc.MaxRunsPerHour, sc.Repeat, sc.TimeOfDay, sc.NextRunAt)
	}
}

func TestRoutineLimitsPerUser(t *testing.T) {
	e := newRuleEnv(t)
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
	// editing a routine at the limit does not count it twice
	first, _ := queries.ListEmailRoutines(ctx, e.pool, a)
	if _, err := e.schedule.Update(ctx, a, first[0].ID, emailFields(`{"from":"other@example.com"}`)); err != nil {
		t.Fatalf("editing at the limit: %v", err)
	}
	if err := e.schedule.Delete(ctx, a, first[0].ID); err != nil {
		t.Fatal(err)
	}
	if _, err := e.schedule.Create(ctx, a, emailFields(`{"from":"boss@example.com"}`)); err != nil {
		t.Fatalf("a deleted routine still counts: %v", err)
	}
}

func TestConcurrentCreatesRespectTheLimit(t *testing.T) {
	e := newRuleEnv(t)
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
	e := newRuleEnv(t)
	ctx := context.Background()
	a, b := testUser(t, e.pool), testUser(t, e.pool)
	r, err := e.schedule.Create(ctx, a, emailFields(`{"from":"boss@example.com"}`))
	if err != nil {
		t.Fatal(err)
	}
	if _, err := e.pool.Exec(ctx, `INSERT INTO trigger_events (id, routine_id, user_id, source_key, summary, created_at)
		VALUES ($1, $2, $3, 'm1', 'New mail', now())`, uuid.New(), r.ID, a); err != nil {
		t.Fatal(err)
	}
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
