/*
 * Copyright (c) 2026. Papermark - All Rights Reserved
 * Unauthorized copying of this file, via any medium is strictly prohibited
 * Proprietary and confidential
 * Written by Ebot Tabi <ebot.tabi@gmail.com>, 17/10/2026
 */
package queries

import (
	"context"
	"time"

	"papermark/internal/models"

	"github.com/google/uuid"
)

const mailboxPollColumns = `account_id, user_id, history_id, next_check_at, locked_until, failures, last_error`

func scanMailboxPoll(row scannable) (*models.MailboxPoll, error) {
	p := &models.MailboxPoll{}
	if err := row.Scan(&p.AccountID, &p.UserID, &p.HistoryID, &p.NextCheckAt, &p.LockedUntil, &p.Failures, &p.LastError); err != nil {
		return nil, notFound(err)
	}
	return p, nil
}

// ClaimMailboxPolls leases up to limit due mailboxes to the caller until
// now+lease. Rows another claimer holds are skipped, so two claimers never
// share one.
func ClaimMailboxPolls(ctx context.Context, db DBTX, now time.Time, limit int, lease time.Duration) ([]*models.MailboxPoll, error) {
	return list(ctx, db, scanMailboxPoll, `
		UPDATE mailbox_polls SET locked_until = $2
		WHERE account_id IN (
			SELECT account_id FROM mailbox_polls
			WHERE next_check_at <= $1 AND (locked_until IS NULL OR locked_until < $1)
			ORDER BY next_check_at LIMIT $3
			FOR UPDATE SKIP LOCKED)
		RETURNING `+mailboxPollColumns, now, now.Add(lease), limit)
}

// GetMailboxPoll returns an account's poll row.
func GetMailboxPoll(ctx context.Context, db DBTX, accountID uuid.UUID) (*models.MailboxPoll, error) {
	return scanMailboxPoll(db.QueryRow(ctx, `SELECT `+mailboxPollColumns+` FROM mailbox_polls WHERE account_id = $1`, accountID))
}

// EnsureMailboxPoll creates an account's poll row, due at next, when it has none.
func EnsureMailboxPoll(ctx context.Context, db DBTX, accountID, userID uuid.UUID, historyID string, next time.Time) error {
	_, err := db.Exec(ctx, `INSERT INTO mailbox_polls (account_id, user_id, history_id, next_check_at) VALUES ($1, $2, $3, $4)
		ON CONFLICT (account_id) DO NOTHING`, accountID, userID, historyID, next)
	return err
}

// DeleteMailboxPollsExcept removes userID's poll rows whose account is not in keep.
func DeleteMailboxPollsExcept(ctx context.Context, db DBTX, userID uuid.UUID, keep []uuid.UUID) error {
	_, err := db.Exec(ctx, `DELETE FROM mailbox_polls WHERE user_id = $1 AND NOT (account_id = ANY($2))`, userID, keep)
	return err
}

// ReleaseMailboxPoll ends a check that moved nothing: it drops the lease,
// schedules the next check and records the failure streak and error.
func ReleaseMailboxPoll(ctx context.Context, db DBTX, accountID uuid.UUID, next time.Time, failures int, lastError string) error {
	_, err := db.Exec(ctx, `UPDATE mailbox_polls SET locked_until = NULL, next_check_at = $2, failures = $3, last_error = $4 WHERE account_id = $1`,
		accountID, next, failures, lastError)
	return err
}

// AdvanceMailboxPoll is ReleaseMailboxPoll for a check that read through to historyID.
func AdvanceMailboxPoll(ctx context.Context, db DBTX, accountID uuid.UUID, historyID string, next time.Time) error {
	_, err := db.Exec(ctx, `UPDATE mailbox_polls SET history_id = $2, locked_until = NULL, next_check_at = $3, failures = 0, last_error = '' WHERE account_id = $1`,
		accountID, historyID, next)
	return err
}

