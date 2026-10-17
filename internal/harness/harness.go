/*
 * Copyright (c) 2026. Papermark - All Rights Reserved
 * Unauthorized copying of this file, via any medium is strictly prohibited
 * Proprietary and confidential
 * Written by Ebot Tabi <ebot.tabi@gmail.com>, 16/10/2026
 */
// Package harness runs the agent loop. A task advances one chat turn per
// job: ChatTurn loads the task from Postgres, builds a turn, and the turn
// calls the model and runs its tools until it answers, parks on the user,
// or is stopped. Every step is stored, so a retried job resumes the turn.
// The package also owns the task event log (TaskTx) both it and the HTTP
// side append to.
package harness

import (
	"context"
	"encoding/json"
	"errors"
	"log"
	"papermark/internal/config"
	"path/filepath"

	"papermark/internal/cache"
	"papermark/internal/integrations/fireworks"
	"papermark/internal/models"
	"papermark/internal/models/queries"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
)

// TurnJob is the job type that runs a task's chat turn; its key is the task
// id, so a task has at most one active turn.
const TurnJob = "chat.turn"

// TurnPayload is the payload of a TurnJob.
type TurnPayload struct {
	TaskID uuid.UUID `json:"taskId"`
}

// Harness runs chat turns as durable jobs. A turn is idempotent: it
// rebuilds everything from Postgres, so a retried or re-enqueued job
// continues where the last one stopped.
type Harness struct {
	// Connectors, Notifier and Routines may be replaced after New, before
	// serving. Without Routines the agent has no routine_create tool.
	Connectors ConnectorProvider
	Notifier   Notifier
	Routines   RoutineCreator

	pool       *pgxpool.Pool
	bus        cache.Bus
	client     *fireworks.Client
	apiKeySet  bool
	web        *web
	workspaces string
	// Every turn runs on qualityModel; speedModel is its fallback.
	qualityModel, speedModel string
	maxTokens                int
	maxSteps                 int
	routineMaxSteps          int
	contextBudget            int
}

// New returns a harness whose tasks keep their files under workspaces.
func New(cfg config.AgentConfig, workspaces string, pool *pgxpool.Pool, bus cache.Bus) *Harness {
	return &Harness{
		Connectors:      noConnectors{},
		Notifier:        noNotifier{},
		pool:            pool,
		bus:             bus,
		client:          fireworks.New(cfg.FireworksAPIKey, cfg.FireworksBaseURL, cfg.ModelTimeout, cfg.ModelMaxRetries),
		apiKeySet:       cfg.FireworksAPIKey != "",
		web:             newWeb(cfg.TavilyAPIKey, []string{cfg.PublicHost}),
		workspaces:      workspaces,
		qualityModel:    cfg.QualityModel,
		speedModel:      cfg.SpeedModel,
		maxTokens:       cfg.MaxTokens,
		maxSteps:        cfg.MaxSteps,
		routineMaxSteps: cfg.RoutineMaxSteps,
		contextBudget:   cfg.ContextBudget,
	}
}

// Workspace returns a task's workspace directory.
func (h *Harness) Workspace(id uuid.UUID) string {
	return filepath.Join(h.workspaces, id.String())
}

// ChatTurn advances a running task until it finishes, parks on the user, is
// stopped, or ctx ends (worker shutdown; the job is then retried).
func (h *Harness) ChatTurn(ctx context.Context, taskID uuid.UUID) error {
	ctx, cancel := context.WithCancelCause(ctx)
	defer cancel(nil)
	frames, unsubscribe, err := h.bus.Subscribe(ctx, taskID.String())
	if err != nil {
		return err
	}
	defer unsubscribe()
	go func() {
		for f := range frames {
			if f.Kind == cache.FrameStop {
				cancel(errStopped)
			}
		}
	}()
	// Read after subscribing, so a stop is seen either here or on the bus.
	task, err := queries.GetTask(ctx, h.pool, taskID)
	if errors.Is(err, queries.ErrNotFound) {
		return nil
	}
	if err != nil {
		return err
	}
	if task.Status != models.TaskRunning {
		return nil
	}
	if task.StopRequested {
		cancel(errStopped)
	}

	err = h.runTurn(ctx, task)
	if errors.Is(context.Cause(ctx), errStopped) {
		return h.stopped(context.WithoutCancel(ctx), taskID)
	}
	if ctx.Err() != nil {
		return ctx.Err()
	}
	if err != nil {
		log.Printf("agent: task %s failed: %v", taskID, err)
		return h.TaskTx(ctx, taskID, func(_ pgx.Tx, t *models.Task, emit Emit) error {
			if t.Status != models.TaskRunning {
				return nil
			}
			return emit(TextEvent{"error", err.Error()})
		})
	}
	return nil
}

func (h *Harness) stopped(ctx context.Context, taskID uuid.UUID) error {
	return h.TaskTx(ctx, taskID, func(tx pgx.Tx, t *models.Task, emit Emit) error {
		if err := queries.SetTaskStopRequested(ctx, tx, taskID, false); err != nil {
			return err
		}
		if t.Status != models.TaskRunning {
			return nil
		}
		return emit(TextEvent{"stopped", "Stopped by the user."})
	})
}

// Complete runs one tool-free call on the speed model, falling back to the
// quality model, and returns the reply text. Callers that classify (smart
// email rules) use it; it records no task usage.
func (h *Harness) Complete(ctx context.Context, system, prompt string, maxTokens int) (string, error) {
	if !h.apiKeySet {
		return "", errors.New("the model is not configured")
	}
	msg, err := userMessage(prompt)
	if err != nil {
		return "", err
	}
	req := fireworks.Request{System: system, Messages: []json.RawMessage{msg}, MaxTokens: maxTokens, Effort: "low"}
	for _, m := range []string{h.speedModel, h.qualityModel} {
		req.Model = m
		res, err := h.client.Stream(ctx, req, func(fireworks.Delta) {})
		if err == nil {
			if res.Message.Content == nil {
				return "", errors.New("the model returned no text")
			}
			return *res.Message.Content, nil
		}
		if !fireworks.Fallback(err) {
			return "", err
		}
	}
	return "", errors.New("the model is unavailable")
}
