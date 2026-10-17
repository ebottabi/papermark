/*
 * Copyright (c) 2026. Papermark - All Rights Reserved
 * Unauthorized copying of this file, via any medium is strictly prohibited
 * Proprietary and confidential
 * Written by Ebot Tabi <ebot.tabi@gmail.com>, 15/10/2026
 */

// Package services holds business logic. Services own transactions and talk
// to Postgres only through internal/models/queries.
package services

import (
	"errors"
	"fmt"
	"log"

	"papermark/internal/cache"
	"papermark/internal/config"
	"papermark/internal/harness"
	"papermark/internal/models/queries"
	"papermark/internal/notifications"
	"papermark/internal/notifications/apns"
	"papermark/internal/secretbox"
	"papermark/internal/services/jobs"

	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/redis/go-redis/v9"
)

// Services is every service the binaries share. cmd/server, cmd/worker and
// the HTTP tests build it with New so their wiring cannot drift.
type Services struct {
	Jobs      *jobs.Service
	Tasks     *TaskService
	Harness   *harness.Harness
	Projects  *ProjectService
	Knowledge *KnowledgeService
	Skills    *SkillService
	Bus       cache.Bus
	Devices   *DeviceService
	Push      *notifications.PushService
	Schedules *ScheduleService
	Prefs     *PreferenceService
	Updates   *UpdateService
	Memories  *MemoryService
	MCP       *ConnectorService
	Auth      *AuthService
}

// New wires the services. rdb may be nil (single-process mode: the bus is in-process).
func New(cfg *config.Config, pool *pgxpool.Pool, rdb *redis.Client) *Services {
	box, err := secretbox.New(cfg.TokenKey)
	if err != nil {
		log.Fatalf("services: token encryption: %v", err)
	}
	if box == nil {
		log.Println("WARNING: TOKEN_ENCRYPTION_KEY is not set; OAuth tokens are stored in plaintext")
	}
	s := &Services{
		Jobs:      jobs.NewService(pool),
		Bus:       cache.NewBus(rdb),
		Devices:   NewDeviceService(pool),
		Push:      notifications.NewPushService(pool, newAPNSSender(cfg.APNS)),
		Schedules: NewScheduleService(pool),
		MCP:       NewConnectorService(pool, cfg.PublicURL, cfg.Connectors, box),
	}
	s.Harness = harness.New(cfg.Agent, cfg.WorkspaceDir, pool, s.Bus)
	s.Harness.Connectors = s.MCP
	s.Harness.Notifier = s.Push
	s.Prefs = NewPreferenceService(pool)
	if nc := (notifications.NtfyConfig{URL: cfg.Ntfy.URL, TopicPrefix: cfg.Ntfy.TopicPrefix, Secret: cfg.Ntfy.TopicSecret, Token: cfg.Ntfy.Token}); nc.Configured() {
		s.Harness.Notifier = notifications.Fan{s.Push, notifications.NewNtfy(pool, nc)}
		s.Prefs.ntfy = &nc
	} else if cfg.Ntfy.URL != "" {
		log.Println("WARNING: ntfy is off: NTFY_TOPIC and NTFY_TOPIC_SECRET (or TOKEN_ENCRYPTION_KEY) are required")
	}
	s.Harness.Routines = s.Schedules
	s.Updates = NewUpdateService(pool)
	s.Memories = NewMemoryService(pool)
	s.Tasks = &TaskService{pool: pool, s: s}
	s.Projects = &ProjectService{pool: pool}
	s.Knowledge = &KnowledgeService{pool: pool}
	s.Skills = &SkillService{pool: pool}
	s.Schedules.SetTaskCreator(s.Tasks)
	s.Auth = NewAuthService(pool, cfg.Auth, NewGoogleVerifier(GoogleCertsURL, cfg.Auth.GoogleClientIDs))
	return s
}

// StatusError is a client error with the HTTP status a controller should send.
type StatusError struct {
	Status  int
	Message string
}

func (e *StatusError) Error() string { return e.Message }

func badRequest(format string, args ...any) error {
	return &StatusError{400, fmt.Sprintf(format, args...)}
}

func conflict(format string, args ...any) error {
	return &StatusError{409, fmt.Sprintf(format, args...)}
}

// IsNotFound reports whether err means the addressed row does not exist.
func IsNotFound(err error) bool { return errors.Is(err, queries.ErrNotFound) }

// newAPNSSender returns nil (push off) when APNS_* is unset or the key is unusable.
func newAPNSSender(c config.APNSConfig) notifications.Sender {
	cfg := apns.Config{KeyPath: c.KeyPath, KeyID: c.KeyID, TeamID: c.TeamID, Topic: c.Topic}
	if !cfg.Configured() {
		log.Println("APNs not configured; push is off")
		return nil
	}
	client, err := apns.NewClient(cfg)
	if err != nil {
		log.Printf("WARNING: push is off: %v", err)
		return nil
	}
	return client
}
