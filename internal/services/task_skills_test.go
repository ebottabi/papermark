/*
 * Copyright (c) 2026. Papermark - All Rights Reserved
 * Unauthorized copying of this file, via any medium is strictly prohibited
 * Proprietary and confidential
 * Written by Ebot Tabi <ebot.tabi@gmail.com>, 18/10/2026
 */

package services

import (
	"context"
	"strings"
	"testing"

	"papermark/internal/config"
	"papermark/internal/models/queries"

	"github.com/google/uuid"
)

func TestRoutineRunsFollowTheirSavedSkills(t *testing.T) {
	ctx := context.Background()
	cfg := config.Load()
	cfg.WorkspaceDir = t.TempDir()
	cfg.Agent.FireworksAPIKey = ""
	s := New(cfg, testPool, nil)
	user := testUser(t, testPool)
	sc, err := s.Schedules.Create(ctx, user, ScheduleFields{Prompt: "Sort my inbox and chase replies", Mode: "quality", Repeat: "daily", Time: "08:00", Enabled: true})
	if err != nil {
		t.Fatal(err)
	}

	// Without a model the pick fails, so nothing is saved and the next run picks again.
	first, err := s.Tasks.CreateTask(ctx, testPool, user, sc.Prompt, sc.Mode, nil, sc.ID)
	if err != nil {
		t.Fatal(err)
	}
	if task, _ := queries.GetTask(ctx, testPool, uuid.MustParse(first)); task != nil {
		if _, _, err := s.Harness.Preparer.Prepare(ctx, task); err != nil {
			t.Fatal(err)
		}
	}
	if got, _ := queries.GetSchedule(ctx, testPool, user, sc.ID); got.Skills != nil {
		t.Fatalf("a failed pick saved %v", got.Skills)
	}

	if err := queries.SetScheduleSkills(ctx, testPool, user, sc.ID, sc.Prompt, []string{"inbox-triage", "follow-ups"}); err != nil {
		t.Fatal(err)
	}
	id, err := s.Tasks.CreateTask(ctx, testPool, user, sc.Prompt, sc.Mode, nil, sc.ID)
	if err != nil {
		t.Fatal(err)
	}
	inbox, err := queries.DrainTaskInbox(ctx, testPool, uuid.MustParse(id))
	if err != nil || len(inbox) != 1 || inbox[0] != sc.Prompt {
		t.Fatalf("creating the task prepared its message: %v, %v", inbox, err)
	}
	task, err := queries.GetTask(ctx, testPool, uuid.MustParse(id))
	if err != nil {
		t.Fatal(err)
	}
	text, picked, err := s.Harness.Preparer.Prepare(ctx, task)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(text, `Skill "inbox-triage":`) || !strings.Contains(text, `Skill "follow-ups":`) || !strings.HasSuffix(text, sc.Prompt) {
		t.Fatalf("the run's message lacks its skills:\n%s", text)
	}
	if strings.Join(picked, ", ") != "inbox-triage, follow-ups" {
		t.Fatalf("picked = %v", picked)
	}

	if _, err := s.Schedules.Update(ctx, user, sc.ID, ScheduleFields{Prompt: "Plan my week", Mode: "quality", Repeat: "daily", Time: "08:00", Enabled: true}); err != nil {
		t.Fatal(err)
	}
	if got, _ := queries.GetSchedule(ctx, testPool, user, sc.ID); got.Skills != nil {
		t.Fatalf("a new prompt kept the old skills %v", got.Skills)
	}
}
