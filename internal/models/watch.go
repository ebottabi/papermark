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

// Watch kinds.
const (
	WatchPage   = "page"
	WatchSearch = "search"
)

// Watch conditions.
const (
	WatchChanged  = "changed"
	WatchBelow    = "below"
	WatchAbove    = "above"
	WatchContains = "contains"
	WatchNewItems = "newItems"
)

// WatchTarget is one thing to fetch, shared by every routine that watches the
// same url (or query) and selector: Key is the hash of that identity.
// Fingerprint is the sha256 of LastValue. IntervalSeconds is the smallest
// interval of its subscriptions. Misses counts fetches in a row that found
// nothing at the selector and Failures fetches in a row that errored.
type WatchTarget struct {
	ID              uuid.UUID  `gorm:"type:uuid;primary_key"`
	Kind            string     `gorm:"type:varchar(8);not null"`
	Key             string     `gorm:"type:varchar(64);not null;uniqueIndex"`
	URL             string     `gorm:"type:text;not null;default:''"`
	Query           string     `gorm:"type:text;not null;default:''"`
	Selector        string     `gorm:"type:text;not null;default:''"`
	Fingerprint     string     `gorm:"type:varchar(64);not null;default:''"`
	LastValue       string     `gorm:"type:text;not null;default:''"`
	ETag            string     `gorm:"column:etag;type:text;not null;default:''"`
	LastModified    string     `gorm:"type:text;not null;default:''"`
	NextCheckAt     time.Time  `gorm:"not null;index"`
	LockedUntil     *time.Time `gorm:""`
	Failures        int        `gorm:"not null;default:0"`
	Misses          int        `gorm:"not null;default:0"`
	LastError       string     `gorm:"type:text;not null;default:''"`
	IntervalSeconds int        `gorm:"not null"`
}

// TableName for GORM.
func (WatchTarget) TableName() string { return "watch_targets" }

// WatchSubscription ties one routine to its target and condition. Armed and
// LastNotifiedFingerprint are the hysteresis of the level conditions: a
// notification disarms, and the condition ceasing to hold re-arms.
type WatchSubscription struct {
	RoutineID               uuid.UUID `gorm:"type:uuid;primary_key"`
	UserID                  uuid.UUID `gorm:"type:uuid;not null"`
	TargetID                uuid.UUID `gorm:"type:uuid;not null;index"`
	Condition               string    `gorm:"type:varchar(16);not null"`
	Threshold               *float64
	Text                    string `gorm:"type:text;not null;default:''"`
	Armed                   bool   `gorm:"not null;default:true"`
	LastNotifiedFingerprint string `gorm:"type:varchar(64);not null;default:''"`
}

// TableName for GORM.
func (WatchSubscription) TableName() string { return "watch_subscriptions" }
