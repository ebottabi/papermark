/*
 * Copyright (c) 2026. Papermark - All Rights Reserved
 * Unauthorized copying of this file, via any medium is strictly prohibited
 * Proprietary and confidential
 * Written by Ebot Tabi <ebot.tabi@gmail.com>, 15/10/2026
 */
package models

import "github.com/google/uuid"

// Project groups tasks; its instructions go into each of its tasks' first message.
type Project struct {
	Base
	UserID       uuid.UUID `gorm:"type:uuid;not null;index" json:"userId"`
	Name         string    `gorm:"type:text;not null" json:"name"`
	Instructions string    `gorm:"type:text;not null;default:''" json:"instructions"`
}

// TableName for GORM.
func (Project) TableName() string { return "projects" }

// Knowledge is a note the user keeps; enabled entries go into every new task's first message.
type Knowledge struct {
	Base
	UserID  uuid.UUID `gorm:"type:uuid;not null;index" json:"userId"`
	Title   string    `gorm:"type:text;not null" json:"title"`
	Content string    `gorm:"type:text;not null;default:''" json:"content"`
	Enabled bool      `gorm:"not null;default:true" json:"enabled"`
}

// TableName for GORM.
func (Knowledge) TableName() string { return "knowledge" }

// Skill is a named instruction set; a message starting with /<name> expands it.
type Skill struct {
	Base
	UserID       uuid.UUID `gorm:"type:uuid;not null;index" json:"userId"`
	Name         string    `gorm:"type:varchar(40);not null" json:"name"` // unique per user, see PostMigrations
	Description  string    `gorm:"type:text;not null;default:''" json:"description"`
	Instructions string    `gorm:"type:text;not null;default:''" json:"instructions"`
}

// TableName for GORM.
func (Skill) TableName() string { return "skills" }
