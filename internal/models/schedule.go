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

// Schedule creates a task from Prompt on a repeating slot in the server's
// local time zone. NextRunAt is nil while the schedule is disabled.
type Schedule struct {
	Base
	Prompt    string     `gorm:"type:text;not null" json:"prompt"`
	Mode      string     `gorm:"type:varchar(16);not null;default:'quality'" json:"mode"`
	ProjectID *uuid.UUID `gorm:"type:uuid" json:"project_id"`
	Repeat    string     `gorm:"type:varchar(16);not null" json:"repeat"`
	TimeOfDay string     `gorm:"type:varchar(5);not null" json:"time"` // "HH:MM"; hourly uses the minute
	Weekday   *int       `json:"weekday"`                              // 1 (Monday) to 7, weekly only
	Enabled   bool       `gorm:"not null;default:true" json:"enabled"`
	LastRunAt *time.Time `json:"last_run_at"`
	NextRunAt *time.Time `gorm:"index" json:"next_run_at"`
}

// TableName for GORM.
func (Schedule) TableName() string { return "schedules" }
