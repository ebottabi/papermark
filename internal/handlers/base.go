/*
 * Copyright (c) 2026. Papermark - All Rights Reserved
 * Unauthorized copying of this file, via any medium is strictly prohibited
 * Proprietary and confidential
 * Written by Ebot Tabi <ebot.tabi@gmail.com>, 15/10/2026
 */

// Package handlers provides the BaseHandler with shared HTTP response helpers
// that controllers embed. Response envelope types live here too.
package handlers

import (
	"log"

	"github.com/gofiber/fiber/v2"
)

// BaseHandler carries response helpers shared by all controllers.
type BaseHandler struct{}

// NewBaseHandler returns a BaseHandler.
func NewBaseHandler() *BaseHandler { return &BaseHandler{} }

// --- Response envelopes ---

type SuccessResponse struct {
	Success bool        `json:"success"`
	Data    interface{} `json:"data,omitempty"`
	Message string      `json:"message,omitempty"`
}

type ErrorResponse struct {
	Success bool   `json:"success"`
	Error   string `json:"error"`
	Code    string `json:"code,omitempty"`
}

type PaginatedResponse struct {
	Success    bool        `json:"success"`
	Data       interface{} `json:"data"`
	Pagination Pagination  `json:"pagination"`
}

type Pagination struct {
	Page       int   `json:"page"`
	PerPage    int   `json:"per_page"`
	Total      int64 `json:"total"`
	TotalPages int   `json:"total_pages"`
}

// --- Success helpers ---

func (h *BaseHandler) SendSuccess(c *fiber.Ctx, data interface{}) error {
	return c.Status(fiber.StatusOK).JSON(SuccessResponse{Success: true, Data: data})
}

func (h *BaseHandler) SendSuccessWithMessage(c *fiber.Ctx, data interface{}, msg string) error {
	return c.Status(fiber.StatusOK).JSON(SuccessResponse{Success: true, Data: data, Message: msg})
}

func (h *BaseHandler) SendCreated(c *fiber.Ctx, data interface{}, msg string) error {
	return c.Status(fiber.StatusCreated).JSON(SuccessResponse{Success: true, Data: data, Message: msg})
}

// SendAccepted answers 202: the work continues in the background.
func (h *BaseHandler) SendAccepted(c *fiber.Ctx, data interface{}) error {
	return c.Status(fiber.StatusAccepted).JSON(SuccessResponse{Success: true, Data: data})
}

func (h *BaseHandler) SendNoContent(c *fiber.Ctx) error {
	return c.SendStatus(fiber.StatusNoContent)
}

func (h *BaseHandler) SendPaginated(c *fiber.Ctx, data interface{}, page, perPage int, total int64) error {
	totalPages := int(total) / max(perPage, 1)
	if int(total)%max(perPage, 1) > 0 {
		totalPages++
	}
	return c.Status(fiber.StatusOK).JSON(PaginatedResponse{
		Success:    true,
		Data:       data,
		Pagination: Pagination{Page: page, PerPage: perPage, Total: total, TotalPages: totalPages},
	})
}

// --- Error helpers ---

func (h *BaseHandler) SendError(c *fiber.Ctx, status int, msg, code string) error {
	logAPIError(c, status, code, msg)
	return c.Status(status).JSON(ErrorResponse{Success: false, Error: msg, Code: code})
}

func (h *BaseHandler) SendBadRequest(c *fiber.Ctx, msg string) error {
	return h.SendError(c, fiber.StatusBadRequest, msg, "BAD_REQUEST")
}

func (h *BaseHandler) SendUnauthorized(c *fiber.Ctx, msg string) error {
	return h.SendError(c, fiber.StatusUnauthorized, msg, "UNAUTHORIZED")
}

func (h *BaseHandler) SendForbidden(c *fiber.Ctx, msg string) error {
	return h.SendError(c, fiber.StatusForbidden, msg, "FORBIDDEN")
}

func (h *BaseHandler) SendNotFound(c *fiber.Ctx, msg string) error {
	return h.SendError(c, fiber.StatusNotFound, msg, "NOT_FOUND")
}

func (h *BaseHandler) SendConflict(c *fiber.Ctx, msg string) error {
	return h.SendError(c, fiber.StatusConflict, msg, "CONFLICT")
}

func (h *BaseHandler) SendInternalError(c *fiber.Ctx, msg string) error {
	return h.SendError(c, fiber.StatusInternalServerError, msg, "INTERNAL_ERROR")
}

func (h *BaseHandler) SendNotImplemented(c *fiber.Ctx) error {
	return h.SendError(c, fiber.StatusNotImplemented, "not implemented", "NOT_IMPLEMENTED")
}

// logAPIError writes a structured line for every 4xx/5xx so log drains show
// the real failure (status alone from Fiber logger is not enough).
func logAPIError(c *fiber.Ctx, status int, code, msg string) {
	if c == nil {
		log.Printf("API status=%d code=%s err=%s", status, code, msg)
		return
	}
	level := "warn"
	if status >= 500 {
		level = "error"
	}
	log.Printf("API %s status=%d method=%s path=%s code=%s err=%q",
		level, status, c.Method(), c.Path(), code, msg)
}

// CodeForStatus maps an HTTP status onto the envelope error code.
func CodeForStatus(status int) string {
	switch status {
	case fiber.StatusBadRequest:
		return "BAD_REQUEST"
	case fiber.StatusUnauthorized:
		return "UNAUTHORIZED"
	case fiber.StatusForbidden:
		return "FORBIDDEN"
	case fiber.StatusNotFound:
		return "NOT_FOUND"
	case fiber.StatusConflict:
		return "CONFLICT"
	case fiber.StatusRequestEntityTooLarge:
		return "PAYLOAD_TOO_LARGE"
	default:
		return "INTERNAL_ERROR"
	}
}

func max(a, b int) int {
	if a > b {
		return a
	}
	return b
}