// InsertTriggerEvents records the fires of a routine and returns the source
// keys that were new. A key already recorded for the routine is skipped.
func InsertTriggerEvents(ctx context.Context, db DBTX, routineID, userID uuid.UUID, events []models.TriggerEvent, now time.Time) ([]models.TriggerEvent, error) {
	var fresh []models.TriggerEvent
	for _, e := range events {
		tag, err := db.Exec(ctx, `INSERT INTO trigger_events (id, routine_id, user_id, source_key, summary, task_id, created_at)
			VALUES ($1, $2, $3, $4, $5, $6, $7) ON CONFLICT (routine_id, source_key) DO NOTHING`,
			uuid.New(), routineID, userID, e.SourceKey, e.Summary, e.TaskID, now)
		if err != nil {
			return nil, err
		}
		if tag.RowsAffected() == 1 {
			fresh = append(fresh, e)
		}
	}
	return fresh, nil
}

// SetTriggerEventsTask links a routine's events recorded at now to their task.
func SetTriggerEventsTask(ctx context.Context, db DBTX, routineID uuid.UUID, keys []string, taskID uuid.UUID) error {
	_, err := db.Exec(ctx, `UPDATE trigger_events SET task_id = $3 WHERE routine_id = $1 AND source_key = ANY($2)`, routineID, keys, taskID)
	return err
}

// ListTriggerEvents returns a routine's fires, newest first.
func ListTriggerEvents(ctx context.Context, db DBTX, userID, routineID uuid.UUID, limit int) ([]*models.TriggerEvent, error) {
	return list(ctx, db, func(row scannable) (*models.TriggerEvent, error) {
		e := &models.TriggerEvent{}
		if err := row.Scan(&e.ID, &e.RoutineID, &e.UserID, &e.SourceKey, &e.Summary, &e.TaskID, &e.CreatedAt); err != nil {
			return nil, notFound(err)
		}
		return e, nil
	}, `SELECT id, routine_id, user_id, source_key, summary, task_id, created_at FROM trigger_events
		WHERE routine_id = $1 AND user_id = $2 ORDER BY created_at DESC, id DESC LIMIT $3`, routineID, userID, limit)
}

// CountRoutineTasksSince counts the tasks a routine started at or after since.
func CountRoutineTasksSince(ctx context.Context, db DBTX, routineID uuid.UUID, since time.Time) (n int, err error) {
	err = db.QueryRow(ctx, `SELECT count(*) FROM tasks WHERE schedule_id = $1 AND created_at >= $2`, routineID, since).Scan(&n)
	return n, err
}

// ClaimLimitNote reports whether the caller may post the "too many runs" note:
// true when none was posted since since, and marks it posted.
func ClaimLimitNote(ctx context.Context, db DBTX, routineID uuid.UUID, now, since time.Time) (bool, error) {
	tag, err := db.Exec(ctx, `UPDATE schedules SET limit_noted_at = $2 WHERE id = $1 AND (limit_noted_at IS NULL OR limit_noted_at < $3)`, routineID, now, since)
	return err == nil && tag.RowsAffected() == 1, err
}

// ListEmailRoutines returns userID's enabled email routines.
func ListEmailRoutines(ctx context.Context, db DBTX, userID uuid.UUID) ([]*models.Schedule, error) {
	return scanSchedules(ctx, db, `SELECT `+scheduleColumns+` FROM schedules
		WHERE user_id = $1 AND trigger = 'email' AND enabled AND deleted_at IS NULL ORDER BY created_at`, userID)
}

// CountTriggerRoutines counts userID's live routines of a trigger, excluding one id.
func CountTriggerRoutines(ctx context.Context, db DBTX, userID uuid.UUID, trigger string, except uuid.UUID) (n int, err error) {
	err = db.QueryRow(ctx, `SELECT count(*) FROM schedules WHERE user_id = $1 AND trigger = $2 AND id <> $3 AND deleted_at IS NULL`,
		userID, trigger, except).Scan(&n)
	return n, err
}

// DisableRoutine turns a routine off and returns its title; ErrNotFound when it is not live.
func DisableRoutine(ctx context.Context, db DBTX, id uuid.UUID) (title string, err error) {
	err = notFound(db.QueryRow(ctx, `UPDATE schedules SET enabled = false, next_run_at = NULL, updated_at = now()
		WHERE id = $1 AND deleted_at IS NULL AND enabled RETURNING title`, id).Scan(&title))
	return title, err
}
