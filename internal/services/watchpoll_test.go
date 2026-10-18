/*
 * Copyright (c) 2026. Papermark - All Rights Reserved
 * Unauthorized copying of this file, via any medium is strictly prohibited
 * Proprietary and confidential
 * Written by Ebot Tabi <ebot.tabi@gmail.com>, 17/10/2026
 */
package services

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"net/netip"
	"net/url"
	"strings"
	"sync"
	"testing"
	"time"

	"papermark/internal/database"
	"papermark/internal/models"
	"papermark/internal/models/queries"
	"papermark/internal/safehttp"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"
)

// fakeSite serves one page whose body the test changes.
type fakeSite struct {
	mu          sync.Mutex
	srv         *httptest.Server
	body        string
	etag        string
	status      int
	hits        int
	conditional int
}

func newFakeSite(t *testing.T) *fakeSite {
	s := &fakeSite{body: `<html><body><span id="price">€129</span></body></html>`, status: http.StatusOK}
	s.srv = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		s.mu.Lock()
		defer s.mu.Unlock()
		s.hits++
		if s.status != http.StatusOK {
			w.WriteHeader(s.status)
			return
		}
		if s.etag != "" {
			if r.Header.Get("If-None-Match") == s.etag {
				s.conditional++
				w.WriteHeader(http.StatusNotModified)
				return
			}
			w.Header().Set("ETag", s.etag)
		}
		fmt.Fprint(w, s.body)
	}))
	t.Cleanup(s.srv.Close)
	return s
}

func (s *fakeSite) set(body, etag string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.body, s.etag = body, etag
}

func (s *fakeSite) price(p string) {
	s.set(`<html><body><span id="price">`+p+`</span></body></html>`, "")
}

func (s *fakeSite) url() string {
	u, _ := url.Parse(s.srv.URL)
	return "http://shop.test:" + u.Port() + "/item"
}

type watchEnv struct {
	pool     *pgxpool.Pool
	svc      *TriggerService
	schedule *ScheduleService
	tasks    *recordingTasks
	site     *fakeSite
	clock    time.Time
	found    []string
}

func newWatchEnv(t *testing.T) *watchEnv {
	t.Helper()
	pool, _ := database.NewTestDatabase(t)
	e := &watchEnv{pool: pool, tasks: &recordingTasks{}, site: newFakeSite(t), clock: time.Now().UTC()}
	public := func(_ context.Context, host string) ([]netip.Addr, error) {
		return []netip.Addr{netip.MustParseAddr("93.184.216.34")}, nil
	}
	e.schedule = NewScheduleService(pool)
	e.schedule.SetTaskCreator(e.tasks)
	e.schedule.SetWebClient(safehttp.New(safehttp.Config{Lookup: public}))
	e.svc = NewTriggerService(pool, e.tasks)
	e.svc.now = func() time.Time { return e.clock }
	e.svc.web = safehttp.New(safehttp.Config{
		Blocked: func(netip.Addr) bool { return false },
		Lookup: func(context.Context, string) ([]netip.Addr, error) {
			return []netip.Addr{netip.MustParseAddr("127.0.0.1")}, nil
		},
	})
	e.svc.search = func(context.Context, string) ([]string, error) { return e.found, nil }
	return e
}

func (e *watchEnv) routine(t *testing.T, user uuid.UUID, rule WatchRule, prompt string) *models.Schedule {
	t.Helper()
	raw, _ := json.Marshal(rule)
	sc, err := e.schedule.Create(context.Background(), user, ScheduleFields{Title: "Watch", Prompt: prompt, Mode: "quality", Enabled: true, Trigger: "watch", Rule: raw})
	if err != nil {
		t.Fatal(err)
	}
	return sc
}

func (e *watchEnv) price(t *testing.T, user uuid.UUID, cond string, threshold float64, prompt string) *models.Schedule {
	return e.routine(t, user, WatchRule{Kind: "page", URL: e.site.url(), Selector: "#price", Condition: cond, Threshold: &threshold, IntervalMinutes: 1}, prompt)
}

