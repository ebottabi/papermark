/*
 * Copyright (c) 2026. Papermark - All Rights Reserved
 * Unauthorized copying of this file, via any medium is strictly prohibited
 * Proprietary and confidential
 * Written by Ebot Tabi <ebot.tabi@gmail.com>, 15/10/2026
 */

package jobs

import (
	"context"
	"errors"
	"sync/atomic"
	"testing"
	"time"

	"papermark/internal/database"
	"papermark/internal/models"
	"papermark/internal/models/queries"
)

func TestBackoff(t *testing.T) {
	cases := map[int]time.Duration{0: 5 * time.Second, 1: 5 * time.Second, 2: 10 * time.Second, 3: 20 * time.Second, 20: time.Hour}
	for n, want := range cases {
		if got := Backoff(n); got != want {
			t.Errorf("Backoff(%d) = %v, want %v", n, got, want)
		}
	}
}

func TestRegistryNoHandler(t *testing.T) {
	err := NewRegistry().Handle(context.Background(), &models.Job{Type: "nope"})
	if err == nil {
		t.Fatal("expected error for unregistered type")
	}
}

type funcHandler struct {
	typ string
	fn  func(ctx context.Context, job *models.Job) error
}

func (h funcHandler) Type() string                                      { return h.typ }
func (h funcHandler) Handle(ctx context.Context, job *models.Job) error { return h.fn(ctx, job) }

func TestEnqueueSameKeyFoldsIntoActiveJob(t *testing.T) {
	pool, _ := database.NewTestDatabase(t)
	ctx := context.Background()
	svc := NewService(pool)
	for i := 0; i < 3; i++ {
		if err := svc.Enqueue(ctx, pool, "t", "k1", nil); err != nil {
			t.Fatal(err)
		}
	}
	jobs, err := queries.ListJobsByKey(ctx, pool, "k1")
	if err != nil {
		t.Fatal(err)
	}
	if len(jobs) != 1 || jobs[0].Status != StatusPending || jobs[0].Rerun {
		t.Fatalf("want one pending job without rerun, got %+v", jobs)
	}

	claimed, err := queries.ClaimJobs(ctx, pool, "w", 10, time.Now().UTC(), time.Now().UTC().Add(-time.Minute))
	if err != nil || len(claimed) != 1 {
		t.Fatalf("claim: %v %d", err, len(claimed))
	}
	if err := svc.Enqueue(ctx, pool, "t", "k1", nil); err != nil {
		t.Fatal(err)
	}
	if err := queries.CompleteJob(ctx, pool, claimed[0].ID, "w"); err != nil {
		t.Fatal(err)
	}
	j, _ := queries.GetJob(ctx, pool, claimed[0].ID)
	if j.Status != StatusPending || j.Rerun || j.Attempts != 0 {
		t.Fatalf("enqueue while running should rerun once: %+v", j)
	}
}

func TestWorkerRunsRetriesAndRespectsKey(t *testing.T) {
	pool, _ := database.NewTestDatabase(t)
	svc := NewService(pool)
	var running, maxRunning, calls atomic.Int32
	reg := NewRegistry()
	reg.Register(funcHandler{"t", func(ctx context.Context, job *models.Job) error {
		n := running.Add(1)
		defer running.Add(-1)
		if n > maxRunning.Load() {
			maxRunning.Store(n)
		}
		time.Sleep(50 * time.Millisecond)
		if calls.Add(1) == 1 {
			return errors.New("first attempt fails")
		}
		return nil
	}})
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	done := make(chan struct{})
	go func() {
		defer close(done)
		_ = NewWorker(svc, reg, WorkerOptions{PollInterval: 20 * time.Millisecond}).Run(ctx)
	}()

	if err := svc.Enqueue(ctx, pool, "t", "same", nil); err != nil {
		t.Fatal(err)
	}
	svc.Notify()
	waitFor(t, func() bool {
		js, _ := queries.ListJobsByKey(ctx, pool, "same")
		return len(js) == 1 && js[0].Status == StatusPending && js[0].Attempts == 1 && js[0].LastError == "first attempt fails"
	})
	// Pull the retry forward instead of waiting out the backoff.
	if _, err := pool.Exec(ctx, `UPDATE jobs SET next_run_at = now() WHERE key = 'same'`); err != nil {
		t.Fatal(err)
	}
	waitFor(t, func() bool {
		js, _ := queries.ListJobsByKey(ctx, pool, "same")
		return len(js) == 1 && js[0].Status == StatusDone
	})
	if maxRunning.Load() != 1 {
		t.Fatalf("jobs with one key ran concurrently: %d", maxRunning.Load())
	}
	cancel()
	<-done
}

func TestCrashedWorkerJobIsReclaimed(t *testing.T) {
	pool, _ := database.NewTestDatabase(t)
	ctx := context.Background()
	svc := NewService(pool)
	if err := svc.Enqueue(ctx, pool, "t", "k", nil); err != nil {
		t.Fatal(err)
	}
	// A worker that claimed the job and then died: it never heartbeats again.
	now := time.Now().UTC()
	if js, err := queries.ClaimJobs(ctx, pool, "dead-worker", 1, now, now.Add(-time.Hour)); err != nil || len(js) != 1 {
		t.Fatalf("claim: %v", err)
	}
	if _, err := pool.Exec(ctx, `UPDATE jobs SET locked_at = now() - interval '1 minute'`); err != nil {
		t.Fatal(err)
	}

	var handled atomic.Int32
	reg := NewRegistry()
	reg.Register(funcHandler{"t", func(context.Context, *models.Job) error { handled.Add(1); return nil }})
	wctx, cancel := context.WithCancel(ctx)
	defer cancel()
	go func() {
		_ = NewWorker(svc, reg, WorkerOptions{Lease: time.Second, PollInterval: 20 * time.Millisecond}).Run(wctx)
	}()
	waitFor(t, func() bool {
		js, _ := queries.ListJobsByKey(ctx, pool, "k")
		return len(js) == 1 && js[0].Status == StatusDone && js[0].Attempts == 2
	})
	if handled.Load() != 1 {
		t.Fatalf("handled %d times", handled.Load())
	}
}

func waitFor(t *testing.T, cond func() bool) {
	t.Helper()
	deadline := time.Now().Add(10 * time.Second)
	for !cond() {
		if time.Now().After(deadline) {
			t.Fatal("condition not met in time")
		}
		time.Sleep(20 * time.Millisecond)
	}
}
