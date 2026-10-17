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

// Task status values. Status is a function of the last event (see services.statusAfter).
const (
	TaskRunning = "running"
	TaskWaiting = "waiting"
	TaskDone    = "done"
	TaskStopped = "stopped"
	TaskError   = "error"
)

// Task is one agent task: its summary, token usage and the state a run needs
// to resume. A task waiting on the user holds no process: Pending names the
// tool call it waits on and PartialResults holds the results of the calls
// before it in the same assistant turn.
type Task struct {
	Base
	UserID           uuid.UUID         `gorm:"type:uuid;not null;index" json:"userId"`
	Prompt           string            `gorm:"type:text;not null" json:"prompt"`
	Status           string            `gorm:"type:varchar(16);not null;index" json:"status"`
	Mode             string            `gorm:"type:varchar(16);not null" json:"mode"`
	ProjectID        *uuid.UUID        `gorm:"type:uuid;index" json:"projectId"`
	ScheduleID       *uuid.UUID        `gorm:"type:uuid;index" json:"scheduleId"`
	Favorite         bool              `gorm:"not null;default:false" json:"favorite"`
	Archived         bool              `gorm:"not null;default:false" json:"archived"`
	InputTokens      int64             `gorm:"not null;default:0" json:"inputTokens"`
	OutputTokens     int64             `gorm:"not null;default:0" json:"outputTokens"`
	CacheReadTokens  int64             `gorm:"not null;default:0" json:"cacheReadTokens"`
	CacheWriteTokens int64             `gorm:"not null;default:0" json:"cacheWriteTokens"`
	EventCount       int               `gorm:"not null;default:0" json:"eventCount"`
	Pending          *PendingAction    `gorm:"type:jsonb;serializer:json" json:"pending"`
	PartialResults   []json.RawMessage `gorm:"type:jsonb;serializer:json;not null;default:'[]'" json:"partialResults"`
	AlwaysAllowed    []string          `gorm:"type:jsonb;serializer:json;not null;default:'[]'" json:"alwaysAllowed"`
	StopRequested    bool              `gorm:"not null;default:false" json:"stopRequested"`
}

// TableName for GORM.
func (Task) TableName() string { return "tasks" }

// PendingAction is the tool call a waiting task parked on. Answer is set by
// /reply (the text) or /confirm (the decision) and consumed by the next run.
type PendingAction struct {
	Kind      string  `json:"kind"` // ask | confirm
	Name      string  `json:"name"`
	ToolUseID string  `json:"toolUseId"`
	Answer    *string `json:"answer,omitempty"`
	NoAlways  bool    `json:"noAlways,omitempty"` // confirm only: "always" is refused
}

// TaskEvent is one entry of a task's append-only event log. Data is the
// AgentEvent JSON exactly as API.md defines it; json (not jsonb) keeps its bytes.
type TaskEvent struct {
	TaskID    uuid.UUID       `gorm:"type:uuid;primaryKey;autoIncrement:false" json:"taskId"`
	Seq       int             `gorm:"primaryKey;autoIncrement:false" json:"seq"`
	Type      string          `gorm:"type:varchar(16);not null" json:"type"`
	Data      json.RawMessage `gorm:"type:json;not null" json:"data"`
	CreatedAt time.Time       `gorm:"autoCreateTime" json:"createdAt"`
}

// TableName for GORM.
func (TaskEvent) TableName() string { return "task_events" }

// TaskMessage is one Messages API message ({role, content}) of a task's
// append-only model conversation, stored verbatim so every request resends
// the same bytes and the prompt cache holds.
type TaskMessage struct {
	TaskID    uuid.UUID       `gorm:"type:uuid;primaryKey;autoIncrement:false" json:"taskId"`
	Seq       int             `gorm:"primaryKey;autoIncrement:false" json:"seq"`
	Message   json.RawMessage `gorm:"type:json;not null" json:"message"`
	CreatedAt time.Time       `gorm:"autoCreateTime" json:"createdAt"`
}

// TableName for GORM.
func (TaskMessage) TableName() string { return "task_messages" }

// TaskInbox holds user messages not yet delivered to the model. A run drains
// it into the next user turn.
type TaskInbox struct {
	ID        int64     `gorm:"primaryKey;autoIncrement" json:"id"`
	TaskID    uuid.UUID `gorm:"type:uuid;not null;index" json:"taskId"`
	Text      string    `gorm:"type:text;not null" json:"text"`
	CreatedAt time.Time `gorm:"autoCreateTime" json:"createdAt"`
}

// TableName for GORM.
func (TaskInbox) TableName() string { return "task_inbox" }
