/*
 * Copyright (c) 2026. Papermark - All Rights Reserved
 * Unauthorized copying of this file, via any medium is strictly prohibited
 * Proprietary and confidential
 * Written by Ebot Tabi <ebot.tabi@gmail.com>, 16/10/2026
 */

package models

import (
	"time"

	"github.com/google/uuid"
)

// User is an account. It is created by the first Google sign-in and keyed by
// the Google subject; passkeys are added to it later.
type User struct {
	Base
	Email     string `gorm:"type:text;not null;uniqueIndex" json:"email"`
	GoogleSub string `gorm:"type:varchar(255);not null;uniqueIndex" json:"-"`
	Name      string `gorm:"type:text;not null;default:''" json:"name"`
	Picture   string `gorm:"type:text;not null;default:''" json:"picture"`
	// Assistant preferences, read and written through Preferences.
	Timezone   string   `gorm:"type:varchar(64);not null;default:'UTC'" json:"-"`
	QuietStart string   `gorm:"type:varchar(5);not null;default:'22:00'" json:"-"`
	QuietEnd   string   `gorm:"type:varchar(5);not null;default:'07:00'" json:"-"`
	PushLimit  int      `gorm:"not null;default:5" json:"-"`
	NewsTopics []string `gorm:"type:jsonb;serializer:json;not null;default:'[]'" json:"-"`
}

// Preferences are the per-user settings the assistant follows: Timezone (IANA)
// places schedules and "today", pushes are held back between QuietStart and
// QuietEnd (HH:MM, may wrap midnight) and after PushLimit pushes in a day.
type Preferences struct {
	Timezone   string
	QuietStart string
	QuietEnd   string
	PushLimit  int
	NewsTopics []string
}

// TableName for GORM.
func (User) TableName() string { return "users" }

// Session is a signed-in device. Only the sha256 of the opaque bearer token is
// stored; deleting the row revokes it.
type Session struct {
	ID         uuid.UUID `gorm:"type:uuid;primary_key" json:"id"`
	UserID     uuid.UUID `gorm:"type:uuid;not null;index" json:"user_id"`
	TokenHash  string    `gorm:"type:varchar(64);not null;uniqueIndex" json:"-"`
	ExpiresAt  time.Time `gorm:"not null;index" json:"expires_at"`
	LastUsedAt time.Time `gorm:"not null" json:"last_used_at"`
	CreatedAt  time.Time `gorm:"not null" json:"created_at"`
}

// TableName for GORM.
func (Session) TableName() string { return "sessions" }

// Passkey is a WebAuthn credential (a platform passkey unlocked with Face ID).
type Passkey struct {
	ID           uuid.UUID `gorm:"type:uuid;primary_key" json:"id"`
	UserID       uuid.UUID `gorm:"type:uuid;not null;index" json:"user_id"`
	CredentialID []byte    `gorm:"type:bytea;not null;uniqueIndex" json:"-"`
	PublicKey    []byte    `gorm:"type:bytea;not null" json:"-"`
	SignCount    int64     `gorm:"not null;default:0" json:"sign_count"`
	// The backup flags from registration; WebAuthn rejects a login whose BE flag differs.
	BackupEligible bool       `gorm:"not null;default:false" json:"-"`
	BackupState    bool       `gorm:"not null;default:false" json:"-"`
	Transports     []string   `gorm:"type:jsonb;serializer:json;not null;default:'[]'" json:"transports"`
	Name           string     `gorm:"type:text;not null;default:''" json:"name"`
	CreatedAt      time.Time  `gorm:"not null" json:"created_at"`
	LastUsedAt     *time.Time `json:"last_used_at"`
}

// TableName for GORM.
func (Passkey) TableName() string { return "passkeys" }

// DeviceKey is a Face ID key: a P-256 key the iOS app keeps in the Secure
// Enclave behind biometry. Login signs a server challenge with it.
type DeviceKey struct {
	ID         uuid.UUID  `gorm:"type:uuid;primary_key" json:"id"`
	UserID     uuid.UUID  `gorm:"type:uuid;not null;index" json:"user_id"`
	Name       string     `gorm:"type:text;not null;default:''" json:"name"`
	PublicKey  []byte     `gorm:"type:bytea;not null" json:"-"` // PKIX DER
	CreatedAt  time.Time  `gorm:"not null" json:"created_at"`
	LastUsedAt *time.Time `json:"last_used_at"`
	RevokedAt  *time.Time `json:"revoked_at"`
}

// TableName for GORM.
func (DeviceKey) TableName() string { return "device_keys" }

// AuthChallenge is the server half of a sign-in ceremony (a passkey
// registration or login, or a Face ID challenge), kept until it is used once
// or expires. UserID is set when a signed-in user began it.
type AuthChallenge struct {
	ID        uuid.UUID  `gorm:"type:uuid;primary_key"`
	Kind      string     `gorm:"type:varchar(16);not null"` // passkey_register | passkey_login | faceid
	UserID    *uuid.UUID `gorm:"type:uuid"`
	Data      []byte     `gorm:"type:jsonb;not null"`
	ExpiresAt time.Time  `gorm:"not null;index"`
}

// TableName for GORM.
func (AuthChallenge) TableName() string { return "auth_challenges" }
