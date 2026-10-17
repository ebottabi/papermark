/*
 * Copyright (c) 2026. Papermark - All Rights Reserved
 * Unauthorized copying of this file, via any medium is strictly prohibited
 * Proprietary and confidential
 * Written by Ebot Tabi <ebot.tabi@gmail.com>, 15/10/2026
 */
// Package notifications delivers push alerts to the user's registered devices.
package notifications

import (
	"context"
	"errors"
	"fmt"
	"log"
	"unicode/utf8"

	"papermark/internal/models/queries"
	"papermark/internal/notifications/apns"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"
)

// maxAlertBytes keeps the payload well under APNs' 4 KB limit.
const maxAlertBytes = 1000

// Sender is the APNs client surface PushService needs.
type Sender interface {
	Send(ctx context.Context, deviceToken, environment string, a apns.Alert) error
}

// PushService sends a task's alert to every registered device and prunes
// tokens APNs reports dead. With a nil sender (APNS_* unset) Send does nothing.
type PushService struct {
	pool   *pgxpool.Pool
	sender Sender
}

// NewPushService constructs a PushService. sender may be nil.
func NewPushService(pool *pgxpool.Pool, sender Sender) *PushService {
	return &PushService{pool: pool, sender: sender}
}

// Send pushes text about taskID (event eventType) to the task owner's devices.
func (s *PushService) Send(ctx context.Context, taskID, eventType, text string) error {
	if s.sender == nil {
		return nil
	}
	id, err := uuid.Parse(taskID)
	if err != nil {
		return fmt.Errorf("push: task id: %w", err)
	}
	devices, err := queries.ListTaskOwnerDevices(ctx, s.pool, id)
	if err != nil {
		return fmt.Errorf("push: list devices: %w", err)
	}
	alert := apns.Alert{Text: truncate(text, maxAlertBytes), TaskID: taskID, Type: eventType}
	var errs []error
	for _, d := range devices {
		err := s.sender.Send(ctx, d.Token, d.Environment, alert)
		switch {
		case err == nil:
		case apns.IsUnregistered(err):
			log.Printf("push: pruning device id=%s: %v", d.ID, err)
			if _, derr := queries.SoftDeleteDeviceByToken(ctx, s.pool, d.UserID, d.Token); derr != nil {
				errs = append(errs, derr)
			}
		default:
			errs = append(errs, fmt.Errorf("device %s: %w", d.ID, err))
		}
	}
	return errors.Join(errs...)
}

func truncate(s string, n int) string {
	if len(s) <= n {
		return s
	}
	for n > 0 && !utf8.RuneStart(s[n]) {
		n--
	}
	return s[:n] + "…"
}
