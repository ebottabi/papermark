/*
 * Copyright (c) 2026. Papermark - All Rights Reserved
 * Unauthorized copying of this file, via any medium is strictly prohibited
 * Proprietary and confidential
 * Written by Ebot Tabi <ebot.tabi@gmail.com>, 15/10/2026
 */
package harness

import (
	"context"
	"encoding/json"
	"fmt"
	"log"
	"strings"

	"papermark/internal/cache"
	"papermark/internal/harness/tools"
	"papermark/internal/models"
	"papermark/internal/models/queries"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
)

// AgentEvent shapes, field for field as API.md defines them.
type (
	TextEvent struct { // notify, ask, reply, stopped, error
		Type string `json:"type"`
		Text string `json:"text"`
	}
	ToolEvent struct {
		Type    string          `json:"type"`
		Name    string          `json:"name"`
		Input   json.RawMessage `json:"input"`
		Output  string          `json:"output"`
		IsError bool            `json:"isError"`
	}
	ConfirmEvent struct {
		Type        string          `json:"type"`
		Name        string          `json:"name"`
		Input       json.RawMessage `json:"input"`
		AllowAlways bool            `json:"allowAlways"`
	}
	DecisionEvent struct {
		Type     string `json:"type"`
		Name     string `json:"name"`
		Decision string `json:"decision"`
	}
	MessageEvent struct {
		Type        string   `json:"type"`
		Text        string   `json:"text"`
		Attachments []string `json:"attachments"`
	}
	UpdateEvent = tools.UpdateEvent // publish_update emits it
	CardEvent   = tools.CardEvent   // flight tools and checkouts emit it
	IdleEvent   struct {
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

// notified are the event types that reach the user's phone; an update only
// when it is a push.
var notified = map[string]bool{"ask": true, "confirm": true, "idle": true, "error": true, "update": true}

// Emit appends one event inside a taskTx.
type Emit func(event any) error

type emitted struct {
	seq  int
	raw  json.RawMessage
	head struct {
		Type, Text, Name, Importance, Title string
		Input                               json.RawMessage
	}
}

// TaskTx runs fn in a transaction that holds the task's row lock. Events
// emitted in fn commit with fn's other writes, then go to the bus and the
// Notifier.
func (h *Harness) TaskTx(ctx context.Context, taskID uuid.UUID, fn func(tx pgx.Tx, task *models.Task, emit Emit) error) error {
	return h.tx(ctx, h.pool, taskID, func(tx pgx.Tx) (*models.Task, error) { return queries.LockTask(ctx, tx, taskID) }, fn)
}

// CreateTx is TaskTx for a task fn's transaction inserts first. db is the
// pool, or a caller's pgx.Tx (the transaction then nests as a savepoint and
// commits with the caller's).
func (h *Harness) CreateTx(ctx context.Context, db Beginner, t *models.Task, fn func(tx pgx.Tx, task *models.Task, emit Emit) error) error {
	return h.tx(ctx, db, t.ID, func(tx pgx.Tx) (*models.Task, error) { return t, queries.InsertTask(ctx, tx, t) }, fn)
}

// Beginner is *pgxpool.Pool or pgx.Tx.
type Beginner interface {
	Begin(ctx context.Context) (pgx.Tx, error)
}

func (h *Harness) tx(ctx context.Context, db Beginner, taskID uuid.UUID, load func(pgx.Tx) (*models.Task, error), fn func(tx pgx.Tx, task *models.Task, emit Emit) error) error {
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
			switch {
			case em.head.Type == "card":
				status = task.Status // a card, even one from the app, never changes what the task is doing
			case !ok:
				status = models.TaskRunning
			}
			if em.seq, err = queries.AppendTaskEvent(ctx, tx, taskID, status, em.head.Type, em.raw); err != nil {
				return err
			}
			task.Status = status
			out = append(out, em)
			return nil
		}
		if err := fn(tx, task, emit); err != nil {
			return err
		}
		return settleRoutine(ctx, tx, task, out)
	})
	if err != nil {
		return err
	}
	for _, em := range out {
		h.bus.Publish(ctx, taskID.String(), cache.Frame{Kind: cache.FrameEvent, Seq: em.seq, Event: em.raw})
		if notified[em.head.Type] && (em.head.Type != "update" || em.head.Importance == models.ImportancePush) {
			text := em.head.Text
			switch em.head.Type {
			case "confirm":
				text = confirmAlert(em.head.Name, em.head.Input)
			case "update":
				text = em.head.Title
			}
			if err := h.Notifier.Send(ctx, taskID.String(), em.head.Type, text); err != nil {
				log.Printf("notify task %s: %v", taskID, err)
			}
		}
	}
	return nil
}

// settleRoutine counts how a routine's task ended: a success clears the
// failure streak, and the failure that completes models.RoutineFailureLimit
// turns the routine off and tells the user in their feed. It runs in the
// transaction that emitted the ending event.
func settleRoutine(ctx context.Context, tx pgx.Tx, task *models.Task, out []emitted) error {
	if task.ScheduleID == nil {
		return nil
	}
	for _, em := range out {
		if em.head.Type != "idle" && em.head.Type != "error" {
			continue
		}
		disabled, title, err := queries.RecordScheduleOutcome(ctx, tx, *task.ScheduleID, em.head.Type == "error", models.RoutineFailureLimit)
		if err != nil {
			return err
		}
		if !disabled {
			continue
		}
		u := &models.Update{UserID: task.UserID, TaskID: &task.ID, ScheduleID: task.ScheduleID, Importance: models.ImportanceFeed,
			Title: title + " turned off",
			Body:  fmt.Sprintf("%q failed %d times in a row, so it was turned off. Turn it back on when you are ready.", title, models.RoutineFailureLimit)}
		if err := queries.InsertUpdate(ctx, tx, u); err != nil {
			return err
		}
	}
	return nil
}

// confirmVerbs says what an approval is for in a push, which has no room for the card.
var confirmVerbs = map[string]string{
	"gmail_send":            "Send an email",
	"gmail_send_draft":      "Send a draft",
	"gmail_forward":         "Forward an email",
	"gmail_trash":           "Move email to Trash",
	"calendar_create_event": "Add an event",
	"calendar_update_event": "Change an event",
	"calendar_delete_event": "Delete an event",
	"calendar_respond":      "Reply to an invite",
	"routine_create":        "Create a routine",
}

// confirmAlert is an approval's push text: "Approve: Send an email · to ana@x.com".
func confirmAlert(name string, input json.RawMessage) string {
	verb, ok := confirmVerbs[name]
	if !ok {
		verb = strings.ReplaceAll(name, "_", " ")
	}
	var fields map[string]any
	json.Unmarshal(input, &fields)
	detail := ""
	for _, k := range []string{"to", "summary", "title", "subject"} {
		switch v := fields[k].(type) {
		case string:
			detail = v
		case []any:
			if len(v) > 0 {
				detail, _ = v[0].(string)
				if len(v) > 1 {
					detail += fmt.Sprintf(" +%d", len(v)-1)
				}
			}
		}
		if detail != "" {
			if k == "to" {
				detail = "to " + detail
			}
			return "Approve: " + verb + " · " + detail
		}
	}
	return "Approve: " + verb
}
