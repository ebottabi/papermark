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
		// Accounts
		&User{},
		&Session{},
		&Passkey{},
		&DeviceKey{},
		&AuthChallenge{},

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
		&GmailAccount{},

		// Routine triggers
		&MailboxPoll{},
		&TriggerEvent{},
		&WatchTarget{},
		&WatchSubscription{},

		// Assistant feed and memory
		&Update{},
		&Memory{},
	}
}

// PostMigrations are idempotent statements AutoMigrate cannot express
// (partial or expression indexes). They run after AutoMigrate, in order.
func PostMigrations() []string {
	return []string{
		`CREATE UNIQUE INDEX IF NOT EXISTS jobs_active_key ON jobs (key) WHERE key <> '' AND status IN ('pending', 'running')`,
		`CREATE UNIQUE INDEX IF NOT EXISTS devices_live_token ON devices (token) WHERE deleted_at IS NULL`,
		`DROP INDEX IF EXISTS connectors_live_name`,
		`CREATE UNIQUE INDEX IF NOT EXISTS connectors_live_user_name ON connectors (user_id, name) WHERE deleted_at IS NULL`,
		// The hosted Gmail MCP rejected the tokens it issued; Gmail is now a built-in
		// connector that needs a new sign-in with the new scopes.
		`UPDATE connectors SET kind = 'gmail', url = 'https://gmail.googleapis.com', status = 'needs_auth', error = NULL, tools = 0
			WHERE name = 'gmail' AND kind = 'mcp' AND url LIKE '%gmailmcp.googleapis.com%'`,
		`DROP INDEX IF EXISTS idx_skills_name`,
		`CREATE UNIQUE INDEX IF NOT EXISTS skills_user_name ON skills (user_id, name)`,
		`UPDATE schedules SET title = left(btrim(regexp_replace(prompt, '\s+', ' ', 'g')), 40) WHERE title = ''`,
		`CREATE INDEX IF NOT EXISTS updates_user_feed ON updates (user_id, created_at DESC) WHERE importance <> 'quiet'`,
		`CREATE UNIQUE INDEX IF NOT EXISTS memories_user_text ON memories (user_id, (btrim(regexp_replace(lower(text), '[[:space:][:punct:]]+', ' ', 'g'))))`,
		`CREATE INDEX IF NOT EXISTS memories_text_fts ON memories USING gin (to_tsvector('english', text))`,
		`ALTER TABLE updates ALTER COLUMN task_id DROP NOT NULL`,
		`DROP INDEX IF EXISTS idx_oauth_credentials_connector_id`,
		// Each gmail connector with a signed-in address becomes one primary account that
		// takes over the connector's credential. The accounts check makes it run once.
		`WITH made AS (
			INSERT INTO gmail_accounts (id, user_id, connector_id, email, status, error, is_primary, history_id, created_at, updated_at)
			SELECT gen_random_uuid(), c.user_id, c.id, c.account, c.status, c.error, true, c.history_id, c.created_at, now()
			FROM connectors c
			WHERE c.kind = 'gmail' AND c.deleted_at IS NULL AND COALESCE(c.account, '') <> ''
				AND NOT EXISTS (SELECT 1 FROM gmail_accounts a WHERE a.connector_id = c.id)
			RETURNING id, connector_id)
		UPDATE oauth_credentials o SET account_id = made.id FROM made WHERE o.connector_id = made.connector_id AND o.account_id IS NULL`,
		`CREATE UNIQUE INDEX IF NOT EXISTS oauth_credentials_connector_only ON oauth_credentials (connector_id) WHERE account_id IS NULL`,
		`CREATE UNIQUE INDEX IF NOT EXISTS oauth_credentials_account ON oauth_credentials (account_id) WHERE account_id IS NOT NULL`,
		`CREATE UNIQUE INDEX IF NOT EXISTS gmail_accounts_user_email ON gmail_accounts (user_id, lower(email))`,
		`CREATE UNIQUE INDEX IF NOT EXISTS gmail_accounts_one_primary ON gmail_accounts (connector_id) WHERE is_primary`,
		`CREATE INDEX IF NOT EXISTS mailbox_polls_due ON mailbox_polls (next_check_at)`,
		`CREATE INDEX IF NOT EXISTS trigger_events_feed ON trigger_events (routine_id, created_at DESC)`,
	}
}

// PreMigrations run before AutoMigrate. The one statement here is the
// single-user to multi-user reset: rows written before accounts existed have
// no owner and cannot get a NOT NULL user_id, so the first boot that creates
// the users table empties the owned tables. It is a no-op on every later boot
// and on a fresh database.
func PreMigrations() []string {
	return []string{
		`DO $$ BEGIN
			IF to_regclass('users') IS NULL AND to_regclass('tasks') IS NOT NULL THEN
				TRUNCATE tasks, task_events, task_messages, task_inbox, projects, knowledge, skills,
					schedules, connectors, oauth_credentials, devices, jobs;
			END IF;
		END $$`,
	}
}

// AutoMigrate runs PreMigrations, GORM auto-migration for every registered model, then PostMigrations.
func AutoMigrate(db *gorm.DB) error {
	for _, stmt := range PreMigrations() {
		if err := db.Exec(stmt).Error; err != nil {
			return err
		}
	}
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
