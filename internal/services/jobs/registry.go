/*
 * Copyright (c) 2026. Papermark - All Rights Reserved
 * Unauthorized copying of this file, via any medium is strictly prohibited
 * Proprietary and confidential
 * Written by Ebot Tabi <ebot.tabi@gmail.com>, 15/10/2026
 */

package jobs

import (
	"context"
	"fmt"

	"papermark/internal/models"
)

// Handler runs one job type. Handle must be safe to run again for the same
// job: a crash or lost lease re-delivers it. Returning an error schedules a
// retry with Backoff; ctx is cancelled on shutdown or when the lease is lost.
type Handler interface {
	Type() string
	Handle(ctx context.Context, job *models.Job) error
}

// Registry maps job types to handlers.
type Registry struct {
	handlers map[string]Handler
}

// NewRegistry returns an empty registry.
func NewRegistry() *Registry {
	return &Registry{handlers: map[string]Handler{}}
}

// Register adds h, replacing any handler for the same type.
func (r *Registry) Register(h Handler) {
	r.handlers[h.Type()] = h
}

// Handle dispatches job to its handler.
func (r *Registry) Handle(ctx context.Context, job *models.Job) error {
	h, ok := r.handlers[job.Type]
	if !ok {
		return fmt.Errorf("jobs: no handler for type %q", job.Type)
	}
	return h.Handle(ctx, job)
}
