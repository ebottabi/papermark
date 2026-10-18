/*
 * Copyright (c) 2026. Papermark - All Rights Reserved
 * Unauthorized copying of this file, via any medium is strictly prohibited
 * Proprietary and confidential
 * Written by Ebot Tabi <ebot.tabi@gmail.com>, 15/10/2026
 */
package controllers_test

import (
	"context"
	"testing"
	"time"

	"papermark/internal/controllers"
	"papermark/internal/database"
	"papermark/internal/models"
	"papermark/internal/models/queries"
	"papermark/internal/router"
	"papermark/internal/services"

	"github.com/google/uuid"
)

type recordingTasks struct{ userIDs, scheduleIDs []uuid.UUID }

func (r *recordingTasks) CreateTask(_ context.Context, _ queries.DBTX, userID uuid.UUID, _, _ string, _ *uuid.UUID, scheduleID uuid.UUID) (string, error) {
	r.userIDs = append(r.userIDs, userID)
	r.scheduleIDs = append(r.scheduleIDs, scheduleID)
	return "task-1", nil
}

func TestSchedulesAPI(t *testing.T) {
	pool, _ := database.NewTestDatabase(t)
	svc := services.NewScheduleService(pool)
	tasks := &recordingTasks{}
	svc.SetTaskCreator(tasks)
	api := newAPI(t, &router.Config{Pool: pool, Schedules: controllers.NewScheduleController(svc)})

	code, body := api.do("POST", "/v1/schedules", `{"prompt":"digest","repeat":"weekly","time":"09:30","weekday":1}`)
	s := body.data()
	if code != 201 || s["mode"] != "quality" || s["enabled"] != true || s["weekday"] != float64(1) ||
		s["nextRunAt"] == nil || s["lastRunAt"] != nil || s["projectId"] != nil || s["time"] != "09:30" {
		t.Fatalf("create: %d %v", code, body)
	}
	id := s["id"].(string)

	for _, bad := range []string{
		`{"repeat":"daily","time":"09:00"}`,
		`{"prompt":"p","repeat":"weekly","time":"09:00"}`,
		`{"prompt":"p","repeat":"daily","time":"9am"}`,
		`{"prompt":"p","repeat":"daily","time":"09:00","mode":"fast"}`,
		`{"prompt":"p","repeat":"daily","time":"09:00","projectId":"nope"}`,
	} {
		if code, body := api.do("POST", "/v1/schedules", bad); code != 400 {
			t.Errorf("%s: %d %v", bad, code, body)
		}
	}

	code, body = api.do("PATCH", "/v1/schedules/"+id, `{"enabled":false}`)
	if s := body.data(); code != 200 || s["enabled"] != false || s["nextRunAt"] != nil || s["prompt"] != "digest" || s["weekday"] != float64(1) {
		t.Fatalf("disable: %d %v", code, body)
	}
	code, body = api.do("PATCH", "/v1/schedules/"+id, `{"enabled":true,"repeat":"daily","mode":"speed"}`)
	if s := body.data(); code != 200 || s["repeat"] != "daily" || s["weekday"] != nil || s["mode"] != "speed" || s["nextRunAt"] == nil {
		t.Fatalf("switch to daily: %d %v", code, body)
	}

	code, body = api.do("POST", "/v1/schedules/"+id+"/run", "")
	if code != 201 || body.data()["id"] != "task-1" || len(tasks.scheduleIDs) != 1 || tasks.scheduleIDs[0].String() != id || tasks.userIDs[0] != api.user {
		t.Fatalf("run: %d %v %v", code, body, tasks.scheduleIDs)
	}

	other := api.as(newUser(t, pool))
	if _, body := other.do("GET", "/v1/schedules", ""); len(body.data()["schedules"].([]any)) != 0 {
		t.Fatalf("other user lists: %v", body)
	}
	for _, r := range [][2]string{{"PATCH", ""}, {"POST", "/run"}, {"DELETE", ""}} {
		if code, _ := other.do(r[0], "/v1/schedules/"+id+r[1], `{"enabled":false}`); code != 404 {
			t.Fatalf("other user %s %s: %d", r[0], r[1], code)
		}
	}
	code, body = api.do("GET", "/v1/schedules", "")
	list, _ := body.data()["schedules"].([]any)
	if code != 200 || len(list) != 1 || list[0].(map[string]any)["lastRunAt"] == nil {
		t.Fatalf("list: %d %v", code, body)
	}

	if code, _ := api.do("DELETE", "/v1/schedules/"+id, ""); code != 200 {
		t.Fatalf("delete: %d", code)
	}
	for _, r := range [][2]string{{"DELETE", ""}, {"PATCH", "/"}, {"POST", "/run"}} {
		path := "/v1/schedules/" + id
		if r[1] != "/" {
			path += r[1]
		}
		if code, body := api.do(r[0], path, `{}`); code != 404 {
			t.Errorf("%s %s after delete: %d %v", r[0], path, code, body)
		}
	}
}

