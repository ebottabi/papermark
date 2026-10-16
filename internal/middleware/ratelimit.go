/*
 * Copyright (c) 2026. Papermark - All Rights Reserved
 * Unauthorized copying of this file, via any medium is strictly prohibited
 * Proprietary and confidential
 * Written by Ebot Tabi <ebot.tabi@gmail.com>, 15/10/2026
 */

package middleware

import (
	"sync"
	"time"

	"github.com/gofiber/fiber/v2"
)

const (
	defaultRateLimitMax    = 20
	defaultRateLimitWindow = time.Minute
	rateLimitCleanupEvery  = 5 * time.Minute
)

// clientWindow tracks request count for a single IP within a fixed window.
type clientWindow struct {
	count int
	reset time.Time
}

// ipRateLimiter is a thread-safe fixed-window rate limiter keyed by client IP.
type ipRateLimiter struct {
	mu      sync.Mutex
	clients map[string]*clientWindow
	max     int
	window  time.Duration
}

func newIPRateLimiter(max int, window time.Duration) *ipRateLimiter {
	if max <= 0 {
		max = defaultRateLimitMax
	}
	if window <= 0 {
		window = defaultRateLimitWindow
	}
	l := &ipRateLimiter{
		clients: make(map[string]*clientWindow),
		max:     max,
		window:  window,
	}
	go l.cleanupLoop()
	return l
}

// allow reports whether the key may proceed and records the attempt when allowed.
func (l *ipRateLimiter) allow(key string) bool {
	now := time.Now()

	l.mu.Lock()
	defer l.mu.Unlock()

	cw, ok := l.clients[key]
	if !ok || now.After(cw.reset) {
		l.clients[key] = &clientWindow{count: 1, reset: now.Add(l.window)}
		return true
	}
	if cw.count >= l.max {
		return false
	}
	cw.count++
	return true
}

// cleanupLoop periodically drops expired windows to bound memory growth.
func (l *ipRateLimiter) cleanupLoop() {
	ticker := time.NewTicker(rateLimitCleanupEvery)
	defer ticker.Stop()
	for range ticker.C {
		now := time.Now()
		l.mu.Lock()
		for k, cw := range l.clients {
			if now.After(cw.reset) {
				delete(l.clients, k)
			}
		}
		l.mu.Unlock()
	}
}

// RateLimit returns Fiber middleware that throttles requests per client IP.
//
// max is the maximum number of requests allowed per window. window is the
// fixed window duration. If max <= 0 or window <= 0, defaults are
// 20 requests per minute.
//
// On limit exceeded, responds with HTTP 429 and
// {"success":false,"error":"too many requests"}.
//
// NOTE: This is an in-memory limiter (per process). For multi-instance
// deployments, prefer a Redis-backed limiter so counts are shared.
func RateLimit(max int, window time.Duration) fiber.Handler {
	limiter := newIPRateLimiter(max, window)
	return func(c *fiber.Ctx) error {
		key := c.IP()
		if key == "" {
			key = "unknown"
		}
		if !limiter.allow(key) {
			return c.Status(fiber.StatusTooManyRequests).JSON(fiber.Map{
				"success": false,
				"error":   "too many requests",
			})
		}
		return c.Next()
	}
}
