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
	"papermark/internal/integrations/anthropic"
	"papermark/internal/models/queries"
	"papermark/internal/notifications"
	"papermark/internal/notifications/apns"
	"papermark/internal/services/jobs"

	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/redis/go-redis/v9"
)

// Services is every service the binaries share. cmd/server, cmd/worker and
// the HTTP tests build it with New so their wiring cannot drift.
type Services struct {
	Jobs      *jobs.Service
	Tasks     *TaskService
	Agent     *AgentService
	Projects  *ProjectService
	Knowledge *KnowledgeService
	Skills    *SkillService
	Bus       cache.Bus
	Devices   *DeviceService
	Push      *notifications.PushService
	Schedules *ScheduleService
	MCP       *ConnectorService

	// Hooks other packages set after New, before serving.
	Connectors ConnectorProvider
	Notifier   Notifier
}

// New wires the services. rdb may be nil (single-process mode: the bus is in-process).
func New(cfg *config.Config, pool *pgxpool.Pool, rdb *redis.Client) *Services {
	s := &Services{
		Jobs:      jobs.NewService(pool),
		Bus:       cache.NewBus(rdb),
		Devices:   NewDeviceService(pool),
		Push:      notifications.NewPushService(pool, newAPNSSender(cfg.APNS)),
		Schedules: NewScheduleService(pool),
		MCP:       NewConnectorService(pool, cfg.PublicURL, cfg.Connectors),
	}
	ev := &events{pool: pool, bus: s.Bus, s: s}
	s.Agent = &AgentService{
		pool: pool, ev: ev, bus: s.Bus, s: s,
		client:     anthropic.New(cfg.Agent.AnthropicAPIKey, cfg.Agent.AnthropicBaseURL),
		workspaces: cfg.WorkspaceDir,
		model:      cfg.Agent.Model,
		maxTokens:  int64(cfg.Agent.MaxTokens),
		maxSteps:   cfg.Agent.MaxSteps,
	}
	s.Tasks = &TaskService{pool: pool, ev: ev, s: s}
	s.Projects = &ProjectService{pool: pool}
	s.Knowledge = &KnowledgeService{pool: pool}
	s.Skills = &SkillService{pool: pool}
	s.Connectors = s.MCP
	s.Notifier = s.Push
	s.Schedules.SetTaskCreator(s.Tasks)
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
