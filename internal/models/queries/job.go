/*
 * Copyright (c) 2026. Papermark - All Rights Reserved
 * Unauthorized copying of this file, via any medium is strictly prohibited
 * Proprietary and confidential
 * Written by Ebot Tabi <ebot.tabi@gmail.com>, 15/10/2026
 */

package queries

import (
	"context"
	"time"

	"papermark/internal/models"

	"github.com/google/uuid"
)

const jobColumns = `id, type, key, payload, status, rerun, attempts, max_attempts,
	next_run_at, last_error, locked_at, locked_by, created_at, updated_at`

const (
	// Enqueueing an active key is idempotent: a pending row absorbs it, a
	// running row is flagged to run once more when it completes.
	enqueueJob = `
		INSERT INTO jobs (id, type, key, payload, status, rerun, attempts, max_attempts,
			next_run_at, last_error, locked_by, created_at, updated_at)
		VALUES ($1, $2, $3, COALESCE($4::jsonb, '{}'::jsonb), 'pending', false, 0, $5, $6, '', '', $7, $7)
		ON CONFLICT (key) WHERE key <> '' AND status IN ('pending', 'running')
		DO UPDATE SET rerun = jobs.rerun OR jobs.status = 'running', updated_at = EXCLUDED.updated_at`

	// Claims due pending jobs and running jobs whose lease expired (their
	// worker died). Attempts counts claims, so a job that keeps crashing
	// its worker eventually goes dead.
	claimJobs = `
		UPDATE jobs AS j
		SET status = 'running', attempts = j.attempts + 1, locked_at = $1, locked_by = $2, updated_at = $1
		FROM (
			SELECT id FROM jobs
			WHERE (status = 'pending' AND next_run_at <= $1)
			   OR (status = 'running' AND locked_at < $4)
			ORDER BY next_run_at ASC
			FOR UPDATE SKIP LOCKED
			LIMIT $3
		) AS sub
		WHERE j.id = sub.id
		RETURNING j.id, j.type, j.key, j.payload, j.status, j.rerun, j.attempts, j.max_attempts,
			j.next_run_at, j.last_error, j.locked_at, j.locked_by, j.created_at, j.updated_at`

	heartbeatJob = `
		UPDATE jobs SET locked_at = $3
		WHERE id = $1 AND locked_by = $2 AND status = 'running'`

	completeJob = `
		UPDATE jobs
		SET status = CASE WHEN rerun THEN 'pending' ELSE 'done' END,
		    attempts = CASE WHEN rerun THEN 0 ELSE attempts END,
		    rerun = false, next_run_at = $3, last_error = '', locked_at = NULL, locked_by = '', updated_at = $3
		WHERE id = $1 AND locked_by = $2 AND status = 'running'`

	failJob = `
		UPDATE jobs
		SET status = CASE WHEN attempts >= max_attempts THEN 'dead' ELSE 'pending' END,
		    next_run_at = $3, last_error = $4, locked_at = NULL, locked_by = '', updated_at = $5
		WHERE id = $1 AND locked_by = $2 AND status = 'running'`

	releaseJob = `
		UPDATE jobs
		SET status = 'pending', attempts = GREATEST(attempts - 1, 0), next_run_at = $3,
		    locked_at = NULL, locked_by = '', updated_at = $3
		WHERE id = $1 AND locked_by = $2 AND status = 'running'`

	selectJobByID = `SELECT ` + jobColumns + ` FROM jobs WHERE id = $1`

	listJobsByKey = `SELECT ` + jobColumns + ` FROM jobs WHERE key = $1 ORDER BY created_at ASC`
)

func scanJob(row scannable) (*models.Job, error) {
	j := &models.Job{}
	err := row.Scan(
		&j.ID, &j.Type, &j.Key, &j.Payload, &j.Status, &j.Rerun, &j.Attempts, &j.MaxAttempts,
		&j.NextRunAt, &j.LastError, &j.LockedAt, &j.LockedBy, &j.CreatedAt, &j.UpdatedAt,
	)
	if err != nil {
		return nil, notFound(err)
	}
	return j, nil
}

// EnqueueJob inserts a pending job, or folds it into the active job for the same key.
func EnqueueJob(ctx context.Context, db DBTX, j *models.Job) error {
	if j.ID == uuid.Nil {
		j.ID = uuid.New()
	}
	if j.MaxAttempts <= 0 {
		j.MaxAttempts = 10
	}
	if j.Payload == nil {
		j.Payload = []byte("{}")
	}
	now := time.Now().UTC()
	if j.NextRunAt.IsZero() {
		j.NextRunAt = now
	}
	_, err := db.Exec(ctx, enqueueJob, j.ID, j.Type, j.Key, j.Payload, j.MaxAttempts, j.NextRunAt, now)
	return err
}

// ClaimJobs claims up to limit due jobs for workerID. Running jobs whose lock
// is older than staleBefore are reclaimed.
func ClaimJobs(ctx context.Context, db DBTX, workerID string, limit int, now, staleBefore time.Time) ([]*models.Job, error) {
	rows, err := db.Query(ctx, claimJobs, now, workerID, limit, staleBefore)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []*models.Job
	for rows.Next() {
		j, err := scanJob(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, j)
	}
	return out, rows.Err()
}

// HeartbeatJob renews the lease. It reports false when workerID no longer owns the job.
func HeartbeatJob(ctx context.Context, db DBTX, id uuid.UUID, workerID string, now time.Time) (bool, error) {
	tag, err := db.Exec(ctx, heartbeatJob, id, workerID, now)
	return tag.RowsAffected() == 1, err
}

// CompleteJob marks a job done, or pending again when a rerun was requested while it ran.
func CompleteJob(ctx context.Context, db DBTX, id uuid.UUID, workerID string) error {
	_, err := db.Exec(ctx, completeJob, id, workerID, time.Now().UTC())
	return err
}

// FailJob schedules a retry at nextRun, or marks the job dead once attempts reach max_attempts.
func FailJob(ctx context.Context, db DBTX, id uuid.UUID, workerID string, nextRun time.Time, errMsg string) error {
	_, err := db.Exec(ctx, failJob, id, workerID, nextRun, errMsg, time.Now().UTC())
	return err
}

// ReleaseJob hands a running job back without counting the attempt (graceful shutdown).
func ReleaseJob(ctx context.Context, db DBTX, id uuid.UUID, workerID string) error {
	_, err := db.Exec(ctx, releaseJob, id, workerID, time.Now().UTC())
	return err
}

// GetJob returns one job by id.
func GetJob(ctx context.Context, db DBTX, id uuid.UUID) (*models.Job, error) {
	return scanJob(db.QueryRow(ctx, selectJobByID, id))
}

// ListJobsByKey returns every job ever enqueued for key, oldest first.
func ListJobsByKey(ctx context.Context, db DBTX, key string) ([]*models.Job, error) {
	rows, err := db.Query(ctx, listJobsByKey, key)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []*models.Job
	for rows.Next() {
		j, err := scanJob(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, j)
	}
	return out, rows.Err()
}
