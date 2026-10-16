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
	"log"
	"sync"
	"time"

	"papermark/internal/models"
	"papermark/internal/models/queries"

	"github.com/google/uuid"
)

// WorkerOptions tune a Worker. Zero values take the defaults.
type WorkerOptions struct {
	Concurrency  int           // jobs run at once (default 16)
	Lease        time.Duration // a job whose lease is this old is reclaimed (default 30s)
	PollInterval time.Duration // idle wait between claims (default 1s)
}

// Worker claims jobs and runs them through a Registry.
type Worker struct {
	svc      *Service
	registry *Registry
	opts     WorkerOptions
	workerID string
}

// NewWorker constructs a worker. registry may be empty (idle-safe).
func NewWorker(svc *Service, registry *Registry, opts WorkerOptions) *Worker {
	if registry == nil {
		registry = NewRegistry()
	}
	if opts.Concurrency <= 0 {
		opts.Concurrency = 16
	}
	if opts.Lease <= 0 {
		opts.Lease = 30 * time.Second
	}
	if opts.PollInterval <= 0 {
		opts.PollInterval = time.Second
	}
	return &Worker{svc: svc, registry: registry, opts: opts, workerID: "worker-" + uuid.NewString()[:8]}
}

// ID returns the lock owner name this worker writes into claimed jobs.
func (w *Worker) ID() string { return w.workerID }

// Run blocks until ctx is cancelled and every running job has returned.
// Jobs interrupted by shutdown are released back to pending.
func (w *Worker) Run(ctx context.Context) error {
	log.Printf("job worker: started id=%s concurrency=%d", w.workerID, w.opts.Concurrency)
	var wg sync.WaitGroup
	slots := make(chan struct{}, w.opts.Concurrency)
	defer func() {
		wg.Wait()
		log.Printf("job worker: stopped id=%s", w.workerID)
	}()
	for {
		if ctx.Err() != nil {
			return ctx.Err()
		}
		free := cap(slots) - len(slots)
		var claimed []*models.Job
		if free > 0 {
			now := time.Now().UTC()
			var err error
			claimed, err = queries.ClaimJobs(ctx, w.svc.pool, w.workerID, free, now, now.Add(-w.opts.Lease))
			if err != nil && ctx.Err() == nil {
				log.Printf("job worker: claim failed: %v", err)
			}
		}
		for _, job := range claimed {
			slots <- struct{}{}
			wg.Add(1)
			go func() {
				defer func() { <-slots; wg.Done() }()
				w.run(ctx, job)
			}()
		}
		if len(claimed) == free && free > 0 {
			continue
		}
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-w.svc.wake:
		case <-time.After(w.opts.PollInterval):
		}
	}
}

func (w *Worker) run(parent context.Context, job *models.Job) {
	ctx, cancel := context.WithCancelCause(parent)
	defer cancel(nil)
	lost := errors.New("job lease lost")
	go func() {
		t := time.NewTicker(w.opts.Lease / 3)
		defer t.Stop()
		for {
			select {
			case <-ctx.Done():
				return
			case <-t.C:
				ok, err := queries.HeartbeatJob(ctx, w.svc.pool, job.ID, w.workerID, time.Now().UTC())
				if err == nil && !ok {
					cancel(lost)
					return
				}
			}
		}
	}()

	err := w.registry.Handle(ctx, job)

	// The parent may be cancelled already; bookkeeping still has to land.
	bctx, bcancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer bcancel()
	switch {
	case errors.Is(context.Cause(ctx), lost):
		log.Printf("job worker: lease lost type=%s id=%s", job.Type, job.ID)
	case parent.Err() != nil:
		if rerr := queries.ReleaseJob(bctx, w.svc.pool, job.ID, w.workerID); rerr != nil {
			log.Printf("job worker: release failed id=%s: %v", job.ID, rerr)
		}
	case err != nil:
		log.Printf("job worker: handle failed type=%s id=%s attempt=%d: %v", job.Type, job.ID, job.Attempts, err)
		next := time.Now().UTC().Add(Backoff(job.Attempts))
		if ferr := queries.FailJob(bctx, w.svc.pool, job.ID, w.workerID, next, err.Error()); ferr != nil {
			log.Printf("job worker: fail bookkeeping failed id=%s: %v", job.ID, ferr)
		}
	default:
		if cerr := queries.CompleteJob(bctx, w.svc.pool, job.ID, w.workerID); cerr != nil {
			log.Printf("job worker: complete failed id=%s: %v", job.ID, cerr)
		}
		w.svc.Notify() // a rerun may be pending now
	}
}
