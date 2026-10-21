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

// TestSettleParentJoinsConcurrentChildren proves the parent-row-lock join is
// race-safe: two sub-agents finishing at the same instant both land in the
// parent's pending children with no lost update, and the parent is re-enqueued.
func TestSettleParentJoinsConcurrentChildren(t *testing.T) {
	cfg := config.Load()
	cfg.WorkspaceDir = t.TempDir()
	s := New(cfg, testPool, nil)
	ctx := context.Background()
	user := testUser(t, testPool)

	parent := &models.Task{Base: models.Base{ID: uuid.New()}, UserID: user, Prompt: "p", Status: models.TaskRunning, Mode: "quality"}
	if err := queries.InsertTask(ctx, testPool, parent); err != nil {
		t.Fatal(err)
	}

	var children []models.ChildAgent
	err := s.Harness.TaskTx(ctx, parent.ID, func(tx pgx.Tx, task *models.Task, emit harness.Emit) error {
		var e error
		children, e = s.Tasks.Spawn(ctx, tx, parent.ID, []tools.AgentSpec{{Title: "A", Prompt: "a"}, {Title: "B", Prompt: "b"}})
		if e != nil {
			return e
		}
		return queries.SetTaskRunState(ctx, tx, parent.ID, &models.PendingAction{Kind: "agents", Name: "agent", ToolUseID: "call_a", Children: children}, task.PartialResults)
	})
	if err != nil {
		t.Fatal(err)
	}
	if len(children) != 2 {
		t.Fatalf("want 2 children, got %d", len(children))
	}

	// Both children reach idle at once; their terminal transactions race to
	// write into the single parent row.
	var wg sync.WaitGroup
	for _, ch := range children {
		wg.Add(1)
		go func(c models.ChildAgent) {
			defer wg.Done()
			e := s.Harness.TaskTx(ctx, c.TaskID, func(_ pgx.Tx, _ *models.Task, emit harness.Emit) error {
				return emit(harness.IdleEvent{Type: "idle", Text: "result " + c.Title})
			})
			if e != nil {
				t.Errorf("finish child %s: %v", c.Title, e)
			}
		}(ch)
	}
	wg.Wait()

	got, err := queries.GetTask(ctx, testPool, parent.ID)
	if err != nil {
		t.Fatal(err)
	}
	if got.Pending == nil || got.Pending.Kind != "agents" {
		t.Fatalf("parent is no longer parked on agents: %+v", got.Pending)
	}
	for i, c := range got.Pending.Children {
		if !c.Done {
			t.Errorf("child %d (%s) not marked done", i, c.Title)
		}
		if c.IsError {
			t.Errorf("child %s unexpectedly errored: %q", c.Title, c.Text)
		}
		if want := "result " + c.Title; c.Text != want {
			t.Errorf("child %s text = %q, want %q", c.Title, c.Text, want)
		}
	}

	var jobs int
	if err := testPool.QueryRow(ctx,
		`SELECT count(*) FROM jobs WHERE type = $1 AND key = $2 AND status IN ('pending','running')`,
		harness.TurnJob, parent.ID.String()).Scan(&jobs); err != nil {
		t.Fatal(err)
	}
	if jobs == 0 {
		t.Error("parent was not re-enqueued after its children finished")
	}
}
