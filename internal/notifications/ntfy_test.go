/*
 * Copyright (c) 2026. Papermark - All Rights Reserved
 * Unauthorized copying of this file, via any medium is strictly prohibited
 * Proprietary and confidential
 * Written by Ebot Tabi <ebot.tabi@gmail.com>, 15/10/2026
 */
package notifications

import (
	"context"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"

	"papermark/internal/database"
	"papermark/internal/models"
	"papermark/internal/models/queries"

	"github.com/google/uuid"
)

type hit struct {
	path, body string
	h          http.Header
}

func ntfyServer(t *testing.T) (*httptest.Server, func() []hit) {
	var mu sync.Mutex
	var hits []hit
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		b, _ := io.ReadAll(r.Body)
		mu.Lock()
		hits = append(hits, hit{r.Method + " " + r.URL.Path, string(b), r.Header.Clone()})
		mu.Unlock()
	}))
	t.Cleanup(srv.Close)
	return srv, func() []hit { mu.Lock(); defer mu.Unlock(); return append([]hit(nil), hits...) }
}

func TestNtfyPublishesToTheOwnersTopic(t *testing.T) {
	pool, _ := database.NewTestDatabase(t)
	ctx := context.Background()
	var users [2]uuid.UUID
	for i, sub := range []string{"owner", "other"} {
		u, _, err := queries.UpsertGoogleUser(ctx, pool, sub, sub+"@gmail.com", sub, "")
		if err != nil {
			t.Fatal(err)
		}
		users[i] = u.ID
	}
	task := &models.Task{UserID: users[0], Prompt: "p", Status: models.TaskRunning, Mode: "quality"}
	if err := queries.InsertTask(ctx, pool, task); err != nil {
		t.Fatal(err)
	}
	srv, hits := ntfyServer(t)
	cfg := NtfyConfig{URL: srv.URL + "/", TopicPrefix: "keen", Secret: "s3cret", Token: "tok"}
	n := NewNtfy(pool, cfg)
	id := task.ID.String()
	for _, c := range []struct{ event, tag, prio string }{
		{"confirm", "warning", "high"}, {"ask", "question", "high"}, {"error", "x", ""}, {"idle", "white_check_mark", ""}, {"update", "bell", ""}} {
		if err := n.Send(ctx, id, c.event, "hello "+c.event); err != nil {
			t.Fatal(err)
		}
		all := hits()
		h := all[len(all)-1]
		if h.path != "POST /"+cfg.Topic(users[0]) || h.body != "hello "+c.event || h.h.Get("Title") != "Keen" ||
			h.h.Get("Click") != "keen://task/"+id || h.h.Get("Tags") != c.tag || h.h.Get("Priority") != c.prio || h.h.Get("Authorization") != "Bearer tok" {
			t.Fatalf("%s: %+v", c.event, h)
		}
	}
	if cfg.Topic(users[0]) == cfg.Topic(users[1]) || len(cfg.Topic(users[0])) != len("keen-")+12 {
		t.Fatalf("topics %s %s", cfg.Topic(users[0]), cfg.Topic(users[1]))
	}
	if other := (NtfyConfig{TopicPrefix: "keen", Secret: "x"}); other.Topic(users[0]) == cfg.Topic(users[0]) {
		t.Fatal("topic must depend on the secret")
	}
	if !cfg.Configured() || (NtfyConfig{URL: "u", TopicPrefix: "p"}).Configured() {
		t.Fatal("Configured needs url, prefix and secret")
	}
}

func TestNtfyUsesTheRoutineTitleAndOmitsEmptyToken(t *testing.T) {
	pool, _ := database.NewTestDatabase(t)
	ctx := context.Background()
	u, _, _ := queries.UpsertGoogleUser(ctx, pool, "s", "s@gmail.com", "s", "")
	sc := &models.Schedule{UserID: u.ID, Title: "Morning brief", Kind: "custom", Prompt: "p", Mode: "quality", Repeat: "daily", TimeOfDay: "07:00", Enabled: false}
	if err := queries.CreateSchedule(ctx, pool, sc); err != nil {
		t.Fatal(err)
	}
	task := &models.Task{UserID: u.ID, Prompt: "p", Status: models.TaskRunning, Mode: "quality", ScheduleID: &sc.ID}
	if err := queries.InsertTask(ctx, pool, task); err != nil {
		t.Fatal(err)
	}
	srv, hits := ntfyServer(t)
	if err := NewNtfy(pool, NtfyConfig{URL: srv.URL, TopicPrefix: "k", Secret: "s"}).Send(ctx, task.ID.String(), "idle", "done"); err != nil {
		t.Fatal(err)
	}
	if h := hits()[0]; h.h.Get("Title") != "Morning brief" || h.h.Get("Authorization") != "" {
		t.Fatalf("%+v", h)
	}
}

func TestNtfyReportsServerErrors(t *testing.T) {
	pool, _ := database.NewTestDatabase(t)
	ctx := context.Background()
	u, _, _ := queries.UpsertGoogleUser(ctx, pool, "s", "s@gmail.com", "s", "")
	task := &models.Task{UserID: u.ID, Prompt: "p", Status: models.TaskRunning, Mode: "quality"}
	if err := queries.InsertTask(ctx, pool, task); err != nil {
		t.Fatal(err)
	}
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) { http.Error(w, "nope", 403) }))
	defer srv.Close()
	err := NewNtfy(pool, NtfyConfig{URL: srv.URL, TopicPrefix: "k", Secret: "s"}).Send(ctx, task.ID.String(), "idle", "done")
	if err == nil || !strings.Contains(err.Error(), "403") {
		t.Fatalf("err %v", err)
	}
}

type stubNotifier struct {
	err  error
	sent int
}

func (s *stubNotifier) Send(context.Context, string, string, string) error { s.sent++; return s.err }

func TestFanContinuesPastAFailingNotifier(t *testing.T) {
	bad, good := &stubNotifier{err: errors.New("down")}, &stubNotifier{}
	if err := (Fan{bad, good}).Send(context.Background(), "t", "idle", "x"); err != nil {
		t.Fatal(err)
	}
	if bad.sent != 1 || good.sent != 1 {
		t.Fatalf("bad %d good %d", bad.sent, good.sent)
	}
}
