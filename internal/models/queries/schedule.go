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

const scheduleColumns = `id, prompt, mode, project_id, repeat, time_of_day, weekday, enabled,
	last_run_at, next_run_at, created_at, updated_at`

const (
	insertSchedule = `
		INSERT INTO schedules (id, prompt, mode, project_id, repeat, time_of_day, weekday, enabled,
			last_run_at, next_run_at, created_at, updated_at)
		VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10, $11, $11)`

	selectScheduleByID = `SELECT ` + scheduleColumns + ` FROM schedules WHERE id = $1 AND deleted_at IS NULL`

	listSchedules = `SELECT ` + scheduleColumns + ` FROM schedules WHERE deleted_at IS NULL ORDER BY created_at ASC`

	// last_run_at is left alone: only runs write it.
	updateSchedule = `
		UPDATE schedules SET prompt = $2, mode = $3, project_id = $4, repeat = $5, time_of_day = $6,
			weekday = $7, enabled = $8, next_run_at = $9, updated_at = $10
		WHERE id = $1 AND deleted_at IS NULL`

	softDeleteSchedule = `UPDATE schedules SET deleted_at = $2, next_run_at = NULL WHERE id = $1 AND deleted_at IS NULL`

	// NO KEY UPDATE, not UPDATE, so a task insert referencing this row (a
	// foreign key takes KEY SHARE) does not block on the claim.
	claimDueSchedule = `
		SELECT ` + scheduleColumns + ` FROM schedules
		WHERE enabled AND deleted_at IS NULL AND next_run_at <= $1
		ORDER BY next_run_at ASC
		LIMIT 1
		FOR NO KEY UPDATE SKIP LOCKED`

	markScheduleRun = `UPDATE schedules SET last_run_at = $2, next_run_at = COALESCE($3, next_run_at), updated_at = $2 WHERE id = $1`
)

func scanSchedule(row scannable) (*models.Schedule, error) {
	s := &models.Schedule{}
	err := row.Scan(&s.ID, &s.Prompt, &s.Mode, &s.ProjectID, &s.Repeat, &s.TimeOfDay, &s.Weekday, &s.Enabled,
		&s.LastRunAt, &s.NextRunAt, &s.CreatedAt, &s.UpdatedAt)
	if err != nil {
		return nil, notFound(err)
	}
	return s, nil
}

func scanSchedules(ctx context.Context, db DBTX, sql string, args ...any) ([]*models.Schedule, error) {
	rows, err := db.Query(ctx, sql, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []*models.Schedule
	for rows.Next() {
		s, err := scanSchedule(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, s)
	}
	return out, rows.Err()
}

// CreateSchedule inserts s, assigning its id and timestamps.
func CreateSchedule(ctx context.Context, db DBTX, s *models.Schedule) error {
	s.ID = uuid.New()
	s.CreatedAt = time.Now().UTC()
	s.UpdatedAt = s.CreatedAt
	_, err := db.Exec(ctx, insertSchedule, s.ID, s.Prompt, s.Mode, s.ProjectID, s.Repeat, s.TimeOfDay, s.Weekday,
		s.Enabled, s.LastRunAt, s.NextRunAt, s.CreatedAt)
	return err
}

// GetSchedule returns one live schedule.
func GetSchedule(ctx context.Context, db DBTX, id uuid.UUID) (*models.Schedule, error) {
	return scanSchedule(db.QueryRow(ctx, selectScheduleByID, id))
}

// ListSchedules returns every live schedule, oldest first.
func ListSchedules(ctx context.Context, db DBTX) ([]*models.Schedule, error) {
	return scanSchedules(ctx, db, listSchedules)
}

// UpdateSchedule writes s's editable fields and next_run_at.
func UpdateSchedule(ctx context.Context, db DBTX, s *models.Schedule) error {
	s.UpdatedAt = time.Now().UTC()
	tag, err := db.Exec(ctx, updateSchedule, s.ID, s.Prompt, s.Mode, s.ProjectID, s.Repeat, s.TimeOfDay, s.Weekday,
		s.Enabled, s.NextRunAt, s.UpdatedAt)
	if err == nil && tag.RowsAffected() == 0 {
		return ErrNotFound
	}
	return err
}

// SoftDeleteSchedule deletes a schedule; ErrNotFound when it is not live.
func SoftDeleteSchedule(ctx context.Context, db DBTX, id uuid.UUID) error {
	tag, err := db.Exec(ctx, softDeleteSchedule, id, time.Now().UTC())
	if err == nil && tag.RowsAffected() == 0 {
		return ErrNotFound
	}
	return err
}

// ClaimDueSchedule locks the most overdue enabled schedule, skipping rows
// another transaction holds. It must run in a transaction; ErrNotFound when none is due.
func ClaimDueSchedule(ctx context.Context, db DBTX, now time.Time) (*models.Schedule, error) {
	return scanSchedule(db.QueryRow(ctx, claimDueSchedule, now))
}

// MarkScheduleRun records a run at ranAt and, when next is non-nil, the next slot.
func MarkScheduleRun(ctx context.Context, db DBTX, id uuid.UUID, ranAt time.Time, next *time.Time) error {
	_, err := db.Exec(ctx, markScheduleRun, id, ranAt, next)
	return err
}
