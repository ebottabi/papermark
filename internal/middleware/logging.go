/*
 * Copyright (c) 2026. Papermark - All Rights Reserved
 * Unauthorized copying of this file, via any medium is strictly prohibited
 * Proprietary and confidential
 * Written by Ebot Tabi <ebot.tabi@gmail.com>, 15/10/2026
 */

package middleware

import "github.com/gofiber/fiber/v2"

// RequestLogger is a placeholder for structured/custom request logging.
//
// NOTE: cmd/server currently uses Fiber's built-in logger middleware. Use this
// only if you need custom fields (request id, tenant, user) or a different sink.
//
// TODO: implement structured logging (e.g. attach a request id, log tenant/user).
func RequestLogger() fiber.Handler {
	return func(c *fiber.Ctx) error {
		// TODO: pre-handler logging / request-id injection.
		return c.Next()
	}
}
