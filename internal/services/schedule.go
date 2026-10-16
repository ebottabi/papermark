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
	"fmt"
	"log"
	"time"

	"papermark/internal/models"
	"papermark/internal/models/queries"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"
)

// Schedule repeat values.
const (
	RepeatHourly = "hourly"
	RepeatDaily  = "daily"
	RepeatWeekly = "weekly"
)

// TaskCreator starts a task. The task service satisfies it; a schedule run
// passes its own id so the task carries scheduleId.
type TaskCreator interface {
	CreateTask(ctx context.Context, db queries.DBTX, prompt, mode string, projectID *uuid.UUID, scheduleID uuid.UUID) (id string, err error)
}

// ErrInvalidSchedule wraps every schedule validation failure.
var ErrInvalidSchedule = errors.New("invalid schedule")

// ErrNoTaskCreator is returned by runs before SetTaskCreator is called.
var ErrNoTaskCreator = errors.New("schedules: no task creator configured")

// ScheduleService manages schedules and runs the due ones.
type ScheduleService struct {
	pool  *pgxpool.Pool
	tasks TaskCreator
	loc   *time.Location
}

// NewScheduleService constructs a ScheduleService computing slots in time.Local.
func NewScheduleService(pool *pgxpool.Pool) *ScheduleService {
	return &ScheduleService{pool: pool, loc: time.Local}
}

// SetTaskCreator wires the service that creates tasks for runs.
func (s *ScheduleService) SetTaskCreator(tc TaskCreator) { s.tasks = tc }

// ScheduleFields are the user-editable fields of a schedule.
type ScheduleFields struct {
	Prompt    string
	Mode      string
	ProjectID *uuid.UUID
	Repeat    string
	Time      string
	Weekday   *int
	Enabled   bool
}

// List returns every schedule.
func (s *ScheduleService) List(ctx context.Context) ([]*models.Schedule, error) {
	return queries.ListSchedules(ctx, s.pool)
}

// Get returns one schedule.
func (s *ScheduleService) Get(ctx context.Context, id uuid.UUID) (*models.Schedule, error) {
	return queries.GetSchedule(ctx, s.pool, id)
}

// Create validates f and stores a schedule whose first run is the next slot.
func (s *ScheduleService) Create(ctx context.Context, f ScheduleFields) (*models.Schedule, error) {
	sc := &models.Schedule{}
	if err := s.apply(sc, f, time.Now()); err != nil {
		return nil, err
	}
	if err := queries.CreateSchedule(ctx, s.pool, sc); err != nil {
		return nil, err
	}
	return sc, nil
}

// Update replaces the editable fields of schedule id with f and recomputes the next slot.
func (s *ScheduleService) Update(ctx context.Context, id uuid.UUID, f ScheduleFields) (*models.Schedule, error) {
	sc, err := queries.GetSchedule(ctx, s.pool, id)
	if err != nil {
		return nil, err
	}
	if err := s.apply(sc, f, time.Now()); err != nil {
		return nil, err
	}
	if err := queries.UpdateSchedule(ctx, s.pool, sc); err != nil {
		return nil, err
	}
	return sc, nil
}

// Delete removes schedule id.
func (s *ScheduleService) Delete(ctx context.Context, id uuid.UUID) error {
	return queries.SoftDeleteSchedule(ctx, s.pool, id)
}

// RunNow creates the schedule's task immediately. The next slot is unchanged.
func (s *ScheduleService) RunNow(ctx context.Context, id uuid.UUID) (string, error) {
	if s.tasks == nil {
		return "", ErrNoTaskCreator
	}
	sc, err := queries.GetSchedule(ctx, s.pool, id)
	if err != nil {
		return "", err
	}
	taskID, err := s.tasks.CreateTask(ctx, s.pool, sc.Prompt, sc.Mode, sc.ProjectID, sc.ID)
	if err != nil {
		return "", err
	}
	return taskID, queries.MarkScheduleRun(ctx, s.pool, sc.ID, time.Now().UTC(), nil)
}

// RunDue runs every schedule due at now and returns how many tasks it created.
// Each schedule is claimed with SKIP LOCKED and advanced to its first slot
// after now in the transaction that creates the task, so concurrent
// schedulers never run a slot twice and a backlog of missed slots runs once.
// A failed task creation rolls back, and the slot is retried on the next tick.
func (s *ScheduleService) RunDue(ctx context.Context, now time.Time) (int, error) {
	if s.tasks == nil {
		return 0, ErrNoTaskCreator
	}
	ran := 0
	for {
		ok, err := s.runOneDue(ctx, now)
		if err != nil || !ok {
			return ran, err
		}
		ran++
	}
}

