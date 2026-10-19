/*
 * Copyright (c) 2026. Papermark - All Rights Reserved
 * Unauthorized copying of this file, via any medium is strictly prohibited
 * Proprietary and confidential
 * Written by Ebot Tabi <ebot.tabi@gmail.com>, 17/10/2026
 */
package tools

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

	"github.com/jackc/pgx/v5"
)

const (
	maxUpdateSections = 10
	maxSectionItems   = 30
	maxUpdateTitle    = 200
	maxUpdateBody     = 5000
	maxItemText       = 500
)

// PublishUpdate puts a result in the user's feed.
var PublishUpdate = Tool{
	Name:        "publish_update",
	Description: "Publish an update to the user's feed. importance quiet is stored only, feed shows in the feed, push also alerts the user (held back to feed during their quiet hours or after their daily push limit). Use push only for what needs attention now.",
	Schema:      json.RawMessage(`{"type":"object","properties":{"importance":{"type":"string","enum":["quiet","feed","push"]},"title":{"type":"string","description":"What happened, under 60 characters, e.g. 4 outreach emails sent"},"body":{"type":"string","description":"1 or 2 sentences, under 200 characters: the outcome and anything the user must do. Put names, links and lists in sections, not here"},"sections":{"type":"array","items":{"type":"object","properties":{"title":{"type":"string"},"items":{"type":"array","items":{"type":"object","properties":{"text":{"type":"string"},"detail":{"type":"string"},"url":{"type":"string","description":"http or https link"}},"required":["text"],"additionalProperties":false}}},"required":["title","items"],"additionalProperties":false}}},"required":["importance","title","body"],"additionalProperties":false}`),
	Run:         publishUpdate,
}

// UpdateEvent is publish_update's task event; importance is what the server applied.
type UpdateEvent struct {
	Type       string           `json:"type"`
	ID         string           `json:"id"`
	Importance string           `json:"importance"`
	Title      string           `json:"title"`
	Body       string           `json:"body"`
	Sections   []models.Section `json:"sections"`
}

// publishUpdate stores the update and emits its task event in one
// transaction. A push is held back to feed when pushHeld says so.
func publishUpdate(ctx context.Context, env *Env, input json.RawMessage) (Output, error) {
	var in struct {
		Importance string
		Title      string
		Body       string
		Sections   []models.Section
	}
	if err := json.Unmarshal(input, &in); err != nil {
		return Output{}, err
	}
	if err := validateUpdate(in.Importance, in.Title, in.Body, in.Sections); err != nil {
		return Output{}, err
	}
	if in.Sections == nil {
		in.Sections = []models.Section{}
	}
	prefs, err := queries.GetPreferences(ctx, env.DB, env.UserID)
	if err != nil {
		return Output{}, err
	}
	out := Output{Text: "published as " + in.Importance}
	err = env.TaskTx(ctx, func(tx pgx.Tx, emit func(event any) error) error {
		importance := in.Importance
		if importance == models.ImportancePush {
			now := time.Now()
			from, to := dayBounds(now, Location(prefs.Timezone))
			pushed, err := queries.CountPushUpdatesSince(ctx, tx, env.UserID, from, to)
			if err != nil {
				return err
			}
			if why := pushHeld(prefs, now, pushed); why != "" {
				importance = models.ImportanceFeed
				out.Text = "published as feed instead of push: " + why
			}
		}
		u := &models.Update{UserID: env.UserID, TaskID: &env.TaskID, ScheduleID: env.ScheduleID, Importance: importance,
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
	t := now.In(Location(p.Timezone))
	cur, s, e := t.Hour()*60+t.Minute(), start.Hour()*60+start.Minute(), end.Hour()*60+end.Minute()
	if (s < e && cur >= s && cur < e) || (s > e && (cur >= s || cur < e)) {
		return "it is the user's quiet hours"
	}
	return ""
}
