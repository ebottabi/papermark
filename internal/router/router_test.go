/*
 * Copyright (c) 2026. Papermark - All Rights Reserved
 * Unauthorized copying of this file, via any medium is strictly prohibited
 * Proprietary and confidential
 * Written by Ebot Tabi <ebot.tabi@gmail.com>, 15/10/2026
 */

package router_test

import (
	"bufio"
	"bytes"
	"context"
	"crypto/rand"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"strings"
	"testing"
	"time"

	"papermark/internal/database"
	"papermark/internal/models/queries"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"
)

// These tests run the real cmd/server binary against a throwaway database and
// Redis, and talk to it over HTTP as a user signed in through a stored session.

var (
	serverBin  string
	sharedDSN  string
	sharedPool *pgxpool.Pool
)

func TestMain(m *testing.M) {
	dir, err := os.MkdirTemp("", "papermark-server-")
	if err != nil {
		panic(err)
	}
	defer os.RemoveAll(dir)
	serverBin = filepath.Join(dir, "server")
	if out, err := exec.Command("go", "build", "-race", "-o", serverBin, "papermark/cmd/server").CombinedOutput(); err != nil {
		fmt.Fprintf(os.Stderr, "build server: %v\n%s", err, out)
		os.Exit(1)
	}
	os.Exit(database.RunWithTestDatabase(m, func(pool *pgxpool.Pool, dsn string) { sharedPool, sharedDSN = pool, dsn }))
}

func redisURL() string {
	if u := os.Getenv("TEST_REDIS_URL"); u != "" {
		return u
	}
	return "redis://localhost:6379/0"
}

type server struct {
	t         *testing.T
	url       string
	workspace string
	cmd       *exec.Cmd
	exited    chan struct{}
	pool      *pgxpool.Pool
	token     string // a session token of the signed-in user
}

var listening = regexp.MustCompile(`listening on (http://\S+)`)

// signIn creates a user with a live session in pool and returns its token.
func signIn(t *testing.T, pool *pgxpool.Pool) string {
	t.Helper()
	ctx := context.Background()
	sub := uuid.NewString()
	u, _, err := queries.UpsertGoogleUser(ctx, pool, sub, sub+"@gmail.com", "Test", "")
	if err != nil {
		t.Fatal(err)
	}
	raw := make([]byte, 32)
	rand.Read(raw)
	token := hex.EncodeToString(raw)
	sum := sha256.Sum256([]byte(token))
	if _, err := queries.InsertSession(ctx, pool, u.ID, hex.EncodeToString(sum[:]), time.Now().Add(time.Hour)); err != nil {
		t.Fatal(err)
	}
	return token
}

// as is the same server seen by another signed-in user.
func (s *server) as(token string) *server {
	c := *s
	c.token = token
	return &c
}

// startServer runs cmd/server with no model key, so no test reaches
// Fireworks; it is SIGKILLed on cleanup if still running.
func startServer(t *testing.T, pool *pgxpool.Pool, dsn, workspace string, env ...string) *server {
	t.Helper()
	cmd := exec.Command(serverBin)
	cmd.Env = append(os.Environ(), "PORT=0", "DATABASE_URL="+dsn, "REDIS_URL="+redisURL(),
		"WORKSPACE_DIR="+workspace, "FIREWORKS_API_KEY=", "FIREWORKS_BASE_URL=http://127.0.0.1:1", "WORKER_POLL_MS=50")
	cmd.Env = append(cmd.Env, env...)
	stderr, err := cmd.StderrPipe()
	if err != nil {
		t.Fatal(err)
	}
	if err := cmd.Start(); err != nil {
		t.Fatal(err)
	}
	s := &server{t: t, workspace: workspace, cmd: cmd, exited: make(chan struct{}), pool: pool, token: signIn(t, pool)}
	found := make(chan string, 1)
	go func() {
		sc := bufio.NewScanner(stderr)
		for sc.Scan() {
			if m := listening.FindStringSubmatch(sc.Text()); m != nil {
				found <- m[1]
			}
			if testing.Verbose() {
				t.Log(sc.Text())
			}
		}
	}()
	go func() { cmd.Wait(); close(s.exited) }()
	t.Cleanup(s.kill)
	select {
	case s.url = <-found:
		s.url += "/v1"
	case <-s.exited:
		t.Fatal("server exited before listening")
	case <-time.After(30 * time.Second):
		t.Fatal("server did not start")
	}
	return s
}

