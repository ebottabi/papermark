/*
 * Copyright (c) 2026. Papermark - All Rights Reserved
 * Unauthorized copying of this file, via any medium is strictly prohibited
 * Proprietary and confidential
 * Written by Ebot Tabi <ebot.tabi@gmail.com>, 15/10/2026
 */
package controllers

import (
	"encoding/json"
	"errors"

	"papermark/internal/handlers"
	"papermark/internal/middleware"
	"papermark/internal/models"
	"papermark/internal/models/queries"
	"papermark/internal/responses"
	"papermark/internal/services"

	"github.com/gofiber/fiber/v2"
	"github.com/google/uuid"
)

// ScheduleController serves /v1/schedules.
type ScheduleController struct {
	*handlers.BaseHandler
	scheduleService *services.ScheduleService
}

// NewScheduleController constructs a ScheduleController.
func NewScheduleController(ss *services.ScheduleService) *ScheduleController {
	return &ScheduleController{BaseHandler: handlers.NewBaseHandler(), scheduleService: ss}
}

// ScheduleRequest is the body of POST and PATCH /v1/schedules. Absent fields
// keep their value on PATCH; projectId and weekday accept null to clear.
type ScheduleRequest struct {
	Title     *string         `json:"title"`
	Kind      *string         `json:"kind"`
	Prompt    *string         `json:"prompt"`
	ProjectID json.RawMessage `json:"projectId"`
	Repeat    *string         `json:"repeat"`
	Time      *string         `json:"time"`
	Weekday   json.RawMessage `json:"weekday"`
	Enabled   *bool           `json:"enabled"`
	Trigger   *string         `json:"trigger"`
	// Rule null clears the rule.
	Rule json.RawMessage `json:"rule"`
}

// merge applies the fields present in r onto f.
func (r ScheduleRequest) merge(f services.ScheduleFields) (services.ScheduleFields, error) {
	if r.Title != nil {
		f.Title = *r.Title
	}
	if r.Kind != nil {
		f.Kind = *r.Kind
	}
	if r.Prompt != nil {
		f.Prompt = *r.Prompt
	}
	if r.Repeat != nil {
		f.Repeat = *r.Repeat
	}
	if r.Time != nil {
		f.Time = *r.Time
	}
	if r.Enabled != nil {
		f.Enabled = *r.Enabled
	}
	if r.Trigger != nil {
		f.Trigger = *r.Trigger
	}
	if r.Rule != nil {
		f.Rule = r.Rule
		if string(r.Rule) == "null" {
			f.Rule = nil
		}
	}
	if r.ProjectID != nil {
		var id *uuid.UUID
		if err := json.Unmarshal(r.ProjectID, &id); err != nil {
			return f, errors.New("projectId must be a UUID or null")
		}
		f.ProjectID = id
	}
	if r.Weekday != nil {
		var wd *int
		if err := json.Unmarshal(r.Weekday, &wd); err != nil {
			return f, errors.New("weekday must be 1-7 or null")
		}
		f.Weekday = wd
	}
	return f, nil
}

func fieldsOf(s *models.Schedule) services.ScheduleFields {
	return services.ScheduleFields{Title: s.Title, Kind: s.Kind, Prompt: s.Prompt, Mode: s.Mode, ProjectID: s.ProjectID, Repeat: s.Repeat,
		Time: s.TimeOfDay, Weekday: s.Weekday, Enabled: s.Enabled,
		Trigger: s.Trigger, Rule: s.Rule, MaxRunsPerHour: s.MaxRunsPerHour}
}

// List handles GET /v1/schedules.
func (c *ScheduleController) List(ctx *fiber.Ctx) error {
	ss, err := c.scheduleService.List(ctx.Context(), middleware.UserID(ctx))
	if err != nil {
		return c.SendInternalError(ctx, "failed to list schedules")
	}
	return c.SendSuccess(ctx, fiber.Map{"schedules": responses.SchedulesResponse(ss)})
}

// Create handles POST /v1/schedules.
func (c *ScheduleController) Create(ctx *fiber.Ctx) error {
	var req ScheduleRequest
	if err := ctx.BodyParser(&req); err != nil {
		return c.SendBadRequest(ctx, "invalid request body")
	}
	f, err := req.merge(services.ScheduleFields{Enabled: true})
	if err != nil {
		return c.SendBadRequest(ctx, err.Error())
	}
	s, err := c.scheduleService.Create(ctx.Context(), middleware.UserID(ctx), f)
	if err != nil {
		return c.sendErr(ctx, err)
	}
	return c.SendCreated(ctx, responses.ScheduleResponse(s), "")
}

