/*
 * Copyright (c) 2026. Papermark - All Rights Reserved
 * Unauthorized copying of this file, via any medium is strictly prohibited
 * Proprietary and confidential
 * Written by Ebot Tabi <ebot.tabi@gmail.com>, 17/10/2026
 */
package responses

import (
	"papermark/internal/models"
)

// PreferencesDTO is API.md's Preferences.
type PreferencesDTO struct {
	Timezone   string   `json:"timezone"`
	QuietStart string   `json:"quietStart"`
	QuietEnd   string   `json:"quietEnd"`
	PushLimit  int      `json:"pushLimit"`
	NewsTopics []string `json:"newsTopics"`
	NtfyTopic  *string  `json:"ntfyTopic"`
}

// PreferencesResponse maps preferences onto their DTO.
func PreferencesResponse(p *models.Preferences, ntfyTopic *string) PreferencesDTO {
	topics := p.NewsTopics
	if topics == nil {
		topics = []string{}
	}
	return PreferencesDTO{p.Timezone, p.QuietStart, p.QuietEnd, p.PushLimit, topics, ntfyTopic}
}

// UpdateDTO is API.md's Update.
type UpdateDTO struct {
	ID         string           `json:"id"`
	TaskID     *string          `json:"taskId"`
	ScheduleID *string          `json:"scheduleId"`
	Importance string           `json:"importance"`
	Title      string           `json:"title"`
	Body       string           `json:"body"`
	Sections   []models.Section `json:"sections"`
	Read       bool             `json:"read"`
	CreatedAt  string           `json:"createdAt"`
}

// UpdatesResponse maps updates onto DTOs (never null).
func UpdatesResponse(us []*models.Update) []UpdateDTO {
	out := make([]UpdateDTO, 0, len(us))
	for _, u := range us {
		d := UpdateDTO{ID: u.ID.String(), Importance: u.Importance, Title: u.Title,
			Body: u.Body, Sections: u.Sections, Read: u.ReadAt != nil, CreatedAt: Time(u.CreatedAt)}
		if d.Sections == nil {
			d.Sections = []models.Section{}
		}
		if u.TaskID != nil {
			id := u.TaskID.String()
			d.TaskID = &id
		}
		if u.ScheduleID != nil {
			id := u.ScheduleID.String()
			d.ScheduleID = &id
		}
		out = append(out, d)
	}
	return out
}

// MemoryDTO is API.md's Memory.
type MemoryDTO struct {
	ID           string  `json:"id"`
	Text         string  `json:"text"`
	SourceTaskID *string `json:"sourceTaskId"`
	CreatedAt    string  `json:"createdAt"`
	UpdatedAt    string  `json:"updatedAt"`
}

// MemoryResponse maps a memory onto its DTO.
func MemoryResponse(m *models.Memory) MemoryDTO {
	d := MemoryDTO{ID: m.ID.String(), Text: m.Text, CreatedAt: Time(m.CreatedAt), UpdatedAt: Time(m.UpdatedAt)}
	if m.SourceTaskID != nil {
		id := m.SourceTaskID.String()
		d.SourceTaskID = &id
	}
	return d
}

// MemoriesResponse maps memories onto DTOs (never null).
func MemoriesResponse(ms []*models.Memory) []MemoryDTO {
	out := make([]MemoryDTO, 0, len(ms))
	for _, m := range ms {
		out = append(out, MemoryResponse(m))
	}
	return out
}
