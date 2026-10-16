/*
 * Copyright (c) 2026. Papermark - All Rights Reserved
 * Unauthorized copying of this file, via any medium is strictly prohibited
 * Proprietary and confidential
 * Written by Ebot Tabi <ebot.tabi@gmail.com>, 15/10/2026
 */
package services

import (
	"context"
	"encoding/json"
	"log"

	"papermark/internal/cache"
	"papermark/internal/models"
	"papermark/internal/models/queries"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
)

// AgentEvent shapes, field for field as API.md defines them.
type (
	textEvent struct { // notify, ask, reply, stopped, error
		Type string `json:"type"`
		Text string `json:"text"`
	}
	toolEvent struct {
		Type    string          `json:"type"`
		Name    string          `json:"name"`
		Input   json.RawMessage `json:"input"`
		Output  string          `json:"output"`
		IsError bool            `json:"isError"`
	}
	confirmEvent struct {
		Type  string          `json:"type"`
		Name  string          `json:"name"`
		Input json.RawMessage `json:"input"`
	}
	decisionEvent struct {
		Type     string `json:"type"`
		Name     string `json:"name"`
		Decision string `json:"decision"`
	}
	messageEvent struct {
		Type        string   `json:"type"`
		Text        string   `json:"text"`
		Attachments []string `json:"attachments"`
	}
	idleEvent struct {
		Type  string   `json:"type"`
		Text  string   `json:"text"`
		Files []string `json:"files"`
	}
)

// statusAfter is the task status each event leaves; every other event means running.
var statusAfter = map[string]string{
	"ask":     models.TaskWaiting,
	"confirm": models.TaskWaiting,
	"idle":    models.TaskDone,
	"stopped": models.TaskStopped,
	"error":   models.TaskError,
}

// notified are the event types that reach the user's phone.
var notified = map[string]bool{"ask": true, "confirm": true, "idle": true, "error": true}

// Emit appends one event inside a taskTx.
type Emit func(event any) error

// events appends task events and fans them out after commit.
type events struct {
	pool *pgxpool.Pool
	bus  cache.Bus
	s    *Services // for the Notifier, which is set after New
}

type emitted struct {
	seq  int
	raw  json.RawMessage
	head struct{ Type, Text, Name string }
}

// taskTx runs fn in a transaction that holds the task's row lock. Events
// emitted in fn commit with fn's other writes, then go to the bus and the
// Notifier.
func (e *events) taskTx(ctx context.Context, taskID uuid.UUID, fn func(tx pgx.Tx, task *models.Task, emit Emit) error) error {
	return e.tx(ctx, e.pool, taskID, func(tx pgx.Tx) (*models.Task, error) { return queries.LockTask(ctx, tx, taskID) }, fn)
}

// createTx is taskTx for a task fn's transaction inserts first. db is the
// pool, or a caller's pgx.Tx (the transaction then nests as a savepoint and
// commits with the caller's).
func (e *events) createTx(ctx context.Context, db beginner, t *models.Task, fn func(tx pgx.Tx, task *models.Task, emit Emit) error) error {
	return e.tx(ctx, db, t.ID, func(tx pgx.Tx) (*models.Task, error) { return t, queries.InsertTask(ctx, tx, t) }, fn)
}

// beginner is *pgxpool.Pool or pgx.Tx.
type beginner interface {
	Begin(ctx context.Context) (pgx.Tx, error)
}

func (e *events) tx(ctx context.Context, db beginner, taskID uuid.UUID, load func(pgx.Tx) (*models.Task, error), fn func(tx pgx.Tx, task *models.Task, emit Emit) error) error {
	var out []emitted
	err := pgx.BeginFunc(ctx, db, func(tx pgx.Tx) error {
		out = out[:0]
		task, err := load(tx)
		if err != nil {
			return err
		}
		emit := func(event any) error {
			var em emitted
			if em.raw, err = json.Marshal(event); err != nil {
				return err
			}
			json.Unmarshal(em.raw, &em.head)
			status, ok := statusAfter[em.head.Type]
			if !ok {
				status = models.TaskRunning
			}
			if em.seq, err = queries.AppendTaskEvent(ctx, tx, taskID, status, em.head.Type, em.raw); err != nil {
				return err
			}
			task.Status = status
			out = append(out, em)
			return nil
		}
		return fn(tx, task, emit)
	})
	if err != nil {
		return err
	}
	for _, em := range out {
		e.bus.Publish(ctx, taskID.String(), cache.Frame{Kind: cache.FrameEvent, Seq: em.seq, Event: em.raw})
		if notified[em.head.Type] {
			text := em.head.Text
			if em.head.Type == "confirm" {
				text = em.head.Name
			}
			if err := e.s.Notifier.Send(ctx, taskID.String(), em.head.Type, text); err != nil {
				log.Printf("notify task %s: %v", taskID, err)
			}
		}
	}
	return nil
}
