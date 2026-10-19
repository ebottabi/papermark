/*
 * Copyright (c) 2026. Papermark - All Rights Reserved
 * Unauthorized copying of this file, via any medium is strictly prohibited
 * Proprietary and confidential
 * Written by Ebot Tabi <ebot.tabi@gmail.com>, 15/10/2026
 */
package models

import (
	"encoding/json"
	"time"

	"github.com/google/uuid"
)

// Schedule kinds.
const (
	ScheduleCustom = "custom"
	ScheduleBrief  = "brief"
)

// DefaultMaxRunsPerHour caps the runs a trigger routine starts per hour.
const DefaultMaxRunsPerHour = 10

// RoutineFailureLimit is how many runs in a row may end in error before the
// schedule is turned off.
const RoutineFailureLimit = 3

// Schedule creates a task from Prompt on a repeating slot in its owner's
// time zone (Trigger time), or when matching mail arrives
// (Trigger email; Rule holds the JSON of the rule and the slot fields
// are empty). NextRunAt is nil while the schedule is disabled and for triggers
// other than time.
type Schedule struct {
	Base
	UserID         uuid.UUID       `gorm:"type:uuid;not null;index" json:"userId"`
	Title          string          `gorm:"type:varchar(120);not null;default:''" json:"title"`
	Kind           string          `gorm:"type:varchar(16);not null;default:'custom'" json:"kind"`
	Prompt         string          `gorm:"type:text;not null" json:"prompt"`
	Mode           string          `gorm:"type:varchar(16);not null;default:'quality'" json:"mode"`
	ProjectID      *uuid.UUID      `gorm:"type:uuid" json:"project_id"`
	Trigger        string          `gorm:"type:varchar(8);not null;default:'time'" json:"trigger"`
	Rule           json.RawMessage `gorm:"type:jsonb" json:"rule"`
	Repeat         string          `gorm:"type:varchar(16);not null;default:''" json:"repeat"`
	TimeOfDay      string          `gorm:"type:varchar(5);not null;default:''" json:"time"` // "HH:MM"; hourly uses the minute
	Weekday        *int            `json:"weekday"`                                         // 1 (Monday) to 7, weekly only
	Enabled        bool            `gorm:"not null;default:true" json:"enabled"`
	LastRunAt      *time.Time      `json:"last_run_at"`
	NextRunAt      *time.Time      `gorm:"index" json:"next_run_at"`
	MaxRunsPerHour int             `gorm:"not null;default:10" json:"maxRunsPerHour"`
	// When the last "too many runs" note was posted, so it is posted once an hour.
	LimitNotedAt *time.Time `json:"-"`
	// Runs in a row that ended in error; any success or edit resets it.
	ConsecutiveFailures int `gorm:"not null;default:0" json:"-"`
	// Skills are the names its runs follow, picked at the first run after the
	// prompt was set; nil until then. The column is added in PostMigrations.
	Skills []string `gorm:"-" json:"-"`
}

// TableName for GORM.
func (Schedule) TableName() string { return "schedules" }
