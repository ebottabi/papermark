/*
 * Copyright (c) 2026. Papermark - All Rights Reserved
 * Unauthorized copying of this file, via any medium is strictly prohibited
 * Proprietary and confidential
 * Written by Ebot Tabi <ebot.tabi@gmail.com>, 17/10/2026
 */
package tools

import (
	"testing"
	"time"

	"papermark/internal/models"
)

func TestPushHeld(t *testing.T) {
	p := &models.Preferences{Timezone: "Europe/Berlin", QuietStart: "22:00", QuietEnd: "07:00", PushLimit: 2}
	at := func(s string) time.Time {
		v, err := time.Parse(time.RFC3339, s)
		if err != nil {
			t.Fatal(err)
		}
		return v
	}
	for _, c := range []struct {
		name   string
		now    time.Time
		pushed int
		held   bool
	}{
		{"daytime under the limit", at("2026-06-10T12:00:00+02:00"), 1, false},
		{"limit reached", at("2026-06-10T12:00:00+02:00"), 2, true},
		{"late evening quiet", at("2026-06-10T23:30:00+02:00"), 0, true},
		{"early morning quiet", at("2026-06-10T06:59:00+02:00"), 0, true},
		{"quiet ends at 07:00", at("2026-06-10T07:00:00+02:00"), 0, false},
		{"quiet starts at 22:00", at("2026-06-10T22:00:00+02:00"), 0, true},
		{"quiet is judged in the user's zone not UTC", at("2026-06-10T21:30:00Z"), 0, true},
	} {
		if got := pushHeld(p, c.now, c.pushed) != ""; got != c.held {
			t.Errorf("%s: held %v, want %v", c.name, got, c.held)
		}
	}
	day := &models.Preferences{Timezone: "UTC", QuietStart: "09:00", QuietEnd: "17:00", PushLimit: 5}
	if pushHeld(day, at("2026-06-10T12:00:00Z"), 0) == "" || pushHeld(day, at("2026-06-10T18:00:00Z"), 0) != "" {
		t.Error("a quiet window within one day must hold only inside it")
	}
	if pushHeld(&models.Preferences{Timezone: "UTC", QuietStart: "22:00", QuietEnd: "07:00", PushLimit: 0}, at("2026-06-10T12:00:00Z"), 0) == "" {
		t.Error("a limit of 0 holds every push")
	}
}

func TestDayBoundsFollowTheUsersZoneAcrossDST(t *testing.T) {
	ny, _ := time.LoadLocation("America/New_York")
	from, to := dayBounds(time.Date(2026, 3, 8, 15, 0, 0, 0, time.UTC), ny)
	if !from.Equal(time.Date(2026, 3, 8, 5, 0, 0, 0, time.UTC)) || to.Sub(from) != 23*time.Hour {
		t.Fatalf("spring-forward day %s to %s", from, to)
	}
}
