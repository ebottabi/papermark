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
	"strings"
	"testing"

	"papermark/internal/harness/skills"
)

func TestSkillReadListsAndReadsSkills(t *testing.T) {
	env := &Env{Skills: []skills.Skill{{Name: "inbox-triage", Description: "Sort mail.", Instructions: "1. read mail"}}}
	tool := SkillRead(env.Skills)
	if !strings.Contains(tool.Description, "\n- inbox-triage: Sort mail.") {
		t.Fatalf("description does not list the skill: %q", tool.Description)
	}
	out, err := tool.Run(context.Background(), env, json.RawMessage(`{"name":"/inbox-triage"}`))
	if err != nil || out.IsError || out.Text != "1. read mail" {
		t.Fatalf("read = %+v, %v", out, err)
	}
	out, err = tool.Run(context.Background(), env, json.RawMessage(`{"name":"nope"}`))
	if err != nil || !out.IsError {
		t.Fatalf("unknown skill = %+v, %v", out, err)
	}
}
