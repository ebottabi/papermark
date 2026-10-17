/*
 * Copyright (c) 2026. Papermark - All Rights Reserved
 * Unauthorized copying of this file, via any medium is strictly prohibited
 * Proprietary and confidential
 * Written by Ebot Tabi <ebot.tabi@gmail.com>, 16/10/2026
 */
package services

import (
	"context"
	"os"
	"strings"
	"testing"

	"papermark/internal/config"
	"papermark/internal/database"
	"papermark/internal/models"
	"papermark/internal/models/queries"

	"github.com/jackc/pgx/v5/pgxpool"
)

var testPool *pgxpool.Pool

func TestMain(m *testing.M) {
	os.Exit(database.RunWithTestDatabase(m, func(pool *pgxpool.Pool, _ string) { testPool = pool }))
}

func TestMissingKeyFailsTheTaskWithAClearError(t *testing.T) {
	cfg := config.Load()
	cfg.WorkspaceDir = t.TempDir()
	cfg.Agent.FireworksAPIKey = ""
	cfg.Agent.FireworksBaseURL = "http://127.0.0.1:1"
	s := New(cfg, testPool, nil)
	id, err := s.Tasks.Create(context.Background(), NewTask{UserID: testUser(t, testPool), Prompt: "go"})
	if err != nil {
		t.Fatal(err)
	}
	if err := s.Harness.ChatTurn(context.Background(), id); err != nil {
		t.Fatal(err)
	}
	task, _ := queries.GetTask(context.Background(), testPool, id)
	rows, _ := s.Tasks.Events(context.Background(), id, -1)
	if task.Status != models.TaskError || !strings.Contains(string(rows[len(rows)-1].Data), "FIREWORKS_API_KEY") {
		t.Fatalf("want an error naming the key, got %s", task.Status)
	}
}
