/*
 * Copyright (c) 2026. Papermark - All Rights Reserved
 * Unauthorized copying of this file, via any medium is strictly prohibited
 * Proprietary and confidential
 * Written by Ebot Tabi <ebot.tabi@gmail.com>, 17/10/2026
 */
package services

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"

	"papermark/internal/database"
	"papermark/internal/models"
	"papermark/internal/models/queries"

	"github.com/google/uuid"
)

func str(s string) *string { return &s }

func TestSlotsAreComputedInTheOwnersTimeZone(t *testing.T) {
	s, _, u := newSchedules(t)
	ctx := context.Background()
	prefs := &PreferenceService{pool: s.pool}
	if _, err := prefs.Update(ctx, u, PreferencesPatch{Timezone: str("Asia/Tokyo")}); err != nil {
		t.Fatal(err)
	}
	sc, err := s.Create(ctx, u, ScheduleFields{Prompt: "p", Repeat: "daily", Time: "09:00", Enabled: true})
	if err != nil {
		t.Fatal(err)
	}
	if got := sc.NextRunAt.In(time.UTC).Format("15:04"); got != "00:00" {
		t.Fatalf("09:00 Tokyo is 00:00 UTC, got %s", got)
	}
	if _, err := prefs.Update(ctx, u, PreferencesPatch{Timezone: str("America/New_York")}); err != nil {
		t.Fatal(err)
	}
	moved, _ := s.Get(ctx, u, sc.ID)
	ny, _ := time.LoadLocation("America/New_York")
	if h := moved.NextRunAt.In(ny).Format("15:04"); h != "09:00" {
		t.Fatalf("changing the zone must keep 09:00 wall time, got %s", h)
	}
}

func TestBriefDefaultsAndTitles(t *testing.T) {
	s, _, u := newSchedules(t)
	ctx := context.Background()
	b, err := s.Create(ctx, u, ScheduleFields{Kind: models.ScheduleBrief, Repeat: "weekdays", Time: "07:30", Enabled: true})
	if err != nil {
		t.Fatal(err)
	}
	if b.Kind != "brief" || b.Title != "Morning brief" || b.Prompt != BriefPrompt || b.Repeat != "weekdays" {
		t.Fatalf("brief %+v", b)
	}
	for _, want := range []string{"gmail_digest", "sinceLast", "publish_update", "Email", "News", "Weather", "detail", "url"} {
		if !strings.Contains(BriefPrompt, want) {
			t.Errorf("brief prompt lacks %q", want)
		}
	}
	long := strings.Repeat("word ", 20)
	c, err := s.Create(ctx, u, ScheduleFields{Prompt: long, Repeat: "daily", Time: "09:00", Enabled: true})
	if err != nil || c.Kind != "custom" || c.Title != strings.TrimSpace(long[:40]) {
		t.Fatalf("custom %+v %v", c, err)
	}
	if _, err := s.Create(ctx, u, ScheduleFields{Kind: "weird", Prompt: "p", Repeat: "daily", Time: "09:00"}); !errors.Is(err, ErrInvalidSchedule) {
		t.Fatalf("bad kind: %v", err)
	}
}

