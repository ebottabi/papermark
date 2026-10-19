/*
 * Copyright (c) 2026. Papermark - All Rights Reserved
 * Unauthorized copying of this file, via any medium is strictly prohibited
 * Proprietary and confidential
 * Written by Ebot Tabi <ebot.tabi@gmail.com>, 17/10/2026
 */
package services

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"time"

	"papermark/internal/models"
	"papermark/internal/models/queries"

	"github.com/google/uuid"
)

// Per-user limits on trigger routines.
const (
	MaxEmailRoutines = 10
	maxRunsCeiling   = 100
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

func invalid(format string, args ...any) error {
	return fmt.Errorf("%w: %s", ErrInvalidSchedule, fmt.Sprintf(format, args...))
}

// normalizeRule validates rule for trigger and returns its stored JSON. A
// time trigger has no rule.
func normalizeRule(trigger string, rule json.RawMessage) (json.RawMessage, error) {
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
	}
	return nil, invalid("trigger must be time or email")
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
	}
	return "Routine"
}

// syncTrigger brings the poll tables in line with schedule sc after it was
// written, disabled or deleted, in the same transaction.
func syncTrigger(ctx context.Context, db queries.DBTX, sc *models.Schedule) error {
	if sc.Trigger == models.TriggerEmail {
		return syncMailboxPolls(ctx, db, sc.UserID)
	}
	return nil
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
