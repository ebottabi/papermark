/*
 * Copyright (c) 2026. Papermark - All Rights Reserved
 * Unauthorized copying of this file, via any medium is strictly prohibited
 * Proprietary and confidential
 * Written by Ebot Tabi <ebot.tabi@gmail.com>, 15/10/2026
 */
package services

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"sync"
	"testing"
	"time"

	"papermark/internal/cache"
	"papermark/internal/config"
	"papermark/internal/database"
	"papermark/internal/integrations/fakeanthropic"
	"papermark/internal/models"
	"papermark/internal/models/queries"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"
)

var testPool *pgxpool.Pool

func TestMain(m *testing.M) {
	os.Exit(database.RunWithTestDatabase(m, func(pool *pgxpool.Pool, _ string) { testPool = pool }))
}

type agentEnv struct {
	t    *testing.T
	ctx  context.Context
	s    *Services
	fake *fakeanthropic.Server
}

func newAgentEnv(t *testing.T, script fakeanthropic.Script) *agentEnv {
	t.Helper()
	fake, err := fakeanthropic.Start(script, "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { fake.Close() })
	cfg := &config.Config{WorkspaceDir: t.TempDir(), Agent: config.AgentConfig{
		AnthropicAPIKey: "fake", AnthropicBaseURL: fake.URL, Model: "claude-opus-5-5", MaxTokens: 16000, MaxSteps: 100,
	}}
	return &agentEnv{t: t, ctx: context.Background(), s: New(cfg, testPool, nil), fake: fake}
}

func turn(blocks ...fakeanthropic.Block) []fakeanthropic.Block { return blocks }

func tool(name string, input any) fakeanthropic.Block { return fakeanthropic.Tool(name, input) }

func (e *agentEnv) create(prompt string) uuid.UUID {
	e.t.Helper()
	id, err := e.s.Tasks.Create(e.ctx, NewTask{Prompt: prompt})
	if err != nil {
		e.t.Fatal(err)
	}
	return id
}

func (e *agentEnv) run(id uuid.UUID) *models.Task {
	e.t.Helper()
	if err := e.s.Agent.Run(e.ctx, id); err != nil {
		e.t.Fatal(err)
	}
	task, err := e.s.Tasks.Get(e.ctx, id)
	if err != nil {
		e.t.Fatal(err)
	}
	return task
}

func (e *agentEnv) must(err error) {
	e.t.Helper()
	if err != nil {
		e.t.Fatal(err)
	}
}

func (e *agentEnv) events(id uuid.UUID) []map[string]any {
	e.t.Helper()
	rows, err := e.s.Tasks.Events(e.ctx, id, -1)
	e.must(err)
	out := make([]map[string]any, len(rows))
	for i, r := range rows {
		e.must(json.Unmarshal(r.Data, &out[i]))
	}
	return out
}

func types(events []map[string]any) []string {
	out := make([]string, len(events))
	for i, ev := range events {
		out[i] = ev["type"].(string)
	}
	return out
}

// messages returns the messages of request i.
func (e *agentEnv) messages(i int) []any {
	return e.fake.Requests()[i]["messages"].([]any)
}

func lastContent(messages []any) []any {
	return messages[len(messages)-1].(map[string]any)["content"].([]any)
}

func wantTypes(t *testing.T, got []map[string]any, want ...string) {
	t.Helper()
	if g := types(got); !reflect.DeepEqual(g, want) {
		t.Fatalf("event types\n got %v\nwant %v", g, want)
	}
}

// connectors is a fake ConnectorProvider; handlers see the env so they can act mid-run.
type connectors struct {
	tools    []ConnectorTool
	approval map[string]bool
	call     func(name string, input map[string]any) string
	mu       sync.Mutex
	calls    []string
}

func (c *connectors) Snapshot(context.Context) (*ConnectorToolset, error) {
	set := &ConnectorToolset{Tools: c.tools, Handlers: map[string]ConnectorHandler{}, NeedsApproval: c.approval}
	for _, t := range c.tools {
		name := t.Name
		set.Handlers[name] = func(_ context.Context, input json.RawMessage) (string, bool, error) {
			var in map[string]any
			json.Unmarshal(input, &in)
			c.mu.Lock()
			c.calls = append(c.calls, name)
			c.mu.Unlock()
			return c.call(name, in), false, nil
		}
	}
	return set, nil
}

func connectorTools(names ...string) []ConnectorTool {
	out := make([]ConnectorTool, len(names))
	for i, n := range names {
		out[i] = ConnectorTool{Name: n, InputSchema: json.RawMessage(`{"type":"object"}`)}
	}
	return out
}

func TestLoopPlansNotifiesAsksKeepsErrorsUsesConnectorAndGoesIdle(t *testing.T) {
	e := newAgentEnv(t, fakeanthropic.Script{
		turn(tool("file_write", map[string]any{"path": "todo.md", "content": "- [ ] search notion"}), tool("message_notify_user", map[string]any{"text": "starting"})),
		turn(tool("message_ask_user", map[string]any{"text": "Which page?"})),
		turn(tool("file_read", map[string]any{"path": "../../etc/passwd"}), tool("notion_search", map[string]any{"query": "Roadmap"})),
		turn(tool("idle", map[string]any{"result": "found Roadmap", "files": []string{"todo.md"}})),
	})
	e.s.Connectors = &connectors{tools: connectorTools("notion_search"), call: func(_ string, in map[string]any) string { return "page:" + in["query"].(string) }}
	id := e.create("find the roadmap")

	if task := e.run(id); task.Status != models.TaskWaiting || task.Pending == nil || task.Pending.Kind != "ask" {
		t.Fatalf("want waiting on ask, got %s %+v", task.Status, task.Pending)
	}
	e.must(e.s.Tasks.Reply(e.ctx, id, "Roadmap"))
	if task := e.run(id); task.Status != models.TaskDone {
		t.Fatalf("want done, got %s", task.Status)
	}

	events := e.events(id)
	wantTypes(t, events, "message", "tool", "notify", "tool", "ask", "reply", "tool", "tool", "idle")
	if got := events[len(events)-1]; !reflect.DeepEqual(got, map[string]any{"type": "idle", "text": "found Roadmap", "files": []any{"todo.md"}}) {
		t.Fatalf("idle event: %v", got)
	}
	raw, err := os.ReadFile(filepath.Join(e.s.Agent.Workspace(id), "todo.md"))
	e.must(err)
	if string(raw) != "- [ ] search notion" {
		t.Fatalf("todo.md: %q", raw)
	}
	if got := lastContent(e.messages(2))[0].(map[string]any)["content"]; got != "Roadmap" {
		t.Fatalf("reply comes back as the ask result, got %v", got)
	}
	results := lastContent(e.messages(3))
	escape, notion := results[0].(map[string]any), results[1].(map[string]any)
	if escape["is_error"] != true || !strings.Contains(escape["content"].(string), "escapes the workspace") {
		t.Fatalf("path escape result: %v", escape)
	}
	if notion["content"] != "page:Roadmap" {
		t.Fatalf("connector result: %v", notion)
	}
	for i := 1; i < len(e.fake.Requests()); i++ {
		prev, cur := e.messages(i-1), e.messages(i)
		if !reflect.DeepEqual(cur[:len(prev)], prev) {
			t.Fatalf("request %d does not extend request %d unchanged", i, i-1)
		}
	}
	if task, _ := e.s.Tasks.Get(e.ctx, id); task.InputTokens != 4000 || task.OutputTokens != 200 {
		t.Fatalf("usage: in %d out %d", task.InputTokens, task.OutputTokens)
	}
}

func TestApprovalDenyStaysInContextAndAlwaysSkipsLaterPrompts(t *testing.T) {
	e := newAgentEnv(t, fakeanthropic.Script{
		turn(tool("gmail_send", map[string]any{"to": "a"})),
		turn(tool("gmail_send", map[string]any{"to": "b"})),
		turn(tool("gmail_send", map[string]any{"to": "c"}), tool("gmail_search", map[string]any{"q": "x"})),
		turn(tool("idle", map[string]any{"result": "sent"})),
	})
	var sent []string
	e.s.Connectors = &connectors{
		tools:    connectorTools("gmail_send", "gmail_search"),
		approval: map[string]bool{"gmail_send": true},
		call: func(name string, in map[string]any) string {
			if name == "gmail_send" {
				sent = append(sent, in["to"].(string))
			}
			return "ok"
		},
	}
	id := e.create("email")
	for _, d := range []string{"deny", "always"} {
		if task := e.run(id); task.Status != models.TaskWaiting || task.Pending.Kind != "confirm" {
			t.Fatalf("want waiting on confirm, got %s", task.Status)
		}
		if err := e.s.Tasks.Reply(e.ctx, id, "x"); err == nil {
			t.Fatal("reply to a confirm must conflict")
		}
		e.must(e.s.Tasks.Confirm(e.ctx, id, d))
	}
	if task := e.run(id); task.Status != models.TaskDone {
		t.Fatalf("want done, got %s", task.Status)
	}
	if !reflect.DeepEqual(sent, []string{"b", "c"}) {
		t.Fatalf("sent %v", sent)
	}
	wantTypes(t, e.events(id), "message", "confirm", "decision", "tool", "confirm", "decision", "tool", "tool", "tool", "idle")
	declined := lastContent(e.messages(1))[0].(map[string]any)
	if declined["is_error"] != true || !strings.Contains(declined["content"].(string), "declined") {
		t.Fatalf("declined result: %v", declined)
	}
	if err := e.s.Tasks.Confirm(e.ctx, id, "maybe"); err == nil {
		t.Fatal("bad decision must fail")
	}
}

func TestDeltasUsageEffortAndWebSearchEvent(t *testing.T) {
	e := newAgentEnv(t, fakeanthropic.Script{
		turn(fakeanthropic.Text("Looking it up."), fakeanthropic.WebSearch("lisbon", fakeanthropic.Result{Title: "Lisbon", URL: "https://example.com"}), tool("message_notify_user", map[string]any{"text": "Found a great guide to Lisbon"})),
		turn(tool("idle", map[string]any{"result": "done"})),
	})
	id, err := e.s.Tasks.Create(e.ctx, NewTask{Prompt: "go", Mode: "speed"})
	e.must(err)
	frames, stop, err := e.s.Bus.Subscribe(e.ctx, id.String())
	e.must(err)
	var deltas []string
	done := make(chan struct{})
	go func() {
		defer close(done)
		for f := range frames {
			if f.Kind == cache.FrameDelta {
				deltas = append(deltas, f.Text)
			}
		}
	}()
	e.run(id)
	stop()
	<-done

	if len(deltas) <= 2 || !strings.HasPrefix(strings.Join(deltas, ""), "Looking it up.Found a great guide to Lisbon") {
		t.Fatalf("deltas %q", deltas)
	}
	req := e.fake.Requests()[0]
	if req["output_config"].(map[string]any)["effort"] != "low" || req["stream"] != true {
		t.Fatalf("request: effort %v stream %v", req["output_config"], req["stream"])
	}
	events := e.events(id)
	want := map[string]any{"type": "tool", "name": "web_search", "input": map[string]any{"query": "lisbon"}, "output": "Lisbon https://example.com", "isError": false}
	if !reflect.DeepEqual(events[1], want) {
		t.Fatalf("web_search event %v", events[1])
	}
	if typ := e.messages(1)[1].(map[string]any)["content"].([]any)[1].(map[string]any)["type"]; typ != "server_tool_use" {
		t.Fatalf("server tool blocks go back unchanged, got %v", typ)
	}
	if task, _ := e.s.Tasks.Get(e.ctx, id); task.InputTokens != 2000 || task.OutputTokens != 100 || task.CacheReadTokens != 1600 || task.CacheWriteTokens != 200 {
		t.Fatalf("usage %+v", task)
	}
}

func TestStoppedWaitingTaskContinuesWithInterruptedResultAndQueuedMessagesRideAlong(t *testing.T) {
	e := newAgentEnv(t, fakeanthropic.Script{
		turn(tool("message_ask_user", map[string]any{"text": "which?"})),
		turn(tool("notion_poke", map[string]any{})),
		turn(tool("idle", map[string]any{"result": "done"})),
	})
	var id uuid.UUID
	e.s.Connectors = &connectors{tools: connectorTools("notion_poke"), call: func(string, map[string]any) string {
		e.must(e.s.Tasks.Message(e.ctx, id, "also do b", nil))
		return "poked"
	}}
	id = e.create("first")
	e.run(id)
	e.must(e.s.Tasks.Stop(e.ctx, id))
	if err := e.s.Tasks.Stop(e.ctx, id); err == nil {
		t.Fatal("stopping a stopped task must conflict")
	}
	e.must(e.s.Tasks.Message(e.ctx, id, "the second one", nil))
	if task := e.run(id); task.Status != models.TaskDone {
		t.Fatalf("want done, got %s", task.Status)
	}

	first := lastContent(e.messages(1))
	want := []any{
		map[string]any{"type": "tool_result", "tool_use_id": "toolu_0_0", "content": "Not run: the task was interrupted.", "is_error": true},
		map[string]any{"type": "text", "text": "the second one"},
	}
	if !reflect.DeepEqual(first, want) {
		t.Fatalf("continuation turn %v", first)
	}
	second := lastContent(e.messages(2))
	if second[0].(map[string]any)["type"] != "tool_result" || !reflect.DeepEqual(second[1], map[string]any{"type": "text", "text": "also do b"}) {
		t.Fatalf("queued message turn %v", second)
	}
	wantTypes(t, e.events(id), "message", "ask", "stopped", "message", "message", "tool", "idle")
}

func TestMessageQueuedAtIdleKeepsTheTaskGoing(t *testing.T) {
	e := newAgentEnv(t, fakeanthropic.Script{
		turn(tool("notion_poke", map[string]any{}), tool("idle", map[string]any{"result": "first"})),
		turn(tool("idle", map[string]any{"result": "second"})),
	})
	var id uuid.UUID
	e.s.Connectors = &connectors{tools: connectorTools("notion_poke"), call: func(string, map[string]any) string {
		e.must(e.s.Tasks.Message(e.ctx, id, "one more thing", nil))
		return "poked"
	}}
	id = e.create("go")
	e.run(id)
	if n := len(e.fake.Requests()); n != 2 {
		t.Fatalf("want 2 requests, got %d", n)
	}
	if last := lastContent(e.messages(1)); !reflect.DeepEqual(last[len(last)-1], map[string]any{"type": "text", "text": "one more thing"}) {
		t.Fatalf("last turn %v", last)
	}
	events := e.events(id)
	if got := events[len(events)-1]["text"]; got != "second" {
		t.Fatalf("result %v", got)
	}
}

func TestShellExecRunsInWorkspaceReportsExitCodesAndTimesOut(t *testing.T) {
	e := newAgentEnv(t, fakeanthropic.Script{
		turn(tool("shell_exec", map[string]any{"command": "pwd; echo hi > out.txt; exit 3"}), tool("shell_exec", map[string]any{"command": "sleep 5", "timeoutSec": 0.2})),
		turn(tool("idle", map[string]any{"result": "done"})),
	})
	id := e.create("go")
	for range 2 {
		e.run(id)
		e.must(e.s.Tasks.Confirm(e.ctx, id, "allow"))
	}
	e.run(id)
	var tools []map[string]any
	for _, ev := range e.events(id) {
		if ev["type"] == "tool" {
			tools = append(tools, ev)
		}
	}
	raw, err := os.ReadFile(filepath.Join(e.s.Agent.Workspace(id), "out.txt"))
	e.must(err)
	if string(raw) != "hi\n" {
		t.Fatalf("out.txt %q", raw)
	}
	if out := tools[0]["output"].(string); !strings.HasSuffix(out, id.String()+"\n\n[exit code 3]") || tools[0]["isError"] != true {
		t.Fatalf("exit code result %v", tools[0])
	}
	if out := tools[1]["output"].(string); !strings.Contains(out, "killed: timed out after 0.2 s") {
		t.Fatalf("timeout result %q", out)
	}
}

func TestShutdownMidToolResumesWithInterruptedResult(t *testing.T) {
	e := newAgentEnv(t, fakeanthropic.Script{
		turn(tool("file_write", map[string]any{"path": "a.md", "content": "a"}), tool("notion_hang", map[string]any{})),
		turn(tool("idle", map[string]any{"result": "done"})),
	})
	ctx, cancel := context.WithCancel(e.ctx)
	e.s.Connectors = &connectors{tools: connectorTools("notion_hang"), call: func(string, map[string]any) string {
		cancel()
		return "too late"
	}}
	id := e.create("go")
	if err := e.s.Agent.Run(ctx, id); err == nil {
		t.Fatal("a run cut short by shutdown must return its error so the job is retried")
	}
	task, _ := e.s.Tasks.Get(e.ctx, id)
	if task.Status != models.TaskRunning || len(task.PartialResults) != 1 {
		t.Fatalf("want running with one recorded result, got %s %d", task.Status, len(task.PartialResults))
	}
	if task := e.run(id); task.Status != models.TaskDone {
		t.Fatalf("want done, got %s", task.Status)
	}
	results := lastContent(e.messages(1))
	if len(results) != 2 || results[0].(map[string]any)["content"] != "wrote 1 chars to a.md" || results[1].(map[string]any)["content"] != interruptedText {
		t.Fatalf("results %v", results)
	}
	if n := len(e.fake.Requests()); n != 2 {
		t.Fatalf("the retry must not call the model again for the finished turn, got %d requests", n)
	}
}

func TestStopKillsRunningShell(t *testing.T) {
	e := newAgentEnv(t, fakeanthropic.Script{
		turn(tool("shell_exec", map[string]any{"command": "sleep 30 & sleep 30; echo never"})),
	})
	id := e.create("go")
	e.run(id)
	e.must(e.s.Tasks.Confirm(e.ctx, id, "allow"))
	go func() {
		time.Sleep(300 * time.Millisecond)
		e.must(e.s.Tasks.Stop(e.ctx, id))
	}()
	start := time.Now()
	if task := e.run(id); task.Status != models.TaskStopped || task.StopRequested {
		t.Fatalf("want stopped with the flag cleared, got %s %v", task.Status, task.StopRequested)
	}
	if time.Since(start) > 5*time.Second {
		t.Fatal("stop did not kill the shell")
	}
	events := e.events(id)
	wantTypes(t, events, "message", "confirm", "decision", "tool", "stopped")
	if out := events[3]["output"].(string); !strings.Contains(out, "[killed: stopped by the user]") {
		t.Fatalf("shell result %q", out)
	}
}

func TestPartialString(t *testing.T) {
	cases := []struct {
		raw, want string
		ok        bool
	}{
		{`{"text":"Hel`, "Hel", true},
		{`{"text":"a\"b\n`, "a\"b\n", true},
		{`{"text":"caf\u00e`, "caf", true},
		{`{"text":"x\`, "x", true},
		{`{"te`, "", false},
	}
	for _, c := range cases {
		if got, ok := partialString(c.raw, "text"); got != c.want || ok != c.ok {
			t.Errorf("partialString(%q) = %q, %v", c.raw, got, ok)
		}
	}
}

func TestSkillsProjectsAndKnowledgeShapeTheFirstMessage(t *testing.T) {
	e := newAgentEnv(t, fakeanthropic.Script{turn(tool("idle", map[string]any{"result": "ok"}))})
	name := "trip-" + uuid.NewString()[:8]
	skill, err := e.s.Skills.Create(e.ctx, name, "", "Plan day by day.")
	e.must(err)
	t.Cleanup(func() { e.s.Skills.Delete(e.ctx, skill.ID) })
	if _, err := e.s.Skills.Create(e.ctx, name, "", ""); err == nil {
		t.Fatal("duplicate skill must conflict")
	}
	if _, err := e.s.Skills.Create(e.ctx, "Bad Name", "", ""); err == nil {
		t.Fatal("bad skill name must fail")
	}
	p, err := e.s.Projects.Create(e.ctx, "Travel", "Prefer trains.")
	e.must(err)
	for _, k := range [][2]string{{"Home", "I live in Porto."}, {"Off", "hidden"}} {
		note, err := e.s.Knowledge.Create(e.ctx, k[0], k[1], k[0] == "Home")
		e.must(err)
		t.Cleanup(func() { e.s.Knowledge.Delete(e.ctx, note.ID) })
	}

	id, err := e.s.Tasks.Create(e.ctx, NewTask{Prompt: "/" + name + " Lisbon", ProjectID: &p.ID})
	e.must(err)
	e.run(id)
	text := lastContent(e.messages(0))[0].(map[string]any)["text"].(string)
	for _, want := range []string{"Prefer trains.", "I live in Porto.", "Plan day by day.", "Lisbon"} {
		if !strings.Contains(text, want) {
			t.Fatalf("first message lacks %q:\n%s", want, text)
		}
	}
	if strings.Contains(text, "hidden") {
		t.Fatal("disabled knowledge leaked into the first message")
	}
	if ev := e.events(id)[0]; ev["text"] != "/"+name+" Lisbon" {
		t.Fatalf("the message event keeps what the user typed, got %v", ev["text"])
	}
	if _, err := e.s.Tasks.Create(e.ctx, NewTask{Prompt: "/nope go"}); err == nil {
		t.Fatal("unknown skill must fail")
	}

	e.must(e.s.Projects.Delete(e.ctx, p.ID))
	task, err := e.s.Tasks.Get(e.ctx, id)
	e.must(err)
	if task.ProjectID != nil {
		t.Fatal("deleting a project must clear its tasks' projectId")
	}
	if _, err := queries.GetProject(e.ctx, e.s.Tasks.pool, p.ID); !IsNotFound(err) {
		t.Fatalf("project still there: %v", err)
	}
}

func TestCreateTaskCommitsWithTheCallersTransaction(t *testing.T) {
	e := newAgentEnv(t, fakeanthropic.Script{turn(tool("idle", map[string]any{"result": "ran"}))})
	schedule := uuid.New()
	create := func(commit bool) uuid.UUID {
		tx, err := testPool.Begin(e.ctx)
		e.must(err)
		defer tx.Rollback(e.ctx)
		raw, err := e.s.Tasks.CreateTask(e.ctx, tx, "scheduled", "speed", nil, schedule)
		e.must(err)
		if commit {
			e.must(tx.Commit(e.ctx))
		}
		return uuid.MustParse(raw)
	}
	if _, err := e.s.Tasks.Get(e.ctx, create(false)); !IsNotFound(err) {
		t.Fatalf("rolled back task exists: %v", err)
	}
	id := create(true)
	jobs, err := queries.ListJobsByKey(e.ctx, testPool, id.String())
	e.must(err)
	if len(jobs) != 1 || jobs[0].Type != RunTaskJob {
		t.Fatalf("want one run job, got %+v", jobs)
	}
	task := e.run(id)
	if task.Status != models.TaskDone || task.ScheduleID == nil || *task.ScheduleID != schedule || task.Mode != "speed" {
		t.Fatalf("task %+v", task)
	}
}