func TestPreferencesValidation(t *testing.T) {
	pool, _ := database.NewTestDatabase(t)
	u := testUser(t, pool)
	prefs := &PreferenceService{pool: pool}
	ctx := context.Background()
	p, err := prefs.Get(ctx, u)
	if err != nil || p.Timezone != "UTC" || p.QuietStart != "22:00" || p.QuietEnd != "07:00" || p.PushLimit != 5 || p.NewsTopics == nil || len(p.NewsTopics) != 0 {
		t.Fatalf("defaults %+v %v", p, err)
	}
	three, over, neg := 3, 21, -1
	for name, patch := range map[string]PreferencesPatch{
		"timezone":  {Timezone: str("Mars/Base")},
		"empty tz":  {Timezone: str("")},
		"Local":     {Timezone: str("Local")},
		"quietHHMM": {QuietStart: str("9pm")},
		"quiet 24":  {QuietEnd: str("24:00")},
		"limit hi":  {PushLimit: &over},
		"limit lo":  {PushLimit: &neg},
	} {
		if _, err := prefs.Update(ctx, u, patch); err == nil {
			t.Errorf("%s accepted", name)
		}
	}
	topics := []string{" chess ", "chess", "", "space"}
	got, err := prefs.Update(ctx, u, PreferencesPatch{PushLimit: &three, NewsTopics: &topics, Timezone: str("Europe/Berlin")})
	if err != nil || got.PushLimit != 3 || strings.Join(got.NewsTopics, ",") != "chess,space" || got.QuietStart != "22:00" {
		t.Fatalf("patch %+v %v", got, err)
	}
	if again, _ := prefs.Get(ctx, u); again.Timezone != "Europe/Berlin" || again.PushLimit != 3 {
		t.Fatalf("not stored: %+v", again)
	}
}

func TestUpdatesPaginateNewestFirstAndStayScopedToTheUser(t *testing.T) {
	pool, _ := database.NewTestDatabase(t)
	ctx := context.Background()
	u, other := testUser(t, pool), testUser(t, pool)
	task := func(user uuid.UUID) uuid.UUID {
		tk := &models.Task{UserID: user, Prompt: "p", Status: models.TaskDone, Mode: "quality"}
		if err := queries.InsertTask(ctx, pool, tk); err != nil {
			t.Fatal(err)
		}
		return tk.ID
	}
	add := func(user uuid.UUID, importance, title string) *models.Update {
		up := &models.Update{UserID: user, TaskID: taskPtr(task(user)), Importance: importance, Title: title, Sections: []models.Section{}}
		if err := queries.InsertUpdate(ctx, pool, up); err != nil {
			t.Fatal(err)
		}
		time.Sleep(3 * time.Millisecond)
		return up
	}
	for i, title := range []string{"a", "b", "c", "d", "e"} {
		imp := models.ImportanceFeed
		if i == 1 {
			imp = models.ImportanceQuiet
		}
		add(u, imp, title)
	}
	foreign := add(other, models.ImportanceFeed, "theirs")
	svc := &UpdateService{pool: pool}

	page1, err := svc.List(ctx, u, nil, 2)
	if err != nil || len(page1) != 2 || page1[0].Title != "e" || page1[1].Title != "d" {
		t.Fatalf("page 1: %v %v", titles(page1), err)
	}
	before := page1[1].CreatedAt
	page2, _ := svc.List(ctx, u, &before, 2)
	if len(page2) != 2 || page2[0].Title != "c" || page2[1].Title != "a" {
		t.Fatalf("page 2 (quiet excluded): %v", titles(page2))
	}
	before = page2[1].CreatedAt
	if page3, _ := svc.List(ctx, u, &before, 2); len(page3) != 0 {
		t.Fatalf("page 3: %v", titles(page3))
	}

	if err := svc.MarkRead(ctx, u, foreign.ID); !errors.Is(err, queries.ErrNotFound) {
		t.Fatalf("marking another user's update: %v", err)
	}
	if theirs, _ := svc.List(ctx, other, nil, 10); theirs[0].ReadAt != nil {
		t.Fatal("another user's update was marked read")
	}
	if err := svc.MarkRead(ctx, u, page1[0].ID); err != nil {
		t.Fatal(err)
	}
	if err := svc.MarkAllRead(ctx, u); err != nil {
		t.Fatal(err)
	}
	all, _ := svc.List(ctx, u, nil, 10)
	for _, up := range all {
		if up.ReadAt == nil {
			t.Errorf("%s unread after read-all", up.Title)
		}
	}
	if theirs, _ := svc.List(ctx, other, nil, 10); theirs[0].ReadAt != nil {
		t.Fatal("read-all touched another user")
	}
}

