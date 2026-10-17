/*
 * Copyright (c) 2026. Papermark - All Rights Reserved
 * Unauthorized copying of this file, via any medium is strictly prohibited
 * Proprietary and confidential
 * Written by Ebot Tabi <ebot.tabi@gmail.com>, 17/10/2026
 */

package queries

import (
	"context"

	"papermark/internal/models"

	"github.com/google/uuid"
)

const memoryColumns = `id, user_id, text, source_task_id, created_at, updated_at`

// textKey is the memories_user_text index expression: case, spacing and ASCII
// punctuation do not make a memory different.
const textKey = `btrim(regexp_replace(lower(text), '[[:space:][:punct:]]+', ' ', 'g'))`

// anyWord turns free text into a tsquery matching any of its words, so a long
// prompt ranks memories by how many words they share instead of needing all.
const anyWord = `replace(plainto_tsquery('english', $2)::text, '&', '|')::tsquery`

func scanMemory(row scannable) (*models.Memory, error) {
	m := &models.Memory{}
	if err := row.Scan(&m.ID, &m.UserID, &m.Text, &m.SourceTaskID, &m.CreatedAt, &m.UpdatedAt); err != nil {
		return nil, notFound(err)
	}
	return m, nil
}

// SaveMemory stores text for userID. When a memory with the same normalized
// text exists it is returned (touched) with created false.
func SaveMemory(ctx context.Context, db DBTX, userID uuid.UUID, text string, sourceTask *uuid.UUID) (m *models.Memory, created bool, err error) {
	m = &models.Memory{}
	err = db.QueryRow(ctx, `INSERT INTO memories (id, user_id, text, source_task_id, created_at, updated_at)
		VALUES ($1, $2, $3, $4, now(), now())
		ON CONFLICT (user_id, (`+textKey+`)) DO UPDATE SET updated_at = now()
		RETURNING `+memoryColumns+`, xmax = 0`, uuid.New(), userID, text, sourceTask).
		Scan(&m.ID, &m.UserID, &m.Text, &m.SourceTaskID, &m.CreatedAt, &m.UpdatedAt, &created)
	return m, created, err
}

// ListMemories returns userID's memories, newest first.
func ListMemories(ctx context.Context, db DBTX, userID uuid.UUID) ([]*models.Memory, error) {
	return list(ctx, db, scanMemory, `SELECT `+memoryColumns+` FROM memories WHERE user_id = $1 ORDER BY updated_at DESC, id`, userID)
}

// SearchMemories returns up to limit of userID's memories sharing a word with
// query, best match first.
func SearchMemories(ctx context.Context, db DBTX, userID uuid.UUID, query string, limit int) ([]*models.Memory, error) {
	return list(ctx, db, scanMemory, `WITH q AS (SELECT `+anyWord+` AS q)
		SELECT `+memoryColumns+` FROM memories, q
		WHERE user_id = $1 AND to_tsvector('english', text) @@ q.q
		ORDER BY ts_rank(to_tsvector('english', text), q.q) DESC, updated_at DESC LIMIT $3`, userID, query, limit)
}

// RelevantMemories returns up to limit of userID's memories for a task
// prompt: the ones sharing words with it first, then the most recent.
func RelevantMemories(ctx context.Context, db DBTX, userID uuid.UUID, prompt string, limit int) ([]*models.Memory, error) {
	return list(ctx, db, scanMemory, `WITH q AS (SELECT `+anyWord+` AS q)
		SELECT `+memoryColumns+` FROM memories, q WHERE user_id = $1
		ORDER BY (to_tsvector('english', text) @@ q.q) DESC,
			ts_rank(to_tsvector('english', text), q.q) DESC, updated_at DESC LIMIT $3`, userID, prompt, limit)
}

// UpdateMemoryText replaces the text of one of userID's memories. ErrDuplicate
// when another memory already says the same.
func UpdateMemoryText(ctx context.Context, db DBTX, userID, id uuid.UUID, text string) (*models.Memory, error) {
	m, err := scanMemory(db.QueryRow(ctx, `UPDATE memories SET text = $3, updated_at = now()
		WHERE id = $1 AND user_id = $2 RETURNING `+memoryColumns, id, userID, text))
	return m, duplicate(err)
}

// DeleteMemory deletes one of userID's memories.
func DeleteMemory(ctx context.Context, db DBTX, userID, id uuid.UUID) error {
	return deleteByID(ctx, db, "memories", userID, id)
}