func (s *server) kill() {
	select {
	case <-s.exited:
	default:
		s.cmd.Process.Kill()
		<-s.exited
	}
}

func newStack(t *testing.T) *server {
	t.Helper()
	return startServer(t, sharedPool, sharedDSN, t.TempDir())
}

type response struct {
	status int
	ctype  string
	raw    []byte
	env    map[string]any // the JSON envelope
}

// data is the envelope's data as a JSON object.
func (r response) data() map[string]any {
	d, _ := r.env["data"].(map[string]any)
	return d
}

func (s *server) api(method, route string, body any) response {
	s.t.Helper()
	var rd io.Reader
	if body != nil {
		raw, err := json.Marshal(body)
		if err != nil {
			s.t.Fatal(err)
		}
		rd = bytes.NewReader(raw)
	}
	req, _ := http.NewRequest(method, s.url+route, rd)
	req.Header.Set("Authorization", "Bearer "+s.token)
	req.Header.Set("Content-Type", "application/json")
	return do(s.t, req)
}

func do(t *testing.T, req *http.Request) response {
	t.Helper()
	res, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer res.Body.Close()
	r := response{status: res.StatusCode, ctype: res.Header.Get("Content-Type")}
	r.raw, _ = io.ReadAll(res.Body)
	if strings.HasPrefix(r.ctype, "application/json") {
		json.Unmarshal(r.raw, &r.env)
	}
	return r
}

func (s *server) create(body map[string]any) string {
	s.t.Helper()
	r := s.api("POST", "/tasks", body)
	if r.status != 202 {
		s.t.Fatalf("create: %d %s", r.status, r.raw)
	}
	return r.data()["id"].(string)
}

func (s *server) task(id string) map[string]any {
	s.t.Helper()
	return s.api("GET", "/tasks/"+id, nil).data()
}

func (s *server) until(id, what string, check func(task map[string]any) bool) map[string]any {
	s.t.Helper()
	var task map[string]any
	waitFor(s.t, what, func() bool { task = s.task(id); return task != nil && check(task) })
	return task
}

func waitFor(t *testing.T, what string, cond func() bool) {
	t.Helper()
	deadline := time.Now().Add(30 * time.Second)
	for !cond() {
		if time.Now().After(deadline) {
			t.Fatalf("timed out waiting for %s", what)
		}
		time.Sleep(20 * time.Millisecond)
	}
}

func events(task map[string]any) []map[string]any {
	raw, _ := task["events"].([]any)
	out := make([]map[string]any, len(raw))
	for i, e := range raw {
		out[i] = e.(map[string]any)
	}
	return out
}

func status(want string) func(map[string]any) bool {
	return func(t map[string]any) bool { return t["status"] == want }
}

func eq(t *testing.T, what string, got, want any) {
	t.Helper()
	g, _ := json.Marshal(got)
	w, _ := json.Marshal(want)
	if !bytes.Equal(g, w) {
		t.Fatalf("%s\n got %s\nwant %s", what, g, w)
	}
}

// at walks decoded JSON: at(v, "messages", -1, "content", 0).
func at(v any, path ...any) any {
	for _, p := range path {
		switch k := p.(type) {
		case string:
			m, _ := v.(map[string]any)
			v = m[k]
		case int:
			a, _ := v.([]any)
			if k < 0 {
				k += len(a)
			}
			if k < 0 || k >= len(a) {
				return nil
			}
			v = a[k]
		}
	}
	return v
}

