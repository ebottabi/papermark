/*
 * Copyright (c) 2026. Papermark - All Rights Reserved
 * Unauthorized copying of this file, via any medium is strictly prohibited
 * Proprietary and confidential
 * Written by Ebot Tabi <ebot.tabi@gmail.com>, 16/10/2026
 */

package middleware

import (
	"context"
	"strings"

	"papermark/internal/handlers"

	"github.com/gofiber/fiber/v2"
	"github.com/google/uuid"
)

const userIDKey = "userID"

// Authenticate resolves a session bearer token to its user; any error means unauthorized.
type Authenticate func(ctx context.Context, token string) (uuid.UUID, error)

// AuthMiddleware requires "Authorization: Bearer <session token>" and puts
// the session's user id in Locals for UserID.
func AuthMiddleware(auth Authenticate) fiber.Handler {
	return func(c *fiber.Ctx) error {
		token, ok := BearerToken(c)
		if !ok {
			return unauthorized(c)
		}
		id, err := auth(c.Context(), token)
		if err != nil {
			return unauthorized(c)
		}
		c.Locals(userIDKey, id)
		return c.Next()
	}
}

// BearerToken returns the request's bearer token.
func BearerToken(c *fiber.Ctx) (string, bool) {
	token, ok := strings.CutPrefix(strings.TrimSpace(c.Get(fiber.HeaderAuthorization)), "Bearer ")
	token = strings.TrimSpace(token)
	return token, ok && token != ""
}

// UserID returns the signed-in user; uuid.Nil outside AuthMiddleware.
func UserID(c *fiber.Ctx) uuid.UUID {
	id, _ := c.Locals(userIDKey).(uuid.UUID)
	return id
}

func unauthorized(c *fiber.Ctx) error {
	return c.Status(fiber.StatusUnauthorized).JSON(handlers.ErrorResponse{
		Success: false, Error: "unauthorized", Code: "UNAUTHORIZED",
	})
}
