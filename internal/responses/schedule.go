/*
 * Copyright (c) 2026. Papermark - All Rights Reserved
 * Unauthorized copying of this file, via any medium is strictly prohibited
 * Proprietary and confidential
 * Written by Ebot Tabi <ebot.tabi@gmail.com>, 15/10/2026
 */
package responses

import (
	"encoding/json"
	"time"

	"papermark/internal/models"
)

// ScheduleDTO is API.md's ScheduledTask.
type ScheduleDTO struct {
	ID        string  `json:"id"`
	Title     string  `json:"title"`
	Kind      string  `json:"kind"`
	Prompt    string  `json:"prompt"`
	Mode      string  `json:"mode"`
	ProjectID *string `json:"projectId"`
	Repeat    *string `json:"repeat"`
	Time      *string `json:"time"`
	Weekday   *int    `json:"weekday"`
	Enabled   bool    `json:"enabled"`
	LastRunAt *string `json:"lastRunAt"`
	NextRunAt *string `json:"nextRunAt"`
	CreatedAt string  `json:"createdAt"`
	Trigger   string  `json:"trigger"`
	// Rule is the email or watch rule; null for time routines.
	Rule           json.RawMessage `json:"rule"`
	MaxRunsPerHour int             `json:"maxRunsPerHour"`
}

// TriggerEventDTO is one entry of a routine's trigger history.
type TriggerEventDTO struct {
	ID        string  `json:"id"`
	Summary   string  `json:"summary"`
	TaskID    *string `json:"taskId"`
	CreatedAt string  `json:"createdAt"`
}

// TriggerEventsResponse maps trigger events onto DTOs (never null).
func TriggerEventsResponse(es []*models.TriggerEvent) []TriggerEventDTO {
	out := make([]TriggerEventDTO, 0, len(es))
	for _, e := range es {
		d := TriggerEventDTO{ID: e.ID.String(), Summary: e.Summary, CreatedAt: Time(e.CreatedAt)}
		if e.TaskID != nil {
			id := e.TaskID.String()
			d.TaskID = &id
		}
		out = append(out, d)
	}
	return out
}

// ScheduleResponse maps a schedule onto its DTO.
func ScheduleResponse(s *models.Schedule) ScheduleDTO {
	d := ScheduleDTO{
		ID: s.ID.String(), Title: s.Title, Kind: s.Kind, Prompt: s.Prompt, Mode: s.Mode,
		Weekday: s.Weekday, Enabled: s.Enabled, LastRunAt: optTime(s.LastRunAt), NextRunAt: optTime(s.NextRunAt),
		CreatedAt: Time(s.CreatedAt), Trigger: s.Trigger, MaxRunsPerHour: s.MaxRunsPerHour,
	}
	if s.Trigger == "" || s.Trigger == "time" {
		d.Trigger, d.Repeat, d.Time = "time", &s.Repeat, &s.TimeOfDay
	} else if len(s.Rule) > 0 {
		d.Rule = s.Rule
	}
	if d.Rule == nil {
		d.Rule = json.RawMessage("null")
	}
	if s.ProjectID != nil {
		id := s.ProjectID.String()
		d.ProjectID = &id
	}
	return d
}

// SchedulesResponse maps schedules onto DTOs (never null).
func SchedulesResponse(ss []*models.Schedule) []ScheduleDTO {
	out := make([]ScheduleDTO, 0, len(ss))
	for _, s := range ss {
		out = append(out, ScheduleResponse(s))
	}
	return out
}

func optTime(t *time.Time) *string {
	if t == nil {
		return nil
	}
	s := Time(*t)
	return &s
}
