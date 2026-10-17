/*
 * Copyright (c) 2026. Papermark - All Rights Reserved
 * Unauthorized copying of this file, via any medium is strictly prohibited
 * Proprietary and confidential
 * Written by Ebot Tabi <ebot.tabi@gmail.com>, 17/10/2026
 */
package harness

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/url"
	"strings"
	"time"
	"unicode/utf8"

	"papermark/internal/models"
	"papermark/internal/models/queries"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
)

const (
	maxUpdateSections = 10
	maxSectionItems   = 30
	maxUpdateTitle    = 200
	maxUpdateBody     = 5000
	maxItemText       = 500
	memoryHits        = 10
)

// assistantTools are offered on every run. routine_create is added by runTurn
// when the harness can create routines and the run is not itself a routine.
var assistantTools = []json.RawMessage{
	fn("publish_update", "Publish an update to the user's feed. importance quiet is stored only, feed shows in the feed, push also alerts the user (held back to feed during their quiet hours or after their daily push limit). Use push only for what needs attention now.", json.RawMessage(`{"type":"object","properties":{"importance":{"type":"string","enum":["quiet","feed","push"]},"title":{"type":"string","description":"One line"},"body":{"type":"string","description":"Short summary"},"sections":{"type":"array","items":{"type":"object","properties":{"title":{"type":"string"},"items":{"type":"array","items":{"type":"object","properties":{"text":{"type":"string"},"detail":{"type":"string"},"url":{"type":"string","description":"http or https link"}},"required":["text"],"additionalProperties":false}}},"required":["title","items"],"additionalProperties":false}}},"required":["importance","title","body"],"additionalProperties":false}`)),
	fn("memory_save", "Remember a lasting fact or preference about the user (not task details). It is shown to you at the start of future tasks. Saving the same fact twice is harmless.", json.RawMessage(`{"type":"object","properties":{"text":{"type":"string","description":"One self-contained sentence"}},"required":["text"],"additionalProperties":false}`)),
	fn("memory_search", "Search what you remember about the user. Returns up to 10 matches.", json.RawMessage(`{"type":"object","properties":{"query":{"type":"string"}},"required":["query"],"additionalProperties":false}`)),
}

var routineCreateTool = fn("routine_create", "Create a routine: a task that runs on a schedule and publishes its result to the user's feed. Needs the user's approval. time is HH:MM in the user's time zone (hourly uses the minute). weekday (1 Monday to 7 Sunday) is for weekly.", json.RawMessage(`{"type":"object","properties":{"title":{"type":"string"},"prompt":{"type":"string","description":"What each run should do"},"repeat":{"type":"string","enum":["hourly","daily","weekdays","weekly"]},"time":{"type":"string","description":"HH:MM"},"weekday":{"type":"integer","minimum":1,"maximum":7}},"required":["title","prompt","repeat","time"],"additionalProperties":false}`))

// RoutineSpec is a routine the agent asks to create.
type RoutineSpec struct {
	Title   string
	Prompt  string
	Repeat  string
	Time    string
	Weekday *int
}

// RoutineCreator creates a custom schedule for a user.
type RoutineCreator interface {
	CreateRoutine(ctx context.Context, userID uuid.UUID, spec RoutineSpec) (*models.Schedule, error)
}

func isAssistantTool(name string) bool {
	switch name {
	case "publish_update", "memory_save", "memory_search", "routine_create":
		return true
	}
	return false
}

func (tn *turn) assistant(ctx context.Context, c toolCall) (ToolOutput, error) {
	switch c.Name {
	case "publish_update":
		return tn.publishUpdate(ctx, c.Input)
	case "memory_save":
		var in struct{ Text string }
		if err := json.Unmarshal(c.Input, &in); err != nil {
			return ToolOutput{}, err
		}
		text, err := models.CleanMemoryText(in.Text)
		if err != nil {
			return ToolOutput{}, err
		}
		_, created, err := queries.SaveMemory(ctx, tn.h.pool, tn.userID, text, &tn.id)
		if err != nil {
			return ToolOutput{}, err
		}
		if !created {
			return ToolOutput{Text: "already remembered"}, nil
		}
		return ToolOutput{Text: "saved"}, nil
	case "memory_search":
		var in struct{ Query string }
		if err := json.Unmarshal(c.Input, &in); err != nil {
			return ToolOutput{}, err
		}
		found, err := queries.SearchMemories(ctx, tn.h.pool, tn.userID, in.Query, memoryHits)
		if err != nil {
			return ToolOutput{}, err
		}
		if len(found) == 0 {
			return ToolOutput{Text: "no matching memories"}, nil
		}
		lines := make([]string, len(found))
		for i, m := range found {
			lines[i] = "- " + m.Text
		}
		return ToolOutput{Text: strings.Join(lines, "\n")}, nil
	default:
		return tn.createRoutine(ctx, c.Input)
	}
}

func (tn *turn) createRoutine(ctx context.Context, input json.RawMessage) (ToolOutput, error) {
	var in struct {
		Title, Prompt, Repeat, Time string
		Weekday                     *int
	}
	if err := json.Unmarshal(input, &in); err != nil {
		return ToolOutput{}, err
	}
	s, err := tn.h.Routines.CreateRoutine(ctx, tn.userID, RoutineSpec{in.Title, in.Prompt, in.Repeat, in.Time, in.Weekday})
	if err != nil {
		return ToolOutput{}, err
	}
	prefs, err := queries.GetPreferences(ctx, tn.h.pool, tn.userID)
	if err != nil {
		return ToolOutput{}, err
	}
	return ToolOutput{Text: fmt.Sprintf("Created routine %q. Next run: %s (%s).", s.Title, s.NextRunAt.In(location(prefs.Timezone)).Format("Mon 2 Jan 15:04"), prefs.Timezone)}, nil
}

