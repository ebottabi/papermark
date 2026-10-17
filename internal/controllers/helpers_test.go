/*
 * Copyright (c) 2026. Papermark - All Rights Reserved
 * Unauthorized copying of this file, via any medium is strictly prohibited
 * Proprietary and confidential
 * Written by Ebot Tabi <ebot.tabi@gmail.com>, 15/10/2026
 */
package controllers_test

import (
	"context"
	"crypto/rand"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/gofiber/fiber/v2"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"

	"papermark/internal/config"
	"papermark/internal/models/queries"
	"papermark/internal/router"
	"papermark/internal/services"
)

type envelope map[string]any

func (e envelope) data() map[string]any { m, _ := e["data"].(map[string]any); return m }

type testAPI struct {
	t     *testing.T
	app   *fiber.App
	token string    // the signed-in user's session token
	user  uuid.UUID // the signed-in user
	pool  *pgxpool.Pool
}

// newAPI builds the app with real session auth on cfg.Pool and signs in a
// fresh user, whose token every request carries.
func newAPI(t *testing.T, cfg *router.Config) *testAPI {
	auth := services.NewAuthService(cfg.Pool, config.AuthConfig{SessionTTL: time.Hour}, nil)
	cfg.Authenticate, cfg.BodyLimit, cfg.Quiet = auth.Authenticate, 1<<20, true
	user, token := newUser(t, cfg.Pool)
	return &testAPI{t: t, app: router.New(cfg), token: token, user: user, pool: cfg.Pool}
}

// newUser inserts a user and a live session for it and returns the user and
// its bearer token.
func newUser(t *testing.T, pool *pgxpool.Pool) (uuid.UUID, string) {
	t.Helper()
	sub := uuid.NewString()
	u, _, err := queries.UpsertGoogleUser(context.Background(), pool, sub, sub+"@gmail.com", "Test", "")
	if err != nil {
		t.Fatal(err)
	}
	raw := make([]byte, 32)
	rand.Read(raw)
	token := hex.EncodeToString(raw)
	sum := sha256.Sum256([]byte(token))
	if _, err := queries.InsertSession(context.Background(), pool, u.ID, hex.EncodeToString(sum[:]), time.Now().Add(time.Hour)); err != nil {
		t.Fatal(err)
	}
	return u.ID, token
}

// as returns a client for the same app signed in with another token.
func (a *testAPI) as(user uuid.UUID, token string) *testAPI {
	return &testAPI{t: a.t, app: a.app, token: token, user: user, pool: a.pool}
}

// raw performs a request with the bearer token and returns the response.
func (a *testAPI) raw(method, path, body string) *http.Response {
	a.t.Helper()
	req := httptest.NewRequest(method, path, strings.NewReader(body))
	req.Header.Set("Authorization", "Bearer "+a.token)
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
