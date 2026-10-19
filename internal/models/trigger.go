/*
 * Copyright (c) 2026. Papermark - All Rights Reserved
 * Unauthorized copying of this file, via any medium is strictly prohibited
 * Proprietary and confidential
 * Written by Ebot Tabi <ebot.tabi@gmail.com>, 17/10/2026
 */

package models

import (
	"time"

	"github.com/google/uuid"
)

// Routine triggers.
const (
	TriggerTime  = "time"
	TriggerEmail = "email"
)

// MailboxPoll schedules the checks of one Gmail account. A row exists only
// while an enabled email routine of the account's owner matches the account.
// HistoryID is the position the next check reads from; it moves only in the
// transaction that records the triggers it produced. LockedUntil is the lease
// of the poller that claimed the row. Failures counts checks in a row that
// ended in a retryable error and sets the back-off.
type MailboxPoll struct {
	AccountID   uuid.UUID  `gorm:"type:uuid;primary_key"`
	UserID      uuid.UUID  `gorm:"type:uuid;not null;index"`
	HistoryID   string     `gorm:"type:varchar(32);not null;default:''"`
	NextCheckAt time.Time  `gorm:"not null;index"`
	LockedUntil *time.Time `gorm:""`
	Failures    int        `gorm:"not null;default:0"`
	LastError   string     `gorm:"type:text;not null;default:''"`
}

// TableName for GORM.
func (MailboxPoll) TableName() string { return "mailbox_polls" }

// TriggerEvent records that a routine fired for SourceKey (a Gmail message id). The unique (routine, source key)
// pair makes a replayed check create nothing. TaskID is nil when the update
// was written without a model run.
type TriggerEvent struct {
	ID        uuid.UUID  `gorm:"type:uuid;primary_key"`
	RoutineID uuid.UUID  `gorm:"type:uuid;not null;uniqueIndex:trigger_events_routine_source,priority:1"`
	UserID    uuid.UUID  `gorm:"type:uuid;not null"`
	SourceKey string     `gorm:"type:text;not null;uniqueIndex:trigger_events_routine_source,priority:2"`
	Summary   string     `gorm:"type:text;not null"`
	TaskID    *uuid.UUID `gorm:"type:uuid"`
	CreatedAt time.Time  `gorm:"not null"`
}

// TableName for GORM.
func (TriggerEvent) TableName() string { return "trigger_events" }
