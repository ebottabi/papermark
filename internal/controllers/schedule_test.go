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

	"papermark/internal/controllers"
	"papermark/internal/database"
	"papermark/internal/models/queries"
	"papermark/internal/router"
	"papermark/internal/services"

	"github.com/google/uuid"
)

type recordingTasks struct{ scheduleIDs []uuid.UUID }

func (r *recordingTasks) CreateTask(_ context.Context, _ queries.DBTX, _, _ string, _ *uuid.UUID, scheduleID uuid.UUID) (string, error) {
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
	if code != 201 || body.data()["id"] != "task-1" || len(tasks.scheduleIDs) != 1 || tasks.scheduleIDs[0].String() != id {
		t.Fatalf("run: %d %v %v", code, body, tasks.scheduleIDs)
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
