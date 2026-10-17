/*
 * Copyright (c) 2026. Papermark - All Rights Reserved
 * Unauthorized copying of this file, via any medium is strictly prohibited
 * Proprietary and confidential
 * Written by Ebot Tabi <ebot.tabi@gmail.com>, 17/10/2026
 */
package harness

import (
	"context"
	"encoding/json"
	"strings"
	"sync"
	"testing"
	"time"

	"papermark/internal/cache"
	"papermark/internal/database"
	"papermark/internal/models"
	"papermark/internal/models/queries"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
)

type pushes struct {
	mu    sync.Mutex
	texts []string
}

func (p *pushes) Send(_ context.Context, _, eventType, text string) error {
	p.mu.Lock()
	defer p.mu.Unlock()
	p.texts = append(p.texts, eventType+":"+text)
	return nil
}

func (p *pushes) got() []string {
	p.mu.Lock()
	defer p.mu.Unlock()
	return append([]string(nil), p.texts...)
}

type fakeRoutines struct{ specs []RoutineSpec }

func (f *fakeRoutines) CreateRoutine(_ context.Context, _ uuid.UUID, s RoutineSpec) (*models.Schedule, error) {
	f.specs = append(f.specs, s)
	next := time.Now().Add(time.Hour)
	return &models.Schedule{Title: s.Title, NextRunAt: &next}, nil
}

type fixture struct {
	h      *Harness
	pool   *pgxpool.Pool
	user   uuid.UUID
	notify *pushes
}

func newFixture(t *testing.T) *fixture {
	pool, _ := database.NewTestDatabase(t)
	sub := uuid.NewString()
	u, _, err := queries.UpsertGoogleUser(context.Background(), pool, sub, sub+"@gmail.com", "Test", "")
	if err != nil {
		t.Fatal(err)
	}
	n := &pushes{}
	return &fixture{h: &Harness{pool: pool, bus: cache.NewBus(nil), Notifier: n}, pool: pool, user: u.ID, notify: n}
}

// turn creates a task (of the given schedule when set) and a turn on it.
func (f *fixture) turn(t *testing.T, schedule *uuid.UUID) *turn {
	task := &models.Task{UserID: f.user, Prompt: "p", Status: models.TaskRunning, Mode: "quality", ScheduleID: schedule}
	if err := queries.InsertTask(context.Background(), f.pool, task); err != nil {
		t.Fatal(err)
	}
	return &turn{h: f.h, id: task.ID, userID: f.user, scheduleID: schedule, conn: &ConnectorToolset{}}
}

func (f *fixture) prefs(t *testing.T, p models.Preferences) {
	if err := queries.SetPreferences(context.Background(), f.pool, f.user, &p); err != nil {
		t.Fatal(err)
	}
}

func publish(t *testing.T, tn *turn, importance string) string {
	t.Helper()
	in, _ := json.Marshal(map[string]any{"importance": importance, "title": "T " + importance, "body": "b",
		"sections": []models.Section{{Title: "News", Items: []models.SectionItem{{Text: "x", URL: "https://example.com/a"}}}}})
	out, err := tn.publishUpdate(context.Background(), in)
	if err != nil {
		t.Fatal(err)
	}
	return out.Text
}

func importances(t *testing.T, f *fixture) []string {
	t.Helper()
	all, err := queries.ListUpdates(context.Background(), f.pool, f.user, nil, 50)
	if err != nil {
		t.Fatal(err)
	}
	var got []string
	for _, u := range all {
		got = append(got, u.Importance)
	}
	return got
}

func TestPushHeld(t *testing.T) {
	p := &models.Preferences{Timezone: "Europe/Berlin", QuietStart: "22:00", QuietEnd: "07:00", PushLimit: 2}
	at := func(s string) time.Time {
		v, err := time.Parse(time.RFC3339, s)
		if err != nil {
			t.Fatal(err)
		}
		return v
	}
	for _, c := range []struct {
		name   string
		now    time.Time
		pushed int
		held   bool
	}{
		{"daytime under the limit", at("2026-06-10T12:00:00+02:00"), 1, false},
		{"limit reached", at("2026-06-10T12:00:00+02:00"), 2, true},
		{"late evening quiet", at("2026-06-10T23:30:00+02:00"), 0, true},
		{"early morning quiet", at("2026-06-10T06:59:00+02:00"), 0, true},
		{"quiet ends at 07:00", at("2026-06-10T07:00:00+02:00"), 0, false},
		{"quiet starts at 22:00", at("2026-06-10T22:00:00+02:00"), 0, true},
		{"quiet is judged in the user's zone not UTC", at("2026-06-10T21:30:00Z"), 0, true},
	} {
		if got := pushHeld(p, c.now, c.pushed) != ""; got != c.held {
			t.Errorf("%s: held %v, want %v", c.name, got, c.held)
		}
	}
	day := &models.Preferences{Timezone: "UTC", QuietStart: "09:00", QuietEnd: "17:00", PushLimit: 5}
	if pushHeld(day, at("2026-06-10T12:00:00Z"), 0) == "" || pushHeld(day, at("2026-06-10T18:00:00Z"), 0) != "" {
		t.Error("a quiet window within one day must hold only inside it")
	}
	if pushHeld(&models.Preferences{Timezone: "UTC", QuietStart: "22:00", QuietEnd: "07:00", PushLimit: 0}, at("2026-06-10T12:00:00Z"), 0) == "" {
		t.Error("a limit of 0 holds every push")
	}
}

