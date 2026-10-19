/*
 * Copyright (c) 2026. Papermark - All Rights Reserved
 * Unauthorized copying of this file, via any medium is strictly prohibited
 * Proprietary and confidential
 * Written by Ebot Tabi <ebot.tabi@gmail.com>, 18/10/2026
 */

package skills

import (
	"context"
	"errors"
	"strings"
	"testing"

	"papermark/internal/database"
	"papermark/internal/models/queries"
)

func TestBuiltinSkillsLoad(t *testing.T) {
	want := []string{"bills-and-renewals", "book-flight", "ctf", "daily-briefing", "expenses", "follow-ups", "inbox-triage", "meeting-follow-up", "meeting-prep", "research-report", "s3-pentest-commands", "schedule-meeting", "security-recon", "travel-itinerary", "web-app-pentest", "weekly-review", "write-in-my-voice"}
	got := Builtin()
	if len(got) != len(want) {
		t.Fatalf("got %d skills, want %d", len(got), len(want))
	}
	for i, s := range got {
		if s.Name != want[i] || s.Description == "" || s.Instructions == "" {
			t.Errorf("skill %d = %q (description %q), want %q with text", i, s.Name, s.Description, want[i])
		}
	}
}

func TestParseRejectsMalformedFiles(t *testing.T) {
	for name, raw := range map[string]string{
		"no-header":  "1. do it",
		"no-desc":    "---\ntitle: x\n---\n1. do it",
		"empty-body": "---\ndescription: x\n---\n  ",
		"Bad_Name":   "---\ndescription: x\n---\n1. do it",
	} {
		if _, err := parse(name, raw); err == nil {
			t.Errorf("%s parsed", name)
		}
	}
	s, err := parse("ok", "---\ndescription:  Does it. \n---\n\n1. do it\n")
	if err != nil || s.Description != "Does it." || s.Instructions != "1. do it" {
		t.Fatalf("parse = %+v, %v", s, err)
	}
}

func TestUserSkillsJoinAndReplaceBuiltins(t *testing.T) {
	ctx := context.Background()
	pool, _ := database.NewTestDatabase(t)
	u, _, err := queries.UpsertGoogleUser(ctx, pool, "skills-sub", "skills@gmail.com", "Test", "")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := queries.InsertSkill(ctx, pool, u.ID, "inbox-triage", "Mine", "my steps"); err != nil {
		t.Fatal(err)
	}
	if _, err := queries.InsertSkill(ctx, pool, u.ID, "aaa-expenses", "Expenses", "file receipts"); err != nil {
		t.Fatal(err)
	}
	list, err := For(ctx, pool, u.ID)
	if err != nil {
		t.Fatal(err)
	}
	if len(list) != len(Builtin())+1 || list[0].Name != "aaa-expenses" {
		t.Fatalf("got %d skills starting %q", len(list), list[0].Name)
	}
	for _, s := range list {
		if s.Name == "inbox-triage" && s.Instructions != "my steps" {
			t.Fatalf("user skill did not replace the built-in one: %q", s.Instructions)
		}
	}
	if b, _ := Find("inbox-triage"); b.Instructions == "my steps" {
		t.Fatal("the user's skill changed the built-in one")
	}
}

func TestPickReadsOneOrManyOrNone(t *testing.T) {
	list := Builtin()
	reply := func(text string) Completer {
		return func(context.Context, string, string, int) (string, error) { return text, nil }
	}
	for in, want := range map[string]string{
		"inbox-triage":               "inbox-triage",
		"meeting-prep, follow-ups\n": "meeting-prep follow-ups",
		"Skills: weekly-review, expenses, travel-itinerary, follow-ups": "weekly-review expenses travel-itinerary",
		"none":                       "",
		"no-such-skill":              "",
		"inbox-triage, inbox-triage": "inbox-triage",
	} {
		picked, ok := Pick(context.Background(), reply(in), "task", list)
		got := strings.Join(Names(picked), " ")
		if !ok {
			t.Errorf("reply %q failed", in)
		}
		if got != want {
			t.Errorf("reply %q picked %q, want %q", in, got, want)
		}
	}
	failing := func(context.Context, string, string, int) (string, error) { return "", errors.New("down") }
	if got, ok := Pick(context.Background(), failing, "task", list); got != nil || ok {
		t.Fatalf("a failed call picked %v, ok %v", got, ok)
	}
}

func TestInjectPutsEverySkillBeforeTheTask(t *testing.T) {
	picked := []Skill{{Name: "a", Instructions: "do a"}, {Name: "b", Instructions: "do b"}}
	got := Inject(picked, "the task")
	if !strings.Contains(got, "Skill \"a\":\ndo a") || !strings.Contains(got, "Skill \"b\":\ndo b") || !strings.HasSuffix(got, "\n\nthe task") {
		t.Fatalf("inject = %q", got)
	}
	if Inject(nil, "the task") != "the task" {
		t.Fatal("no skills changed the task")
	}
}
