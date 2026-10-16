/*
 * Copyright (c) 2026. Papermark - All Rights Reserved
 * Unauthorized copying of this file, via any medium is strictly prohibited
 * Proprietary and confidential
 * Written by Ebot Tabi <ebot.tabi@gmail.com>, 15/10/2026
 */
package workers

import (
	"context"
	"log"
	"time"

	"papermark/internal/services"
)

// SchedulerInterval is how often RunScheduler looks for due schedules.
const SchedulerInterval = 15 * time.Second

// RunScheduler runs due schedules every interval until ctx is cancelled.
// Every server and worker process may run it: claims use SKIP LOCKED.
func RunScheduler(ctx context.Context, s *services.ScheduleService, interval time.Duration) {
	t := time.NewTicker(interval)
	defer t.Stop()
	for {
		if _, err := s.RunDue(ctx, time.Now()); err != nil && ctx.Err() == nil {
			log.Printf("scheduler: %v", err)
		}
		select {
		case <-ctx.Done():
			return
		case <-t.C:
		}
	}
}
