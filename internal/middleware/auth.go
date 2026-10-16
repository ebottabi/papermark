/*
 * Copyright (c) 2026. Papermark - All Rights Reserved
 * Unauthorized copying of this file, via any medium is strictly prohibited
 * Proprietary and confidential
 * Written by Ebot Tabi <ebot.tabi@gmail.com>, 15/10/2026
 */

package middleware

import (
	"crypto/subtle"
	"strings"

	"papermark/internal/handlers"

	"github.com/gofiber/fiber/v2"
)

// AuthMiddleware requires "Authorization: Bearer <token>" and compares in
// constant time. An empty token rejects every request rather than opening the API.
func AuthMiddleware(token string) fiber.Handler {
	want := []byte("Bearer " + token)
	return func(c *fiber.Ctx) error {
		got := []byte(strings.TrimSpace(c.Get(fiber.HeaderAuthorization)))
		if token == "" || subtle.ConstantTimeCompare(got, want) != 1 {
			return c.Status(fiber.StatusUnauthorized).JSON(handlers.ErrorResponse{
				Success: false, Error: "unauthorized", Code: "UNAUTHORIZED",
			})
		}
		return c.Next()
	}
}
