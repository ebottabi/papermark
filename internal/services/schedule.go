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
	"strings"
	"time"
	_ "time/tzdata" // zone names must resolve on hosts without a zoneinfo database
	"unicode/utf8"

	"papermark/internal/harness"
	"papermark/internal/models"
	"papermark/internal/models/queries"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"
)

// Schedule repeat values.
const (
	RepeatHourly   = "hourly"
	RepeatDaily    = "daily"
	RepeatWeekdays = "weekdays"
	RepeatWeekly   = "weekly"
)

// BriefTitle and BriefPrompt fill a brief schedule created without them. The
// run learns the user's time zone and news topics from its routine guidance.
const (
	BriefTitle  = "Morning brief"
	BriefPrompt = `Prepare my morning brief.
1. Email: if you have a gmail_digest tool, call it with sinceLast true and pick the few messages that matter (people, deadlines, anything needing a reply). If you have no gmail tool, skip email.
2. News: search the web for the latest on my news topics (or the general top headlines if I have none) and keep the few most important stories. Use web_fetch on a page only when the search snippet is not enough.
3. Weather: find today's weather for the main city of my time zone. If you cannot get it reliably, skip it.
Then call publish_update once with importance feed, title "Morning brief", a one-line body, and sections named Email, News and Weather (leave out any section with nothing in it). Keep items short. For Email items put the sender in detail. For News items put the article link in url.`
)

// TaskCreator starts a task. The task service satisfies it; a schedule run
// passes its own id so the task carries scheduleId.
type TaskCreator interface {
	CreateTask(ctx context.Context, db queries.DBTX, userID uuid.UUID, prompt, mode string, projectID *uuid.UUID, scheduleID uuid.UUID) (id string, err error)
}

// ErrInvalidSchedule wraps every schedule validation failure.
var ErrInvalidSchedule = errors.New("invalid schedule")

// ErrNoTaskCreator is returned by runs before SetTaskCreator is called.
var ErrNoTaskCreator = errors.New("schedules: no task creator configured")

// ScheduleService manages schedules and runs the due ones.
type ScheduleService struct {
	pool  *pgxpool.Pool
	tasks TaskCreator
}

// NewScheduleService constructs a ScheduleService. Slots are computed in each
// schedule owner's time zone.
func NewScheduleService(pool *pgxpool.Pool) *ScheduleService {
	return &ScheduleService{pool: pool}
}

// SetTaskCreator wires the service that creates tasks for runs.
func (s *ScheduleService) SetTaskCreator(tc TaskCreator) { s.tasks = tc }

// ScheduleFields are the user-editable fields of a schedule.
type ScheduleFields struct {
	Title     string
	Kind      string
	Prompt    string
	Mode      string
	ProjectID *uuid.UUID
	Repeat    string
	Time      string
	Weekday   *int
	Enabled   bool
}

// List returns the user's schedules.
func (s *ScheduleService) List(ctx context.Context, user uuid.UUID) ([]*models.Schedule, error) {
	return queries.ListSchedules(ctx, s.pool, user)
}

// Get returns one of the user's schedules.
func (s *ScheduleService) Get(ctx context.Context, user, id uuid.UUID) (*models.Schedule, error) {
	return queries.GetSchedule(ctx, s.pool, user, id)
}

// Create validates f and stores a schedule whose first run is the next slot.
func (s *ScheduleService) Create(ctx context.Context, user uuid.UUID, f ScheduleFields) (*models.Schedule, error) {
	sc := &models.Schedule{UserID: user}
	loc, err := userLocation(ctx, s.pool, user)
	if err != nil {
		return nil, err
	}
	if err := apply(sc, f, time.Now(), loc); err != nil {
		return nil, err
	}
	if err := queries.CreateSchedule(ctx, s.pool, sc); err != nil {
		return nil, err
	}
	return sc, nil
}

// Update replaces the editable fields of schedule id with f and recomputes the next slot.
func (s *ScheduleService) Update(ctx context.Context, user, id uuid.UUID, f ScheduleFields) (*models.Schedule, error) {
	sc, err := queries.GetSchedule(ctx, s.pool, user, id)
	if err != nil {
		return nil, err
	}
	loc, err := userLocation(ctx, s.pool, user)
	if err != nil {
		return nil, err
	}
	if err := apply(sc, f, time.Now(), loc); err != nil {
		return nil, err
	}
	if err := queries.UpdateSchedule(ctx, s.pool, sc); err != nil {
		return nil, err
	}
	return sc, nil
}

// CreateRoutine creates an enabled custom schedule on behalf of the agent.
func (s *ScheduleService) CreateRoutine(ctx context.Context, user uuid.UUID, r harness.RoutineSpec) (*models.Schedule, error) {
	return s.Create(ctx, user, ScheduleFields{Title: r.Title, Kind: models.ScheduleCustom, Prompt: r.Prompt,
		Repeat: r.Repeat, Time: r.Time, Weekday: r.Weekday, Enabled: true})
}

