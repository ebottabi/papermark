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
)

// Job is one durable unit of background work (outbox pattern). Workers claim
// due jobs with FOR UPDATE SKIP LOCKED. A non-empty Key has at most one active
// (pending or running) row, enforced by a partial unique index created in
// models.PostMigrations: enqueueing an active key is a no-op, or, while it
// runs, sets Rerun so the job goes back to pending when it completes.
type Job struct {
	ID          uuid.UUID  `gorm:"type:uuid;primary_key" json:"id"`
	Type        string     `gorm:"type:varchar(64);not null" json:"type"`
	Key         string     `gorm:"type:varchar(128);not null;default:'';index" json:"key"`
	Payload     []byte     `gorm:"type:jsonb;not null;default:'{}'" json:"payload,omitempty"`
	Status      string     `gorm:"type:varchar(16);not null;default:'pending';index" json:"status"`
	Rerun       bool       `gorm:"not null;default:false" json:"rerun"`
	Attempts    int        `gorm:"not null;default:0" json:"attempts"`
	MaxAttempts int        `gorm:"not null;default:10" json:"max_attempts"`
	NextRunAt   time.Time  `gorm:"not null;index" json:"next_run_at"`
	LastError   string     `gorm:"type:text;not null;default:''" json:"last_error,omitempty"`
	LockedAt    *time.Time `json:"locked_at,omitempty"`
	LockedBy    string     `gorm:"type:varchar(128);not null;default:''" json:"locked_by,omitempty"`
	CreatedAt   time.Time  `gorm:"autoCreateTime" json:"created_at"`
	UpdatedAt   time.Time  `gorm:"autoUpdateTime" json:"updated_at"`
}

// TableName for GORM.
func (Job) TableName() string { return "jobs" }
