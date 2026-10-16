/*
 * Copyright (c) 2026. Papermark - All Rights Reserved
 * Unauthorized copying of this file, via any medium is strictly prohibited
 * Proprietary and confidential
 * Written by Ebot Tabi <ebot.tabi@gmail.com>, 15/10/2026
 */

package controllers

import (
	"papermark/internal/handlers"
	"papermark/internal/responses"
	"papermark/internal/services"
	"papermark/internal/utils"

	"github.com/gofiber/fiber/v2"
)

// DeviceController serves /v1/devices.
type DeviceController struct {
	*handlers.BaseHandler
	deviceService *services.DeviceService
}

// NewDeviceController constructs a DeviceController.
func NewDeviceController(ds *services.DeviceService) *DeviceController {
	return &DeviceController{BaseHandler: handlers.NewBaseHandler(), deviceService: ds}
}

// RegisterDeviceRequest is the body of POST /v1/devices.
type RegisterDeviceRequest struct {
	Token       string `json:"token" validate:"required,max=200"`
	Platform    string `json:"platform" validate:"omitempty,oneof=ios"`
	Environment string `json:"environment" validate:"required,oneof=sandbox production"`
}

// Register handles POST /v1/devices.
func (c *DeviceController) Register(ctx *fiber.Ctx) error {
	var req RegisterDeviceRequest
	if err := ctx.BodyParser(&req); err != nil {
		return c.SendBadRequest(ctx, "invalid request body")
	}
	if err := utils.ValidateStruct(req); err != nil {
		return c.SendBadRequest(ctx, err.Error())
	}
	if req.Platform == "" {
		req.Platform = "ios"
	}
	d, err := c.deviceService.Register(ctx.Context(), req.Token, req.Platform, req.Environment)
	if err != nil {
		return c.SendInternalError(ctx, "failed to register device")
	}
	return c.SendSuccess(ctx, responses.DeviceResponse(d))
}

// Unregister handles DELETE /v1/devices/:token.
func (c *DeviceController) Unregister(ctx *fiber.Ctx) error {
	if err := c.deviceService.Unregister(ctx.Context(), ctx.Params("token")); err != nil {
		return c.SendInternalError(ctx, "failed to remove device")
	}
	return c.SendSuccessWithMessage(ctx, nil, "device removed")
}
