/*
 * Copyright (c) 2026. Papermark - All Rights Reserved
 * Unauthorized copying of this file, via any medium is strictly prohibited
 * Proprietary and confidential
 * Written by Ebot Tabi <ebot.tabi@gmail.com>, 15/10/2026
 */
package queries

import (
	"context"
	"encoding/json"
	"time"

	"papermark/internal/models"

	"github.com/google/uuid"
)

const taskColumns = `id, user_id, created_at, updated_at, prompt, status, mode, project_id, schedule_id, favorite, archived,
	input_tokens, output_tokens, cache_read_tokens, cache_write_tokens, event_count,
	pending, partial_results, always_allowed, stop_requested`

func scanTask(row scannable) (*models.Task, error) {
	var t models.Task
	err := row.Scan(&t.ID, &t.UserID, &t.CreatedAt, &t.UpdatedAt, &t.Prompt, &t.Status, &t.Mode, &t.ProjectID, &t.ScheduleID,
		&t.Favorite, &t.Archived, &t.InputTokens, &t.OutputTokens, &t.CacheReadTokens, &t.CacheWriteTokens,
		&t.EventCount, &t.Pending, &t.PartialResults, &t.AlwaysAllowed, &t.StopRequested)
	if err != nil {
		return nil, notFound(err)
	}
	return &t, nil
}