func (s *ScheduleService) runOneDue(ctx context.Context, now time.Time) (bool, error) {
	tx, err := s.pool.Begin(ctx)
	if err != nil {
		return false, err
	}
	defer tx.Rollback(ctx)

	sc, err := queries.ClaimDueSchedule(ctx, tx, now)
	if errors.Is(err, queries.ErrNotFound) {
		return false, nil
	}
	if err != nil {
		return false, err
	}
	next, err := NextRun(sc.Repeat, sc.TimeOfDay, sc.Weekday, now, s.loc)
	if err != nil {
		return false, err
	}
	if err := queries.MarkScheduleRun(ctx, tx, sc.ID, now.UTC(), &next); err != nil {
		return false, err
	}
	// The task commits with the claim; the job worker's poll picks up its run.
	taskID, err := s.tasks.CreateTask(ctx, tx, sc.Prompt, sc.Mode, sc.ProjectID, sc.ID)
	if err != nil {
		return false, fmt.Errorf("schedule %s: create task: %w", sc.ID, err)
	}
	if err := tx.Commit(ctx); err != nil {
		return false, err
	}
	log.Printf("schedule: ran id=%s task=%s next=%s", sc.ID, taskID, next.Format(time.RFC3339))
	return true, nil
}

// apply validates f onto sc and sets the next slot after now (nil when disabled).
func (s *ScheduleService) apply(sc *models.Schedule, f ScheduleFields, now time.Time) error {
	if f.Prompt == "" {
		return fmt.Errorf("%w: prompt is required", ErrInvalidSchedule)
	}
	if f.Mode == "" {
		f.Mode = "quality"
	}
	if f.Mode != "speed" && f.Mode != "quality" {
		return fmt.Errorf("%w: mode must be speed or quality", ErrInvalidSchedule)
	}
	if f.Repeat != RepeatWeekly {
		f.Weekday = nil
	}
	next, err := NextRun(f.Repeat, f.Time, f.Weekday, now, s.loc)
	if err != nil {
		return err
	}
	sc.Prompt, sc.Mode, sc.ProjectID = f.Prompt, f.Mode, f.ProjectID
	sc.Repeat, sc.TimeOfDay, sc.Weekday, sc.Enabled = f.Repeat, f.Time, f.Weekday, f.Enabled
	sc.NextRunAt = nil
	if f.Enabled {
		utc := next.UTC()
		sc.NextRunAt = &utc
	}
	return nil
}

// NextRun returns the first slot strictly after after, in loc.
// hourly fires at minute MM of every hour, daily at HH:MM, weekly at HH:MM on
// weekday (1 Monday to 7 Sunday). Wall-clock slots are built with time.Date,
// so they stay at HH:MM local across DST changes. A repeated wall time (fall
// back) resolves to its first occurrence; one inside a spring-forward gap
// resolves with the pre-transition offset, an hour earlier on the wall clock.
func NextRun(repeat, hhmm string, weekday *int, after time.Time, loc *time.Location) (time.Time, error) {
	hm, err := time.Parse("15:04", hhmm)
	if err != nil || len(hhmm) != 5 {
		return time.Time{}, fmt.Errorf("%w: time must be HH:MM", ErrInvalidSchedule)
	}
	h, m := hm.Hour(), hm.Minute()
	t := after.In(loc)
	switch repeat {
	case RepeatHourly:
		// The hour is advanced in absolute time so a repeated or skipped DST hour still fires once per real hour.
		c := time.Date(t.Year(), t.Month(), t.Day(), t.Hour(), m, 0, 0, loc)
		for !c.After(after) {
			c = c.Add(time.Hour)
		}
		return c, nil
	case RepeatDaily:
		c := time.Date(t.Year(), t.Month(), t.Day(), h, m, 0, 0, loc)
		if !c.After(after) {
			c = time.Date(t.Year(), t.Month(), t.Day()+1, h, m, 0, 0, loc)
		}
		return c, nil
	case RepeatWeekly:
		if weekday == nil || *weekday < 1 || *weekday > 7 {
			return time.Time{}, fmt.Errorf("%w: weekly needs weekday 1-7", ErrInvalidSchedule)
		}
		iso := (int(t.Weekday())+6)%7 + 1
		days := (*weekday - iso + 7) % 7
		c := time.Date(t.Year(), t.Month(), t.Day()+days, h, m, 0, 0, loc)
		if !c.After(after) {
			c = time.Date(t.Year(), t.Month(), t.Day()+days+7, h, m, 0, 0, loc)
		}
		return c, nil
	default:
		return time.Time{}, fmt.Errorf("%w: repeat must be hourly, daily or weekly", ErrInvalidSchedule)
	}
}