func TestTriggerRoutinesAPI(t *testing.T) {
	pool, _ := database.NewTestDatabase(t)
	svc := services.NewScheduleService(pool)
	svc.SetTaskCreator(&recordingTasks{})
	api := newAPI(t, &router.Config{Pool: pool, Schedules: controllers.NewScheduleController(svc)})

	code, body := api.do("POST", "/v1/schedules", `{"title":"Invoices","prompt":"file it","trigger":"email","rule":{"from":"billing@example.com","hasAttachment":true}}`)
	s := body.data()
	rule, _ := s["rule"].(map[string]any)
	if code != 201 || s["trigger"] != "email" || rule["from"] != "billing@example.com" || rule["hasAttachment"] != true ||
		s["repeat"] != nil || s["time"] != nil || s["weekday"] != nil || s["nextRunAt"] != nil || s["maxRunsPerHour"] != float64(10) {
		t.Fatalf("create email routine: %d %v", code, body)
	}
	id := s["id"].(string)

	code, body = api.do("POST", "/v1/schedules", `{"prompt":"digest","repeat":"daily","time":"09:00"}`)
	if s := body.data(); code != 201 || s["trigger"] != "time" || s["rule"] != nil || s["repeat"] != "daily" {
		t.Fatalf("time schedule: %d %v", code, body)
	}

	code, body = api.do("PATCH", "/v1/schedules/"+id, `{"maxRunsPerHour":3}`)
	if s := body.data(); code != 200 || s["maxRunsPerHour"] != float64(3) || s["trigger"] != "email" {
		t.Fatalf("patch: %d %v", code, body)
	}

	for _, bad := range []string{
		`{"prompt":"p","trigger":"email"}`,
		`{"prompt":"p","trigger":"email","rule":{"account":"a@example.com"}}`,
		`{"prompt":"p","trigger":"watch","rule":{"kind":"page","url":"http://169.254.169.254/","condition":"changed"}}`,
		`{"prompt":"p","trigger":"watch","rule":{"kind":"page","url":"https://example.com","condition":"below"}}`,
		`{"prompt":"p","trigger":"push","rule":{}}`,
		`{"prompt":"p","trigger":"email","rule":{"from":"a"},"maxRunsPerHour":101}`,
	} {
		if code, body := api.do("POST", "/v1/schedules", bad); code != 400 {
			t.Errorf("%s: %d %v", bad, code, body)
		}
	}

	if code, body := api.do("GET", "/v1/schedules/"+id+"/triggers?limit=5", ""); code != 200 {
		t.Fatalf("triggers: %d %v", code, body)
	} else if list, ok := body.data()["triggers"].([]any); !ok || len(list) != 0 {
		t.Fatalf("triggers must be an empty array: %v", body)
	}
	other := api.as(newUser(t, pool))
	if code, _ := other.do("GET", "/v1/schedules/"+id+"/triggers", ""); code != 404 {
		t.Fatalf("other user triggers: %d", code)
	}
	if code, _ := api.do("GET", "/v1/schedules/"+uuid.NewString()+"/triggers", ""); code != 404 {
		t.Fatalf("unknown routine: %d", code)
	}
}

func TestTriggerHistoryShape(t *testing.T) {
	pool, _ := database.NewTestDatabase(t)
	svc := services.NewScheduleService(pool)
	api := newAPI(t, &router.Config{Pool: pool, Schedules: controllers.NewScheduleController(svc)})
	_, body := api.do("POST", "/v1/schedules", `{"prompt":"p","trigger":"email","rule":{"from":"a@example.com"}}`)
	id := uuid.MustParse(body.data()["id"].(string))
	ctx := context.Background()
	if _, err := queries.InsertTriggerEvents(ctx, pool, id, api.user, []models.TriggerEvent{{SourceKey: "k1", Summary: "first"}}, time.Now().Add(-time.Minute)); err != nil {
		t.Fatal(err)
	}
	if _, err := queries.InsertTriggerEvents(ctx, pool, id, api.user, []models.TriggerEvent{{SourceKey: "k2", Summary: "second"}}, time.Now()); err != nil {
		t.Fatal(err)
	}
	_, body = api.do("GET", "/v1/schedules/"+id.String()+"/triggers", "")
	list := body.data()["triggers"].([]any)
	first := list[0].(map[string]any)
	if len(list) != 2 || first["summary"] != "second" || first["taskId"] != nil || first["createdAt"] == nil || first["id"] == nil {
		t.Fatalf("triggers: %v", body)
	}
	_, body = api.do("GET", "/v1/schedules/"+id.String()+"/triggers?limit=1", "")
	if len(body.data()["triggers"].([]any)) != 1 {
		t.Fatalf("limit ignored: %v", body)
	}
}
