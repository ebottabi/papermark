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
)

func TestDevicesRegisterIsIdempotentByToken(t *testing.T) {
	pool, _ := database.NewTestDatabase(t)
	api := newAPI(t, &router.Config{Pool: pool, Devices: controllers.NewDeviceController(services.NewDeviceService(pool))})
	ctx := context.Background()

	if code, body := api.do("POST", "/v1/devices", `{"token":"abc","platform":"ios","environment":"sandbox"}`); code != 200 || body.data()["token"] != "abc" || body.data()["environment"] != "sandbox" {
		t.Fatalf("register: %d %v", code, body)
	}
	if code, body := api.do("POST", "/v1/devices", `{"token":"abc","environment":"production"}`); code != 200 || body.data()["environment"] != "production" || body.data()["platform"] != "ios" {
		t.Fatalf("re-register: %d %v", code, body)
	}
	ds, _ := queries.ListDevices(ctx, pool)
	if len(ds) != 1 || ds[0].Environment != "production" {
		t.Fatalf("devices: %+v", ds)
	}
	for _, bad := range []string{`{"environment":"sandbox"}`, `{"token":"x","environment":"dev"}`, `{"token":"x","platform":"android","environment":"sandbox"}`, `nope`} {
		if code, body := api.do("POST", "/v1/devices", bad); code != 400 || body["code"] != "BAD_REQUEST" {
			t.Errorf("%s: %d %v", bad, code, body)
		}
	}

	for i := 0; i < 2; i++ {
		if code, body := api.do("DELETE", "/v1/devices/abc", ""); code != 200 || body["success"] != true {
			t.Fatalf("delete %d: %d %v", i, code, body)
		}
	}
	if ds, _ := queries.ListDevices(ctx, pool); len(ds) != 0 {
		t.Fatalf("devices after delete: %+v", ds)
	}
	if code, _ := api.do("POST", "/v1/devices", `{"token":"abc","environment":"sandbox"}`); code != 200 {
		t.Fatalf("register after delete: %d", code)
	}
}
