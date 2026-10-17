/*
 * Copyright (c) 2026. Papermark - All Rights Reserved
 * Unauthorized copying of this file, via any medium is strictly prohibited
 * Proprietary and confidential
 * Written by Ebot Tabi <ebot.tabi@gmail.com>, 15/10/2026
 */

package controllers

import (
	"papermark/internal/handlers"
	"papermark/internal/middleware"
	"papermark/internal/responses"
	"papermark/internal/services"

	"github.com/gofiber/fiber/v2"
	"github.com/google/uuid"
)

// fields reads optional string fields; the first one that is not a string
// names the error.
func (b body) fields(keys ...string) ([]*string, string) {
	out := make([]*string, len(keys))
	for i, k := range keys {
		v, ok := b.str(k)
		if !ok {
			return nil, k + " must be a string"
		}
		out[i] = v
	}
	return out, ""
}

// crud holds what the project, knowledge and skill controllers share: id
// parsing, body parsing and the not-found message.
type crud struct {
	*handlers.BaseHandler
	notFound string
}

func (h crud) fail(c *fiber.Ctx, err error) error {
	return sendServiceError(h.BaseHandler, c, err, h.notFound)
}

// parse returns the :id (when the route has one) and the body's string fields.
func (h crud) parse(c *fiber.Ctx, withID bool, keys ...string) (uuid.UUID, body, []*string, bool, error) {
	var id uuid.UUID
	if withID {
		var ok bool
		if id, ok = idParam(c); !ok {
			return id, nil, nil, false, h.SendNotFound(c, h.notFound)
		}
	}
	b, err := parseBody(c)
	if err != nil {
		return id, nil, nil, false, h.SendBadRequest(c, err.Error())
	}
	vals, bad := b.fields(keys...)
	if bad != "" {
		return id, nil, nil, false, h.SendBadRequest(c, bad)
	}
	return id, b, vals, true, nil
}

func (h crud) deleted(c *fiber.Ctx, del func(c *fiber.Ctx, id uuid.UUID) error) error {
	id, ok := idParam(c)
	if !ok {
		return h.SendNotFound(c, h.notFound)
	}
	if err := del(c, id); err != nil {
		return h.fail(c, err)
	}
	return h.SendSuccess(c, fiber.Map{"ok": true})
}

// ProjectController serves /projects.
type ProjectController struct {
	crud
	projects *services.ProjectService
}

func NewProjectController(projects *services.ProjectService) *ProjectController {
	return &ProjectController{crud{&handlers.BaseHandler{}, "no such project"}, projects}
}

func (h *ProjectController) List(c *fiber.Ctx) error {
	list, err := h.projects.List(c.Context(), middleware.UserID(c))
	if err != nil {
		return h.fail(c, err)
	}
	return h.SendSuccess(c, fiber.Map{"projects": responses.ProjectsResponse(list)})
}

func (h *ProjectController) Create(c *fiber.Ctx) error {
	_, _, f, ok, err := h.parse(c, false, "name", "instructions")
	if !ok {
		return err
	}
	p, err := h.projects.Create(c.Context(), middleware.UserID(c), deref(f[0]), deref(f[1]))
	if err != nil {
		return h.fail(c, err)
	}
	return h.SendCreated(c, responses.ProjectResponse(p), "")
}

func (h *ProjectController) Update(c *fiber.Ctx) error {
	id, _, f, ok, err := h.parse(c, true, "name", "instructions")
	if !ok {
		return err
	}
	p, err := h.projects.Update(c.Context(), middleware.UserID(c), id, f[0], f[1])
	if err != nil {
		return h.fail(c, err)
	}
	return h.SendSuccess(c, responses.ProjectResponse(p))
}

func (h *ProjectController) Delete(c *fiber.Ctx) error {
	return h.deleted(c, func(c *fiber.Ctx, id uuid.UUID) error {
		return h.projects.Delete(c.Context(), middleware.UserID(c), id)
	})
}

// KnowledgeController serves /knowledge.
type KnowledgeController struct {
	crud
	knowledge *services.KnowledgeService
}

func NewKnowledgeController(knowledge *services.KnowledgeService) *KnowledgeController {
	return &KnowledgeController{crud{&handlers.BaseHandler{}, "no such knowledge entry"}, knowledge}
}

func (h *KnowledgeController) List(c *fiber.Ctx) error {
	list, err := h.knowledge.List(c.Context(), middleware.UserID(c))
	if err != nil {
		return h.fail(c, err)
	}
	return h.SendSuccess(c, fiber.Map{"knowledge": responses.KnowledgeListResponse(list)})
}

func (h *KnowledgeController) Create(c *fiber.Ctx) error {
	_, b, f, ok, err := h.parse(c, false, "title", "content")
	if !ok {
		return err
	}
	enabled, ok := b.boolean("enabled")
	if !ok {
		return h.SendBadRequest(c, "enabled must be a boolean")
	}
	k, err := h.knowledge.Create(c.Context(), middleware.UserID(c), deref(f[0]), deref(f[1]), enabled == nil || *enabled)
	if err != nil {
		return h.fail(c, err)
	}
	return h.SendCreated(c, responses.KnowledgeResponse(k), "")
}

func (h *KnowledgeController) Update(c *fiber.Ctx) error {
	id, b, f, ok, err := h.parse(c, true, "title", "content")
	if !ok {
		return err
	}
	enabled, ok := b.boolean("enabled")
	if !ok {
		return h.SendBadRequest(c, "enabled must be a boolean")
	}
	k, err := h.knowledge.Update(c.Context(), middleware.UserID(c), id, f[0], f[1], enabled)
	if err != nil {
		return h.fail(c, err)
	}
	return h.SendSuccess(c, responses.KnowledgeResponse(k))
}

func (h *KnowledgeController) Delete(c *fiber.Ctx) error {
	return h.deleted(c, func(c *fiber.Ctx, id uuid.UUID) error {
		return h.knowledge.Delete(c.Context(), middleware.UserID(c), id)
	})
}

// SkillController serves /skills.
type SkillController struct {
	crud
	skills *services.SkillService
}

func NewSkillController(skills *services.SkillService) *SkillController {
	return &SkillController{crud{&handlers.BaseHandler{}, "no such skill"}, skills}
}

func (h *SkillController) List(c *fiber.Ctx) error {
	list, err := h.skills.List(c.Context(), middleware.UserID(c))
	if err != nil {
		return h.fail(c, err)
	}
	return h.SendSuccess(c, fiber.Map{"skills": responses.SkillsResponse(list)})
}

func (h *SkillController) Create(c *fiber.Ctx) error {
	_, _, f, ok, err := h.parse(c, false, "name", "description", "instructions")
	if !ok {
		return err
	}
	s, err := h.skills.Create(c.Context(), middleware.UserID(c), deref(f[0]), deref(f[1]), deref(f[2]))
	if err != nil {
		return h.fail(c, err)
	}
	return h.SendCreated(c, responses.SkillResponse(s), "")
}

func (h *SkillController) Update(c *fiber.Ctx) error {
	id, _, f, ok, err := h.parse(c, true, "name", "description", "instructions")
	if !ok {
		return err
	}
	s, err := h.skills.Update(c.Context(), middleware.UserID(c), id, f[0], f[1], f[2])
	if err != nil {
		return h.fail(c, err)
	}
	return h.SendSuccess(c, responses.SkillResponse(s))
}

func (h *SkillController) Delete(c *fiber.Ctx) error {
	return h.deleted(c, func(c *fiber.Ctx, id uuid.UUID) error { return h.skills.Delete(c.Context(), middleware.UserID(c), id) })
}
