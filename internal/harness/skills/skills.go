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
	"io/fs"
	"os"
	"path"
	"path/filepath"
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
//go:embed trader
//go:embed mr-robot
var files embed.FS

// validName is the rule user skills follow too (API.md).
var validName = regexp.MustCompile(`^[a-z0-9-]{1,40}$`)

// skillsBase is where a skill's embedded scripts land on disk so the agent's
// shell can run them; ${SKILLS_DIR} in a skill resolves to its own subfolder
// here. Re-extracted on every start (a server restart gets a private tmp).
var skillsBase = filepath.Join(os.TempDir(), "keen-skills")

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

// mustLoad parses every <name>.md: a "---" block with a description line, then
// the instructions. A folder with a SKILL.md is a multi-file skill, named by
// its frontmatter or folder. It also extracts the embedded trader/ scripts to
// disk and resolves each skill's ${SKILLS_DIR} to its own extracted folder.
func mustLoad() []Skill {
	extractScripts()
	entries, err := files.ReadDir(".")
	if err != nil {
		panic(err)
	}
	var out []Skill
	for _, e := range entries {
		if e.IsDir() {
			// trader/ is a tree of script-only skills, not a SKILL.md skill.
			raw, err := files.ReadFile(path.Join(e.Name(), "SKILL.md"))
			if err != nil {
				continue
			}
			s, err := parse(e.Name(), string(raw))
			if err != nil {
				panic(fmt.Sprintf("skill %s: %v", e.Name(), err))
			}
			s.Instructions = strings.ReplaceAll(s.Instructions, "${SKILLS_DIR}", filepath.Join(skillsBase, s.Name))
			out = append(out, s)
			continue
		}
		raw, err := files.ReadFile(e.Name())
		if err != nil {
			panic(err)
		}
		s, err := parse(strings.TrimSuffix(e.Name(), path.Ext(e.Name())), string(raw))
		if err != nil {
			panic(fmt.Sprintf("skill %s: %v", e.Name(), err))
		}
		s.Instructions = strings.ReplaceAll(s.Instructions, "${SKILLS_DIR}", filepath.Join(skillsBase, s.Name))
		out = append(out, s)
	}
	return out // ReadDir sorts by file name
}

// Materialize puts the multi-file mr-robot skill in a task's workspace. Its
// files land under skills/mr-robot, exactly as upstream lays them out, and
// references, assets and scripts are also linked at the workspace root because
// the skill mixes both forms (skills/mr-robot/scripts/..., references/...).
// Never overwrites a path the task already has.
func Materialize(workspace string) error {
	root := filepath.Join(workspace, "skills", "mr-robot")
	if _, err := os.Stat(root); err != nil {
		if !os.IsNotExist(err) {
			return err
		}
		if err := os.MkdirAll(filepath.Dir(root), 0o755); err != nil {
			return err
		}
		tree, err := fs.Sub(files, "mr-robot")
		if err != nil {
			return err
		}
		if err := os.CopyFS(root, tree); err != nil {
			return err
		}
		for _, name := range []string{"scripts/backdoor-scan.sh", "scripts/race-probe.sh",
			"scripts/scan-all.sh", "scripts/score.mjs", "scripts/setup-tools.sh"} {
			if err := os.Chmod(filepath.Join(root, filepath.FromSlash(name)), 0o755); err != nil {
				return err
			}
		}
	}
	for _, dir := range []string{"references", "assets", "scripts"} {
		link := filepath.Join(workspace, dir)
		if _, err := os.Lstat(link); err == nil {
			continue
		} else if !os.IsNotExist(err) {
			return err
		}
		if err := os.Symlink(filepath.Join("skills", "mr-robot", dir), link); err != nil {
			return err
		}
	}
	return nil
}

// extractScripts writes the embedded trader/ tree under skillsBase, preserving
// subpaths, so trader/<name>/script.py lands at <skillsBase>/<name>/script.py.
// A missing or unwritable script is a build-time bug, so it panics like the rest.
func extractScripts() {
	err := fs.WalkDir(files, "trader", func(p string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		dst := filepath.Join(skillsBase, filepath.FromSlash(strings.TrimPrefix(p, "trader/")))
		if d.IsDir() {
			return os.MkdirAll(dst, 0o755)
		}
		data, err := files.ReadFile(p)
		if err != nil {
			return err
		}
		return os.WriteFile(dst, data, 0o644)
	})
	if err != nil {
		panic(err)
	}
}

func parse(name, raw string) (Skill, error) {
	if !validName.MatchString(name) {
		return Skill{}, fmt.Errorf("bad name %q", name)
	}
	head, body, ok := strings.Cut(strings.TrimPrefix(raw, "---\n"), "\n---\n")
	if !ok || !strings.HasPrefix(raw, "---\n") {
		return Skill{}, fmt.Errorf("missing --- header")
	}
	var desc string
	for _, line := range strings.Split(head, "\n") {
		key, value, ok := strings.Cut(strings.TrimSpace(line), ":")
		if !ok {
			continue
		}
		switch strings.TrimSpace(key) {
		case "name":
			name = strings.TrimSpace(value)
			if !validName.MatchString(name) {
				return Skill{}, fmt.Errorf("bad name %q", name)
			}
		case "description":
			desc = strings.TrimSpace(value)
		}
	}
	if desc == "" {
		return Skill{}, fmt.Errorf("missing description")
	}
	body = strings.TrimSpace(body)
	if body == "" {
		return Skill{}, fmt.Errorf("empty instructions")
	}
	return Skill{Name: name, Description: strings.TrimSpace(desc), Instructions: body}, nil
}
