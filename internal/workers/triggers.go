/*
 * Copyright (c) 2026. Papermark - All Rights Reserved
 * Unauthorized copying of this file, via any medium is strictly prohibited
 * Proprietary and confidential
 * Written by Ebot Tabi <ebot.tabi@gmail.com>, 17/10/2026
 */
package workers

import (
	"context"
	"log"
	"time"

	"papermark/internal/services"
)

// TriggerInterval is how often RunTriggers looks for mailboxes that are due.
const TriggerInterval = 5 * time.Second

// RunTriggers checks due mailboxes and contacts syncs every interval until ctx
// is cancelled. Every server and worker process may run it: claims use SKIP LOCKED.
func RunTriggers(ctx context.Context, t *services.TriggerService, interval time.Duration) {
	// A full contacts sync can take a minute; it runs beside mail so mail triggers stay prompt.
	go runContacts(ctx, t, 6*interval)
	tick := time.NewTicker(interval)
	defer tick.Stop()
	for {
		if _, err := t.RunMail(ctx); err != nil && ctx.Err() == nil {
			log.Printf("triggers: mail: %v", err)
		}
		select {
		case <-ctx.Done():
			return
		case <-tick.C:
		}
	}
}

func runContacts(ctx context.Context, t *services.TriggerService, interval time.Duration) {
	tick := time.NewTicker(interval)
	defer tick.Stop()
	for {
		if _, err := t.RunContacts(ctx); err != nil && ctx.Err() == nil {
			log.Printf("triggers: contacts: %v", err)
		}
		select {
		case <-ctx.Done():
			return
		case <-tick.C:
		}
	}
}
