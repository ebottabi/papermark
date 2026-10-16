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
	"papermark/internal/models/queries"
	"papermark/internal/notifications/apns"
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

func TestSendReachesEveryDeviceAndPrunesDeadTokens(t *testing.T) {
	pool, _ := database.NewTestDatabase(t)
	ctx := context.Background()
	for _, d := range [][2]string{{"live", "sandbox"}, {"dead", "production"}} {
		if _, err := queries.UpsertDevice(ctx, pool, d[0], "ios", d[1]); err != nil {
			t.Fatal(err)
		}
	}
	f := &fakeSender{dead: map[string]bool{"dead": true}}
	if err := NewPushService(pool, f).Send(ctx, "task-1", "ask", "Which city?"); err != nil {
		t.Fatal(err)
	}
	if got := strings.Join(f.sent, ","); got != "live/sandbox/task-1/ask/Which city?,dead/production/task-1/ask/Which city?" {
		t.Fatalf("sent %s", got)
	}
	left, _ := queries.ListDevices(ctx, pool)
	if len(left) != 1 || left[0].Token != "live" {
		t.Fatalf("devices after prune: %+v", left)
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