// publishUpdate stores the update and emits its task event in one
// transaction. A push is held back to feed when pushHeld says so.
func (tn *turn) publishUpdate(ctx context.Context, input json.RawMessage) (ToolOutput, error) {
	var in struct {
		Importance string
		Title      string
		Body       string
		Sections   []models.Section
	}
	if err := json.Unmarshal(input, &in); err != nil {
		return ToolOutput{}, err
	}
	if err := validateUpdate(in.Importance, in.Title, in.Body, in.Sections); err != nil {
		return ToolOutput{}, err
	}
	if in.Sections == nil {
		in.Sections = []models.Section{}
	}
	prefs, err := queries.GetPreferences(ctx, tn.h.pool, tn.userID)
	if err != nil {
		return ToolOutput{}, err
	}
	out := ToolOutput{Text: "published as " + in.Importance}
	err = tn.h.TaskTx(ctx, tn.id, func(tx pgx.Tx, _ *models.Task, emit Emit) error {
		importance := in.Importance
		if importance == models.ImportancePush {
			now := time.Now()
			from, to := dayBounds(now, location(prefs.Timezone))
			pushed, err := queries.CountPushUpdatesSince(ctx, tx, tn.userID, from, to)
			if err != nil {
				return err
			}
			if why := pushHeld(prefs, now, pushed); why != "" {
				importance = models.ImportanceFeed
				out.Text = "published as feed instead of push: " + why
			}
		}
		u := &models.Update{UserID: tn.userID, TaskID: &tn.id, ScheduleID: tn.scheduleID, Importance: importance,
			Title: in.Title, Body: in.Body, Sections: in.Sections}
		if err := queries.InsertUpdate(ctx, tx, u); err != nil {
			return err
		}
		return emit(UpdateEvent{"update", u.ID.String(), importance, u.Title, u.Body, u.Sections})
	})
	return out, err
}

func validateUpdate(importance, title, body string, sections []models.Section) error {
	switch {
	case importance != models.ImportanceQuiet && importance != models.ImportanceFeed && importance != models.ImportancePush:
		return errors.New("importance must be quiet, feed or push")
	case strings.TrimSpace(title) == "" || utf8.RuneCountInString(title) > maxUpdateTitle:
		return fmt.Errorf("title is required, at most %d characters", maxUpdateTitle)
	case utf8.RuneCountInString(body) > maxUpdateBody:
		return fmt.Errorf("body must be at most %d characters", maxUpdateBody)
	case len(sections) > maxUpdateSections:
		return fmt.Errorf("at most %d sections", maxUpdateSections)
	}
	for _, s := range sections {
		if strings.TrimSpace(s.Title) == "" || len(s.Items) > maxSectionItems {
			return fmt.Errorf("each section needs a title and at most %d items", maxSectionItems)
		}
		for _, it := range s.Items {
			if strings.TrimSpace(it.Text) == "" || utf8.RuneCountInString(it.Text) > maxItemText {
				return fmt.Errorf("each item needs text of at most %d characters", maxItemText)
			}
			if u, err := url.Parse(it.URL); it.URL != "" && (err != nil || (u.Scheme != "http" && u.Scheme != "https") || u.Host == "") {
				return fmt.Errorf("item url must be an http or https link: %s", it.URL)
			}
		}
	}
	return nil
}

// location loads an IANA zone; the preferences API only stores valid ones,
// so UTC here means the server lacks the zone data.
func location(name string) *time.Location {
	if loc, err := time.LoadLocation(name); err == nil {
		return loc
	}
	return time.UTC
}

func dayBounds(now time.Time, loc *time.Location) (from, to time.Time) {
	t := now.In(loc)
	return time.Date(t.Year(), t.Month(), t.Day(), 0, 0, 0, 0, loc), time.Date(t.Year(), t.Month(), t.Day()+1, 0, 0, 0, 0, loc)
}

// pushHeld says why a push must be downgraded to feed at now, or "" when it may go out.
func pushHeld(p *models.Preferences, now time.Time, pushedToday int) string {
	if pushedToday >= p.PushLimit {
		return "the daily push limit is reached"
	}
	start, err1 := time.Parse("15:04", p.QuietStart)
	end, err2 := time.Parse("15:04", p.QuietEnd)
	if err1 != nil || err2 != nil {
		return ""
	}
	t := now.In(location(p.Timezone))
	cur, s, e := t.Hour()*60+t.Minute(), start.Hour()*60+start.Minute(), end.Hour()*60+end.Minute()
	if (s < e && cur >= s && cur < e) || (s > e && (cur >= s || cur < e)) {
		return "it is the user's quiet hours"
	}
	return ""
}

// routineGuidance is added to the system prompt of runs started by a routine.
func routineGuidance(p *models.Preferences, now time.Time) string {
	topics := "none set; use general top headlines"
	if len(p.NewsTopics) > 0 {
		topics = strings.Join(p.NewsTopics, ", ")
	}
	return fmt.Sprintf(`

This run was started by a routine and nobody is watching it.
- Do the routine's work, then end with exactly one publish_update call that carries the result, then call idle. Publish once only.
- Do not call message_ask_user or shell_exec, and avoid connector tools that change anything: no one is there to answer or approve.
- The user's time zone is %s. Today is %s there. Their news topics: %s.`,
		p.Timezone, now.In(location(p.Timezone)).Format("Monday 2 January 2006"), topics)
}
