/*
 * Copyright (c) 2026. Papermark - All Rights Reserved
 * Unauthorized copying of this file, via any medium is strictly prohibited
 * Proprietary and confidential
 * Written by Ebot Tabi <ebot.tabi@gmail.com>, 15/10/2026
 */
package services

import (
	"context"
	"errors"
	"sync"
	"testing"
	"time"

	"papermark/internal/database"
	"papermark/internal/models/queries"

	"github.com/google/uuid"
)

func TestNextRun(t *testing.T) {
	ny, err := time.LoadLocation("America/New_York")
	if err != nil {
		t.Fatal(err)
	}
	at := func(s string) time.Time {
		v, err := time.Parse(time.RFC3339, s)
		if err != nil {
			t.Fatal(err)
		}
		return v
	}
	wd := func(n int) *int { return &n }
	cases := []struct {
		name, repeat, hhmm string
		weekday            *int
		after, want        string
	}{
		{"daily later today", "daily", "08:00", nil, "2026-06-10T07:59:00-04:00", "2026-06-10T08:00:00-04:00"},
		{"daily exactly at slot moves to tomorrow", "daily", "09:00", nil, "2026-06-10T09:00:00-04:00", "2026-06-11T09:00:00-04:00"},
		{"daily across spring forward keeps 09:00 wall", "daily", "09:00", nil, "2026-03-07T10:00:00-05:00", "2026-03-08T09:00:00-04:00"},
		{"daily across fall back keeps 09:00 wall", "daily", "09:00", nil, "2026-10-31T10:00:00-04:00", "2026-11-01T09:00:00-05:00"},
		{"daily in spring-forward gap", "daily", "02:30", nil, "2026-03-08T00:00:00-05:00", "2026-03-08T01:30:00-05:00"},
		{"daily repeated wall time fires on first occurrence", "daily", "01:30", nil, "2026-11-01T00:00:00-04:00", "2026-11-01T01:30:00-04:00"},
		{"daily repeated wall time does not fire twice", "daily", "01:30", nil, "2026-11-01T01:30:00-04:00", "2026-11-02T01:30:00-05:00"},
		{"hourly later this hour", "hourly", "00:45", nil, "2026-06-10T10:15:00-04:00", "2026-06-10T10:45:00-04:00"},
		{"hourly ignores the hour of time", "hourly", "17:15", nil, "2026-06-10T10:15:00-04:00", "2026-06-10T11:15:00-04:00"},
		{"hourly through the repeated hour", "hourly", "00:30", nil, "2026-11-01T01:30:00-04:00", "2026-11-01T01:30:00-05:00"},
		{"hourly over the skipped hour", "hourly", "00:30", nil, "2026-03-08T01:45:00-05:00", "2026-03-08T03:30:00-04:00"},
		{"weekly Monday from Sunday", "weekly", "09:00", wd(1), "2026-06-14T12:00:00-04:00", "2026-06-15T09:00:00-04:00"},
		{"weekly Monday after the slot", "weekly", "09:00", wd(1), "2026-06-15T10:00:00-04:00", "2026-06-22T09:00:00-04:00"},
		{"weekly Sunday is 7", "weekly", "18:00", wd(7), "2026-06-10T10:00:00-04:00", "2026-06-14T18:00:00-04:00"},
		{"weekly across spring forward", "weekly", "09:00", wd(7), "2026-03-01T09:00:00-05:00", "2026-03-08T09:00:00-04:00"},
	}
	for _, c := range cases {
		got, err := NextRun(c.repeat, c.hhmm, c.weekday, at(c.after), ny)
		if err != nil {
			t.Errorf("%s: %v", c.name, err)
			continue
		}
		if !got.Equal(at(c.want)) {
			t.Errorf("%s: got %s, want %s", c.name, got.Format(time.RFC3339), c.want)
		}
	}

	bad := []struct {
		repeat, hhmm string
		weekday      *int
	}{
		{"daily", "9:00", nil}, {"daily", "24:00", nil}, {"daily", "12:60", nil}, {"daily", "", nil},
		{"monthly", "09:00", nil}, {"weekly", "09:00", nil}, {"weekly", "09:00", wd(0)}, {"weekly", "09:00", wd(8)},
	}
	for _, b := range bad {
		if _, err := NextRun(b.repeat, b.hhmm, b.weekday, time.Now(), ny); !errors.Is(err, ErrInvalidSchedule) {
			t.Errorf("%s %q %v: err %v", b.repeat, b.hhmm, b.weekday, err)
		}
	}
}

type fakeTasks struct {
	mu    sync.Mutex
	calls map[uuid.UUID]int
	fail  error
	delay time.Duration
}

func (f *fakeTasks) CreateTask(_ context.Context, _ queries.DBTX, prompt, mode string, projectID *uuid.UUID, scheduleID uuid.UUID) (string, error) {
	time.Sleep(f.delay)
	f.mu.Lock()
	defer f.mu.Unlock()
	if f.fail != nil {
		return "", f.fail
	}
	if f.calls == nil {
		f.calls = map[uuid.UUID]int{}
	}
	f.calls[scheduleID]++
	return uuid.NewString(), nil
}

func (f *fakeTasks) total() int {
	f.mu.Lock()
	defer f.mu.Unlock()
	n := 0
	for _, c := range f.calls {
		n += c
	}
	return n
}

func newSchedules(t *testing.T) (*ScheduleService, *fakeTasks) {
	pool, _ := database.NewTestDatabase(t)
	s := NewScheduleService(pool)
	f := &fakeTasks{}
	s.SetTaskCreator(f)
	return s, f
}