func scanTasks(db DBTX, ctx context.Context, sql string, args ...any) ([]*models.Task, error) {
	rows, err := db.Query(ctx, sql, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []*models.Task
	for rows.Next() {
		t, err := scanTask(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, t)
	}
	return out, rows.Err()
}

// InsertTask inserts t (ID, UserID, Prompt, Status, Mode, ProjectID, ScheduleID).
func InsertTask(ctx context.Context, db DBTX, t *models.Task) error {
	if t.ID == uuid.Nil {
		t.ID = uuid.New()
	}
	row := db.QueryRow(ctx, `
		INSERT INTO tasks (id, user_id, created_at, updated_at, prompt, status, mode, project_id, schedule_id)
		VALUES ($1, $7, now(), now(), $2, $3, $4, $5, $6)
		RETURNING `+taskColumns,
		t.ID, t.Prompt, t.Status, t.Mode, t.ProjectID, t.ScheduleID, t.UserID)
	got, err := scanTask(row)
	if err != nil {
		return err
	}
	*t = *got
	return nil
}

// GetUserTask returns one of userID's tasks; another user's task is ErrNotFound.
func GetUserTask(ctx context.Context, db DBTX, userID, id uuid.UUID) (*models.Task, error) {
	return scanTask(db.QueryRow(ctx, `SELECT `+taskColumns+` FROM tasks WHERE id = $1 AND user_id = $2`, id, userID))
}

// GetTask returns one task whatever its owner; the harness uses it for a task it already holds.
func GetTask(ctx context.Context, db DBTX, id uuid.UUID) (*models.Task, error) {
	return scanTask(db.QueryRow(ctx, `SELECT `+taskColumns+` FROM tasks WHERE id = $1`, id))
}

// LockTask returns one task, locking its row until the transaction ends. Every
// write that depends on a task's status takes this lock first.
func LockTask(ctx context.Context, db DBTX, id uuid.UUID) (*models.Task, error) {
	return scanTask(db.QueryRow(ctx, `SELECT `+taskColumns+` FROM tasks WHERE id = $1 FOR UPDATE`, id))
}

// ListTasks returns userID's tasks newest updated_at first, filtered by
// archived and (when projectID is non-nil) project.
func ListTasks(ctx context.Context, db DBTX, userID uuid.UUID, archived bool, projectID *uuid.UUID) ([]*models.Task, error) {
	return scanTasks(db, ctx, `SELECT `+taskColumns+` FROM tasks
		WHERE user_id = $3 AND archived = $1 AND ($2::uuid IS NULL OR project_id = $2)
		ORDER BY updated_at DESC, id`, archived, projectID, userID)
}

// ListTasksCreatedSince returns userID's tasks created at or after since.
func ListTasksCreatedSince(ctx context.Context, db DBTX, userID uuid.UUID, since time.Time) ([]*models.Task, error) {
	return scanTasks(db, ctx, `SELECT `+taskColumns+` FROM tasks WHERE user_id = $1 AND created_at >= $2`, userID, since)
}

// UpdateTaskFlags sets favorite and archived on one of userID's tasks (nil
// leaves a flag unchanged).
func UpdateTaskFlags(ctx context.Context, db DBTX, userID, id uuid.UUID, favorite, archived *bool) (*models.Task, error) {
	return scanTask(db.QueryRow(ctx, `
		UPDATE tasks SET favorite = COALESCE($2, favorite), archived = COALESCE($3, archived), updated_at = now()
		WHERE id = $1 AND user_id = $4 RETURNING `+taskColumns, id, favorite, archived, userID))
}

// SetTaskRunState stores what a run needs to resume: the pending call and the
// results of the calls before it.
func SetTaskRunState(ctx context.Context, db DBTX, id uuid.UUID, pending *models.PendingAction, partial []json.RawMessage) error {
	if partial == nil {
		partial = []json.RawMessage{}
	}
	_, err := db.Exec(ctx, `UPDATE tasks SET pending = $2, partial_results = $3 WHERE id = $1`, id, pending, partial)
	return err
}

// SetTaskAlwaysAllowed replaces the tool names the user approved for the rest of the task.
func SetTaskAlwaysAllowed(ctx context.Context, db DBTX, id uuid.UUID, names []string) error {
	_, err := db.Exec(ctx, `UPDATE tasks SET always_allowed = $2 WHERE id = $1`, id, names)
	return err
}

// SetTaskStopRequested sets or clears the stop flag a running task's worker checks.
func SetTaskStopRequested(ctx context.Context, db DBTX, id uuid.UUID, stop bool) error {
	_, err := db.Exec(ctx, `UPDATE tasks SET stop_requested = $2 WHERE id = $1`, id, stop)
	return err
}

// AddTaskUsage adds one response's token counts.
func AddTaskUsage(ctx context.Context, db DBTX, id uuid.UUID, input, output, cacheRead, cacheWrite int64) error {
	_, err := db.Exec(ctx, `UPDATE tasks SET input_tokens = input_tokens + $2, output_tokens = output_tokens + $3,
		cache_read_tokens = cache_read_tokens + $4, cache_write_tokens = cache_write_tokens + $5, updated_at = now()
		WHERE id = $1`, id, input, output, cacheRead, cacheWrite)
	return err
}

// ClearTasksProject detaches every task from a deleted project.
func ClearTasksProject(ctx context.Context, db DBTX, projectID uuid.UUID) error {
	_, err := db.Exec(ctx, `UPDATE tasks SET project_id = NULL WHERE project_id = $1`, projectID)
	return err
}

// AppendTaskEvent appends an event and sets the task's status in one
// statement pair; run it in the transaction that holds the task lock. It
// returns the event's sequence number.
func AppendTaskEvent(ctx context.Context, db DBTX, taskID uuid.UUID, status, eventType string, data json.RawMessage) (int, error) {
	var seq int
	err := db.QueryRow(ctx, `UPDATE tasks SET event_count = event_count + 1, status = $2, updated_at = now()
		WHERE id = $1 RETURNING event_count - 1`, taskID, status).Scan(&seq)
	if err != nil {
		return 0, notFound(err)
	}
	_, err = db.Exec(ctx, `INSERT INTO task_events (task_id, seq, type, data, created_at) VALUES ($1, $2, $3, $4, now())`,
		taskID, seq, eventType, string(data))
	return seq, err
}

// ListTaskEvents returns the events with seq > after, in order.
func ListTaskEvents(ctx context.Context, db DBTX, taskID uuid.UUID, after int) ([]*models.TaskEvent, error) {
	rows, err := db.Query(ctx, `SELECT task_id, seq, type, data::text, created_at FROM task_events
		WHERE task_id = $1 AND seq > $2 ORDER BY seq`, taskID, after)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []*models.TaskEvent
	for rows.Next() {
		var e models.TaskEvent
		var data string
		if err := rows.Scan(&e.TaskID, &e.Seq, &e.Type, &data, &e.CreatedAt); err != nil {
			return nil, err
		}
		e.Data = json.RawMessage(data)
		out = append(out, &e)
	}
	return out, rows.Err()
}

// AppendTaskMessage appends one model message; run it under the task lock.
func AppendTaskMessage(ctx context.Context, db DBTX, taskID uuid.UUID, message json.RawMessage) error {
	_, err := db.Exec(ctx, `INSERT INTO task_messages (task_id, seq, message, created_at)
		SELECT $1, COALESCE(MAX(seq) + 1, 0), $2::json, now() FROM task_messages WHERE task_id = $1`, taskID, string(message))
	return err
}

// ListTaskMessages returns a task's model conversation in order, byte for byte.
func ListTaskMessages(ctx context.Context, db DBTX, taskID uuid.UUID) ([]json.RawMessage, error) {
	rows, err := db.Query(ctx, `SELECT message::text FROM task_messages WHERE task_id = $1 ORDER BY seq`, taskID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []json.RawMessage
	for rows.Next() {
		var m string
		if err := rows.Scan(&m); err != nil {
			return nil, err
		}
		out = append(out, json.RawMessage(m))
	}
	return out, rows.Err()
}

// CopyTaskMessages copies one task's conversation onto another (fork).
func CopyTaskMessages(ctx context.Context, db DBTX, from, to uuid.UUID) error {
	_, err := db.Exec(ctx, `INSERT INTO task_messages (task_id, seq, message, created_at)
		SELECT $2, seq, message, now() FROM task_messages WHERE task_id = $1`, from, to)
	return err
}

// InsertTaskInbox queues a message for the model's next user turn.
func InsertTaskInbox(ctx context.Context, db DBTX, taskID uuid.UUID, text string) error {
	_, err := db.Exec(ctx, `INSERT INTO task_inbox (task_id, text, created_at) VALUES ($1, $2, now())`, taskID, text)
	return err
}

// DrainTaskInbox removes and returns the queued messages, oldest first.
func DrainTaskInbox(ctx context.Context, db DBTX, taskID uuid.UUID) ([]string, error) {
	rows, err := db.Query(ctx, `WITH d AS (DELETE FROM task_inbox WHERE task_id = $1 RETURNING id, text)
		SELECT text FROM d ORDER BY id`, taskID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []string
	for rows.Next() {
		var s string
		if err := rows.Scan(&s); err != nil {
			return nil, err
		}
		out = append(out, s)
	}
	return out, rows.Err()
}
