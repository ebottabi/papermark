/*
 * Copyright (c) 2026. Papermark - All Rights Reserved
 * Unauthorized copying of this file, via any medium is strictly prohibited
 * Proprietary and confidential
 * Written by Ebot Tabi <ebot.tabi@gmail.com>, 17/10/2026
 */
package controllers

import (
	"strconv"
	"time"

	"papermark/internal/handlers"
	"papermark/internal/middleware"
	"papermark/internal/responses"
	"papermark/internal/services"

	"github.com/gofiber/fiber/v2"
	"github.com/google/uuid"
)

// PreferencesController serves /v1/me/preferences.
type PreferencesController struct {
	*handlers.BaseHandler
	prefs *services.PreferenceService
}

// NewPreferencesController constructs a PreferencesController.
func NewPreferencesController(p *services.PreferenceService) *PreferencesController {
	return &PreferencesController{BaseHandler: handlers.NewBaseHandler(), prefs: p}
}

// Get handles GET /v1/me/preferences.
func (c *PreferencesController) Get(ctx *fiber.Ctx) error {
	p, err := c.prefs.Get(ctx.Context(), middleware.UserID(ctx))
	if err != nil {
		return sendServiceError(c.BaseHandler, ctx, err, "no such user")
	}
	return c.SendSuccess(ctx, responses.PreferencesResponse(p, c.prefs.NtfyTopic(middleware.UserID(ctx))))
}

// Patch handles PATCH /v1/me/preferences.
func (c *PreferencesController) Patch(ctx *fiber.Ctx) error {
	var req struct {
		Timezone   *string   `json:"timezone"`
		QuietStart *string   `json:"quietStart"`
		QuietEnd   *string   `json:"quietEnd"`
		PushLimit  *int      `json:"pushLimit"`
		NewsTopics *[]string `json:"newsTopics"`
	}
	if err := ctx.BodyParser(&req); err != nil {
		return c.SendBadRequest(ctx, "invalid request body")
	}
	p, err := c.prefs.Update(ctx.Context(), middleware.UserID(ctx), services.PreferencesPatch{
		Timezone: req.Timezone, QuietStart: req.QuietStart, QuietEnd: req.QuietEnd, PushLimit: req.PushLimit, NewsTopics: req.NewsTopics})
	if err != nil {
		return sendServiceError(c.BaseHandler, ctx, err, "no such user")
	}
	return c.SendSuccess(ctx, responses.PreferencesResponse(p, c.prefs.NtfyTopic(middleware.UserID(ctx))))
}

// UpdateController serves /v1/updates.
type UpdateController struct {
	*handlers.BaseHandler
	updates *services.UpdateService
}

// NewUpdateController constructs an UpdateController.
func NewUpdateController(u *services.UpdateService) *UpdateController {
	return &UpdateController{BaseHandler: handlers.NewBaseHandler(), updates: u}
}

const (
	defaultUpdatesLimit = 30
	maxUpdatesLimit     = 100
)

// List handles GET /v1/updates?before=&limit=.
func (c *UpdateController) List(ctx *fiber.Ctx) error {
	var before *time.Time
	if s := ctx.Query("before"); s != "" {
		t, err := time.Parse(time.RFC3339, s)
		if err != nil {
			return c.SendBadRequest(ctx, "before must be an RFC 3339 time")
		}
		before = &t
	}
	limit := defaultUpdatesLimit
	if s := ctx.Query("limit"); s != "" {
		n, err := strconv.Atoi(s)
		if err != nil || n < 1 || n > maxUpdatesLimit {
			return c.SendBadRequest(ctx, "limit must be 1 to 100")
		}
		limit = n
	}
	us, err := c.updates.List(ctx.Context(), middleware.UserID(ctx), before, limit)
	if err != nil {
		return c.SendInternalError(ctx, "failed to list updates")
	}
	return c.SendSuccess(ctx, fiber.Map{"updates": responses.UpdatesResponse(us)})
}

// Read handles POST /v1/updates/:id/read.
func (c *UpdateController) Read(ctx *fiber.Ctx) error {
	id, err := uuid.Parse(ctx.Params("id"))
	if err != nil {
		return c.SendNotFound(ctx, "update not found")
	}
	if err := c.updates.MarkRead(ctx.Context(), middleware.UserID(ctx), id); err != nil {
		return sendServiceError(c.BaseHandler, ctx, err, "update not found")
	}
	return c.SendNoContent(ctx)
}

// ReadAll handles POST /v1/updates/read-all.
func (c *UpdateController) ReadAll(ctx *fiber.Ctx) error {
	if err := c.updates.MarkAllRead(ctx.Context(), middleware.UserID(ctx)); err != nil {
		return c.SendInternalError(ctx, "failed to mark updates read")
	}
	return c.SendNoContent(ctx)
}

// MemoryController serves /v1/memories.
type MemoryController struct {
	*handlers.BaseHandler
	memories *services.MemoryService
}

// NewMemoryController constructs a MemoryController.
func NewMemoryController(m *services.MemoryService) *MemoryController {
	return &MemoryController{BaseHandler: handlers.NewBaseHandler(), memories: m}
}

type memoryRequest struct {
	Text string `json:"text"`
}

// List handles GET /v1/memories.
func (c *MemoryController) List(ctx *fiber.Ctx) error {
	ms, err := c.memories.List(ctx.Context(), middleware.UserID(ctx))
	if err != nil {
		return c.SendInternalError(ctx, "failed to list memories")
	}
	return c.SendSuccess(ctx, fiber.Map{"memories": responses.MemoriesResponse(ms)})
}

// Create handles POST /v1/memories. Saving text the user already has returns
// that memory with 200.
func (c *MemoryController) Create(ctx *fiber.Ctx) error {
	var req memoryRequest
	if err := ctx.BodyParser(&req); err != nil {
		return c.SendBadRequest(ctx, "invalid request body")
	}
	m, created, err := c.memories.Create(ctx.Context(), middleware.UserID(ctx), req.Text)
	if err != nil {
		return sendServiceError(c.BaseHandler, ctx, err, "memory not found")
	}
	if !created {
		return c.SendSuccess(ctx, responses.MemoryResponse(m))
	}
	return c.SendCreated(ctx, responses.MemoryResponse(m), "")
}

// Update handles PATCH /v1/memories/:id.
func (c *MemoryController) Update(ctx *fiber.Ctx) error {
	id, err := uuid.Parse(ctx.Params("id"))
	if err != nil {
		return c.SendNotFound(ctx, "memory not found")
	}
	var req memoryRequest
	if err := ctx.BodyParser(&req); err != nil {
		return c.SendBadRequest(ctx, "invalid request body")
	}
	m, err := c.memories.Update(ctx.Context(), middleware.UserID(ctx), id, req.Text)
	if err != nil {
		return sendServiceError(c.BaseHandler, ctx, err, "memory not found")
	}
	return c.SendSuccess(ctx, responses.MemoryResponse(m))
}

// Delete handles DELETE /v1/memories/:id.
func (c *MemoryController) Delete(ctx *fiber.Ctx) error {
	id, err := uuid.Parse(ctx.Params("id"))
	if err != nil {
		return c.SendNotFound(ctx, "memory not found")
	}
	if err := c.memories.Delete(ctx.Context(), middleware.UserID(ctx), id); err != nil {
		return sendServiceError(c.BaseHandler, ctx, err, "memory not found")
	}
	return c.SendNoContent(ctx)
}