// poll runs one pass two minutes after the last.
func (e *watchEnv) poll(t *testing.T) int {
	t.Helper()
	e.clock = e.clock.Add(2 * time.Minute)
	n, err := e.svc.RunWatch(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	return n
}

func (e *watchEnv) updates(t *testing.T, routine uuid.UUID) []string {
	t.Helper()
	rows, err := e.pool.Query(context.Background(), `SELECT body FROM updates WHERE schedule_id = $1 AND task_id IS NULL ORDER BY created_at, id`, routine)
	if err != nil {
		t.Fatal(err)
	}
	defer rows.Close()
	var out []string
	for rows.Next() {
		var b string
		rows.Scan(&b)
		out = append(out, b)
	}
	return out
}

func (e *watchEnv) count(t *testing.T, table string) (n int) {
	t.Helper()
	e.pool.QueryRow(context.Background(), `SELECT count(*) FROM `+table).Scan(&n)
	return n
}

func TestWatchBelowHysteresisWithDirectUpdate(t *testing.T) {
	e := newWatchEnv(t)
	ctx := context.Background()
	user := testUser(t, e.pool)
	r := e.price(t, user, "below", 120, "")

	e.poll(t) // €129: not met, armed
	if len(e.updates(t, r.ID)) != 0 {
		t.Fatal("notified while the condition is not met")
	}
	e.site.price("€118")
	e.poll(t)
	got := e.updates(t, r.ID)
	if len(got) != 1 || got[0] != "€129 → €118" {
		t.Fatalf("updates: %q", got)
	}
	evs, _ := queries.ListTriggerEvents(ctx, e.pool, user, r.ID, 10)
	if len(evs) != 1 || evs[0].TaskID != nil || evs[0].Summary != "€129 → €118" || e.tasks.count() != 0 {
		t.Fatalf("events=%+v tasks=%d", evs, e.tasks.count())
	}
	var title string
	var imp string
	e.pool.QueryRow(ctx, `SELECT title, importance FROM updates WHERE schedule_id = $1`, r.ID).Scan(&title, &imp)
	if title != "Watch" || imp != models.ImportanceFeed {
		t.Fatalf("update: %q %q", title, imp)
	}

	e.site.price("€117") // still below: disarmed, silent
	e.poll(t)
	if len(e.updates(t, r.ID)) != 1 {
		t.Fatal("notified again while the condition stayed met")
	}
	e.site.price("€130") // back above: re-arms
	e.poll(t)
	e.site.price("€110")
	e.poll(t)
	if got := e.updates(t, r.ID); len(got) != 2 || got[1] != "€130 → €110" {
		t.Fatalf("after re-arming: %q", got)
	}
}

func TestWatchAboveContainsAndChanged(t *testing.T) {
	e := newWatchEnv(t)
	user := testUser(t, e.pool)
	above := e.price(t, user, "above", 1000, "")
	contains := e.routine(t, user, WatchRule{Kind: "page", URL: e.site.url(), Selector: "#price", Condition: "contains", Text: "sale", IntervalMinutes: 1}, "")
	changed := e.routine(t, user, WatchRule{Kind: "page", URL: e.site.url(), Selector: "#price", Condition: "changed", IntervalMinutes: 1}, "")
	e.poll(t)
	e.site.price("$1,299.50 sale")
	e.poll(t)
	e.poll(t)
	for name, r := range map[string]*models.Schedule{"above": above, "contains": contains, "changed": changed} {
		if got := e.updates(t, r.ID); len(got) != 1 {
			t.Errorf("%s: %q", name, got)
		}
	}
	if e.site.hits != 3 {
		t.Fatalf("three routines on one page fetched it %d times in 3 passes", e.site.hits)
	}
}

func TestWatchNotModifiedIsNoOp(t *testing.T) {
	e := newWatchEnv(t)
	user := testUser(t, e.pool)
	r := e.price(t, user, "changed", 0, "")
	e.site.set(`<html><body><span id="price">€129</span></body></html>`, `"v1"`)
	e.poll(t)
	e.poll(t)
	e.poll(t)
	if e.site.conditional != 2 || len(e.updates(t, r.ID)) != 0 || e.count(t, "trigger_events") != 0 {
		t.Fatalf("conditional=%d updates=%d events=%d", e.site.conditional, len(e.updates(t, r.ID)), e.count(t, "trigger_events"))
	}
	e.site.set(`<html><body><span id="price">€99</span></body></html>`, `"v2"`)
	e.poll(t)
	if got := e.updates(t, r.ID); len(got) != 1 || got[0] != "€129 → €99" {
		t.Fatalf("after change: %q", got)
	}
}

func TestWatchSharedTargetAcrossUsersAndOrphanCleanup(t *testing.T) {
	e := newWatchEnv(t)
	ctx := context.Background()
	a, b := testUser(t, e.pool), testUser(t, e.pool)
	ra := e.price(t, a, "below", 120, "")
	rb := e.price(t, b, "below", 100, "Tell me about the price")
	if e.count(t, "watch_targets") != 1 || e.count(t, "watch_subscriptions") != 2 {
		t.Fatalf("targets=%d subscriptions=%d", e.count(t, "watch_targets"), e.count(t, "watch_subscriptions"))
	}
	e.poll(t)
	e.site.price("€110")
	e.poll(t)
	if e.site.hits != 2 {
		t.Fatalf("two users, one page: %d fetches over 2 passes", e.site.hits)
	}
	if len(e.updates(t, ra.ID)) != 1 || len(e.updates(t, rb.ID)) != 0 {
		t.Fatalf("only the user whose threshold was crossed hears: %v %v", e.updates(t, ra.ID), e.updates(t, rb.ID))
	}
	e.site.price("€90")
	e.poll(t)
	if e.tasks.count() != 1 {
		t.Fatalf("tasks: %d", e.tasks.count())
	}
	p := e.tasks.last()
	for _, want := range []string{"Tell me about the price", "untrusted", "€110", "€90"} {
		if !strings.Contains(p, want) {
			t.Errorf("prompt lacks %q:\n%s", want, p)
		}
	}
	evs, _ := queries.ListTriggerEvents(ctx, e.pool, b, rb.ID, 10)
	if len(evs) != 1 || evs[0].TaskID == nil {
		t.Fatalf("events: %+v", evs)
	}

	if err := e.schedule.Delete(ctx, a, ra.ID); err != nil {
		t.Fatal(err)
	}
	if e.count(t, "watch_targets") != 1 {
		t.Fatal("target removed while a subscriber remains")
	}
	if _, err := e.schedule.Update(ctx, b, rb.ID, ScheduleFields{Title: "Watch", Prompt: "x", Mode: "quality", Enabled: false, Trigger: "watch", Rule: rb.Rule}); err != nil {
		t.Fatal(err)
	}
	if e.count(t, "watch_targets") != 0 || e.count(t, "watch_subscriptions") != 0 {
		t.Fatalf("orphans: targets=%d subscriptions=%d", e.count(t, "watch_targets"), e.count(t, "watch_subscriptions"))
	}
}

func TestWatchSearchNewItems(t *testing.T) {
	e := newWatchEnv(t)
	user := testUser(t, e.pool)
	r := e.routine(t, user, WatchRule{Kind: "search", Query: "keen launch", Condition: "newItems", IntervalMinutes: 1}, "Summarise what is new")
	e.found = []string{"https://b.example/2", "https://a.example/1"}
	e.poll(t)
	e.found = []string{"https://a.example/1", "https://b.example/2"} // same set, other order
	e.poll(t)
	if e.tasks.count() != 0 {
		t.Fatalf("baseline or reorder fired: %d", e.tasks.count())
	}
	e.found = []string{"https://a.example/1", "https://b.example/2", "https://c.example/3"}
	e.poll(t)
	if e.tasks.count() != 1 || !strings.Contains(e.tasks.last(), "https://c.example/3") || !strings.Contains(e.tasks.last(), "Summarise what is new") {
		t.Fatalf("tasks=%d", e.tasks.count())
	}
	evs, _ := queries.ListTriggerEvents(context.Background(), e.pool, user, r.ID, 10)
	if len(evs) != 1 || !strings.Contains(evs[0].Summary, "1 new") {
		t.Fatalf("events: %+v", evs)
	}
	e.found = []string{"https://a.example/1", "https://b.example/2"} // an item disappearing is not new
	e.poll(t)
	if e.tasks.count() != 1 {
		t.Fatalf("removal fired: %d", e.tasks.count())
	}
}

func TestWatchLayoutChangeNotesOnce(t *testing.T) {
	e := newWatchEnv(t)
	user := testUser(t, e.pool)
	r := e.price(t, user, "below", 120, "")
	e.poll(t)
	e.site.set(`<html><body><p>redesigned</p></body></html>`, "")
	for i := 0; i < 5; i++ {
		e.poll(t)
	}
	got := e.updates(t, r.ID)
	if len(got) != 1 || !strings.Contains(got[0], "layout changed") {
		t.Fatalf("updates: %q", got)
	}
	e.site.price("€150")
	e.poll(t)
	tg, _ := queries.ListWatchRoutines(context.Background(), e.pool, mustTarget(t, e, r.ID))
	if len(tg) != 1 {
		t.Fatal("routine lost")
	}
}

func mustTarget(t *testing.T, e *watchEnv, routine uuid.UUID) uuid.UUID {
	t.Helper()
	id, err := queries.WatchSubscriptionTarget(context.Background(), e.pool, routine)
	if err != nil || id == uuid.Nil {
		t.Fatalf("target: %v %v", id, err)
	}
	return id
}

func TestWatchTenFailuresDisableTheRoutine(t *testing.T) {
	e := newWatchEnv(t)
	ctx := context.Background()
	user := testUser(t, e.pool)
	r := e.price(t, user, "below", 120, "")
	e.poll(t)
	e.site.mu.Lock()
	e.site.status = http.StatusInternalServerError
	e.site.mu.Unlock()
	for i := 0; i < 9; i++ {
		e.poll(t)
	}
	if sc, _ := queries.GetSchedule(ctx, e.pool, user, r.ID); !sc.Enabled {
		t.Fatal("disabled after nine failures")
	}
	e.poll(t)
	sc, _ := queries.GetSchedule(ctx, e.pool, user, r.ID)
	if sc.Enabled {
		t.Fatal("still enabled after ten failures")
	}
	got := e.updates(t, r.ID)
	if len(got) != 1 || !strings.Contains(got[0], "Paused") {
		t.Fatalf("updates: %q", got)
	}
	if e.count(t, "watch_targets") != 0 {
		t.Fatal("target of a disabled routine survives")
	}
}

func TestWatchMaxRunsPerHour(t *testing.T) {
	e := newWatchEnv(t)
	user := testUser(t, e.pool)
	threshold := 0.0
	raw, _ := json.Marshal(WatchRule{Kind: "page", URL: e.site.url(), Selector: "#price", Condition: "changed", Threshold: &threshold, IntervalMinutes: 1})
	r, err := e.schedule.Create(context.Background(), user, ScheduleFields{Title: "W", Mode: "quality", Enabled: true, Trigger: "watch", Rule: raw, MaxRunsPerHour: 2})
	if err != nil {
		t.Fatal(err)
	}
	e.poll(t)
	for i := 0; i < 5; i++ {
		e.site.price(fmt.Sprintf("€%d", 100+i))
		e.poll(t)
	}
	got := e.updates(t, r.ID)
	if len(got) != 3 { // two updates and one limit note
		t.Fatalf("updates: %q", got)
	}
}

func TestWatchUpdateListsChangesAsLinks(t *testing.T) {
	body, sections := watchUpdate("a → b", "https://a.example/1\nhttps://a.example/2", "https://a.example/2\nhttps://b.example/3", "")
	if body != "1 new, 1 gone" || len(sections) != 2 {
		t.Fatalf("list: %q %+v", body, sections)
	}
	if now := sections[0]; now.Title != "Now" || len(now.Items) != 1 || now.Items[0].URL != "https://b.example/3" {
		t.Fatalf("now: %+v", now)
	}
	if gone := sections[1]; gone.Title != "Before" || gone.Items[0].URL != "https://a.example/1" {
		t.Fatalf("before: %+v", gone)
	}

	body, sections = watchUpdate("€129 → €118", "€129", "€118", "https://shop.example/item")
	if body != "€129 → €118" || len(sections) != 3 || sections[0].Items[0].URL != "" || sections[2].Items[0].URL != "https://shop.example/item" {
		t.Fatalf("value: %q %+v", body, sections)
	}

	many := make([]string, 12)
	for i := range many {
		many[i] = fmt.Sprintf("javascript:alert(%d)", i)
	}
	_, sections = watchUpdate("", "", strings.Join(many, "\n"), "")
	if items := sections[0].Items; len(items) != 11 || items[0].URL != "" || items[10].Text != "and 2 more" {
		t.Fatalf("capped: %+v", items)
	}
}

func TestFirstNumber(t *testing.T) {
	for in, want := range map[string]float64{
		"€129": 129, "$1,299.50": 1299.5, "1.299,50 EUR": 1299.5, "12,5 kg": 12.5, "Price: 1,299": 1299, "was 99.99 now 79": 99.99,
		"3.": 3, "2024 items": 2024, "USD 5": 5,
	} {
		if got, ok := firstNumber(in); !ok || got != want {
			t.Errorf("%q: got %v %v, want %v", in, got, ok, want)
		}
	}
	if _, ok := firstNumber("no digits"); ok {
		t.Error("parsed a number out of words")
	}
}

func TestExtractValue(t *testing.T) {
	page := []byte(`<html><head><style>x{}</style></head><body><script>var a=1</script><ul><li class="i"> one </li><li class="i">two</li></ul><p>  hello   world </p></body></html>`)
	if v, found, _ := extractValue(page, "li.i"); !found || v != "one\ntwo" {
		t.Errorf("selector: %q %v", v, found)
	}
	if _, found, _ := extractValue(page, "#missing"); found {
		t.Error("missing selector found")
	}
	if v, _, _ := extractValue(page, ""); v != "one two hello world" {
		t.Errorf("visible text: %q", v)
	}
}
