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

// Update importance values.
const (
	ImportanceQuiet = "quiet"
	ImportanceFeed  = "feed"
	ImportancePush  = "push"
)

// Update is one entry of a user's feed, published by a task through
// publish_update, or by a watch routine directly (TaskID nil). A push update is also sent to the user's devices.
type Update struct {
	ID         uuid.UUID  `gorm:"type:uuid;primary_key"`
	UserID     uuid.UUID  `gorm:"type:uuid;not null"`
	TaskID     *uuid.UUID `gorm:"type:uuid;index"`
	ScheduleID *uuid.UUID `gorm:"type:uuid"`
	Importance string     `gorm:"type:varchar(8);not null"`
	Title      string     `gorm:"type:text;not null"`
	Body       string     `gorm:"type:text;not null;default:''"`
	Sections   []Section  `gorm:"type:jsonb;serializer:json;not null;default:'[]'"`
	ReadAt     *time.Time
	CreatedAt  time.Time `gorm:"not null"`
}

// TableName for GORM.
func (Update) TableName() string { return "updates" }

// Section is a titled list inside an update.
type Section struct {
	Title string        `json:"title"`
	Items []SectionItem `json:"items"`
}

// SectionItem is one line of a section; Detail is secondary text (a sender)
// and URL a page to open.
type SectionItem struct {
	Text   string `json:"text"`
	Detail string `json:"detail,omitempty"`
	URL    string `json:"url,omitempty"`
}
