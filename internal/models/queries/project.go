/*
 * Copyright (c) 2026. Papermark - All Rights Reserved
 * Unauthorized copying of this file, via any medium is strictly prohibited
 * Proprietary and confidential
 * Written by Ebot Tabi <ebot.tabi@gmail.com>, 15/10/2026
 */
package queries

import (
	"context"

	"papermark/internal/models"

	"github.com/google/uuid"
)

func list[T any](ctx context.Context, db DBTX, scan func(scannable) (*T, error), sql string, args ...any) ([]*T, error) {
	rows, err := db.Query(ctx, sql, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []*T{}
	for rows.Next() {
		v, err := scan(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, v)
	}
	return out, rows.Err()
}

// deleteByID hard-deletes one of userID's rows and reports ErrNotFound when none matched.
func deleteByID(ctx context.Context, db DBTX, table string, userID, id uuid.UUID) error {
	tag, err := db.Exec(ctx, `DELETE FROM `+table+` WHERE id = $1 AND user_id = $2`, id, userID)
	if err == nil && tag.RowsAffected() == 0 {
		return ErrNotFound
	}
	return err
}

// --- projects ---

func scanProject(row scannable) (*models.Project, error) {
	var p models.Project
	if err := row.Scan(&p.ID, &p.CreatedAt, &p.UpdatedAt, &p.Name, &p.Instructions); err != nil {
		return nil, notFound(err)
	}
	return &p, nil
}

const projectColumns = `id, created_at, updated_at, name, instructions`

func InsertProject(ctx context.Context, db DBTX, userID uuid.UUID, name, instructions string) (*models.Project, error) {
	return scanProject(db.QueryRow(ctx, `INSERT INTO projects (id, user_id, created_at, updated_at, name, instructions)
		VALUES ($1, $4, now(), now(), $2, $3) RETURNING `+projectColumns, uuid.New(), name, instructions, userID))
}

func GetProject(ctx context.Context, db DBTX, userID, id uuid.UUID) (*models.Project, error) {
	return scanProject(db.QueryRow(ctx, `SELECT `+projectColumns+` FROM projects WHERE id = $1 AND user_id = $2`, id, userID))
}

func ListProjects(ctx context.Context, db DBTX, userID uuid.UUID) ([]*models.Project, error) {
	return list(ctx, db, scanProject, `SELECT `+projectColumns+` FROM projects WHERE user_id = $1 ORDER BY created_at, id`, userID)
}

// UpdateProject sets the non-nil fields.
func UpdateProject(ctx context.Context, db DBTX, userID, id uuid.UUID, name, instructions *string) (*models.Project, error) {
	return scanProject(db.QueryRow(ctx, `UPDATE projects SET name = COALESCE($2, name),
		instructions = COALESCE($3, instructions), updated_at = now() WHERE id = $1 AND user_id = $4 RETURNING `+projectColumns,
		id, name, instructions, userID))
}

func DeleteProject(ctx context.Context, db DBTX, userID, id uuid.UUID) error {
	return deleteByID(ctx, db, "projects", userID, id)
}

// --- knowledge ---

func scanKnowledge(row scannable) (*models.Knowledge, error) {
	var k models.Knowledge
	if err := row.Scan(&k.ID, &k.CreatedAt, &k.UpdatedAt, &k.Title, &k.Content, &k.Enabled); err != nil {
		return nil, notFound(err)
	}
	return &k, nil
}

const knowledgeColumns = `id, created_at, updated_at, title, content, enabled`

func InsertKnowledge(ctx context.Context, db DBTX, userID uuid.UUID, title, content string, enabled bool) (*models.Knowledge, error) {
	return scanKnowledge(db.QueryRow(ctx, `INSERT INTO knowledge (id, user_id, created_at, updated_at, title, content, enabled)
		VALUES ($1, $5, now(), now(), $2, $3, $4) RETURNING `+knowledgeColumns, uuid.New(), title, content, enabled, userID))
}

func ListKnowledge(ctx context.Context, db DBTX, userID uuid.UUID, enabledOnly bool) ([]*models.Knowledge, error) {
	return list(ctx, db, scanKnowledge, `SELECT `+knowledgeColumns+` FROM knowledge
		WHERE user_id = $2 AND (enabled OR NOT $1) ORDER BY created_at, id`, enabledOnly, userID)
}

// UpdateKnowledge sets the non-nil fields.
func UpdateKnowledge(ctx context.Context, db DBTX, userID, id uuid.UUID, title, content *string, enabled *bool) (*models.Knowledge, error) {
	return scanKnowledge(db.QueryRow(ctx, `UPDATE knowledge SET title = COALESCE($2, title),
		content = COALESCE($3, content), enabled = COALESCE($4, enabled), updated_at = now()
		WHERE id = $1 AND user_id = $5 RETURNING `+knowledgeColumns, id, title, content, enabled, userID))
}

func DeleteKnowledge(ctx context.Context, db DBTX, userID, id uuid.UUID) error {
	return deleteByID(ctx, db, "knowledge", userID, id)
}

// --- skills ---

func scanSkill(row scannable) (*models.Skill, error) {
	var s models.Skill
	if err := row.Scan(&s.ID, &s.CreatedAt, &s.UpdatedAt, &s.Name, &s.Description, &s.Instructions); err != nil {
		return nil, notFound(err)
	}
	return &s, nil
}

const skillColumns = `id, created_at, updated_at, name, description, instructions`

// InsertSkill returns ErrDuplicate when the user already has the name.
func InsertSkill(ctx context.Context, db DBTX, userID uuid.UUID, name, description, instructions string) (*models.Skill, error) {
	s, err := scanSkill(db.QueryRow(ctx, `INSERT INTO skills (id, user_id, created_at, updated_at, name, description, instructions)
		VALUES ($1, $5, now(), now(), $2, $3, $4) RETURNING `+skillColumns, uuid.New(), name, description, instructions, userID))
	return s, duplicate(err)
}

func GetSkillByName(ctx context.Context, db DBTX, userID uuid.UUID, name string) (*models.Skill, error) {
	return scanSkill(db.QueryRow(ctx, `SELECT `+skillColumns+` FROM skills WHERE user_id = $1 AND name = $2`, userID, name))
}

func ListSkills(ctx context.Context, db DBTX, userID uuid.UUID) ([]*models.Skill, error) {
	return list(ctx, db, scanSkill, `SELECT `+skillColumns+` FROM skills WHERE user_id = $1 ORDER BY name`, userID)
}

// UpdateSkill sets the non-nil fields; ErrDuplicate when the user already has the new name.
func UpdateSkill(ctx context.Context, db DBTX, userID, id uuid.UUID, name, description, instructions *string) (*models.Skill, error) {
	s, err := scanSkill(db.QueryRow(ctx, `UPDATE skills SET name = COALESCE($2, name),
		description = COALESCE($3, description), instructions = COALESCE($4, instructions), updated_at = now()
		WHERE id = $1 AND user_id = $5 RETURNING `+skillColumns, id, name, description, instructions, userID))
	return s, duplicate(err)
}

func DeleteSkill(ctx context.Context, db DBTX, userID, id uuid.UUID) error {
	return deleteByID(ctx, db, "skills", userID, id)
}
