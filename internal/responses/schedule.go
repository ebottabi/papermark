/*
 * Copyright (c) 2026. Papermark - All Rights Reserved
 * Unauthorized copying of this file, via any medium is strictly prohibited
 * Proprietary and confidential
 * Written by Ebot Tabi <ebot.tabi@gmail.com>, 15/10/2026
 */
package responses

import (
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
	Repeat    string  `json:"repeat"`
	Time      string  `json:"time"`
	Weekday   *int    `json:"weekday"`
	Enabled   bool    `json:"enabled"`
	LastRunAt *string `json:"lastRunAt"`
	NextRunAt *string `json:"nextRunAt"`
	CreatedAt string  `json:"createdAt"`
}

// ScheduleResponse maps a schedule onto its DTO.
func ScheduleResponse(s *models.Schedule) ScheduleDTO {
	d := ScheduleDTO{
		ID: s.ID.String(), Title: s.Title, Kind: s.Kind, Prompt: s.Prompt, Mode: s.Mode, Repeat: s.Repeat, Time: s.TimeOfDay,
		Weekday: s.Weekday, Enabled: s.Enabled, LastRunAt: optTime(s.LastRunAt), NextRunAt: optTime(s.NextRunAt),
		CreatedAt: Time(s.CreatedAt),
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
