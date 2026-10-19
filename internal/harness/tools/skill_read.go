/*
 * Copyright (c) 2026. Papermark - All Rights Reserved
 * Unauthorized copying of this file, via any medium is strictly prohibited
 * Proprietary and confidential
 * Written by Ebot Tabi <ebot.tabi@gmail.com>, 18/10/2026
 */

package tools

import (
	"context"
	"encoding/json"
	"fmt"
	"slices"
	"strings"

	"papermark/internal/harness/skills"

	"github.com/jackc/pgx/v5"
)

// SkillRead gives the agent a skill's full instructions. Its description
// lists every skill, so the model knows which exist without reading them.
func SkillRead(list []skills.Skill) Tool {
	var b strings.Builder
	b.WriteString("Read a skill: step-by-step instructions for a kind of task. Before starting a task that matches a skill, read it and follow it. Skills:")
	for _, s := range list {
		fmt.Fprintf(&b, "\n- %s: %s", s.Name, s.Description)
	}
	return Tool{
		Name:        "skill_read",
		Description: b.String(),
		Schema:      json.RawMessage(`{"type":"object","properties":{"name":{"type":"string"}},"required":["name"],"additionalProperties":false}`),
		Run:         skillRead,
	}
}

func skillRead(ctx context.Context, env *Env, input json.RawMessage) (Output, error) {
	var in struct{ Name string }
	if err := json.Unmarshal(input, &in); err != nil {
		return Output{}, err
	}
	name := strings.TrimPrefix(strings.TrimSpace(in.Name), "/")
	i := slices.IndexFunc(env.Skills, func(s skills.Skill) bool { return s.Name == name })
	if i < 0 {
		return Output{Text: fmt.Sprintf("No skill named %q.", name), IsError: true}, nil
	}
	if env.TaskTx != nil {
		err := env.TaskTx(ctx, func(_ pgx.Tx, emit func(any) error) error {
			return emit(SkillsInUse(env.Skills, []string{name}))
		})
		if err != nil {
			return Output{}, err
		}
	}
	return Output{Text: env.Skills[i].Instructions}, nil
}

// SkillUse is a skill as the app shows it: a readable title and what it does.
type SkillUse struct {
	Name        string `json:"name"`
	Title       string `json:"title"`
	Description string `json:"description"`
}

// SkillsEvent tells the app which skills Keen is following.
type SkillsEvent struct {
	Type   string     `json:"type"`
	Skills []SkillUse `json:"skills"`
}

// SkillsInUse describes names from list; a name missing from list keeps only its title.
func SkillsInUse(list []skills.Skill, names []string) SkillsEvent {
	out := make([]SkillUse, len(names))
	for i, n := range names {
		title := strings.ReplaceAll(n, "-", " ")
		if title != "" {
			title = strings.ToUpper(title[:1]) + title[1:]
		}
		out[i] = SkillUse{Name: n, Title: title}
		if j := slices.IndexFunc(list, func(s skills.Skill) bool { return s.Name == n }); j >= 0 {
			out[i].Description = list[j].Description
		}
	}
	return SkillsEvent{"skills", out}
}