func TestDayBoundsFollowTheUsersZoneAcrossDST(t *testing.T) {
	ny, _ := time.LoadLocation("America/New_York")
	from, to := dayBounds(time.Date(2026, 3, 8, 15, 0, 0, 0, time.UTC), ny)
	if !from.Equal(time.Date(2026, 3, 8, 5, 0, 0, 0, time.UTC)) || to.Sub(from) != 23*time.Hour {
		t.Fatalf("spring-forward day %s to %s", from, to)
	}
}

func TestPublishUpdateWritesFeedRowAndEvent(t *testing.T) {
	f := newFixture(t)
	f.prefs(t, models.Preferences{Timezone: "UTC", QuietStart: "00:00", QuietEnd: "00:00", PushLimit: 5})
	tn := f.turn(t, nil)
	if got := publish(t, tn, "feed"); got != "published as feed" {
		t.Fatalf("output %q", got)
	}
	events, _ := queries.ListTaskEvents(context.Background(), f.pool, tn.id, -1)
	var ev struct {
		Type, ID, Importance, Title string
		Sections                    []models.Section
	}
	if len(events) != 1 || events[0].Type != "update" {
		t.Fatalf("events %+v", events)
	}
	json.Unmarshal(events[0].Data, &ev)
	task, _ := queries.GetTask(context.Background(), f.pool, tn.id)
	ups, _ := queries.ListUpdates(context.Background(), f.pool, f.user, nil, 10)
	if len(ups) != 1 || ev.ID != ups[0].ID.String() || ev.Importance != "feed" || len(ev.Sections) != 1 || task.Status != models.TaskRunning {
		t.Fatalf("event %+v update %+v status %s", ev, ups, task.Status)
	}
	if len(f.notify.got()) != 0 {
		t.Fatalf("a feed update must not notify: %v", f.notify.got())
	}
	if _, err := tn.publishUpdate(context.Background(), json.RawMessage(`{"importance":"urgent","title":"x","body":""}`)); err == nil {
		t.Fatal("unknown importance must be rejected")
	}
}

func TestPushDowngradedInQuietHoursAndAtTheLimit(t *testing.T) {
	f := newFixture(t)
	f.prefs(t, models.Preferences{Timezone: "UTC", QuietStart: "00:00", QuietEnd: "00:00", PushLimit: 2})
	tn := f.turn(t, nil)
	publish(t, tn, "push")
	publish(t, tn, "push")
	if got := publish(t, tn, "push"); !strings.Contains(got, "daily push limit") {
		t.Fatalf("third push: %q", got)
	}
	if got := strings.Join(importances(t, f), ","); got != "feed,push,push" {
		t.Fatalf("stored %s", got)
	}
	if got := strings.Join(f.notify.got(), "|"); got != "update:T push|update:T push" {
		t.Fatalf("notified %q", got)
	}

	q := newFixture(t)
	now := time.Now().UTC()
	q.prefs(t, models.Preferences{Timezone: "UTC", QuietStart: now.Add(-time.Hour).Format("15:04"), QuietEnd: now.Add(time.Hour).Format("15:04"), PushLimit: 5})
	if got := publish(t, q.turn(t, nil), "push"); !strings.Contains(got, "quiet hours") {
		t.Fatalf("quiet push: %q", got)
	}
	if got := strings.Join(importances(t, q), ","); got != "feed" || len(q.notify.got()) != 0 {
		t.Fatalf("stored %s notified %v", got, q.notify.got())
	}
}

func TestConfirmStillNotifiesAtThePushLimit(t *testing.T) {
	f := newFixture(t)
	f.prefs(t, models.Preferences{Timezone: "UTC", QuietStart: "00:00", QuietEnd: "00:00", PushLimit: 1})
	tn := f.turn(t, nil)
	publish(t, tn, "push")
	if got := publish(t, tn, "push"); !strings.Contains(got, "daily push limit") {
		t.Fatalf("second push: %q", got)
	}
	err := f.h.TaskTx(context.Background(), tn.id, func(_ pgx.Tx, _ *models.Task, emit Emit) error {
		return emit(ConfirmEvent{"confirm", "gmail_send", json.RawMessage(`{}`), false})
	})
	if err != nil {
		t.Fatal(err)
	}
	if got := strings.Join(f.notify.got(), "|"); got != "update:T push|confirm:gmail_send" {
		t.Fatalf("notified %q", got)
	}
}

