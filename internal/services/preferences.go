/*
 * Copyright (c) 2026. Papermark - All Rights Reserved
 * Unauthorized copying of this file, via any medium is strictly prohibited
 * Proprietary and confidential
 * Written by Ebot Tabi <ebot.tabi@gmail.com>, 17/10/2026
 */
package services

import (
	"context"
	"errors"
	"slices"
	"strings"
	"time"
	"unicode/utf8"

	"papermark/internal/models"
	"papermark/internal/models/queries"
	"papermark/internal/notifications"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
)

const (
	maxPushLimit  = 20
	maxNewsTopics = 20
	maxTopicRunes = 60
)

// PreferenceService reads and changes a user's assistant preferences.
type PreferenceService struct {
	pool *pgxpool.Pool
	ntfy *notifications.NtfyConfig // nil when ntfy is off
}

// NewPreferenceService constructs a PreferenceService.
func NewPreferenceService(pool *pgxpool.Pool) *PreferenceService {
	return &PreferenceService{pool: pool}
}

// NtfyTopic is the ntfy topic user subscribes to, or nil when ntfy is off.
func (s *PreferenceService) NtfyTopic(user uuid.UUID) *string {
	if s.ntfy == nil {
		return nil
	}
	t := s.ntfy.Topic(user)
	return &t
}

// PreferencesPatch carries the preferences a request changes; nil keeps the stored value.
type PreferencesPatch struct {
	Timezone   *string
	QuietStart *string
	QuietEnd   *string
	PushLimit  *int
	NewsTopics *[]string
}

// Get returns the user's preferences.
func (s *PreferenceService) Get(ctx context.Context, user uuid.UUID) (*models.Preferences, error) {
	return queries.GetPreferences(ctx, s.pool, user)
}

// Update validates and stores the patch. A new time zone moves the next slot
// of every enabled schedule so it keeps its wall-clock time.
func (s *PreferenceService) Update(ctx context.Context, user uuid.UUID, patch PreferencesPatch) (*models.Preferences, error) {
	var p *models.Preferences
	err := pgx.BeginFunc(ctx, s.pool, func(tx pgx.Tx) error {
		var err error
		if p, err = queries.GetPreferences(ctx, tx, user); err != nil {
			return err
		}
		zoneChanged := patch.Timezone != nil && *patch.Timezone != p.Timezone
		if patch.Timezone != nil {
			if _, err := time.LoadLocation(*patch.Timezone); err != nil || *patch.Timezone == "" || *patch.Timezone == "Local" {
				return badRequest("timezone must be an IANA time zone such as Europe/Berlin")
			}
			p.Timezone = *patch.Timezone
		}
		for field, v := range map[string]struct {
			in  *string
			out *string
		}{"quietStart": {patch.QuietStart, &p.QuietStart}, "quietEnd": {patch.QuietEnd, &p.QuietEnd}} {
			if v.in == nil {
				continue
			}
			if _, err := time.Parse("15:04", *v.in); err != nil || len(*v.in) != 5 {
				return badRequest("%s must be HH:MM", field)
			}
			*v.out = *v.in
		}
		if patch.PushLimit != nil {
			if *patch.PushLimit < 0 || *patch.PushLimit > maxPushLimit {
				return badRequest("pushLimit must be 0 to %d", maxPushLimit)
			}
			p.PushLimit = *patch.PushLimit
		}
		if patch.NewsTopics != nil {
			if p.NewsTopics, err = cleanTopics(*patch.NewsTopics); err != nil {
				return err
			}
		}
		if err := queries.SetPreferences(ctx, tx, user, p); err != nil {
			return err
		}
		if zoneChanged {
			return moveSchedules(ctx, tx, user, p.Timezone)
		}
		return nil
	})
	return p, err
}

func cleanTopics(in []string) ([]string, error) {
	out := []string{}
	for _, t := range in {
		if t = strings.TrimSpace(t); t == "" || slices.Contains(out, t) {
			continue
		}
		if utf8.RuneCountInString(t) > maxTopicRunes {
			return nil, badRequest("a news topic must be at most %d characters", maxTopicRunes)
		}
		out = append(out, t)
	}
	if len(out) > maxNewsTopics {
		return nil, badRequest("newsTopics holds at most %d topics", maxNewsTopics)
	}
	return out, nil
}

// moveSchedules recomputes the next slot of user's enabled schedules in zone.
func moveSchedules(ctx context.Context, db queries.DBTX, user uuid.UUID, zone string) error {
	loc, err := time.LoadLocation(zone)
	if err != nil {
		return err
	}
	list, err := queries.ListSchedules(ctx, db, user)
	if err != nil {
		return err
	}
	for _, sc := range list {
		if !sc.Enabled || sc.NextRunAt == nil {
			continue
		}
		next, err := NextRun(sc.Repeat, sc.TimeOfDay, sc.Weekday, time.Now(), loc)
		if err != nil {
			return err
		}
		if err := queries.SetScheduleNextRun(ctx, db, sc.ID, next.UTC()); err != nil {
			return err
		}
	}
	return nil
}

// UpdateService serves a user's update feed.
type UpdateService struct{ pool *pgxpool.Pool }

// NewUpdateService constructs a UpdateService.
func NewUpdateService(pool *pgxpool.Pool) *UpdateService { return &UpdateService{pool: pool} }

// List returns the feed, newest first, strictly older than before.
func (s *UpdateService) List(ctx context.Context, user uuid.UUID, before *time.Time, limit int) ([]*models.Update, error) {
	return queries.ListUpdates(ctx, s.pool, user, before, limit)
}

// MarkRead marks one update read.
func (s *UpdateService) MarkRead(ctx context.Context, user, id uuid.UUID) error {
	return queries.MarkUpdateRead(ctx, s.pool, user, id)
}

// MarkAllRead marks every update read.
func (s *UpdateService) MarkAllRead(ctx context.Context, user uuid.UUID) error {
	return queries.MarkAllUpdatesRead(ctx, s.pool, user)
}

// MemoryService manages what the assistant remembers about a user.
type MemoryService struct{ pool *pgxpool.Pool }

// NewMemoryService constructs a MemoryService.
func NewMemoryService(pool *pgxpool.Pool) *MemoryService { return &MemoryService{pool: pool} }

// List returns the user's memories, newest first.
func (s *MemoryService) List(ctx context.Context, user uuid.UUID) ([]*models.Memory, error) {
	return queries.ListMemories(ctx, s.pool, user)
}

// Create saves text; created is false when the user already has that memory.
func (s *MemoryService) Create(ctx context.Context, user uuid.UUID, text string) (m *models.Memory, created bool, err error) {
	if text, err = models.CleanMemoryText(text); err != nil {
		return nil, false, badRequest("%s", err.Error())
	}
	return queries.SaveMemory(ctx, s.pool, user, text, nil)
}

// Update replaces the text of memory id.
func (s *MemoryService) Update(ctx context.Context, user, id uuid.UUID, text string) (*models.Memory, error) {
	text, err := models.CleanMemoryText(text)
	if err != nil {
		return nil, badRequest("%s", err.Error())
	}
	m, err := queries.UpdateMemoryText(ctx, s.pool, user, id, text)
	if errors.Is(err, queries.ErrDuplicate) {
		return nil, conflict("you already have that memory")
	}
	return m, err
}

// Delete removes memory id.
func (s *MemoryService) Delete(ctx context.Context, user, id uuid.UUID) error {
	return queries.DeleteMemory(ctx, s.pool, user, id)
}
