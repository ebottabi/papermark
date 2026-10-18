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

const watchTargetColumns = `id, kind, key, url, query, selector, fingerprint, last_value, etag, last_modified,
	next_check_at, locked_until, failures, misses, last_error, interval_seconds`

func scanWatchTarget(row scannable) (*models.WatchTarget, error) {
	t := &models.WatchTarget{}
	if err := row.Scan(&t.ID, &t.Kind, &t.Key, &t.URL, &t.Query, &t.Selector, &t.Fingerprint, &t.LastValue, &t.ETag, &t.LastModified,
		&t.NextCheckAt, &t.LockedUntil, &t.Failures, &t.Misses, &t.LastError, &t.IntervalSeconds); err != nil {
		return nil, notFound(err)
	}
	return t, nil
}

const watchSubscriptionColumns = `routine_id, user_id, target_id, condition, threshold, text, armed, last_notified_fingerprint`

func scanWatchSubscription(row scannable) (*models.WatchSubscription, error) {
	s := &models.WatchSubscription{}
	if err := row.Scan(&s.RoutineID, &s.UserID, &s.TargetID, &s.Condition, &s.Threshold, &s.Text, &s.Armed, &s.LastNotifiedFingerprint); err != nil {
		return nil, notFound(err)
	}
	return s, nil
}

// UpsertWatchTarget returns the target for t.Key, creating it due at next. The
// target is returned as it is; RefreshWatchTarget sets its interval once the
// subscription exists.
func UpsertWatchTarget(ctx context.Context, db DBTX, t *models.WatchTarget, now time.Time) (*models.WatchTarget, error) {
	return scanWatchTarget(db.QueryRow(ctx, `
		INSERT INTO watch_targets (id, kind, key, url, query, selector, next_check_at, interval_seconds)
		VALUES ($1, $2, $3, $4, $5, $6, $7, $8)
		ON CONFLICT (key) DO UPDATE SET kind = watch_targets.kind
		RETURNING `+watchTargetColumns, uuid.New(), t.Kind, t.Key, t.URL, t.Query, t.Selector, now, t.IntervalSeconds))
}

// UpsertWatchSubscription stores a routine's subscription. A changed target or
// condition resets its hysteresis.
func UpsertWatchSubscription(ctx context.Context, db DBTX, s *models.WatchSubscription) error {
	_, err := db.Exec(ctx, `
		INSERT INTO watch_subscriptions (`+watchSubscriptionColumns+`) VALUES ($1, $2, $3, $4, $5, $6, true, '')
		ON CONFLICT (routine_id) DO UPDATE SET target_id = EXCLUDED.target_id, condition = EXCLUDED.condition,
			threshold = EXCLUDED.threshold, text = EXCLUDED.text,
			armed = CASE WHEN watch_subscriptions.target_id = EXCLUDED.target_id AND watch_subscriptions.condition = EXCLUDED.condition
				AND watch_subscriptions.threshold IS NOT DISTINCT FROM EXCLUDED.threshold AND watch_subscriptions.text = EXCLUDED.text
				THEN watch_subscriptions.armed ELSE true END,
			last_notified_fingerprint = CASE WHEN watch_subscriptions.target_id = EXCLUDED.target_id AND watch_subscriptions.condition = EXCLUDED.condition
				AND watch_subscriptions.threshold IS NOT DISTINCT FROM EXCLUDED.threshold AND watch_subscriptions.text = EXCLUDED.text
				THEN watch_subscriptions.last_notified_fingerprint ELSE '' END`,
		s.RoutineID, s.UserID, s.TargetID, s.Condition, s.Threshold, s.Text)
	return err
}

// DeleteWatchSubscription removes a routine's subscription and returns the
// target it left, or uuid.Nil when it had none.
func DeleteWatchSubscription(ctx context.Context, db DBTX, routineID uuid.UUID) (uuid.UUID, error) {
	var target uuid.UUID
	err := notFound(db.QueryRow(ctx, `DELETE FROM watch_subscriptions WHERE routine_id = $1 RETURNING target_id`, routineID).Scan(&target))
	if err == ErrNotFound {
		return uuid.Nil, nil
	}
	return target, err
}

// RefreshWatchTarget deletes a target no subscription uses. Otherwise it sets
// the target's interval to the smallest of its routines' and pulls its next
// check in when that interval is shorter than the wait left.
func RefreshWatchTarget(ctx context.Context, db DBTX, target uuid.UUID) error {
	if _, err := db.Exec(ctx, `DELETE FROM watch_targets t WHERE t.id = $1 AND NOT EXISTS (SELECT 1 FROM watch_subscriptions s WHERE s.target_id = t.id)`, target); err != nil {
		return err
	}
	_, err := db.Exec(ctx, `
		UPDATE watch_targets t SET interval_seconds = x.secs, next_check_at = LEAST(t.next_check_at, now() + make_interval(secs => x.secs))
		FROM (SELECT min(COALESCE((r.rule->>'intervalMinutes')::int, 1)) * 60 AS secs
			FROM watch_subscriptions w JOIN schedules r ON r.id = w.routine_id WHERE w.target_id = $1) x
		WHERE t.id = $1 AND x.secs IS NOT NULL`, target)
	return err
}