// DraftRequest is the body of POST /v1/schedules/draft.
type DraftRequest struct {
	Text string `json:"text"`
}

// Draft handles POST /v1/schedules/draft: it turns one sentence into a routine
// the app previews before creating it with POST /v1/schedules.
func (c *ScheduleController) Draft(ctx *fiber.Ctx) error {
	var req DraftRequest
	if err := ctx.BodyParser(&req); err != nil {
		return c.SendBadRequest(ctx, "invalid request body")
	}
	d, err := c.scheduleService.Draft(ctx.Context(), middleware.UserID(ctx), req.Text)
	switch {
	case errors.Is(err, services.ErrInvalidSchedule):
		return c.SendBadRequest(ctx, err.Error())
	case errors.Is(err, services.ErrDraftUnusable):
		return c.SendError(ctx, fiber.StatusBadGateway, "Keen couldn't turn that into a routine. Try saying what to do and when.", "DRAFT_UNUSABLE")
	case err != nil:
		return c.SendError(ctx, fiber.StatusBadGateway, "Keen couldn't draft that routine right now. Try again.", "DRAFT_FAILED")
	}
	return c.SendSuccess(ctx, fiber.Map{"draft": d,
		"summary": fiber.Map{"when": d.When, "does": d.Does, "tells": d.Tells}, "question": d.Question})
}

// Update handles PATCH /v1/schedules/:id.
func (c *ScheduleController) Update(ctx *fiber.Ctx) error {
	id, err := uuid.Parse(ctx.Params("id"))
	if err != nil {
		return c.SendNotFound(ctx, "schedule not found")
	}
	var req ScheduleRequest
	if err := ctx.BodyParser(&req); err != nil {
		return c.SendBadRequest(ctx, "invalid request body")
	}
	cur, err := c.scheduleService.Get(ctx.Context(), middleware.UserID(ctx), id)
	if err != nil {
		return c.sendErr(ctx, err)
	}
	f, err := req.merge(fieldsOf(cur))
	if err != nil {
		return c.SendBadRequest(ctx, err.Error())
	}
	s, err := c.scheduleService.Update(ctx.Context(), middleware.UserID(ctx), id, f)
	if err != nil {
		return c.sendErr(ctx, err)
	}
	return c.SendSuccess(ctx, responses.ScheduleResponse(s))
}

// Delete handles DELETE /v1/schedules/:id.
func (c *ScheduleController) Delete(ctx *fiber.Ctx) error {
	id, err := uuid.Parse(ctx.Params("id"))
	if err != nil {
		return c.SendNotFound(ctx, "schedule not found")
	}
	if err := c.scheduleService.Delete(ctx.Context(), middleware.UserID(ctx), id); err != nil {
		return c.sendErr(ctx, err)
	}
	return c.SendSuccessWithMessage(ctx, nil, "schedule deleted")
}

// Run handles POST /v1/schedules/:id/run.
func (c *ScheduleController) Run(ctx *fiber.Ctx) error {
	id, err := uuid.Parse(ctx.Params("id"))
	if err != nil {
		return c.SendNotFound(ctx, "schedule not found")
	}
	taskID, err := c.scheduleService.RunNow(ctx.Context(), middleware.UserID(ctx), id)
	if err != nil {
		return c.sendErr(ctx, err)
	}
	return c.SendCreated(ctx, fiber.Map{"id": taskID}, "")
}

// Triggers handles GET /v1/schedules/:id/triggers.
func (c *ScheduleController) Triggers(ctx *fiber.Ctx) error {
	id, err := uuid.Parse(ctx.Params("id"))
	if err != nil {
		return c.SendNotFound(ctx, "schedule not found")
	}
	limit := ctx.QueryInt("limit", 30)
	if limit < 1 {
		limit = 30
	}
	limit = min(limit, 100)
	es, err := c.scheduleService.Triggers(ctx.Context(), middleware.UserID(ctx), id, limit)
	if err != nil {
		return c.sendErr(ctx, err)
	}
	return c.SendSuccess(ctx, fiber.Map{"triggers": responses.TriggerEventsResponse(es)})
}

func (c *ScheduleController) sendErr(ctx *fiber.Ctx, err error) error {
	switch {
	case errors.Is(err, services.ErrInvalidSchedule):
		return c.SendBadRequest(ctx, err.Error())
	case errors.Is(err, queries.ErrNotFound):
		return c.SendNotFound(ctx, "schedule not found")
	default:
		return c.SendInternalError(ctx, "schedule request failed")
	}
}
