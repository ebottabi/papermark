/*
 * Copyright (c) 2026. Papermark - All Rights Reserved
 * Unauthorized copying of this file, via any medium is strictly prohibited
 * Proprietary and confidential
 * Written by Ebot Tabi <ebot.tabi@gmail.com>, 15/10/2026
 */
package responses

import "papermark/internal/models"

type ProjectDTO struct {
	ID           string `json:"id"`
	Name         string `json:"name"`
	Instructions string `json:"instructions"`
	CreatedAt    string `json:"createdAt"`
}

func ProjectResponse(p *models.Project) ProjectDTO {
	return ProjectDTO{p.ID.String(), p.Name, p.Instructions, Time(p.CreatedAt)}
}

func ProjectsResponse(list []*models.Project) []ProjectDTO {
	out := make([]ProjectDTO, len(list))
	for i, p := range list {
		out[i] = ProjectResponse(p)
	}
	return out
}

type KnowledgeDTO struct {
	ID        string `json:"id"`
	Title     string `json:"title"`
	Content   string `json:"content"`
	Enabled   bool   `json:"enabled"`
	CreatedAt string `json:"createdAt"`
}

func KnowledgeResponse(k *models.Knowledge) KnowledgeDTO {
	return KnowledgeDTO{k.ID.String(), k.Title, k.Content, k.Enabled, Time(k.CreatedAt)}
}

func KnowledgeListResponse(list []*models.Knowledge) []KnowledgeDTO {
	out := make([]KnowledgeDTO, len(list))
	for i, k := range list {
		out[i] = KnowledgeResponse(k)
	}
	return out
}

type SkillDTO struct {
	ID           string `json:"id"`
	Name         string `json:"name"`
	Description  string `json:"description"`
	Instructions string `json:"instructions"`
	CreatedAt    string `json:"createdAt"`
}

func SkillResponse(s *models.Skill) SkillDTO {
	return SkillDTO{s.ID.String(), s.Name, s.Description, s.Instructions, Time(s.CreatedAt)}
}

func SkillsResponse(list []*models.Skill) []SkillDTO {
	out := make([]SkillDTO, len(list))
	for i, s := range list {
		out[i] = SkillResponse(s)
	}
	return out
}
