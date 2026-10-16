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
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"reflect"
	"regexp"
	"strconv"
	"strings"
	"sync"
	"syscall"
	"testing"
	"time"

	"papermark/internal/database"
	"papermark/internal/integrations/fakeanthropic"

	"github.com/jackc/pgx/v5/pgxpool"
)

// These tests run the real cmd/server binary against a throwaway database, a
// scripted fake Messages API and Redis, and talk to it over HTTP.

const token = "test-token"

var (
	serverBin string
	sharedDSN string
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
	os.Exit(database.RunWithTestDatabase(m, func(_ *pgxpool.Pool, dsn string) { sharedDSN = dsn }))
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
}

var listening = regexp.MustCompile(`listening on (http://\S+)`)

// startServer runs cmd/server; it is SIGKILLed on cleanup if still running.
func startServer(t *testing.T, fakeURL, dsn, workspace string, env ...string) *server {
	t.Helper()
	cmd := exec.Command(serverBin)
	cmd.Env = append(os.Environ(), "API_TOKEN="+token, "PORT=0", "DATABASE_URL="+dsn, "REDIS_URL="+redisURL(),
		"WORKSPACE_DIR="+workspace, "ANTHROPIC_BASE_URL="+fakeURL, "ANTHROPIC_API_KEY=fake", "WORKER_POLL_MS=50")
	cmd.Env = append(cmd.Env, env...)
	stderr, err := cmd.StderrPipe()
	if err != nil {
		t.Fatal(err)
	}
	if err := cmd.Start(); err != nil {
		t.Fatal(err)
	}
	s := &server{t: t, workspace: workspace, cmd: cmd, exited: make(chan struct{})}
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

type stack struct {
	*server
	fake *fakeanthropic.Server
}

func newStack(t *testing.T, script fakeanthropic.Script) *stack {
	t.Helper()
	fake := startFake(t, script)
	return &stack{startServer(t, fake.URL, sharedDSN, t.TempDir()), fake}
}

func startFake(t *testing.T, script fakeanthropic.Script) *fakeanthropic.Server {
	t.Helper()
	fake, err := fakeanthropic.Start(script, "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { fake.Close() })
	return fake
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
	req.Header.Set("Authorization", "Bearer "+token)
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

func (s *server) get(route string) response {
	s.t.Helper()
	req, _ := http.NewRequest("GET", s.url+route, nil)
	req.Header.Set("Authorization", "Bearer "+token)
	return do(s.t, req)
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

func last(task map[string]any) map[string]any {
	ev := events(task)
	if len(ev) == 0 {
		return map[string]any{}
	}
	return ev[len(ev)-1]
}

func status(want string) func(map[string]any) bool {
	return func(t map[string]any) bool { return t["status"] == want }
}

func types(task map[string]any) []string {
	var out []string
	for _, e := range events(task) {
		ty := e["type"].(string)
		if ty == "tool" {
			ty += ":" + e["name"].(string)
		}
		out = append(out, ty)
	}
	return out
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

type frame struct {
	id, event string
	data      map[string]any
}

type sseStream struct {
	mu     sync.Mutex
	frames []frame
	cancel context.CancelFunc
}

func (s *server) sse(id string, header map[string]string) *sseStream {
	s.t.Helper()
	ctx, cancel := context.WithCancel(context.Background())
	st := &sseStream{cancel: cancel}
	s.t.Cleanup(cancel)
	req, _ := http.NewRequestWithContext(ctx, "GET", s.url+"/tasks/"+id+"/events", nil)
	req.Header.Set("Authorization", "Bearer "+token)
	for k, v := range header {
		req.Header.Set(k, v)
	}
	res, err := http.DefaultClient.Do(req)
	if err != nil {
		s.t.Fatal(err)
	}
	if res.StatusCode != 200 || res.Header.Get("Content-Type") != "text/event-stream" {
		s.t.Fatalf("events: %d %s", res.StatusCode, res.Header.Get("Content-Type"))
	}
	go func() {
		defer res.Body.Close()
		sc := bufio.NewScanner(res.Body)
		var f frame
		for sc.Scan() {
			line := sc.Text()
			if line == "" {
				st.mu.Lock()
				st.frames = append(st.frames, f)
				st.mu.Unlock()
				f = frame{}
				continue
			}
			key, val, _ := strings.Cut(line, ": ")
			switch key {
			case "id":
				f.id = val
			case "event":
				f.event = val
			case "data":
				json.Unmarshal([]byte(val), &f.data)
			}
		}
	}()
	return st
}

func (st *sseStream) snapshot() []frame {
	st.mu.Lock()
	defer st.mu.Unlock()
	return append([]frame(nil), st.frames...)
}

func lisbon(t *testing.T) fakeanthropic.Script {
	raw, err := os.ReadFile("../../testdata/lisbon.json")
	if err != nil {
		t.Fatal(err)
	}
	var s fakeanthropic.Script
	if err := json.Unmarshal(raw, &s); err != nil {
		t.Fatal(err)
	}
	return s
}

func turn(name string, input any) []fakeanthropic.Block {
	return []fakeanthropic.Block{fakeanthropic.Tool(name, input)}
}

func TestFullRunStreamsEventsAndDeltasAndResumesFromLastEventID(t *testing.T) {
	s := newStack(t, lisbon(t))
	id := s.create(map[string]any{"task": "Plan a weekend in Lisbon", "mode": "speed"})
	live := s.sse(id, nil)

	s.until(id, "the ask", func(t map[string]any) bool { return t["status"] == "waiting" && last(t)["type"] == "ask" })
	eq(t, "confirm while asking", s.api("POST", "/tasks/"+id+"/confirm", map[string]any{"decision": "allow"}).status, 409)
	eq(t, "message while waiting", s.api("POST", "/tasks/"+id+"/messages", map[string]any{"text": "hi"}).status, 409)
	eq(t, "reply", s.api("POST", "/tasks/"+id+"/reply", map[string]any{"text": "Saturday"}).status, 200)
	s.until(id, "the confirm", func(t map[string]any) bool { return t["status"] == "waiting" && last(t)["type"] == "confirm" })
	eq(t, "confirm", s.api("POST", "/tasks/"+id+"/confirm", map[string]any{"decision": "allow"}).status, 200)
	done := s.until(id, "idle", status("done"))

	eq(t, "events", types(done), []string{"message", "tool:file_write", "tool:web_search", "notify", "tool:message_notify_user", "ask", "reply", "confirm", "decision", "tool:shell_exec", "idle"})
	ev := events(done)
	eq(t, "first event", ev[0], map[string]any{"type": "message", "text": "Plan a weekend in Lisbon", "attachments": []string{}})
	eq(t, "search output", ev[2]["output"], "48 hours in Lisbon https://example.com/lisbon-48h\nLisbon neighborhoods guide https://example.com/lisbon-neighborhoods")
	eq(t, "reply event", ev[6], map[string]any{"type": "reply", "text": "Saturday"})
	eq(t, "decision event", ev[8], map[string]any{"decision": "allow", "name": "shell_exec", "type": "decision"})
	eq(t, "idle event", ev[10], map[string]any{"files": []string{"options.md"}, "text": "Your Lisbon weekend options are in options.md.", "type": "idle"})
	reqs := s.fake.Requests()
	eq(t, "reply as tool result", at(reqs[3], "messages", -1, "content", 0, "content"), "Saturday")
	for _, r := range reqs {
		eq(t, "effort", at(r, "output_config", "effort"), "low")
	}
	eq(t, "usage", done["usage"], map[string]any{"cacheReadTokens": 4000, "cacheWriteTokens": 500, "inputTokens": 5000, "outputTokens": 250})
	eq(t, "mode", done["mode"], "speed")

	var frames []frame
	waitFor(t, "idle on the stream", func() bool {
		frames = live.snapshot()
		for _, f := range frames {
			if f.data["type"] == "idle" {
				return true
			}
		}
		return false
	})
	var ids []string
	idleAt, decisionAt := -1, -1
	for i, f := range frames {
		if f.id != "" {
			ids = append(ids, f.id)
		}
		switch f.data["type"] {
		case "idle":
			idleAt = i
		case "decision":
			decisionAt = i
		}
	}
	eq(t, "stream ids", ids, []string{"0", "1", "2", "3", "4", "5", "6", "7", "8", "9", "10"})
	var text strings.Builder
	n := 0
	for _, f := range frames[decisionAt:idleAt] {
		if f.event == "delta" {
			if f.id != "" {
				t.Fatal("a delta carries an id")
			}
			text.WriteString(f.data["text"].(string))
			n++
		}
	}
	if n < 2 {
		t.Fatalf("want several deltas, got %d", n)
	}
	eq(t, "deltas preview the idle text", text.String(), "Your Lisbon weekend options are in options.md.")

	resumed := s.sse(id, map[string]string{"Last-Event-ID": "7"})
	waitFor(t, "the replay", func() bool { return len(resumed.snapshot()) >= 3 })
	time.Sleep(100 * time.Millisecond)
	var replay []string
	for _, f := range resumed.snapshot() {
		if f.event != "" {
			t.Fatal("deltas are replayed")
		}
		replay = append(replay, f.id)
	}
	eq(t, "replay after Last-Event-ID 7", replay, []string{"8", "9", "10"})

	file := s.get("/tasks/" + id + "/files/options.md")
	eq(t, "file type", file.ctype, "text/markdown; charset=utf-8")
	if !strings.HasPrefix(string(file.raw), "# Lisbon options\n\n1. Alfama") {
		t.Fatalf("options.md: %q", file.raw)
	}
	tr := s.get("/tasks/" + id + "/transcript")
	eq(t, "transcript type", tr.ctype, "text/markdown; charset=utf-8")
	for _, want := range []string{"# Plan a weekend in Lisbon", "**Question:** Which date?\n\n**You:** Saturday", "Files: options.md"} {
		if !strings.Contains(string(tr.raw), want) {
			t.Fatalf("transcript lacks %q:\n%s", want, tr.raw)
		}
	}
	if !strings.HasPrefix(string(tr.raw), "# Plan a weekend in Lisbon") {
		t.Fatalf("transcript: %s", tr.raw)
	}
}

func TestWaitingTaskSurvivesRestartsAndTheReplyContinuesIt(t *testing.T) {
	fake := startFake(t, fakeanthropic.Script{turn("message_ask_user", map[string]any{"text": "Which date?"}), turn("idle", map[string]any{"result": "booked"})})
	workspace := t.TempDir()
	// The kill can land before the parked run's job is marked done; a short
	// lease lets the next server reclaim it quickly.
	lease := "WORKER_LEASE_SECONDS=1"
	s := startServer(t, fake.URL, sharedDSN, workspace, lease)
	id := s.create(map[string]any{"task": "Book Lisbon"})
	s.until(id, "the ask", status("waiting"))
	for range 2 {
		s.kill()
		s = startServer(t, fake.URL, sharedDSN, workspace, lease)
		task := s.task(id)
		eq(t, "status after restart", task["status"], "waiting")
		eq(t, "events after restart", types(task), []string{"message", "ask"})
	}
	eq(t, "reply", s.api("POST", "/tasks/"+id+"/reply", map[string]any{"text": "Saturday"}).status, 200)
	done := s.until(id, "idle", status("done"))
	eq(t, "events", types(done), []string{"message", "ask", "reply", "idle"})
	reqs := fake.Requests()
	eq(t, "requests", len(reqs), 2)
	eq(t, "reply as tool result", at(reqs[1], "messages", -1, "content", 0, "content"), "Saturday")
}

func TestKilledServerMidToolIsReclaimedAndTheLostCallReportedInterrupted(t *testing.T) {
	fake := startFake(t, fakeanthropic.Script{
		turn("shell_exec", map[string]any{"command": "sleep 10"}),
		turn("idle", map[string]any{"result": "recovered"}),
	})
	workspace := t.TempDir()
	lease := "WORKER_LEASE_SECONDS=1"
	s := startServer(t, fake.URL, sharedDSN, workspace, lease)
	id := s.create(map[string]any{"task": "long job"})
	s.until(id, "the confirm", status("waiting"))
	s.api("POST", "/tasks/"+id+"/confirm", map[string]any{"decision": "allow"})
	waitFor(t, "the shell to start", func() bool { return len(fake.Requests()) == 1 && s.task(id)["status"] == "running" })
	time.Sleep(300 * time.Millisecond)
	s.kill()

	s = startServer(t, fake.URL, sharedDSN, workspace, lease)
	done := s.until(id, "idle after reclaim", status("done"))
	eq(t, "the approved call does not run twice", types(done), []string{"message", "confirm", "decision", "idle"})
	reqs := fake.Requests()
	eq(t, "requests", len(reqs), 2)
	eq(t, "lost call", at(reqs[1], "messages", -1, "content", 0), map[string]any{
		"content": "Not run: the task was interrupted.", "is_error": true, "tool_use_id": "toolu_0_0", "type": "tool_result",
	})
}

func TestMessagesQueueWhileRunningAndContinueAFinishedTask(t *testing.T) {
	s := newStack(t, fakeanthropic.Script{
		turn("shell_exec", map[string]any{"command": "true"}),
		turn("shell_exec", map[string]any{"command": "sleep 1"}),
		turn("idle", map[string]any{"result": "first done"}),
	})
	id := s.create(map[string]any{"task": "work"})
	s.until(id, "the confirm", status("waiting"))
	s.api("POST", "/tasks/"+id+"/confirm", map[string]any{"decision": "always"})
	waitFor(t, "the second turn", func() bool { return len(s.fake.Requests()) == 2 })
	eq(t, "status", s.task(id)["status"], "running")
	eq(t, "queued", s.api("POST", "/tasks/"+id+"/messages", map[string]any{"text": "also check prices"}).status, 202)
	s.until(id, "idle", status("done"))

	third := at(s.fake.Requests()[2], "messages").([]any)
	eq(t, "tool result first", at(third, -1, "content", 0, "type"), "tool_result")
	eq(t, "queued message rides along", at(third, -1, "content", 1), map[string]any{"type": "text", "text": "also check prices"})

	eq(t, "continue", s.api("POST", "/tasks/"+id+"/messages", map[string]any{"text": "now summarize"}).status, 202)
	done := s.until(id, "the second idle", func(t map[string]any) bool { return t["status"] == "done" && last(t)["text"] == "Script finished." })
	ty := types(done)
	eq(t, "tail", ty[len(ty)-3:], []string{"idle", "message", "idle"})
	fourth := at(s.fake.Requests()[3], "messages").([]any)
	if !reflect.DeepEqual(fourth[:len(third)], third) {
		t.Fatal("history is rewritten, not extended")
	}
	eq(t, "continuation", at(fourth, -1, "content"), []any{map[string]any{"type": "text", "text": "now summarize"}})
}

func TestForkCopiesHistoryAndWorkspace(t *testing.T) {
	s := newStack(t, fakeanthropic.Script{
		turn("file_write", map[string]any{"path": "a.txt", "content": "from the source"}),
		turn("idle", map[string]any{"result": "wrote a"}),
	})
	id := s.create(map[string]any{"task": "write a", "mode": "speed"})
	s.until(id, "idle", status("done"))
	fork := s.api("POST", "/tasks/"+id+"/fork", map[string]any{"task": "now write b"})
	eq(t, "fork status", fork.status, 202)
	forkID := fork.data()["id"].(string)
	forked := s.until(forkID, "the fork's idle", status("done"))

	eq(t, "prompt", forked["prompt"], "now write b")
	eq(t, "mode", forked["mode"], "speed")
	eq(t, "events", types(forked), []string{"message", "idle"})
	eq(t, "first message", events(forked)[0]["text"], "now write b")
	eq(t, "copied file", string(s.get("/tasks/"+forkID+"/files/a.txt").raw), "from the source")
	reqs := s.fake.Requests()
	source := at(reqs[1], "messages").([]any)
	forkReq := at(reqs[2], "messages").([]any)
	if !reflect.DeepEqual(forkReq[:len(source)], source) {
		t.Fatal("the fork does not start from the source's history")
	}
	eq(t, "source untouched", len(events(s.task(id))), 3)
}

func TestFilesAndAttachments(t *testing.T) {
	s := newStack(t, fakeanthropic.Script{turn("idle", map[string]any{"result": "ok"})})
	attachment := map[string]any{"name": "../notes.txt", "base64": base64.StdEncoding.EncodeToString([]byte("hello"))}
	id := s.create(map[string]any{"task": "read my notes", "attachments": []any{attachment}})
	s.until(id, "idle", status("done"))

	eq(t, "first event", events(s.task(id))[0], map[string]any{"attachments": []string{"uploads/notes.txt"}, "text": "read my notes", "type": "message"})
	first := at(s.fake.Requests()[0], "messages", 0, "content", 0, "text").(string)
	if !strings.Contains(first, "read my notes\n\nAttached files (workspace paths): uploads/notes.txt") {
		t.Fatalf("first message: %q", first)
	}
	s.api("POST", "/tasks/"+id+"/messages", map[string]any{"text": "and these", "attachments": []any{attachment}})
	s.until(id, "the second idle", func(t map[string]any) bool { return t["status"] == "done" && len(events(t)) == 4 })
	eq(t, "renamed upload", events(s.task(id))[2]["attachments"], []string{"uploads/notes-1.txt"})
	eq(t, "bad name", s.api("POST", "/tasks/"+id+"/messages", map[string]any{"text": "x", "attachments": []any{map[string]any{"name": "..", "base64": ""}}}).status, 400)

	ws := filepath.Join(s.workspace, id)
	if err := os.WriteFile(filepath.Join(ws, "data.csv"), []byte("a,b\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink("/etc/hosts", filepath.Join(ws, "hosts")); err != nil {
		t.Fatal(err)
	}
	files := at(s.api("GET", "/tasks/"+id+"/files", nil).env, "data", "files").([]any)
	var paths []string
	for _, f := range files {
		paths = append(paths, at(f, "path").(string))
	}
	eq(t, "symlinks are not listed", paths, []string{"data.csv", "uploads/notes-1.txt", "uploads/notes.txt"})
	eq(t, "size", at(files, 0, "size"), 4)
	if !regexp.MustCompile(`^\d{4}-\d\d-\d\dT.*Z$`).MatchString(at(files, 0, "modifiedAt").(string)) {
		t.Fatalf("modifiedAt %v", at(files, 0, "modifiedAt"))
	}

	dl := s.get("/tasks/" + id + "/files/uploads/notes.txt")
	eq(t, "download type", dl.ctype, "text/plain; charset=utf-8")
	eq(t, "download", string(dl.raw), "hello")
	for p, want := range map[string]int{
		"data.csv": 200, "..%2Fsecret.txt": 400, "..%2F..%2F..%2Fetc%2Fhosts": 400, "hosts": 400, "missing.txt": 404, "uploads": 404,
	} {
		eq(t, "status of "+p, s.get("/tasks/"+id+"/files/"+p).status, want)
	}
}

func TestStopKillsTheShellAndEverythingItStarted(t *testing.T) {
	s := newStack(t, fakeanthropic.Script{turn("shell_exec", map[string]any{"command": "sleep 30 & echo $! > sleep.pid; wait"})})
	id := s.create(map[string]any{"task": "wait"})
	s.until(id, "the confirm", status("waiting"))
	s.api("POST", "/tasks/"+id+"/confirm", map[string]any{"decision": "allow"})
	var pid int
	waitFor(t, "the pid file", func() bool {
		raw, _ := os.ReadFile(filepath.Join(s.workspace, id, "sleep.pid"))
		pid, _ = strconv.Atoi(strings.TrimSpace(string(raw)))
		return pid > 0
	})
	eq(t, "stop", s.api("POST", "/tasks/"+id+"/stop", nil).status, 200)
	stopped := s.until(id, "stopped", status("stopped"))
	var output string
	for _, e := range events(stopped) {
		if e["type"] == "tool" {
			output = e["output"].(string)
		}
	}
	if !strings.Contains(output, "killed: stopped by the user") {
		t.Fatalf("tool output %q", output)
	}
	waitFor(t, "the sleep to die", func() bool { return errors.Is(syscall.Kill(pid, 0), syscall.ESRCH) })
	eq(t, "stop again", s.api("POST", "/tasks/"+id+"/stop", nil).status, 409)
}

func TestTaskListFiltersPatchAndUsage(t *testing.T) {
	_, dsn := database.NewTestDatabase(t)
	fake := startFake(t, nil)
	s := startServer(t, fake.URL, dsn, t.TempDir())
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
		s.until(id, "idle", status("done"))
	}
	eq(t, "patch", s.api("PATCH", "/tasks/"+ids[1], map[string]any{"archived": true, "favorite": true}).data()["favorite"], true)
	bad := s.api("PATCH", "/tasks/"+ids[1], map[string]any{"archived": "yes"})
	eq(t, "bad patch", []any{bad.status, bad.env["error"]}, []any{400, "archived must be a boolean"})
	s.api("POST", "/tasks/"+ids[0]+"/messages", map[string]any{"text": "again"})
	s.until(ids[0], "the second idle", func(t map[string]any) bool { return t["status"] == "done" && len(events(t)) == 4 })

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
	eq(t, "input tokens", at(usage, "total", "inputTokens"), 4000)
	eq(t, "today is the total", at(days, -1, "usage"), usage["total"])

	eq(t, "delete project", s.api("DELETE", "/projects/"+p1, nil).status, 200)
	eq(t, "tasks leave the project", s.task(ids[0])["projectId"], nil)
}

func TestEnvelopeAuthAndValidation(t *testing.T) {
	s := newStack(t, nil)
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
