/*
 * Copyright (c) 2026. Papermark - All Rights Reserved
 * Unauthorized copying of this file, via any medium is strictly prohibited
 * Proprietary and confidential
 * Written by Ebot Tabi <ebot.tabi@gmail.com>, 17/10/2026
 */
package services

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"net/url"
	"strings"
	"time"

	"papermark/internal/models"
	"papermark/internal/models/queries"
	"papermark/internal/safehttp"

	"github.com/google/uuid"
)

// Per-user limits on trigger routines.
const (
	MaxEmailRoutines = 10
	MaxWatchRoutines = 20
	maxRunsCeiling   = 100
	maxIntervalMins  = 1440
)

// EmailRule picks the mail that fires an email routine. Every set field must
// match. Account limits the rule to one mailbox.
type EmailRule struct {
	Account       string `json:"account,omitempty"`
	From          string `json:"from,omitempty"`
	Subject       string `json:"subject,omitempty"`
	Label         string `json:"label,omitempty"`
	HasAttachment *bool  `json:"hasAttachment,omitempty"`
	Smart         string `json:"smart,omitempty"`
}

// WatchRule says what a watch routine looks at and when it fires.
type WatchRule struct {
	Kind            string   `json:"kind"`
	URL             string   `json:"url,omitempty"`
	Query           string   `json:"query,omitempty"`
	Selector        string   `json:"selector,omitempty"`
	Condition       string   `json:"condition"`
	Threshold       *float64 `json:"threshold,omitempty"`
	Text            string   `json:"text,omitempty"`
	IntervalMinutes int      `json:"intervalMinutes"`
}

func invalid(format string, args ...any) error {
	return fmt.Errorf("%w: %s", ErrInvalidSchedule, fmt.Sprintf(format, args...))
}

// normalizeRule validates rule for trigger and returns its stored JSON. A
// time trigger has no rule.
func (s *ScheduleService) normalizeRule(ctx context.Context, trigger string, rule json.RawMessage) (json.RawMessage, error) {
	switch trigger {
	case models.TriggerTime:
		return nil, nil
	case models.TriggerEmail:
		var r EmailRule
		if err := decodeRule(rule, &r); err != nil {
			return nil, err
		}
		r.Account, r.From, r.Subject, r.Label, r.Smart = strings.TrimSpace(r.Account), strings.TrimSpace(r.From), strings.TrimSpace(r.Subject),
			strings.TrimSpace(r.Label), strings.TrimSpace(r.Smart)
		if r.From == "" && r.Subject == "" && r.Label == "" && r.Smart == "" && r.HasAttachment == nil {
			return nil, invalid("an email rule needs at least one of from, subject, label, hasAttachment or smart")
		}
		return json.Marshal(r)
	case models.TriggerWatch:
		var r WatchRule
		if err := decodeRule(rule, &r); err != nil {
			return nil, err
		}
		r.URL, r.Query, r.Selector, r.Text = strings.TrimSpace(r.URL), strings.TrimSpace(r.Query), strings.TrimSpace(r.Selector), strings.TrimSpace(r.Text)
		if r.IntervalMinutes == 0 {
			r.IntervalMinutes = 1
		}
		if r.IntervalMinutes < 1 || r.IntervalMinutes > maxIntervalMins {
			return nil, invalid("intervalMinutes must be between 1 and %d", maxIntervalMins)
		}
		switch r.Kind {
		case models.WatchPage:
			if r.URL == "" {
				return nil, invalid("a page watch needs a url")
			}
			if err := s.checkURL(ctx, r.URL); err != nil {
				return nil, err
			}
		case models.WatchSearch:
			if r.Query == "" {
				return nil, invalid("a search watch needs a query")
			}
		default:
			return nil, invalid("kind must be page or search")
		}
		switch r.Condition {
		case models.WatchChanged, models.WatchNewItems:
		case models.WatchBelow, models.WatchAbove:
			if r.Threshold == nil {
				return nil, invalid("%s needs a threshold", r.Condition)
			}
		case models.WatchContains:
			if r.Text == "" {
				return nil, invalid("contains needs text")
			}
		default:
			return nil, invalid("condition must be changed, below, above, contains or newItems")
		}
		return json.Marshal(r)
	}
	return nil, invalid("trigger must be time, email or watch")
}

func decodeRule(raw json.RawMessage, into any) error {
	if len(raw) == 0 || string(raw) == "null" {
		return invalid("rule is required for this trigger")
	}
	if err := json.Unmarshal(raw, into); err != nil {
		return invalid("rule is not valid: %v", err)
	}
	return nil
}

// checkURL refuses a watch url that is not a public http(s) address. A host
// that does not resolve yet passes; the fetch fails later and is counted.
func (s *ScheduleService) checkURL(ctx context.Context, raw string) error {
	if _, err := s.web.Check(ctx, raw); err != nil {
		return invalid("%s", watchRefusal(raw, err))
	}
	return nil
}

