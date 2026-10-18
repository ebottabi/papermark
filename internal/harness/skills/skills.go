/*
 * Copyright (c) 2026. Papermark - All Rights Reserved
 * Unauthorized copying of this file, via any medium is strictly prohibited
 * Proprietary and confidential
 * Written by Ebot Tabi <ebot.tabi@gmail.com>, 18/10/2026
 */

// Package skills holds the built-in skills: step-by-step instructions for a
// kind of task, one Markdown file each, that the agent reads before it starts.
package skills

import (
	"context"
	"embed"
	"fmt"
	"path"
	"regexp"
	"slices"
	"strings"

	"papermark/internal/models/queries"

	"github.com/google/uuid"
)

// Skill is a named instruction set, built in or written by the user.
type Skill struct {
	Name         string
	Description  string
	Instructions string
}

//go:embed *.md
var files embed.FS

// validName is the rule user skills follow too (API.md).
var validName = regexp.MustCompile(`^[a-z0-9-]{1,40}$`)

var builtin = mustLoad()

// Builtin returns the built-in skills, sorted by name.
func Builtin() []Skill { return slices.Clone(builtin) }

// Find returns the built-in skill with the name.
func Find(name string) (Skill, bool) {
	i := slices.IndexFunc(builtin, func(s Skill) bool { return s.Name == name })
	if i < 0 {
		return Skill{}, false
	}
	return builtin[i], true
}

// For returns the skills a user's agent can read, sorted by name: the
// built-in ones and the user's own, which replace a built-in of the same name.
func For(ctx context.Context, db queries.DBTX, userID uuid.UUID) ([]Skill, error) {
	own, err := queries.ListSkills(ctx, db, userID)
	if err != nil {
		return nil, err
	}
	out := Builtin()
	for _, s := range own {
		mine := Skill{Name: s.Name, Description: s.Description, Instructions: s.Instructions}
		if i := slices.IndexFunc(out, func(b Skill) bool { return b.Name == s.Name }); i >= 0 {
			out[i] = mine
		} else {
			out = append(out, mine)
		}
	}
	slices.SortFunc(out, func(a, b Skill) int { return strings.Compare(a.Name, b.Name) })
	return out, nil
}

// mustLoad parses every <name>.md: a "---" block with a description line, then the instructions.
func mustLoad() []Skill {
	entries, err := files.ReadDir(".")
	if err != nil {
		panic(err)
	}
	var out []Skill
	for _, e := range entries {
		raw, err := files.ReadFile(e.Name())
		if err != nil {
			panic(err)
		}
		s, err := parse(strings.TrimSuffix(e.Name(), path.Ext(e.Name())), string(raw))
		if err != nil {
			panic(fmt.Sprintf("skill %s: %v", e.Name(), err))
		}
		out = append(out, s)
	}
	return out // ReadDir sorts by file name
}

func parse(name, raw string) (Skill, error) {
	if !validName.MatchString(name) {
		return Skill{}, fmt.Errorf("bad name %q", name)
	}
	head, body, ok := strings.Cut(strings.TrimPrefix(raw, "---\n"), "\n---\n")
	if !ok || !strings.HasPrefix(raw, "---\n") {
		return Skill{}, fmt.Errorf("missing --- header")
	}
	desc, ok := strings.CutPrefix(strings.TrimSpace(head), "description:")
	if !ok || strings.TrimSpace(desc) == "" {
		return Skill{}, fmt.Errorf("missing description")
	}
	body = strings.TrimSpace(body)
	if body == "" {
		return Skill{}, fmt.Errorf("empty instructions")
	}
	return Skill{Name: name, Description: strings.TrimSpace(desc), Instructions: body}, nil
}
