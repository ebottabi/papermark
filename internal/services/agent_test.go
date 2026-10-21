/*
 * Copyright (c) 2026. Papermark - All Rights Reserved
 * Unauthorized copying of this file, via any medium is strictly prohibited
 * Proprietary and confidential
 * Written by Ebot Tabi <ebot.tabi@gmail.com>, 21/10/2026
 */
package services

import (
	"context"
	"sync"
	"testing"

	"papermark/internal/config"
	"papermark/internal/harness"
	"papermark/internal/harness/tools"
	"papermark/internal/models"
	"papermark/internal/models/queries"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
)

// Two sub-agents that finish at the same instant must both land in the
// parent's pending children. The join locks the parent row, so a lost update
// here would leave one child not Done and the parent parked forever.
func TestConcurrentChildrenBothSettleIntoTheParent(t *testing.T) {
	cfg := config.Load()
	cfg.WorkspaceDir = t.TempDir()
	cfg.Agent.FireworksAPIKey = "" // no model: the test drives the children's terminal events directly
	s := New(cfg, testPool, nil)
	ctx := context.Background()

	parent, err := s.Tasks.Create(ctx, NewTask{UserID: testUser(t, testPool), Prompt: "fan out"})
	if err != nil {
		t.Fatal(err)
	}

	// Park the parent on two sub-agents, as parkAgents does.
	specs := []tools.AgentSpec{{Title: "A", Prompt: "do a"}, {Title: "B", Prompt: "do b"}}
	var children []models.ChildAgent
	err = s.Harness.TaskTx(ctx, parent, func(tx pgx.Tx, _ *models.Task, _ harness.Emit) error {
		children, err = s.Tasks.Spawn(ctx, tx, parent, specs)
		if err != nil {
			return err
		}
		return queries.SetTaskRunState(ctx, tx, parent,
			&models.PendingAction{Kind: "agents", Name: "agent", ToolUseID: "call_1", Children: children}, nil)
	})
	if err != nil {
		t.Fatal(err)
	}

	// Both children reach idle at once.
	var wg sync.WaitGroup
	for _, ch := range children {
		wg.Add(1)
		go func(id uuid.UUID, text string) {
			defer wg.Done()
			err := s.Harness.TaskTx(ctx, id, func(_ pgx.Tx, _ *models.Task, emit harness.Emit) error {
				return emit(harness.IdleEvent{Type: "idle", Text: text})
			})
			if err != nil {
				t.Errorf("child idle: %v", err)
			}
		}(ch.TaskID, ch.Title+" result")
	}
	wg.Wait()

	got, err := queries.GetTask(ctx, testPool, parent)
	if err != nil {
		t.Fatal(err)
	}
	if got.Pending == nil || got.Pending.Kind != "agents" {
		t.Fatalf("parent lost its agents pending: %+v", got.Pending)
	}
	if len(got.Pending.Children) != 2 {
		t.Fatalf("want 2 children, got %d", len(got.Pending.Children))
	}
	for _, ch := range got.Pending.Children {
		if !ch.Done {
			t.Fatalf("child %q not settled: %+v", ch.Title, ch)
		}
		if ch.IsError || ch.Text != ch.Title+" result" {
			t.Fatalf("child %q wrong outcome: %+v", ch.Title, ch)
		}
	}
}
