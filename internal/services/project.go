/*
 * Copyright (c) 2026. Papermark - All Rights Reserved
 * Unauthorized copying of this file, via any medium is strictly prohibited
 * Proprietary and confidential
 * Written by Ebot Tabi <ebot.tabi@gmail.com>, 15/10/2026
 */
package services

import (
	"context"
	"errors"
	"regexp"
	"strings"

	"papermark/internal/models"
	"papermark/internal/models/queries"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
)

func required(field string, v *string) error {
	if v != nil && strings.TrimSpace(*v) == "" {
		return badRequest("%s is required", field)
	}
	return nil
}

// ProjectService manages projects.
type ProjectService struct{ pool *pgxpool.Pool }

func (s *ProjectService) List(ctx context.Context) ([]*models.Project, error) {
	return queries.ListProjects(ctx, s.pool)
}

func (s *ProjectService) Create(ctx context.Context, name, instructions string) (*models.Project, error) {
	if err := required("name", &name); err != nil {
		return nil, err
	}
	return queries.InsertProject(ctx, s.pool, name, instructions)
}

// Update sets the non-nil fields.
func (s *ProjectService) Update(ctx context.Context, id uuid.UUID, name, instructions *string) (*models.Project, error) {
	if err := required("name", name); err != nil {
		return nil, err
	}
	return queries.UpdateProject(ctx, s.pool, id, name, instructions)
}

// Delete removes a project; its tasks stay, with no project.
func (s *ProjectService) Delete(ctx context.Context, id uuid.UUID) error {
	return pgx.BeginFunc(ctx, s.pool, func(tx pgx.Tx) error {
		if err := queries.ClearTasksProject(ctx, tx, id); err != nil {
			return err
		}
		return queries.DeleteProject(ctx, tx, id)
	})
}

// KnowledgeService manages knowledge entries.
type KnowledgeService struct{ pool *pgxpool.Pool }

func (s *KnowledgeService) List(ctx context.Context) ([]*models.Knowledge, error) {
	return queries.ListKnowledge(ctx, s.pool, false)
}

func (s *KnowledgeService) Create(ctx context.Context, title, content string, enabled bool) (*models.Knowledge, error) {
	if err := required("title", &title); err != nil {
		return nil, err
	}
	return queries.InsertKnowledge(ctx, s.pool, title, content, enabled)
}

// Update sets the non-nil fields.
func (s *KnowledgeService) Update(ctx context.Context, id uuid.UUID, title, content *string, enabled *bool) (*models.Knowledge, error) {
	if err := required("title", title); err != nil {
		return nil, err
	}
	return queries.UpdateKnowledge(ctx, s.pool, id, title, content, enabled)
}

func (s *KnowledgeService) Delete(ctx context.Context, id uuid.UUID) error {
	return queries.DeleteKnowledge(ctx, s.pool, id)
}

// SkillService manages skills.
type SkillService struct{ pool *pgxpool.Pool }

var skillName = regexp.MustCompile(`^[a-z0-9-]{1,40}$`)

func validSkillName(name *string) error {
	if name != nil && !skillName.MatchString(*name) {
		return badRequest("name must match ^[a-z0-9-]{1,40}$")
	}
	return nil
}

func skillTaken(name string, err error) error {
	if errors.Is(err, queries.ErrDuplicate) {
		return conflict("a skill named %s already exists", name)
	}
	return err
}

func (s *SkillService) List(ctx context.Context) ([]*models.Skill, error) {
	return queries.ListSkills(ctx, s.pool)
}

func (s *SkillService) Create(ctx context.Context, name, description, instructions string) (*models.Skill, error) {
	if err := validSkillName(&name); err != nil {
		return nil, err
	}
	sk, err := queries.InsertSkill(ctx, s.pool, name, description, instructions)
	return sk, skillTaken(name, err)
}

// Update sets the non-nil fields.
func (s *SkillService) Update(ctx context.Context, id uuid.UUID, name, description, instructions *string) (*models.Skill, error) {
	if err := validSkillName(name); err != nil {
		return nil, err
	}
	sk, err := queries.UpdateSkill(ctx, s.pool, id, name, description, instructions)
	if name != nil {
		err = skillTaken(*name, err)
	}
	return sk, err
}

func (s *SkillService) Delete(ctx context.Context, id uuid.UUID) error {
	return queries.DeleteSkill(ctx, s.pool, id)
}