// backdate makes a schedule overdue by d.
func backdate(t *testing.T, s *ScheduleService, id uuid.UUID, d time.Duration) {
	t.Helper()
	if _, err := s.pool.Exec(context.Background(), "UPDATE schedules SET next_run_at = $2 WHERE id = $1", id, time.Now().Add(-d)); err != nil {
		t.Fatal(err)
	}
}

func TestRunDueRunsAMissedBacklogOnce(t *testing.T) {
	s, f := newSchedules(t)
	ctx := context.Background()
	sc, err := s.Create(ctx, ScheduleFields{Prompt: "news", Repeat: "daily", Time: "09:00", Enabled: true})
	if err != nil {
		t.Fatal(err)
	}
	if n, err := s.RunDue(ctx, time.Now()); n != 0 || err != nil {
		t.Fatalf("not yet due: ran %d, %v", n, err)
	}
	backdate(t, s, sc.ID, 72*time.Hour) // three missed slots

	now := time.Now()
	if n, err := s.RunDue(ctx, now); n != 1 || err != nil {
		t.Fatalf("first tick: ran %d, %v", n, err)
	}
	if n, err := s.RunDue(ctx, now.Add(time.Second)); n != 0 || err != nil {
		t.Fatalf("second tick: ran %d, %v", n, err)
	}
	got, _ := s.Get(ctx, sc.ID)
	want, _ := NextRun("daily", "09:00", nil, now, time.Local)
	if got.NextRunAt == nil || !got.NextRunAt.Equal(want) || got.LastRunAt == nil {
		t.Fatalf("after run: next %v (want %v) last %v", got.NextRunAt, want, got.LastRunAt)
	}
	if f.calls[sc.ID] != 1 {
		t.Fatalf("tasks created: %v", f.calls)
	}
}

func TestRunDueSkipsDisabledAndDeleted(t *testing.T) {
	s, f := newSchedules(t)
	ctx := context.Background()
	off, _ := s.Create(ctx, ScheduleFields{Prompt: "a", Repeat: "hourly", Time: "00:00", Enabled: false})
	gone, _ := s.Create(ctx, ScheduleFields{Prompt: "b", Repeat: "hourly", Time: "00:00", Enabled: true})
	if off.NextRunAt != nil {
		t.Fatalf("disabled schedule has next run %v", off.NextRunAt)
	}
	backdate(t, s, gone.ID, time.Hour)
	if err := s.Delete(ctx, gone.ID); err != nil {
		t.Fatal(err)
	}
	if n, err := s.RunDue(ctx, time.Now()); n != 0 || err != nil || f.total() != 0 {
		t.Fatalf("ran %d, %v, calls %v", n, err, f.calls)
	}
}

func TestRunDueRetriesAfterTaskCreationFails(t *testing.T) {
	s, f := newSchedules(t)
	ctx := context.Background()
	sc, _ := s.Create(ctx, ScheduleFields{Prompt: "a", Repeat: "daily", Time: "09:00", Enabled: true})
	backdate(t, s, sc.ID, time.Minute)
	before, _ := s.Get(ctx, sc.ID)

	f.fail = errors.New("db down")
	if _, err := s.RunDue(ctx, time.Now()); err == nil {
		t.Fatal("expected the creation error")
	}
	after, _ := s.Get(ctx, sc.ID)
	if !after.NextRunAt.Equal(*before.NextRunAt) || after.LastRunAt != nil {
		t.Fatalf("failed run advanced the schedule: %v -> %v", before.NextRunAt, after.NextRunAt)
	}
	f.fail = nil
	if n, err := s.RunDue(ctx, time.Now()); n != 1 || err != nil {
		t.Fatalf("retry: ran %d, %v", n, err)
	}
}

func TestConcurrentSchedulersNeverDoubleRun(t *testing.T) {
	pool, _ := database.NewTestDatabase(t)
	ctx := context.Background()
	f := &fakeTasks{delay: 5 * time.Millisecond}
	var schedulers []*ScheduleService
	for i := 0; i < 4; i++ { // four processes sharing one database
		s := NewScheduleService(pool)
		s.SetTaskCreator(f)
		schedulers = append(schedulers, s)
	}
	const due = 12
	for i := 0; i < due; i++ {
		sc, err := schedulers[0].Create(ctx, ScheduleFields{Prompt: "p", Repeat: "hourly", Time: "00:00", Enabled: true})
		if err != nil {
			t.Fatal(err)
		}
		backdate(t, schedulers[0], sc.ID, 3*time.Hour)
	}

	now := time.Now()
	var wg sync.WaitGroup
	for _, s := range schedulers {
		for j := 0; j < 2; j++ {
			wg.Add(1)
			go func() {
				defer wg.Done()
				if _, err := s.RunDue(ctx, now); err != nil {
					t.Error(err)
				}
			}()
		}
	}
	wg.Wait()
	if len(f.calls) != due || f.total() != due {
		t.Fatalf("want %d schedules run once each, got %v", due, f.calls)
	}
	left, _ := queries.ListSchedules(ctx, pool)
	for _, sc := range left {
		if !sc.NextRunAt.After(now) {
			t.Fatalf("schedule %s still due: %v", sc.ID, sc.NextRunAt)
		}
	}
}

func TestRunDueWithoutTaskCreator(t *testing.T) {
	pool, _ := database.NewTestDatabase(t)
	if _, err := NewScheduleService(pool).RunDue(context.Background(), time.Now()); !errors.Is(err, ErrNoTaskCreator) {
		t.Fatalf("err %v", err)
	}
}
