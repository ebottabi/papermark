/*
 * Copyright (c) 2026. Papermark - All Rights Reserved
 * Unauthorized copying of this file, via any medium is strictly prohibited
 * Proprietary and confidential
 * Written by Ebot Tabi <ebot.tabi@gmail.com>, 15/10/2026
 */
package notifications

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"log"
	"net/http"
	"strings"
	"time"

	"papermark/internal/models/queries"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"
)

// ntfyTimeout bounds one publish.
const ntfyTimeout = 10 * time.Second

// Notifier is what the harness calls when a task needs the user.
type Notifier interface {
	Send(ctx context.Context, taskID, eventType, text string) error
}

// Fan delivers to every notifier. A failure in one is logged and does not
// stop the others; Send returns nil.
type Fan []Notifier

// Send delivers to each notifier in turn.
func (f Fan) Send(ctx context.Context, taskID, eventType, text string) error {
	for _, n := range f {
		if err := n.Send(ctx, taskID, eventType, text); err != nil {
			log.Printf("notify: %T: %v", n, err)
		}
	}
	return nil
}

// NtfyConfig points at an ntfy server. Delivery is off unless URL, TopicPrefix
// and Secret are all set.
type NtfyConfig struct {
	URL         string
	TopicPrefix string
	Secret      string
	Token       string // bearer token, optional
}

// Configured reports whether ntfy delivery is on.
func (c NtfyConfig) Configured() bool { return c.URL != "" && c.TopicPrefix != "" && c.Secret != "" }

// Topic is the user's unguessable topic: the prefix and 12 hex characters of
// sha256(userID + secret).
func (c NtfyConfig) Topic(user uuid.UUID) string {
	sum := sha256.Sum256([]byte(user.String() + c.Secret))
	return c.TopicPrefix + "-" + hex.EncodeToString(sum[:])[:12]
}

// Ntfy publishes a task's alert to its owner's ntfy topic.
type Ntfy struct {
	pool   *pgxpool.Pool
	cfg    NtfyConfig
	client *http.Client
}

// NewNtfy constructs an Ntfy sender.
func NewNtfy(pool *pgxpool.Pool, cfg NtfyConfig) *Ntfy {
	cfg.URL = strings.TrimRight(cfg.URL, "/")
	return &Ntfy{pool: pool, cfg: cfg, client: &http.Client{Timeout: ntfyTimeout}}
}

var ntfyTags = map[string]string{"confirm": "warning", "error": "x", "idle": "white_check_mark", "update": "bell", "ask": "question"}

// Send publishes text about taskID (event eventType) to the task owner's topic.
func (n *Ntfy) Send(ctx context.Context, taskID, eventType, text string) error {
	id, err := uuid.Parse(taskID)
	if err != nil {
		return fmt.Errorf("ntfy: task id: %w", err)
	}
	owner, title, err := queries.TaskOwnerAndTitle(ctx, n.pool, id)
	if err != nil {
		return fmt.Errorf("ntfy: task owner: %w", err)
	}
	if title == "" {
		title = "Keen"
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, n.cfg.URL+"/"+n.cfg.Topic(owner), strings.NewReader(truncate(text, maxAlertBytes)))
	if err != nil {
		return fmt.Errorf("ntfy: %w", err)
	}
	req.Header.Set("Title", title)
	req.Header.Set("Click", "keen://task/"+taskID)
	if tag := ntfyTags[eventType]; tag != "" {
		req.Header.Set("Tags", tag)
	}
	if eventType == "ask" || eventType == "confirm" {
		req.Header.Set("Priority", "high")
	}
	if n.cfg.Token != "" {
		req.Header.Set("Authorization", "Bearer "+n.cfg.Token)
	}
	resp, err := n.client.Do(req)
	if err != nil {
		return fmt.Errorf("ntfy: %w", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode/100 != 2 {
		b, _ := io.ReadAll(io.LimitReader(resp.Body, 200))
		return errors.New("ntfy: status " + resp.Status + ": " + strings.TrimSpace(string(b)))
	}
	return nil
}
