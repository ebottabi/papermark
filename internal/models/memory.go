/*
 * Copyright (c) 2026. Papermark - All Rights Reserved
 * Unauthorized copying of this file, via any medium is strictly prohibited
 * Proprietary and confidential
 * Written by Ebot Tabi <ebot.tabi@gmail.com>, 17/10/2026
 */

package models

import (
	"errors"
	"fmt"
	"strings"
	"time"
	"unicode"
	"unicode/utf8"

	"github.com/google/uuid"
)

// Memory is one thing the assistant remembers about its user, saved by the
// agent (memory_save) or written by the user. Texts that differ only in case,
// spacing or punctuation are the same memory (the memories_user_text index).
type Memory struct {
	ID           uuid.UUID  `gorm:"type:uuid;primary_key"`
	UserID       uuid.UUID  `gorm:"type:uuid;not null"`
	Text         string     `gorm:"type:text;not null"`
	SourceTaskID *uuid.UUID `gorm:"type:uuid"`
	CreatedAt    time.Time  `gorm:"not null"`
	UpdatedAt    time.Time  `gorm:"not null"`
}

// TableName for GORM.
func (Memory) TableName() string { return "memories" }

// MaxMemoryRunes is the longest memory text.
const MaxMemoryRunes = 1000

// CleanMemoryText trims text and checks it can be stored as a memory.
func CleanMemoryText(text string) (string, error) {
	text = strings.TrimSpace(text)
	switch {
	case !strings.ContainsFunc(text, func(r rune) bool { return unicode.IsLetter(r) || unicode.IsDigit(r) }):
		return "", errors.New("text is required")
	case utf8.RuneCountInString(text) > MaxMemoryRunes:
		return "", fmt.Errorf("text must be at most %d characters", MaxMemoryRunes)
	}
	return text, nil
}
