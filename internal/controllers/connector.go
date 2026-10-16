/*
 * Copyright (c) 2026. Papermark - All Rights Reserved
 * Unauthorized copying of this file, via any medium is strictly prohibited
 * Proprietary and confidential
 * Written by Ebot Tabi <ebot.tabi@gmail.com>, 15/10/2026
 */
package controllers

import (
	"errors"
	"log"
	"net/url"

	"papermark/internal/handlers"
	"papermark/internal/models"
	"papermark/internal/models/queries"
	"papermark/internal/responses"
	"papermark/internal/services"

	"github.com/gofiber/fiber/v2"
)

// ConnectorController serves /v1/connectors and the public /v1/oauth/callback.
type ConnectorController struct {
	*handlers.BaseHandler
	connectorService *services.ConnectorService
}

// NewConnectorController constructs a ConnectorController.
func NewConnectorController(cs *services.ConnectorService) *ConnectorController {
	return &ConnectorController{BaseHandler: handlers.NewBaseHandler(), connectorService: cs}
}

// AddConnectorRequest is the body of POST /v1/connectors.
type AddConnectorRequest struct {
	Name string `json:"name"`
	URL  string `json:"url"`
}

// List handles GET /v1/connectors.
func (c *ConnectorController) List(ctx *fiber.Ctx) error {
	cs, err := c.connectorService.List(ctx.Context())
	if err != nil {
		return c.SendInternalError(ctx, "failed to list connectors")
	}
	return c.SendSuccess(ctx, fiber.Map{"connectors": responses.ConnectorsResponse(cs)})
}

// Add handles POST /v1/connectors.
func (c *ConnectorController) Add(ctx *fiber.Ctx) error {
	var req AddConnectorRequest
	if err := ctx.BodyParser(&req); err != nil {
		return c.SendBadRequest(ctx, "invalid request body")
	}
	conn, err := c.connectorService.Add(ctx.Context(), req.Name, req.URL)
	switch {
	case errors.Is(err, services.ErrInvalidConnector):
		return c.SendBadRequest(ctx, err.Error())
	case errors.Is(err, queries.ErrConflict):
		return c.SendConflict(ctx, "a connector with this name exists")
	case err != nil:
		return c.SendInternalError(ctx, "failed to add connector")
	}
	return c.SendCreated(ctx, responses.ConnectorResponse(conn), "")
}

// Delete handles DELETE /v1/connectors/:name.
func (c *ConnectorController) Delete(ctx *fiber.Ctx) error {
	err := c.connectorService.Delete(ctx.Context(), ctx.Params("name"))
	switch {
	case errors.Is(err, queries.ErrNotFound):
		return c.SendNotFound(ctx, "connector not found")
	case err != nil:
		return c.SendInternalError(ctx, "failed to delete connector")
	}
	return c.SendSuccessWithMessage(ctx, nil, "connector deleted")
}

// Authorize handles POST /v1/connectors/:name/authorize.
func (c *ConnectorController) Authorize(ctx *fiber.Ctx) error {
	u, err := c.connectorService.Authorize(ctx.Context(), ctx.Params("name"))
	switch {
	case errors.Is(err, queries.ErrNotFound):
		return c.SendNotFound(ctx, "connector not found")
	case err != nil:
		return c.SendError(ctx, fiber.StatusBadGateway, "authorization unavailable: "+err.Error(), "BAD_GATEWAY")
	}
	return c.SendSuccess(ctx, fiber.Map{"authorizationUrl": u})
}

// Callback handles GET /v1/oauth/callback, the browser's return from the
// authorization server. It has no bearer auth: state authenticates it.
func (c *ConnectorController) Callback(ctx *fiber.Ctx) error {
	conn, err := c.connectorService.Callback(ctx.Context(), ctx.Query("state"), ctx.Query("code"), ctx.Query("error"))
	q := url.Values{"status": {models.ConnectorConnected}}
	if conn != nil {
		q.Set("connector", conn.Name)
	}
	if err != nil {
		log.Printf("oauth callback: %v", err)
		q.Set("status", models.ConnectorError)
	}
	return ctx.Redirect("keen://oauth?"+q.Encode(), fiber.StatusFound)
}
