/*
 * Copyright (c) 2026. Papermark - All Rights Reserved
 * Unauthorized copying of this file, via any medium is strictly prohibited
 * Proprietary and confidential
 * Written by Ebot Tabi <ebot.tabi@gmail.com>, 18/10/2026
 */
package services

import (
	"errors"
	"testing"
	"time"
)

func TestDraftFrom(t *testing.T) {
	loc, _ := time.LoadLocation("Europe/Lisbon")
	now := time.Date(2026, 10, 18, 9, 0, 0, 0, loc)
	one := []string{"me@example.com"}
	two := []string{"me@example.com", "work@example.com"}

	d, err := draftFrom("Sure:\n"+`{"title":"Invoices","prompt":"Add new invoices to expenses.csv","trigger":"time","repeat":"weekdays","time":"08:00","weekday":3,"does":"Adds invoices.","tells":"Feed"}`, one, now, loc)
	if err != nil || *d.Repeat != "weekdays" || *d.Time != "08:00" || d.Weekday != nil || d.When != "Weekdays at 08:00" || d.Question != nil || string(d.Rule) != "null" {
		t.Fatalf("time draft: %+v %v", d, err)
	}

	d, err = draftFrom(`{"title":"Bank","prompt":"Tell me what failed","trigger":"email","emailCondition":"from my bank about a failed payment","emailAccount":"ME@example.com"}`, one, now, loc)
	if err != nil || d.Repeat != nil || string(d.Rule) != `{"account":"me@example.com","smart":"from my bank about a failed payment"}` ||
		d.When != "When an email arrives from my bank about a failed payment" || d.Question != nil {
		t.Fatalf("email draft: %+v %s %v", d, d.Rule, err)
	}

	d, err = draftFrom(`{"title":"Bank","prompt":"p","trigger":"email","emailCondition":"from my bank"}`, two, now, loc)
	if err != nil || d.Question == nil || *d.Question != "Which inbox should Keen check: me@example.com or work@example.com?" {
		t.Fatalf("ambiguous account must ask: %+v %v", d, err)
	}

	d, err = draftFrom(`{"title":"Weekly","prompt":"Review my week","trigger":"time","repeat":"weekly","time":"17:00","weekday":5,"question":"Which day?"}`, one, now, loc)
	if err != nil || d.When != "Fridays at 17:00" || d.Question == nil || *d.Question != "Which day?" {
		t.Fatalf("weekly draft with question: %+v %v", d, err)
	}

	for _, bad := range []string{
		`no json here`,
		`{"title":"x","prompt":"p","trigger":"watch"}`,
		`{"title":"x","prompt":"p","trigger":"time","repeat":"monthly","time":"08:00"}`,
		`{"title":"x","prompt":"p","trigger":"time","repeat":"daily","time":"8am"}`,
		`{"title":"x","prompt":"p","trigger":"time","repeat":"weekly","time":"08:00"}`,
		`{"title":"x","prompt":"","trigger":"time","repeat":"daily","time":"08:00"}`,
		`{"title":"x","prompt":"p","trigger":"email","emailCondition":""}`,
	} {
		if _, err := draftFrom(bad, one, now, loc); !errors.Is(err, ErrDraftUnusable) {
			t.Errorf("%s: %v", bad, err)
		}
	}
}

func TestWhenSentence(t *testing.T) {
	mon := 1
	sun := 7
	for _, c := range []struct {
		repeat, hhmm string
		wd           *int
		want         string
	}{
		{"daily", "07:30", nil, "Every day at 07:30"},
		{"hourly", "00:15", nil, "Every hour at :15"},
		{"weekly", "09:00", &mon, "Mondays at 09:00"},
		{"weekly", "09:00", &sun, "Sundays at 09:00"},
	} {
		if got := WhenSentence("time", c.repeat, c.hhmm, c.wd, nil); got != c.want {
			t.Errorf("%s %s: %q", c.repeat, c.hhmm, got)
		}
	}
}