func TestTaskListFiltersPatchAndUsage(t *testing.T) {
	pool, dsn := database.NewTestDatabase(t)
	s := startServer(t, pool, dsn, t.TempDir())
	project := s.api("POST", "/projects", map[string]any{"name": "Trips"})
	eq(t, "project status", project.status, 201)
	p1 := project.data()["id"].(string)

	var ids []string
	for _, c := range []struct {
		task    string
		project any
	}{{"one", p1}, {"two", nil}, {"three", p1}} {
		id := s.create(map[string]any{"task": c.task, "projectId": c.project})
		ids = append(ids, id)
		s.until(id, "the missing-key error", status("error"))
	}
	eq(t, "patch", s.api("PATCH", "/tasks/"+ids[1], map[string]any{"archived": true, "favorite": true}).data()["favorite"], true)
	bad := s.api("PATCH", "/tasks/"+ids[1], map[string]any{"archived": "yes"})
	eq(t, "bad patch", []any{bad.status, bad.env["error"]}, []any{400, "archived must be a boolean"})
	s.api("POST", "/tasks/"+ids[0]+"/messages", map[string]any{"text": "again"})
	s.until(ids[0], "the second error", func(t map[string]any) bool { return t["status"] == "error" && len(events(t)) == 4 })

	list := func(q string) []string {
		var out []string
		for _, task := range at(s.api("GET", "/tasks"+q, nil).env, "data", "tasks").([]any) {
			out = append(out, at(task, "prompt").(string))
		}
		return out
	}
	eq(t, "active", list(""), []string{"one", "three"})
	eq(t, "archived", list("?archived=true"), []string{"two"})
	eq(t, "by project", list("?projectId="+p1), []string{"one", "three"})
	summary := at(s.api("GET", "/tasks", nil).env, "data", "tasks", 0).(map[string]any)
	var keys []string
	for k := range summary {
		keys = append(keys, k)
	}
	eq(t, "summary keys", len(keys), 11)
	for _, k := range []string{"archived", "createdAt", "favorite", "id", "mode", "projectId", "prompt", "scheduleId", "status", "updatedAt", "usage"} {
		if _, ok := summary[k]; !ok {
			t.Fatalf("summary lacks %s", k)
		}
	}
	eq(t, "default mode", summary["mode"], "quality")

	usage := s.api("GET", "/usage?days=7", nil).data()
	days := usage["days"].([]any)
	eq(t, "days", len(days), 7)
	eq(t, "today", at(days, -1, "date"), time.Now().UTC().Format("2006-01-02"))
	eq(t, "tasks today", at(days, -1, "tasks"), 3)
	eq(t, "input tokens", at(usage, "total", "inputTokens"), 0)
	eq(t, "today is the total", at(days, -1, "usage"), usage["total"])

	eq(t, "delete project", s.api("DELETE", "/projects/"+p1, nil).status, 200)
	eq(t, "tasks leave the project", s.task(ids[0])["projectId"], nil)
}

