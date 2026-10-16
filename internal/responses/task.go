/*
 * Copyright (c) 2026. Papermark - All Rights Reserved
 * Unauthorized copying of this file, via any medium is strictly prohibited
 * Proprietary and confidential
 * Written by Ebot Tabi <ebot.tabi@gmail.com>, 15/10/2026
 */
package responses

import (
	"encoding/json"

	"papermark/internal/models"
	"papermark/internal/services"
	"papermark/internal/storage"
)

// UsageDTO is API.md's Usage.
type UsageDTO struct {
	InputTokens      int64 `json:"inputTokens"`
	OutputTokens     int64 `json:"outputTokens"`
	CacheReadTokens  int64 `json:"cacheReadTokens"`
	CacheWriteTokens int64 `json:"cacheWriteTokens"`
}

// TaskSummaryDTO is API.md's TaskSummary.
type TaskSummaryDTO struct {
	ID         string   `json:"id"`
	Prompt     string   `json:"prompt"`
	CreatedAt  string   `json:"createdAt"`
	UpdatedAt  string   `json:"updatedAt"`
	Status     string   `json:"status"`
	Mode       string   `json:"mode"`
	ProjectID  *string  `json:"projectId"`
	ScheduleID *string  `json:"scheduleId"`
	Favorite   bool     `json:"favorite"`
	Archived   bool     `json:"archived"`
	Usage      UsageDTO `json:"usage"`
}

// TaskDTO is a TaskSummary with its events.
type TaskDTO struct {
	TaskSummaryDTO
	Events []json.RawMessage `json:"events"`
}

func TaskSummaryResponse(t *models.Task) TaskSummaryDTO {
	var project, schedule *string
	if t.ProjectID != nil {
		s := t.ProjectID.String()
		project = &s
	}
	if t.ScheduleID != nil {
		s := t.ScheduleID.String()
		schedule = &s
	}
	return TaskSummaryDTO{
		ID: t.ID.String(), Prompt: t.Prompt, CreatedAt: Time(t.CreatedAt), UpdatedAt: Time(t.UpdatedAt),
		Status: t.Status, Mode: t.Mode, ProjectID: project, ScheduleID: schedule,
		Favorite: t.Favorite, Archived: t.Archived,
		Usage: UsageDTO{t.InputTokens, t.OutputTokens, t.CacheReadTokens, t.CacheWriteTokens},
	}
}

func TaskSummariesResponse(tasks []*models.Task) []TaskSummaryDTO {
	out := make([]TaskSummaryDTO, len(tasks))
	for i, t := range tasks {
		out[i] = TaskSummaryResponse(t)
	}
	return out
}

func TaskResponse(t *models.Task, events []*models.TaskEvent) TaskDTO {
	out := TaskDTO{TaskSummaryDTO: TaskSummaryResponse(t), Events: make([]json.RawMessage, len(events))}
	for i, e := range events {
		out.Events[i] = e.Data
	}
	return out
}

// FileDTO is one workspace file.
type FileDTO struct {
	Path       string `json:"path"`
	Size       int64  `json:"size"`
	ModifiedAt string `json:"modifiedAt"`
}

func FilesResponse(files []storage.FileInfo) []FileDTO {
	out := make([]FileDTO, len(files))
	for i, f := range files {
		out[i] = FileDTO{f.Path, f.Size, Time(f.ModifiedAt)}
	}
	return out
}

// UsageDayDTO is one day of GET /usage.
type UsageDayDTO struct {
	Date  string   `json:"date"`
	Usage UsageDTO `json:"usage"`
	Tasks int      `json:"tasks"`
}

func usage(u services.Usage) UsageDTO {
	return UsageDTO{u.InputTokens, u.OutputTokens, u.CacheReadTokens, u.CacheWriteTokens}
}

// UsageResponse is GET /usage's data.
func UsageResponse(days []services.UsageDay, total services.Usage) map[string]any {
	out := make([]UsageDayDTO, len(days))
	for i, d := range days {
		out[i] = UsageDayDTO{d.Date, usage(d.Usage), d.Tasks}
	}
	return map[string]any{"days": out, "total": usage(total)}
}
