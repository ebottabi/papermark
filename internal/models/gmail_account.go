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

// Gmail account statuses reuse the connector statuses.

// GmailAccount is one signed-in mailbox of a user's gmail connector. Its
// tokens live in the oauth_credentials row whose account_id is its id.
// HistoryID is the position of its last "since last" digest. Exactly one live
// account of a connector is primary (partial index in PostMigrations).
type GmailAccount struct {
	ID          uuid.UUID `gorm:"type:uuid;primary_key"`
	UserID      uuid.UUID `gorm:"type:uuid;not null;index"`
	ConnectorID uuid.UUID `gorm:"type:uuid;not null;index"`
	Email       string    `gorm:"type:text;not null"`
	Status      string    `gorm:"type:varchar(16);not null"`
	Error       *string   `gorm:"type:text"`
	Primary     bool      `gorm:"column:is_primary;not null;default:false"`
	HistoryID   string    `gorm:"type:varchar(32);not null;default:''"`
	CreatedAt   time.Time `gorm:"not null"`
	UpdatedAt   time.Time `gorm:"not null"`
}

// TableName for GORM.
func (GmailAccount) TableName() string { return "gmail_accounts" }
