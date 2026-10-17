/*
 * Copyright (c) 2026. Papermark - All Rights Reserved
 * Unauthorized copying of this file, via any medium is strictly prohibited
 * Proprietary and confidential
 * Written by Ebot Tabi <ebot.tabi@gmail.com>, 15/10/2026
 */
package notifications

import (
	"context"
	"net/http"
	"strings"
	"sync"
	"testing"

	"papermark/internal/database"
	"papermark/internal/models"
	"papermark/internal/models/queries"
	"papermark/internal/notifications/apns"

	"github.com/google/uuid"
)

type fakeSender struct {
	mu   sync.Mutex
	sent []string
	dead map[string]bool
}

func (f *fakeSender) Send(_ context.Context, token, env string, a apns.Alert) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.sent = append(f.sent, token+"/"+env+"/"+a.TaskID+"/"+a.Type+"/"+a.Text)
	if f.dead[token] {
		return &apns.Error{Status: http.StatusGone, Reason: "Unregistered"}
	}
	return nil
}

func TestSendReachesTheOwnersDevicesAndPrunesDeadTokens(t *testing.T) {
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
	for _, d := range []struct {
		user       uuid.UUID
		token, env string
	}{{users[0], "live", "sandbox"}, {users[0], "dead", "production"}, {users[1], "theirs", "sandbox"}} {
		if _, err := queries.UpsertDevice(ctx, pool, d.user, d.token, "ios", d.env); err != nil {
			t.Fatal(err)
		}
	}
	task := &models.Task{UserID: users[0], Prompt: "p", Status: models.TaskRunning, Mode: "quality"}
	if err := queries.InsertTask(ctx, pool, task); err != nil {
		t.Fatal(err)
	}
	id := task.ID.String()
	f := &fakeSender{dead: map[string]bool{"dead": true}}
	if err := NewPushService(pool, f).Send(ctx, id, "ask", "Which city?"); err != nil {
		t.Fatal(err)
	}
	if got := strings.Join(f.sent, ","); got != "live/sandbox/"+id+"/ask/Which city?,dead/production/"+id+"/ask/Which city?" {
		t.Fatalf("sent %s", got)
	}
	left, _ := queries.ListDevices(ctx, pool, users[0])
	if len(left) != 1 || left[0].Token != "live" {
		t.Fatalf("devices after prune: %+v", left)
	}
	if theirs, _ := queries.ListDevices(ctx, pool, users[1]); len(theirs) != 1 {
		t.Fatalf("other user's devices: %+v", theirs)
	}
}

func TestSendWithoutSenderIsNoop(t *testing.T) {
	if err := NewPushService(nil, nil).Send(context.Background(), "t", "idle", "done"); err != nil {
		t.Fatal(err)
	}
}

func TestTruncateKeepsRunesWhole(t *testing.T) {
	s := strings.Repeat("é", 600) // 1200 bytes
	got := truncate(s, maxAlertBytes)
	if !strings.HasSuffix(got, "…") || len(got) > maxAlertBytes+len("…") || !strings.HasPrefix(s, strings.TrimSuffix(got, "…")) {
		t.Fatalf("truncate: len %d", len(got))
	}
	if truncate("short", 10) != "short" {
		t.Fatal("short string changed")
	}
}
