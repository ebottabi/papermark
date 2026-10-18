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

// ContactValue is one labelled value of a contact: an address, a number or a relation.
type ContactValue struct {
	Value string `json:"value"`
	Type  string `json:"type,omitempty"`
}

// Contact is a copy of one Google contact of a contacts account, kept in
// sync by the contact sync worker. Saved is true for the user's own contacts
// (people/...) and false for "other contacts" Google collected from mail
// (otherContacts/...). EmailKeys holds the lowercased addresses and PhoneKeys
// the normalised numbers, both GIN indexed for lookups (PostMigrations).
// UpdatedAt is Google's last change of the contact when it reports one.
type Contact struct {
	ID           uuid.UUID      `gorm:"type:uuid;primary_key"`
	UserID       uuid.UUID      `gorm:"type:uuid;not null;index"`
	AccountID    uuid.UUID      `gorm:"type:uuid;not null;uniqueIndex:contacts_account_resource,priority:1"`
	ResourceName string         `gorm:"type:text;not null;uniqueIndex:contacts_account_resource,priority:2"`
	Saved        bool           `gorm:"not null"`
	Name         string         `gorm:"type:text;not null;default:''"`
	Emails       []ContactValue `gorm:"type:jsonb;serializer:json;not null;default:'[]'"`
	Phones       []ContactValue `gorm:"type:jsonb;serializer:json;not null;default:'[]'"`
	EmailKeys    []string       `gorm:"type:jsonb;serializer:json;not null;default:'[]'"`
	PhoneKeys    []string       `gorm:"type:jsonb;serializer:json;not null;default:'[]'"`
	Organization string         `gorm:"type:text;not null;default:''"`
	Title        string         `gorm:"type:text;not null;default:''"`
	Notes        string         `gorm:"type:text;not null;default:''"`
	Relations    []ContactValue `gorm:"type:jsonb;serializer:json;not null;default:'[]'"`
	Birthday     string         `gorm:"type:text;not null;default:''"`
	Groups       []string       `gorm:"type:jsonb;serializer:json;not null;default:'[]'"`
	ETag         string         `gorm:"column:etag;type:text;not null;default:''"`
	UpdatedAt    time.Time      `gorm:"not null"`
}

// TableName for GORM.
func (Contact) TableName() string { return "contacts" }

// ContactSync schedules the syncs of one contacts account. ConnectionsToken
// and OtherToken are the People API sync tokens of the saved and the other
// contacts; empty means the next sync is a full one. LockedUntil is the lease
// of the worker that claimed the row; Failures sets the back-off.
type ContactSync struct {
	AccountID        uuid.UUID  `gorm:"type:uuid;primary_key"`
	UserID           uuid.UUID  `gorm:"type:uuid;not null;index"`
	ConnectionsToken string     `gorm:"type:text;not null;default:''"`
	OtherToken       string     `gorm:"type:text;not null;default:''"`
	NextSyncAt       time.Time  `gorm:"not null"`
	LockedUntil      *time.Time `gorm:""`
	Failures         int        `gorm:"not null;default:0"`
	LastError        string     `gorm:"type:text;not null;default:''"`
	SyncedAt         *time.Time `gorm:""`
}

// TableName for GORM.
func (ContactSync) TableName() string { return "contact_syncs" }
