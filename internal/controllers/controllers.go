/*
 * Copyright (c) 2026. Papermark - All Rights Reserved
 * Unauthorized copying of this file, via any medium is strictly prohibited
 * Proprietary and confidential
 * Written by Ebot Tabi <ebot.tabi@gmail.com>, 15/10/2026
 */

// Package controllers turns HTTP requests into service calls and service
// results into the API.md shapes.
package controllers

import (
	"encoding/json"
	"errors"

	"papermark/internal/handlers"
	"papermark/internal/services"

	"github.com/gofiber/fiber/v2"
	"github.com/google/uuid"
)

// sendServiceError maps a service error to its status: a StatusError keeps
// its own, a missing row is 404 with notFound, anything else is 500.
func sendServiceError(h *handlers.BaseHandler, c *fiber.Ctx, err error, notFound string) error {
	var se *services.StatusError
	switch {
	case errors.As(err, &se):
		return h.SendError(c, se.Status, se.Message, handlers.CodeForStatus(se.Status))
	case services.IsNotFound(err):
		return h.SendNotFound(c, notFound)
	}
	return h.SendInternalError(c, err.Error())
}

// body is a JSON object body; an empty body is {}. Fields are type-checked
// one by one so errors name the field, as API.md's clients expect.
type body map[string]any

func parseBody(c *fiber.Ctx) (body, error) {
	b := body{}
	if len(c.Body()) == 0 {
		return b, nil
	}
	if err := json.Unmarshal(c.Body(), &b); err != nil {
		return nil, errors.New("body must be a JSON object")
	}
	return b, nil
}

// str returns the string at key; present reports whether the key was sent, ok whether it is a string.
func (b body) str(key string) (v *string, ok bool) {
	raw, present := b[key]
	if !present {
		return nil, true
	}
	s, ok := raw.(string)
	return &s, ok
}

func (b body) boolean(key string) (v *bool, ok bool) {
	raw, present := b[key]
	if !present {
		return nil, true
	}
	x, ok := raw.(bool)
	return &x, ok
}

// raw returns the value at key as JSON, or nil when absent.
func (b body) raw(key string) json.RawMessage {
	v, present := b[key]
	if !present {
		return nil
	}
	out, _ := json.Marshal(v)
	return out
}

func idParam(c *fiber.Ctx) (uuid.UUID, bool) {
	id, err := uuid.Parse(c.Params("id"))
	return id, err == nil
}

func deref(s *string) string {
	if s == nil {
		return ""
	}
	return *s
}
