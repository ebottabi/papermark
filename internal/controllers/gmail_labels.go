/*
 * Copyright (c) 2026. Papermark - All Rights Reserved
 * Unauthorized copying of this file, via any medium is strictly prohibited
 * Proprietary and confidential
 * Written by Ebot Tabi <ebot.tabi@gmail.com>, 17/10/2026
 */
package controllers

import (
	"errors"

	"papermark/internal/handlers"
	"papermark/internal/integrations/gmail"
	"papermark/internal/middleware"
	"papermark/internal/models/queries"
	"papermark/internal/services"

	"github.com/gofiber/fiber/v2"
	"github.com/google/uuid"
)

// GmailLabelController serves a Gmail account's labels to the routine editor.
type GmailLabelController struct {
	*handlers.BaseHandler
	labels *services.GmailLabelService
}

// NewGmailLabelController constructs a GmailLabelController.
func NewGmailLabelController(s *services.GmailLabelService) *GmailLabelController {
	return &GmailLabelController{BaseHandler: handlers.NewBaseHandler(), labels: s}
}

// List handles GET /v1/connectors/gmail/accounts/:id/labels.
func (c *GmailLabelController) List(ctx *fiber.Ctx) error {
	id, err := uuid.Parse(ctx.Params("id"))
	if err != nil {
		return c.SendNotFound(ctx, "account not found")
	}
	labels, err := c.labels.List(ctx.Context(), middleware.UserID(ctx), id)
	switch {
	case errors.Is(err, queries.ErrNotFound):
		return c.SendNotFound(ctx, "account not found")
	case errors.Is(err, gmail.ErrNeedsAuth):
		return c.SendError(ctx, fiber.StatusConflict, "the account needs to sign in again", "NEEDS_AUTH")
	case err != nil:
		return c.SendError(ctx, fiber.StatusBadGateway, "gmail unavailable", "BAD_GATEWAY")
	}
	return c.SendSuccess(ctx, fiber.Map{"labels": labels})
}
