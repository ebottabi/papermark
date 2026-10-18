/*
 * Copyright (c) 2026. Papermark - All Rights Reserved
 * Unauthorized copying of this file, via any medium is strictly prohibited
 * Proprietary and confidential
 * Written by Ebot Tabi <ebot.tabi@gmail.com>, 15/10/2026
 */
package queries

import (
	"context"
	"errors"
	"time"

	"papermark/internal/models"

	"github.com/google/uuid"
)

const scheduleColumns = `id, user_id, title, kind, prompt, mode, project_id, repeat, time_of_day, weekday, enabled,
	last_run_at, next_run_at, created_at, updated_at, trigger, rule, max_runs_per_hour, skills`

const (
	insertSchedule = `
		INSERT INTO schedules (id, user_id, title, kind, prompt, mode, project_id, repeat, time_of_day, weekday, enabled,
			last_run_at, next_run_at, created_at, updated_at, trigger, rule, max_runs_per_hour, skills)
		VALUES ($1, $14, $2, $3, $4, $5, $6, $7, $8, $9, $10, $11, $12, $13, $13, $15, $16, $17, $18)`

	selectScheduleByID = `SELECT ` + scheduleColumns + ` FROM schedules WHERE id = $1 AND user_id = $2 AND deleted_at IS NULL`

	listSchedules = `SELECT ` + scheduleColumns + ` FROM schedules WHERE user_id = $1 AND deleted_at IS NULL ORDER BY created_at ASC`

	// last_run_at is left alone: only runs write it. An edit clears the failure streak.
	updateSchedule = `
		UPDATE schedules SET title = $2, kind = $3, prompt = $4, mode = $5, project_id = $6, repeat = $7, time_of_day = $8,
			weekday = $9, enabled = $10, next_run_at = $11, updated_at = $12, consecutive_failures = 0,
			trigger = $14, rule = $15, max_runs_per_hour = $16, skills = $17
		WHERE id = $1 AND user_id = $13 AND deleted_at IS NULL`

	softDeleteSchedule = `UPDATE schedules SET deleted_at = $3, next_run_at = NULL WHERE id = $1 AND user_id = $2 AND deleted_at IS NULL`

	// NO KEY UPDATE, not UPDATE, so a task insert referencing this row (a
	// foreign key takes KEY SHARE) does not block on the claim.
	claimDueSchedule = `
		SELECT ` + scheduleColumns + ` FROM schedules
		WHERE enabled AND trigger = 'time' AND deleted_at IS NULL AND next_run_at <= $1
		ORDER BY next_run_at ASC
		LIMIT 1
		FOR NO KEY UPDATE SKIP LOCKED`

	markScheduleRun = `UPDATE schedules SET last_run_at = $2, next_run_at = COALESCE($3, next_run_at), updated_at = $2 WHERE id = $1`
)

func scanSchedule(row scannable) (*models.Schedule, error) {
	s := &models.Schedule{}
	err := row.Scan(&s.ID, &s.UserID, &s.Title, &s.Kind, &s.Prompt, &s.Mode, &s.ProjectID, &s.Repeat, &s.TimeOfDay, &s.Weekday, &s.Enabled,
		&s.LastRunAt, &s.NextRunAt, &s.CreatedAt, &s.UpdatedAt, &s.Trigger, &s.Rule, &s.MaxRunsPerHour, &s.Skills)
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
	_, err := db.Exec(ctx, insertSchedule, s.ID, s.Title, s.Kind, s.Prompt, s.Mode, s.ProjectID, s.Repeat, s.TimeOfDay, s.Weekday,
		s.Enabled, s.LastRunAt, s.NextRunAt, s.CreatedAt, s.UserID, s.Trigger, s.Rule, s.MaxRunsPerHour, s.Skills)
	return err
}

// GetSchedule returns one of userID's live schedules.
// SetScheduleSkills saves the skills a schedule's runs follow, unless an
// edit cleared them for a new prompt since they were picked from prompt.
func SetScheduleSkills(ctx context.Context, db DBTX, userID, id uuid.UUID, prompt string, skills []string) error {
	_, err := db.Exec(ctx, `UPDATE schedules SET skills = $4 WHERE id = $1 AND user_id = $2 AND prompt = $3 AND skills IS NULL`,
		id, userID, prompt, skills)
	return err
}

func GetSchedule(ctx context.Context, db DBTX, userID, id uuid.UUID) (*models.Schedule, error) {
	return scanSchedule(db.QueryRow(ctx, selectScheduleByID, id, userID))
}

// ListSchedules returns userID's live schedules, oldest first.
func ListSchedules(ctx context.Context, db DBTX, userID uuid.UUID) ([]*models.Schedule, error) {
	return scanSchedules(ctx, db, listSchedules, userID)
}

// UpdateSchedule writes s's editable fields and next_run_at; s.UserID must own it.
func UpdateSchedule(ctx context.Context, db DBTX, s *models.Schedule) error {
	s.UpdatedAt = time.Now().UTC()
	tag, err := db.Exec(ctx, updateSchedule, s.ID, s.Title, s.Kind, s.Prompt, s.Mode, s.ProjectID, s.Repeat, s.TimeOfDay, s.Weekday,
		s.Enabled, s.NextRunAt, s.UpdatedAt, s.UserID, s.Trigger, s.Rule, s.MaxRunsPerHour, s.Skills)
	if err == nil && tag.RowsAffected() == 0 {
		return ErrNotFound
	}
	return err
}

// SoftDeleteSchedule deletes one of userID's schedules; ErrNotFound when it is not live.
func SoftDeleteSchedule(ctx context.Context, db DBTX, userID, id uuid.UUID) error {
	tag, err := db.Exec(ctx, softDeleteSchedule, id, userID, time.Now().UTC())
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

// SetScheduleNextRun moves a schedule's next slot (its owner changed time zone).
func SetScheduleNextRun(ctx context.Context, db DBTX, id uuid.UUID, next time.Time) error {
	_, err := db.Exec(ctx, `UPDATE schedules SET next_run_at = $2 WHERE id = $1`, id, next)
	return err
}

// RecordScheduleOutcome counts the end of a schedule's task: a success clears
// the failure streak, a failure extends it. The failure that reaches limit
// turns the schedule off and reports disabled with its title.
func RecordScheduleOutcome(ctx context.Context, db DBTX, id uuid.UUID, failed bool, limit int) (disabled bool, title string, err error) {
	if !failed {
		_, err = db.Exec(ctx, `UPDATE schedules SET consecutive_failures = 0 WHERE id = $1 AND consecutive_failures <> 0`, id)
		return false, "", err
	}
	var streak int
	err = db.QueryRow(ctx, `UPDATE schedules SET consecutive_failures = consecutive_failures + 1,
			enabled = enabled AND consecutive_failures + 1 < $2,
			next_run_at = CASE WHEN consecutive_failures + 1 < $2 THEN next_run_at END
		WHERE id = $1 AND deleted_at IS NULL RETURNING title, consecutive_failures`, id, limit).Scan(&title, &streak)
	err = notFound(err)
	if errors.Is(err, ErrNotFound) {
		return false, "", nil
	}
	return streak == limit, title, err
}