func TestEnvelopeAuthAndValidation(t *testing.T) {
	s := newStack(t)
	root := strings.TrimSuffix(s.url, "/v1")
	for path, want := range map[string]string{"/health": "healthy", "/ready": "ready"} {
		req, _ := http.NewRequest("GET", root+path, nil)
		r := do(t, req)
		eq(t, path, []any{r.status, r.env["status"]}, []any{200, want})
	}
	req, _ := http.NewRequest("GET", s.url+"/tasks", nil)
	eq(t, "no token", do(t, req).env, map[string]any{"code": "UNAUTHORIZED", "error": "unauthorized", "success": false})
	req, _ = http.NewRequest("GET", s.url+"/tasks", nil)
	req.Header.Set("Authorization", "Bearer wrong")
	eq(t, "wrong token", do(t, req).status, 401)

	cases := []struct {
		method, route string
		body          any
		status        int
		error         string
	}{
		{"POST", "/tasks", map[string]any{}, 400, "task is required"},
		{"POST", "/tasks", map[string]any{"task": "x", "mode": "fast"}, 400, "mode must be speed or quality"},
		{"POST", "/tasks", map[string]any{"task": "x", "projectId": 7}, 400, "projectId must be a string"},
		{"POST", "/tasks", map[string]any{"task": "/no-such-skill go"}, 400, "no such skill: /no-such-skill"},
		{"GET", "/tasks/00000000-0000-0000-0000-000000000000", nil, 404, "no such task"},
		{"GET", "/tasks/not-a-uuid", nil, 404, "no such task"},
		{"POST", "/tasks/00000000-0000-0000-0000-000000000000/reply", map[string]any{"text": "x"}, 404, "no such task"},
		{"POST", "/skills", map[string]any{"name": "Bad Name"}, 400, "name must match ^[a-z0-9-]{1,40}$"},
		{"POST", "/knowledge", map[string]any{"title": " "}, 400, "title is required"},
		{"GET", "/nope", nil, 404, "Not Found"},
	}
	for _, c := range cases {
		r := s.api(c.method, c.route, c.body)
		eq(t, c.method+" "+c.route, []any{r.status, r.env["success"], r.env["error"]}, []any{c.status, false, c.error})
	}

	ok := s.api("GET", "/tasks", nil)
	eq(t, "success envelope", ok.env["success"], true)

	name := fmt.Sprintf("skill-%d", time.Now().UnixNano())
	created := s.api("POST", "/skills", map[string]any{"name": name, "instructions": "Do it well."})
	eq(t, "skill created", created.status, 201)
	eq(t, "duplicate skill", s.api("POST", "/skills", map[string]any{"name": name}).status, 409)
	skillID := created.data()["id"].(string)
	eq(t, "rename", s.api("PATCH", "/skills/"+skillID, map[string]any{"description": "d"}).data()["description"], "d")
	eq(t, "delete skill", s.api("DELETE", "/skills/"+skillID, nil).status, 200)
	eq(t, "delete again", s.api("DELETE", "/skills/"+skillID, nil).status, 404)
}

// TestUsersAreIsolated checks that another user's task and project answer 404
// to every route, and lists stay per user.
func TestUsersAreIsolated(t *testing.T) {
	alice := newStack(t)
	bob := alice.as(signIn(t, alice.pool))
	p := alice.api("POST", "/projects", map[string]any{"name": "Private"}).data()["id"].(string)
	id := alice.create(map[string]any{"task": "secret", "projectId": p})
	alice.until(id, "the missing-key error", status("error"))

	for _, c := range []struct {
		method, route string
		body          any
	}{
		{"GET", "/tasks/" + id, nil},
		{"PATCH", "/tasks/" + id, map[string]any{"favorite": true}},
		{"POST", "/tasks/" + id + "/fork", map[string]any{"task": "mine now"}},
		{"POST", "/tasks/" + id + "/messages", map[string]any{"text": "hi"}},
		{"POST", "/tasks/" + id + "/reply", map[string]any{"text": "hi"}},
		{"POST", "/tasks/" + id + "/stop", nil},
		{"GET", "/tasks/" + id + "/files", nil},
		{"GET", "/tasks/" + id + "/transcript", nil},
		{"PATCH", "/projects/" + p, map[string]any{"name": "Taken"}},
		{"DELETE", "/projects/" + p, nil},
	} {
		r := bob.api(c.method, c.route, c.body)
		eq(t, "bob "+c.method+" "+c.route, r.status, 404)
	}
	req, _ := http.NewRequest("GET", bob.url+"/tasks/"+id+"/events", nil)
	req.Header.Set("Authorization", "Bearer "+bob.token)
	eq(t, "bob streams events", do(t, req).status, 404)

	eq(t, "bob's tasks", len(at(bob.api("GET", "/tasks", nil).env, "data", "tasks").([]any)), 0)
	eq(t, "bob's projects", len(at(bob.api("GET", "/projects", nil).env, "data", "projects").([]any)), 0)
	eq(t, "bob's usage", at(bob.api("GET", "/usage?days=1", nil).data(), "days", 0, "tasks"), 0)

	task := alice.task(id)
	eq(t, "alice's task survives", []any{task["favorite"], task["projectId"], task["status"]}, []any{false, p, "error"})
	eq(t, "alice's project survives", at(alice.api("GET", "/projects", nil).env, "data", "projects", 0, "name"), "Private")
}