// ListWatchSubscriptions returns the subscriptions of a target.
func ListWatchSubscriptions(ctx context.Context, db DBTX, target uuid.UUID) ([]*models.WatchSubscription, error) {
	return list(ctx, db, scanWatchSubscription, `SELECT `+watchSubscriptionColumns+` FROM watch_subscriptions WHERE target_id = $1 ORDER BY routine_id`, target)
}

// ClaimWatchTargets leases up to limit due targets until now+lease, as ClaimMailboxPolls does.
func ClaimWatchTargets(ctx context.Context, db DBTX, now time.Time, limit int, lease time.Duration) ([]*models.WatchTarget, error) {
	return list(ctx, db, scanWatchTarget, `
		UPDATE watch_targets SET locked_until = $2
		WHERE id IN (
			SELECT id FROM watch_targets
			WHERE next_check_at <= $1 AND (locked_until IS NULL OR locked_until < $1)
			ORDER BY next_check_at LIMIT $3
			FOR UPDATE SKIP LOCKED)
		RETURNING `+watchTargetColumns, now, now.Add(lease), limit)
}

// WatchRoutine is an enabled routine with its subscription, for one check.
type WatchRoutine struct {
	Routine *models.Schedule
	Sub     *models.WatchSubscription
}

// ListWatchRoutines returns the enabled, live routines subscribed to target.
func ListWatchRoutines(ctx context.Context, db DBTX, target uuid.UUID) ([]WatchRoutine, error) {
	subs, err := ListWatchSubscriptions(ctx, db, target)
	if err != nil {
		return nil, err
	}
	var out []WatchRoutine
	for _, s := range subs {
		r, err := scanSchedule(db.QueryRow(ctx, `SELECT `+scheduleColumns+` FROM schedules WHERE id = $1 AND enabled AND deleted_at IS NULL`, s.RoutineID))
		if err == ErrNotFound {
			continue
		}
		if err != nil {
			return nil, err
		}
		out = append(out, WatchRoutine{r, s})
	}
	return out, nil
}

// ReleaseWatchTarget ends a check that learned nothing new: it drops the lease
// and sets the next check, the failure and miss streaks and the last error.
func ReleaseWatchTarget(ctx context.Context, db DBTX, id uuid.UUID, next time.Time, failures, misses int, lastError string) error {
	_, err := db.Exec(ctx, `UPDATE watch_targets SET locked_until = NULL, next_check_at = $2, failures = $3, misses = $4, last_error = $5 WHERE id = $1`,
		id, next, failures, misses, lastError)
	return err
}

// SaveWatchTarget stores a fetched value and the validators of the response.
func SaveWatchTarget(ctx context.Context, db DBTX, t *models.WatchTarget, next time.Time) error {
	_, err := db.Exec(ctx, `UPDATE watch_targets SET fingerprint = $2, last_value = $3, etag = $4, last_modified = $5,
		locked_until = NULL, next_check_at = $6, failures = 0, misses = 0, last_error = '' WHERE id = $1`,
		t.ID, t.Fingerprint, t.LastValue, t.ETag, t.LastModified, next)
	return err
}

// SetWatchArmed records a subscription's hysteresis state.
func SetWatchArmed(ctx context.Context, db DBTX, routineID uuid.UUID, armed bool, notified string) error {
	_, err := db.Exec(ctx, `UPDATE watch_subscriptions SET armed = $2, last_notified_fingerprint = $3 WHERE routine_id = $1`, routineID, armed, notified)
	return err
}

// SweepWatchTargets removes targets no subscription points at.
func SweepWatchTargets(ctx context.Context, db DBTX) error {
	_, err := db.Exec(ctx, `DELETE FROM watch_targets t WHERE NOT EXISTS (SELECT 1 FROM watch_subscriptions s WHERE s.target_id = t.id)`)
	return err
}

// WatchSubscriptionTarget returns the target a routine is subscribed to, or uuid.Nil.
func WatchSubscriptionTarget(ctx context.Context, db DBTX, routineID uuid.UUID) (uuid.UUID, error) {
	var target uuid.UUID
	err := notFound(db.QueryRow(ctx, `SELECT target_id FROM watch_subscriptions WHERE routine_id = $1`, routineID).Scan(&target))
	if err == ErrNotFound {
		return uuid.Nil, nil
	}
	return target, err
}

// CountTriggerEventsSince counts a routine's fires at or after since.
func CountTriggerEventsSince(ctx context.Context, db DBTX, routineID uuid.UUID, since time.Time) (n int, err error) {
	err = db.QueryRow(ctx, `SELECT count(*) FROM trigger_events WHERE routine_id = $1 AND created_at >= $2`, routineID, since).Scan(&n)
	return n, err
}