// Delete removes schedule id.
func (s *ScheduleService) Delete(ctx context.Context, user, id uuid.UUID) error {
	return queries.SoftDeleteSchedule(ctx, s.pool, user, id)
}

// RunNow creates the schedule's task immediately. The next slot is unchanged.
func (s *ScheduleService) RunNow(ctx context.Context, user, id uuid.UUID) (string, error) {
	if s.tasks == nil {
		return "", ErrNoTaskCreator
	}
	sc, err := queries.GetSchedule(ctx, s.pool, user, id)
	if err != nil {
		return "", err
	}
	taskID, err := s.tasks.CreateTask(ctx, s.pool, sc.UserID, sc.Prompt, sc.Mode, sc.ProjectID, sc.ID)
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
	loc, err := userLocation(ctx, tx, sc.UserID)
	if err != nil {
		return false, err
	}
	next, err := NextRun(sc.Repeat, sc.TimeOfDay, sc.Weekday, now, loc)
	if err != nil {
		return false, err
	}
	if err := queries.MarkScheduleRun(ctx, tx, sc.ID, now.UTC(), &next); err != nil {
		return false, err
	}
	// The task commits with the claim; the job worker's poll picks up its run.
	taskID, err := s.tasks.CreateTask(ctx, tx, sc.UserID, sc.Prompt, sc.Mode, sc.ProjectID, sc.ID)
	if err != nil {
		return false, fmt.Errorf("schedule %s: create task: %w", sc.ID, err)
	}
	if err := tx.Commit(ctx); err != nil {
		return false, err
	}
	log.Printf("schedule: ran id=%s task=%s next=%s", sc.ID, taskID, next.Format(time.RFC3339))
	return true, nil
}

// apply validates f onto sc and sets the next slot after now in loc (nil when disabled).
// A brief without a prompt or title gets BriefPrompt and BriefTitle; a custom
// schedule without a title gets the start of its prompt.
func apply(sc *models.Schedule, f ScheduleFields, now time.Time, loc *time.Location) error {
	if f.Kind == "" {
		f.Kind = models.ScheduleCustom
	}
	if f.Kind != models.ScheduleCustom && f.Kind != models.ScheduleBrief {
		return fmt.Errorf("%w: kind must be custom or brief", ErrInvalidSchedule)
	}
	if f.Kind == models.ScheduleBrief {
		if f.Prompt == "" {
			f.Prompt = BriefPrompt
		}
		if strings.TrimSpace(f.Title) == "" {
			f.Title = BriefTitle
		}
	}
	if f.Prompt == "" {
		return fmt.Errorf("%w: prompt is required", ErrInvalidSchedule)
	}
	if strings.TrimSpace(f.Title) == "" {
		f.Title = titleOf(f.Prompt)
	}
	if utf8.RuneCountInString(f.Title) > 120 {
		return fmt.Errorf("%w: title must be at most 120 characters", ErrInvalidSchedule)
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
	next, err := NextRun(f.Repeat, f.Time, f.Weekday, now, loc)
	if err != nil {
		return err
	}
	sc.Title, sc.Kind = strings.TrimSpace(f.Title), f.Kind
	sc.Prompt, sc.Mode, sc.ProjectID = f.Prompt, f.Mode, f.ProjectID
	sc.Repeat, sc.TimeOfDay, sc.Weekday, sc.Enabled = f.Repeat, f.Time, f.Weekday, f.Enabled
	sc.NextRunAt = nil
	if f.Enabled {
		utc := next.UTC()
		sc.NextRunAt = &utc
	}
	return nil
}

// titleOf is the first 40 characters of prompt with whitespace collapsed.
func titleOf(prompt string) string {
	t := []rune(strings.Join(strings.Fields(prompt), " "))
	return string(t[:min(len(t), 40)])
}

// userLocation is the time zone of user's slots.
func userLocation(ctx context.Context, db queries.DBTX, user uuid.UUID) (*time.Location, error) {
	p, err := queries.GetPreferences(ctx, db, user)
	if err != nil {
		return nil, err
	}
	return time.LoadLocation(p.Timezone)
}

// NextRun returns the first slot strictly after after, in loc.
// hourly fires at minute MM of every hour, daily at HH:MM, weekdays at HH:MM
// Monday to Friday, weekly at HH:MM on weekday (1 Monday to 7 Sunday). Wall-clock slots are built with time.Date,
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
	case RepeatWeekdays:
		for i := 0; ; i++ {
			c := time.Date(t.Year(), t.Month(), t.Day()+i, h, m, 0, 0, loc)
			if wd := c.Weekday(); wd != time.Saturday && wd != time.Sunday && c.After(after) {
				return c, nil
			}
		}
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
		return time.Time{}, fmt.Errorf("%w: repeat must be hourly, daily, weekdays or weekly", ErrInvalidSchedule)
	}
}
