/*
 * Copyright (c) 2026. Papermark - All Rights Reserved
 * Unauthorized copying of this file, via any medium is strictly prohibited
 * Proprietary and confidential
 * Written by Ebot Tabi <ebot.tabi@gmail.com>, 15/10/2026
 */

// Package jobs is the durable background job queue (outbox pattern) shared by
// cmd/server and cmd/worker. Producers enqueue rows in their own transaction;
// workers claim them with FOR UPDATE SKIP LOCKED, renew a lease while a
// handler runs, and reclaim jobs whose worker died.
package jobs

import (
	"context"
	"encoding/json"
	"time"

	"papermark/internal/models"
	"papermark/internal/models/queries"

	"github.com/jackc/pgx/v5/pgxpool"
)

// Job status values.
const (
	StatusPending = "pending"
	StatusRunning = "running"
	StatusDone    = "done"
	StatusDead    = "dead"
)

// Service enqueues jobs. Notify wakes workers in this process right away;
// workers in other processes pick the job up on their next poll.
type Service struct {
	pool *pgxpool.Pool
	wake chan struct{}
}

// NewService constructs a job Service.
func NewService(pool *pgxpool.Pool) *Service {
	return &Service{pool: pool, wake: make(chan struct{}, 1)}
}

// Enqueue adds a job of type jobType in db (the pool or the caller's
// transaction). A non-empty key keeps at most one active job: see models.Job.
// Call Notify after the transaction commits.
func (s *Service) Enqueue(ctx context.Context, db queries.DBTX, jobType, key string, payload any) error {
	raw, err := json.Marshal(payload)
	if err != nil {
		return err
	}
	if payload == nil {
		raw = []byte("{}")
	}
	return queries.EnqueueJob(ctx, db, &models.Job{Type: jobType, Key: key, Payload: raw})
}

// Notify wakes an in-process worker without waiting for its poll interval.
func (s *Service) Notify() {
	select {
	case s.wake <- struct{}{}:
	default:
	}
}

// Backoff returns the retry delay after attempt n (1-based): 5s doubling, capped at 1h.
func Backoff(n int) time.Duration {
	if n < 1 {
		n = 1
	}
	d := 5 * time.Second
	for i := 1; i < n && d < time.Hour; i++ {
		d *= 2
	}
	if d > time.Hour {
		d = time.Hour
	}
	return d
}