// watchRefusal says in plain words why a page can't be watched, for the app to show as is.
func watchRefusal(raw string, err error) string {
	if !errors.Is(err, safehttp.ErrBlocked) {
		return "Enter a full web address that starts with http:// or https://."
	}
	host := raw
	if u, perr := url.Parse(strings.TrimSpace(raw)); perr == nil && u.Hostname() != "" {
		host = u.Hostname()
	}
	return fmt.Sprintf("Keen can only watch public web pages. %s is a private address.", host)
}

// ruleTitle names a trigger routine that was created without a title.
func ruleTitle(trigger string, rule json.RawMessage) string {
	switch trigger {
	case models.TriggerEmail:
		var r EmailRule
		json.Unmarshal(rule, &r)
		for _, p := range []string{r.From, r.Subject, r.Smart} {
			if p != "" {
				return "Email: " + p
			}
		}
		return "Email routine"
	case models.TriggerWatch:
		var r WatchRule
		json.Unmarshal(rule, &r)
		if r.Kind == models.WatchSearch {
			return "Watch: " + r.Query
		}
		if u, err := url.Parse(r.URL); err == nil {
			return "Watch: " + u.Hostname()
		}
	}
	return "Routine"
}

func watchKey(r WatchRule) string {
	id := r.URL
	if r.Kind == models.WatchSearch {
		id = r.Query
	}
	sum := sha256.Sum256([]byte(r.Kind + "\x00" + id + "\x00" + r.Selector))
	return hex.EncodeToString(sum[:])
}

// syncTrigger brings the poll tables in line with schedule sc after it was
// written, disabled or deleted, in the same transaction.
func syncTrigger(ctx context.Context, db queries.DBTX, sc *models.Schedule, live bool) error {
	if sc.Trigger == models.TriggerEmail {
		if err := syncMailboxPolls(ctx, db, sc.UserID); err != nil {
			return err
		}
	}
	if sc.Trigger == models.TriggerWatch && live && sc.Enabled {
		return subscribeWatch(ctx, db, sc)
	}
	return unsubscribeWatch(ctx, db, sc.ID)
}

func subscribeWatch(ctx context.Context, db queries.DBTX, sc *models.Schedule) error {
	var r WatchRule
	if err := json.Unmarshal(sc.Rule, &r); err != nil {
		return err
	}
	old, err := queries.WatchSubscriptionTarget(ctx, db, sc.ID)
	if err != nil {
		return err
	}
	t, err := queries.UpsertWatchTarget(ctx, db, &models.WatchTarget{Kind: r.Kind, Key: watchKey(r), URL: r.URL, Query: r.Query, Selector: r.Selector,
		IntervalSeconds: r.IntervalMinutes * 60}, time.Now().UTC())
	if err != nil {
		return err
	}
	if err := queries.UpsertWatchSubscription(ctx, db, &models.WatchSubscription{RoutineID: sc.ID, UserID: sc.UserID, TargetID: t.ID,
		Condition: r.Condition, Threshold: r.Threshold, Text: r.Text}); err != nil {
		return err
	}
	if old != uuid.Nil && old != t.ID {
		if err := queries.RefreshWatchTarget(ctx, db, old); err != nil {
			return err
		}
	}
	return queries.RefreshWatchTarget(ctx, db, t.ID)
}

func unsubscribeWatch(ctx context.Context, db queries.DBTX, routine uuid.UUID) error {
	target, err := queries.DeleteWatchSubscription(ctx, db, routine)
	if err != nil || target == uuid.Nil {
		return err
	}
	return queries.RefreshWatchTarget(ctx, db, target)
}

// syncMailboxPolls keeps user's mailbox_polls rows to the connected accounts
// an enabled email routine looks at. A new row starts with no history id: its
// first check records where the mailbox is and fires nothing.
func syncMailboxPolls(ctx context.Context, db queries.DBTX, user uuid.UUID) error {
	routines, err := queries.ListEmailRoutines(ctx, db, user)
	if err != nil {
		return err
	}
	accounts, err := queries.ListUserGmailAccounts(ctx, db, user)
	if err != nil {
		return err
	}
	keep := []uuid.UUID{}
	for _, a := range accounts {
		if a.Status != models.ConnectorConnected {
			continue
		}
		for _, sc := range routines {
			var r EmailRule
			if json.Unmarshal(sc.Rule, &r) == nil && (r.Account == "" || strings.EqualFold(r.Account, a.Email)) {
				keep = append(keep, a.ID)
				break
			}
		}
	}
	for _, id := range keep {
		if err := queries.EnsureMailboxPoll(ctx, db, id, user, "", time.Now().UTC()); err != nil {
			return err
		}
	}
	return queries.DeleteMailboxPollsExcept(ctx, db, user, keep)
}
