/*
 * Copyright (c) 2026. Papermark - All Rights Reserved
 * Unauthorized copying of this file, via any medium is strictly prohibited
 * Proprietary and confidential
 * Written by Ebot Tabi <ebot.tabi@gmail.com>, 15/10/2026
 */

// Package workers holds the background job handlers and the registry both
// binaries run.
package workers

import (
	"papermark/internal/services"
	"papermark/internal/services/jobs"
)

// NewRegistry returns the job registry with every handler registered.
// Add a handler here when you add a job type.
func NewRegistry(s *services.Services) *jobs.Registry {
	r := jobs.NewRegistry()
	r.Register(chatTurn{s.Harness})
	return r
}
