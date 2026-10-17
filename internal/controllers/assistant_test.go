/*
 * Copyright (c) 2026. Papermark - All Rights Reserved
 * Unauthorized copying of this file, via any medium is strictly prohibited
 * Proprietary and confidential
 * Written by Ebot Tabi <ebot.tabi@gmail.com>, 17/10/2026
 */
package controllers_test

import (
	"context"
	"net/url"
	"testing"
	"time"

	"papermark/internal/controllers"
	"papermark/internal/database"
	"papermark/internal/models"
	"papermark/internal/models/queries"
	"papermark/internal/router"
	"papermark/internal/services"
)

func TestPreferencesMemoriesAndUpdatesAPI(t *testing.T) {
	pool, _ := database.NewTestDatabase(t)
	sched := services.NewScheduleService(pool)
	sched.SetTaskCreator(&recordingTasks{})
	api := newAPI(t, &router.Config{Pool: pool, Schedules: controllers.NewScheduleController(sched),
		Prefs: controllers.NewPreferencesController(services.NewPreferenceService(pool)), Updates: controllers.NewUpdateController(services.NewUpdateService(pool)),
		Memories: controllers.NewMemoryController(services.NewMemoryService(pool))})

	code, body := api.do("GET", "/v1/me/preferences", "")
	p := body.data()
	if code != 200 || p["timezone"] != "UTC" || p["quietStart"] != "22:00" || p["quietEnd"] != "07:00" || p["pushLimit"] != float64(5) || len(p["newsTopics"].([]any)) != 0 {
		t.Fatalf("defaults: %d %v", code, body)
	}
	code, body = api.do("PATCH", "/v1/me/preferences", `{"timezone":"Europe/Paris","newsTopics":["ai"]}`)
	if p := body.data(); code != 200 || p["timezone"] != "Europe/Paris" || p["pushLimit"] != float64(5) || p["newsTopics"].([]any)[0] != "ai" {
		t.Fatalf("patch: %d %v", code, body)
	}
	for _, bad := range []string{`{"timezone":"Nope/Zone"}`, `{"quietStart":"25:00"}`, `{"pushLimit":21}`, `{"pushLimit":-1}`} {
		if code, _ := api.do("PATCH", "/v1/me/preferences", bad); code != 400 {
			t.Errorf("%s: %d", bad, code)
		}
	}

	code, body = api.do("POST", "/v1/schedules", `{"kind":"brief","time":"07:00","repeat":"weekdays"}`)
	if s := body.data(); code != 201 || s["title"] != "Morning brief" || s["kind"] != "brief" || s["repeat"] != "weekdays" || s["prompt"] == "" {
		t.Fatalf("brief: %d %v", code, body)
	}

	code, body = api.do("POST", "/v1/memories", `{"text":"Likes tea"}`)
	m := body.data()
	if code != 201 || m["text"] != "Likes tea" || m["sourceTaskId"] != nil || m["createdAt"] == nil || m["updatedAt"] == nil {
		t.Fatalf("create memory: %d %v", code, body)
	}
	id := m["id"].(string)
	if code, body = api.do("POST", "/v1/memories", `{"text":"likes TEA."}`); code != 200 || body.data()["id"] != id {
		t.Fatalf("duplicate: %d %v", code, body)
	}
	if code, _ = api.do("POST", "/v1/memories", `{"text":""}`); code != 400 {
		t.Fatalf("empty memory: %d", code)
	}
	if code, body = api.do("PATCH", "/v1/memories/"+id, `{"text":"Likes green tea"}`); code != 200 || body.data()["text"] != "Likes green tea" {
		t.Fatalf("patch memory: %d %v", code, body)
	}
	other := api.as(newUser(t, pool))
	if code, _ = other.do("PATCH", "/v1/memories/"+id, `{"text":"x"}`); code != 404 {
		t.Fatalf("other user patch: %d", code)
	}
	if code, _ = other.do("DELETE", "/v1/memories/"+id, ""); code != 404 {
		t.Fatalf("other user delete: %d", code)
	}
	if _, body = other.do("GET", "/v1/memories", ""); len(body.data()["memories"].([]any)) != 0 {
		t.Fatalf("other user lists %v", body)
	}
	if api.raw("DELETE", "/v1/memories/"+id, "").StatusCode != 204 {
		t.Fatal("delete memory")
	}

	ctx := context.Background()
	task := &models.Task{UserID: api.user, Prompt: "p", Status: models.TaskDone, Mode: "quality"}
	if err := queries.InsertTask(ctx, pool, task); err != nil {
		t.Fatal(err)
	}
	var ids []string
	for _, title := range []string{"one", "two", "three"} {
		u := &models.Update{UserID: api.user, TaskID: task.ID, Importance: models.ImportanceFeed, Title: title,
			Sections: []models.Section{{Title: "News", Items: []models.SectionItem{{Text: "a", URL: "https://e.com"}}}}}
		if err := queries.InsertUpdate(ctx, pool, u); err != nil {
			t.Fatal(err)
		}
		ids = append(ids, u.ID.String())
		time.Sleep(3 * time.Millisecond)
	}
	code, body = api.do("GET", "/v1/updates?limit=2", "")
	list := body.data()["updates"].([]any)
	first := list[0].(map[string]any)
	if code != 200 || len(list) != 2 || first["title"] != "three" || first["read"] != false || first["scheduleId"] != nil ||
		first["taskId"] != task.ID.String() || first["importance"] != "feed" || first["sections"].([]any)[0].(map[string]any)["items"] == nil {
		t.Fatalf("list: %d %v", code, body)
	}
	before := list[1].(map[string]any)["createdAt"].(string)
	_, body = api.do("GET", "/v1/updates?before="+url.QueryEscape(before), "")
	if rest := body.data()["updates"].([]any); len(rest) != 1 || rest[0].(map[string]any)["title"] != "one" {
		t.Fatalf("page 2: %v", body)
	}
	if code, _ = api.do("GET", "/v1/updates?before=yesterday", ""); code != 400 {
		t.Fatalf("bad before: %d", code)
	}
	if code, _ = other.do("POST", "/v1/updates/"+ids[0]+"/read", ""); code != 404 {
		t.Fatalf("other user read: %d", code)
	}
	if api.raw("POST", "/v1/updates/"+ids[0]+"/read", "").StatusCode != 204 {
		t.Fatal("read")
	}
	if api.raw("POST", "/v1/updates/read-all", "").StatusCode != 204 {
		t.Fatal("read-all")
	}
	_, body = api.do("GET", "/v1/updates", "")
	for _, u := range body.data()["updates"].([]any) {
		if u.(map[string]any)["read"] != true {
			t.Fatalf("unread after read-all: %v", u)
		}
	}
}
