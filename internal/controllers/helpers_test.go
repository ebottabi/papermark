/*
 * Copyright (c) 2026. Papermark - All Rights Reserved
 * Unauthorized copying of this file, via any medium is strictly prohibited
 * Proprietary and confidential
 * Written by Ebot Tabi <ebot.tabi@gmail.com>, 15/10/2026
 */
package controllers_test

import (
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/gofiber/fiber/v2"

	"papermark/internal/router"
)

type envelope map[string]any

func (e envelope) data() map[string]any { m, _ := e["data"].(map[string]any); return m }

type testAPI struct {
	t   *testing.T
	app *fiber.App
}

func newAPI(t *testing.T, cfg *router.Config) *testAPI {
	cfg.APIToken, cfg.BodyLimit, cfg.Quiet = "secret", 1<<20, true
	return &testAPI{t: t, app: router.New(cfg)}
}

// raw performs a request with the bearer token and returns the response.
func (a *testAPI) raw(method, path, body string) *http.Response {
	a.t.Helper()
	req := httptest.NewRequest(method, path, strings.NewReader(body))
	req.Header.Set("Authorization", "Bearer secret")
	req.Header.Set("Content-Type", "application/json")
	res, err := a.app.Test(req, -1)
	if err != nil {
		a.t.Fatal(err)
	}
	return res
}

func (a *testAPI) do(method, path, body string) (int, envelope) {
	a.t.Helper()
	res := a.raw(method, path, body)
	raw, _ := io.ReadAll(res.Body)
	var e envelope
	if err := json.Unmarshal(raw, &e); err != nil {
		a.t.Fatalf("%s %s: %d %v: %s", method, path, res.StatusCode, err, raw)
	}
	return res.StatusCode, e
}
