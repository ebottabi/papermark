/*
 * Copyright (c) 2026. Papermark - All Rights Reserved
 * Unauthorized copying of this file, via any medium is strictly prohibited
 * Proprietary and confidential
 * Written by Ebot Tabi <ebot.tabi@gmail.com>, 15/10/2026
 */

package models

import (
	"time"

	"github.com/google/uuid"
	"gorm.io/gorm"
)

// Base contains the common fields embedded by every mutable model:
// a UUID primary key, created/updated timestamps and a soft-delete marker.
// Append-only tables (events, messages, jobs) own their keys and never soft-delete.
type Base struct {
	ID        uuid.UUID      `gorm:"type:uuid;primary_key" json:"id"`
	CreatedAt time.Time      `gorm:"autoCreateTime" json:"created_at"`
	UpdatedAt time.Time      `gorm:"autoUpdateTime" json:"updated_at"`
	DeletedAt gorm.DeletedAt `gorm:"index" json:"deleted_at,omitempty"`
}

// BeforeCreate assigns a UUID before insert if one isn't already set.
func (b *Base) BeforeCreate(tx *gorm.DB) error {
	if b.ID == uuid.Nil {
		b.ID = uuid.New()
	}
	return nil
}

// AllModels returns models registered for GORM AutoMigrate only.
// Runtime data access is raw SQL via internal/models/queries (not GORM).
// Add a model here when you add its table.
func AllModels() []interface{} {
	return []interface{}{
		// Background job queue (outbox)
		&Job{},

		// Tasks and their logs
		&Task{},
		&TaskEvent{},
		&TaskMessage{},
		&TaskInbox{},

		// Library
		&Project{},
		&Knowledge{},
		&Skill{},

		// Push devices, schedules, MCP connectors
		&Device{},
		&Schedule{},
		&Connector{},
		&OAuthCredential{},
	}
}

// PostMigrations are idempotent statements AutoMigrate cannot express
// (partial or expression indexes). They run after AutoMigrate, in order.
func PostMigrations() []string {
	return []string{
		`CREATE UNIQUE INDEX IF NOT EXISTS jobs_active_key ON jobs (key) WHERE key <> '' AND status IN ('pending', 'running')`,
		`CREATE UNIQUE INDEX IF NOT EXISTS devices_live_token ON devices (token) WHERE deleted_at IS NULL`,
		`CREATE UNIQUE INDEX IF NOT EXISTS connectors_live_name ON connectors (name) WHERE deleted_at IS NULL`,
	}
}

// AutoMigrate runs GORM auto-migration for every registered model, then PostMigrations.
func AutoMigrate(db *gorm.DB) error {
	if err := db.AutoMigrate(AllModels()...); err != nil {
		return err
	}
	for _, stmt := range PostMigrations() {
		if err := db.Exec(stmt).Error; err != nil {
			return err
		}
	}
	return nil
}
