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

	"papermark/internal/database"
	"papermark/internal/models"
	"papermark/internal/models/queries"

	"github.com/google/uuid"
)

func TestConversationSearchFindsOnlyTheUsersOtherTasks(t *testing.T) {
	ctx := context.Background()
	pool, _ := database.NewTestDatabase(t)
	user := func(sub string) uuid.UUID {
		u, _, err := queries.UpsertGoogleUser(ctx, pool, sub, sub+"@gmail.com", "Test", "")
		if err != nil {
			t.Fatal(err)
		}
		return u.ID
	}
	task := func(owner uuid.UUID, events ...string) uuid.UUID {
		tk := &models.Task{Base: models.Base{ID: uuid.New()}, UserID: owner, Prompt: events[1], Status: models.TaskRunning, Mode: "agent"}
		if err := queries.InsertTask(ctx, pool, tk); err != nil {
			t.Fatal(err)
		}
		for i := 0; i < len(events); i += 2 {
			data, _ := json.Marshal(map[string]string{"type": events[i], "text": events[i+1]})
			if _, err := queries.AppendTaskEvent(ctx, pool, tk.ID, models.TaskRunning, events[i], data); err != nil {
				t.Fatal(err)
			}
		}
		return tk.ID
	}
	me, other := user("search-me"), user("search-other")
	dentist := task(me, "message", "Book the dentist for next week", "tool", "dentist tool output", "idle", "Booked Dr Mills for Tuesday 10:00.")
	task(me, "message", "Summarise the quarterly report", "idle", "Done.")
	task(other, "message", "Call my dentist", "idle", "Called.")
	current := task(me, "message", "When is my dentist appointment?")

	out, err := conversationSearch(ctx, &Env{DB: pool, UserID: me, TaskID: current}, json.RawMessage(`{"query":"dentist appointment"}`))
	if err != nil || out.IsError {
		t.Fatalf("search = %+v, %v", out, err)
	}
	if !strings.Contains(out.Text, dentist.String()) || !strings.Contains(out.Text, "Booked Dr Mills for Tuesday 10:00.") {
		t.Fatalf("missing the dentist task or its result:\n%s", out.Text)
	}
	for _, leaked := range []string{"Called.", "quarterly", "dentist tool output", current.String()} {
		if strings.Contains(out.Text, leaked) {
			t.Fatalf("result has %q:\n%s", leaked, out.Text)
		}
	}
	if out, _ := conversationSearch(ctx, &Env{DB: pool, UserID: me, TaskID: current}, json.RawMessage(`{"query":"zebra"}`)); out.Text != "no matching past tasks" {
		t.Fatalf("no-match search = %q", out.Text)
	}
}