func titles(us []*models.Update) []string {
	var out []string
	for _, u := range us {
		out = append(out, u.Title)
	}
	return out
}

func TestMemoriesDedupeSearchAndStayScopedToTheUser(t *testing.T) {
	pool, _ := database.NewTestDatabase(t)
	ctx := context.Background()
	u, other := testUser(t, pool), testUser(t, pool)
	svc := &MemoryService{pool: pool}

	m, created, err := svc.Create(ctx, u, "Lives in Lisbon and works remotely")
	if err != nil || !created {
		t.Fatal(created, err)
	}
	dup, created, err := svc.Create(ctx, u, "  lives in LISBON, and works remotely! ")
	if err != nil || created || dup.ID != m.ID {
		t.Fatalf("near-identical text must dedupe: %v %v", dup, err)
	}
	if _, created, _ := svc.Create(ctx, other, "Lives in Lisbon and works remotely"); !created {
		t.Fatal("dedupe must be per user")
	}
	svc.Create(ctx, u, "Allergic to peanuts")
	svc.Create(ctx, u, "Prefers window seats on flights")
	if _, _, err := svc.Create(ctx, u, "   "); err == nil {
		t.Fatal("blank memory accepted")
	}

	found, _ := queries.SearchMemories(ctx, pool, u, "flights seats", 10)
	if len(found) != 1 || found[0].Text != "Prefers window seats on flights" {
		t.Fatalf("search %v", found)
	}
	if none, _ := queries.SearchMemories(ctx, pool, other, "peanuts", 10); len(none) != 0 {
		t.Fatalf("another user's search found %v", none)
	}
	rel, _ := queries.RelevantMemories(ctx, pool, u, "book flights to Lisbon", 2)
	if len(rel) != 2 {
		t.Fatalf("relevant %v", rel)
	}
	all, _ := queries.RelevantMemories(ctx, pool, u, "zzz nothing matches", 15)
	if len(all) != 3 {
		t.Fatalf("with no match the most recent fill in: %v", all)
	}

	if _, err := svc.Update(ctx, other, m.ID, "hijack"); !errors.Is(err, queries.ErrNotFound) {
		t.Fatalf("update another user's memory: %v", err)
	}
	if err := svc.Delete(ctx, other, m.ID); !errors.Is(err, queries.ErrNotFound) {
		t.Fatalf("delete another user's memory: %v", err)
	}
	upd, err := svc.Update(ctx, u, m.ID, "Lives in Porto")
	if err != nil || upd.Text != "Lives in Porto" || !upd.UpdatedAt.After(m.UpdatedAt) {
		t.Fatalf("update %+v %v", upd, err)
	}
	if _, err := svc.Update(ctx, u, m.ID, "allergic to PEANUTS"); err == nil {
		t.Fatal("renaming onto an existing memory must fail")
	}
	if err := svc.Delete(ctx, u, m.ID); err != nil {
		t.Fatal(err)
	}
	if list, _ := svc.List(ctx, u); len(list) != 2 {
		t.Fatalf("list %v", list)
	}
}

func TestFirstMessageCarriesRelevantMemories(t *testing.T) {
	pool, _ := database.NewTestDatabase(t)
	ctx := context.Background()
	u := testUser(t, pool)
	(&MemoryService{pool: pool}).Create(ctx, u, "Allergic to peanuts")
	s := &TaskService{pool: pool}
	got, err := s.withContext(ctx, u, nil, "what snacks are safe for me, peanuts?")
	if err != nil || !strings.Contains(got, "What you remember about the user:\n- Allergic to peanuts") || !strings.HasSuffix(got, "Task:\nwhat snacks are safe for me, peanuts?") {
		t.Fatalf("%q %v", got, err)
	}
}

func taskPtr(id uuid.UUID) *uuid.UUID { return &id }