func TestMemoryToolsDedupeAndScopeToTheUser(t *testing.T) {
	f := newFixture(t)
	tn := f.turn(t, nil)
	call := func(name, input string) string {
		out, err := tn.assistant(context.Background(), toolCall{Name: name, Input: json.RawMessage(input)})
		if err != nil {
			t.Fatal(err)
		}
		return out.Text
	}
	if got := call("memory_save", `{"text":"Prefers metric units"}`); got != "saved" {
		t.Fatal(got)
	}
	if got := call("memory_save", `{"text":"  prefers   METRIC units. "}`); got != "already remembered" {
		t.Fatal(got)
	}
	if got := call("memory_search", `{"query":"units"}`); got != "- Prefers metric units" {
		t.Fatalf("search %q", got)
	}
	if got := call("memory_search", `{"query":"giraffes"}`); got != "no matching memories" {
		t.Fatalf("search %q", got)
	}
	other := newFixture(t)
	other.h = f.h
	other.user = func() uuid.UUID {
		sub := uuid.NewString()
		u, _, _ := queries.UpsertGoogleUser(context.Background(), f.pool, sub, sub+"@gmail.com", "Other", "")
		return u.ID
	}()
	otn := other.turn(t, nil)
	out, _ := otn.assistant(context.Background(), toolCall{Name: "memory_search", Input: json.RawMessage(`{"query":"metric"}`)})
	if out.Text != "no matching memories" {
		t.Fatalf("another user's search saw %q", out.Text)
	}
	if _, err := tn.assistant(context.Background(), toolCall{Name: "memory_save", Input: json.RawMessage(`{"text":"  ..  "}`)}); err == nil {
		t.Fatal("text without letters or digits must be rejected")
	}
}

func TestRoutineCreateNeedsApprovalThenCreates(t *testing.T) {
	f := newFixture(t)
	routines := &fakeRoutines{}
	f.h.Routines = routines
	tn := f.turn(t, nil)
	if !tn.needsApproval("routine_create") || tn.needsApproval("publish_update") || tn.needsApproval("memory_save") {
		t.Fatal("only routine_create among the assistant tools needs approval")
	}
	if !tn.conn.NoAlways["routine_create"] == false {
		t.Fatal("routine_create must offer always-allow")
	}
	c := toolCall{ID: "call_a", Name: "routine_create", Input: json.RawMessage(`{"title":"Standup","prompt":"summarise","repeat":"weekdays","time":"09:00"}`)}
	if out, _ := tn.answered(context.Background(), c, "confirm", "deny"); !out.IsError || len(routines.specs) != 0 {
		t.Fatalf("a denied routine must not be created: %+v %v", out, routines.specs)
	}
	out, _ := tn.answered(context.Background(), c, "confirm", "allow")
	if out.IsError || len(routines.specs) != 1 || routines.specs[0].Repeat != "weekdays" || !strings.Contains(out.Text, "Standup") {
		t.Fatalf("approved: %+v %v", out, routines.specs)
	}
}

func TestRoutineRunsAreStrictAndToldToPublishOnce(t *testing.T) {
	g := routineGuidance(&models.Preferences{Timezone: "Asia/Tokyo", NewsTopics: []string{"chess", "space"}}, time.Date(2026, 6, 10, 20, 0, 0, 0, time.UTC))
	for _, want := range []string{"exactly one publish_update", "Asia/Tokyo", "Thursday 11 June 2026", "chess, space"} {
		if !strings.Contains(g, want) {
			t.Errorf("guidance lacks %q: %s", want, g)
		}
	}
}

func TestThreeFailedRoutineRunsTurnItOffAndTellTheUser(t *testing.T) {
	f := newFixture(t)
	ctx := context.Background()
	sc := &models.Schedule{UserID: f.user, Title: "Standup", Kind: models.ScheduleCustom, Prompt: "p", Mode: "quality", Repeat: "daily", TimeOfDay: "09:00", Enabled: true}
	next := time.Now().Add(time.Hour)
	sc.NextRunAt = &next
	if err := queries.CreateSchedule(ctx, f.pool, sc); err != nil {
		t.Fatal(err)
	}
	end := func(event any) {
		t.Helper()
		tn := f.turn(t, &sc.ID)
		if err := f.h.TaskTx(ctx, tn.id, func(_ pgx.Tx, _ *models.Task, emit Emit) error { return emit(event) }); err != nil {
			t.Fatal(err)
		}
	}
	state := func() *models.Schedule {
		s, err := queries.GetSchedule(ctx, f.pool, f.user, sc.ID)
		if err != nil {
			t.Fatal(err)
		}
		return s
	}
	fail := TextEvent{"error", "boom"}
	end(fail)
	end(fail)
	end(TextEvent{"idle", "done"}) // a success resets the streak
	end(fail)
	end(fail)
	if s := state(); !s.Enabled || s.NextRunAt == nil {
		t.Fatal("two failures after a success must not turn it off")
	}
	if len(importances(t, f)) != 0 {
		t.Fatal("no notice before the limit")
	}
	end(fail)
	if s := state(); s.Enabled || s.NextRunAt != nil {
		t.Fatalf("third consecutive failure left it enabled: %+v", s)
	}
	ups, _ := queries.ListUpdates(ctx, f.pool, f.user, nil, 10)
	if len(ups) != 1 || ups[0].Title != "Standup turned off" || ups[0].ScheduleID == nil || *ups[0].ScheduleID != sc.ID {
		t.Fatalf("notice %+v", ups)
	}
}
