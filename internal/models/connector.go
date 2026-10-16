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

// Connector statuses.
const (
	ConnectorConnected = "connected"
	ConnectorNeedsAuth = "needs_auth"
	ConnectorError     = "error"
)

// Connector is a remote MCP server whose tools tasks can call. Name is unique
// among live rows (partial index in PostMigrations) and prefixes its tool names.
// Status, Error and Tools record the outcome of the latest connection attempt.
type Connector struct {
	Base
	Name   string  `gorm:"type:varchar(32);not null" json:"name"`
	URL    string  `gorm:"type:text;not null" json:"url"`
	Status string  `gorm:"type:varchar(16);not null" json:"status"`
	Error  *string `gorm:"type:text" json:"error"`
	Tools  int     `gorm:"not null;default:0" json:"tools"`
}

// TableName for GORM.
func (Connector) TableName() string { return "connectors" }

// OAuthCredential is a connector's OAuth client registration, its pending
// authorization (PKCE verifier and hashed state) and its tokens. Deleting the
// connector hard-deletes it. Secrets are never serialized.
type OAuthCredential struct {
	ID             uuid.UUID  `gorm:"type:uuid;primary_key" json:"id"`
	ConnectorID    uuid.UUID  `gorm:"type:uuid;not null;uniqueIndex" json:"connector_id"`
	Resource       string     `gorm:"type:text;not null" json:"resource"`
	AuthURL        string     `gorm:"type:text;not null" json:"auth_url"`
	TokenURL       string     `gorm:"type:text;not null" json:"token_url"`
	Scopes         string     `gorm:"type:text;not null;default:''" json:"scopes"` // space separated
	ClientID       string     `gorm:"type:text;not null" json:"client_id"`
	ClientSecret   string     `gorm:"type:text;not null;default:''" json:"-"`
	Verifier       string     `gorm:"type:text;not null;default:''" json:"-"`
	StateHash      string     `gorm:"type:varchar(64);not null;default:'';index" json:"-"`
	StateExpiresAt *time.Time `json:"-"`
	AccessToken    string     `gorm:"type:text;not null;default:''" json:"-"`
	RefreshToken   string     `gorm:"type:text;not null;default:''" json:"-"`
	TokenType      string     `gorm:"type:varchar(32);not null;default:''" json:"token_type"`
	Expiry         *time.Time `json:"expiry"`
	CreatedAt      time.Time  `json:"created_at"`
	UpdatedAt      time.Time  `json:"updated_at"`
}

// TableName for GORM.
func (OAuthCredential) TableName() string { return "oauth_credentials" }
