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
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"

	"papermark/internal/database"
	"papermark/internal/integrations/gmail"
	"papermark/internal/models"
	"papermark/internal/models/queries"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"
)

// fakeMail stands in for the Gmail API of one mailbox.
type fakeMail struct {
	mu          sync.Mutex
	srv         *httptest.Server
	profile     string   // historyId the profile reports
	added       []string // ids history.list reports
	next        string   // historyId history.list reports
	historyCode int
	msgs        map[string]gmail.Message
	fetched     int
}

func newFakeMail(t *testing.T) *fakeMail {
	f := &fakeMail{profile: "100", next: "100", historyCode: http.StatusOK, msgs: map[string]gmail.Message{}}
	mux := http.NewServeMux()
	mux.HandleFunc("/gmail/v1/users/me/profile", func(w http.ResponseWriter, r *http.Request) {
		f.mu.Lock()
		defer f.mu.Unlock()
		json.NewEncoder(w).Encode(map[string]string{"emailAddress": "me@example.com", "historyId": f.profile})
	})
	mux.HandleFunc("/gmail/v1/users/me/history", func(w http.ResponseWriter, r *http.Request) {
		f.mu.Lock()
		defer f.mu.Unlock()
		if f.historyCode != http.StatusOK {
			http.Error(w, `{"error":{"message":"x"}}`, f.historyCode)
			return
		}
		var h []map[string]any
		for _, id := range f.added {
			h = append(h, map[string]any{"messagesAdded": []map[string]any{{"message": map[string]string{"id": id}}}})
		}
		json.NewEncoder(w).Encode(map[string]any{"history": h, "historyId": f.next})
	})
	mux.HandleFunc("/gmail/v1/users/me/messages/", func(w http.ResponseWriter, r *http.Request) {
		f.mu.Lock()
		defer f.mu.Unlock()
		f.fetched++
		m, ok := f.msgs[strings.TrimPrefix(r.URL.Path, "/gmail/v1/users/me/messages/")]
		if !ok {
			http.Error(w, `{"error":{"message":"not found"}}`, http.StatusNotFound)
			return
		}
		json.NewEncoder(w).Encode(m)
	})
	f.srv = httptest.NewServer(mux)
	t.Cleanup(f.srv.Close)
	return f
}

// put adds a message to the mailbox and lists it as new.
func (f *fakeMail) put(id, from, subject string, labels ...string) {
	f.mu.Lock()
	defer f.mu.Unlock()
	if labels == nil {
		labels = []string{"INBOX"}
	}
	f.msgs[id] = gmail.Message{ID: id, Snippet: "snippet of " + id, LabelIDs: labels, Payload: gmail.Part{Headers: []gmail.Header{{Name: "From", Value: from}, {Name: "Subject", Value: subject}}}}
	f.added = append(f.added, id)
}

func (f *fakeMail) setHeader(id, name, value string) {
	f.mu.Lock()
	defer f.mu.Unlock()
	m := f.msgs[id]
	m.Payload.Headers = append(m.Payload.Headers, gmail.Header{Name: name, Value: value})
	f.msgs[id] = m
}

// advance moves the history position and forgets the listed messages.
func (f *fakeMail) advance(next string) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.next, f.added = next, nil
}

// recordingTasks inserts a real task row per call, as the task service does.
type recordingTasks struct {
	mu      sync.Mutex
	prompts []string
}

func (r *recordingTasks) CreateTask(ctx context.Context, db queries.DBTX, userID uuid.UUID, prompt, mode string, projectID *uuid.UUID, scheduleID uuid.UUID) (string, error) {
	t := &models.Task{UserID: userID, Prompt: prompt, Status: models.TaskRunning, Mode: mode, ProjectID: projectID, ScheduleID: &scheduleID}
	if err := queries.InsertTask(ctx, db, t); err != nil {
		return "", err
	}
	r.mu.Lock()
	r.prompts = append(r.prompts, prompt)
	r.mu.Unlock()
	return t.ID.String(), nil
}

func (r *recordingTasks) count() int {
	r.mu.Lock()
	defer r.mu.Unlock()
	return len(r.prompts)
}

func (r *recordingTasks) last() string {
	r.mu.Lock()
	defer r.mu.Unlock()
	return r.prompts[len(r.prompts)-1]
}

type fakeModel struct {
	mu      sync.Mutex
	calls   int
	prompts []string
	reply   string
	err     error
}

func (m *fakeModel) Complete(_ context.Context, _, prompt string, _ int) (string, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.calls++
	m.prompts = append(m.prompts, prompt)
	return m.reply, m.err
}

type mailEnv struct {
	pool     *pgxpool.Pool
	svc      *TriggerService
	schedule *ScheduleService
	tasks    *recordingTasks
	mail     *fakeMail
	model    *fakeModel
	user     uuid.UUID
	acct     *models.GmailAccount
	clock    time.Time
	authed   []uuid.UUID
	clientEr error
}

func newMailEnv(t *testing.T) *mailEnv {
	t.Helper()
	pool, _ := database.NewTestDatabase(t)
	e := &mailEnv{pool: pool, tasks: &recordingTasks{}, mail: newFakeMail(t), model: &fakeModel{reply: "[]"}, user: testUser(t, pool), clock: time.Now().UTC()}
	e.schedule = NewScheduleService(pool)
	e.schedule.SetTaskCreator(e.tasks)
	e.svc = NewTriggerService(pool, e.tasks)
	e.svc.model = e.model
	e.svc.now = func() time.Time { return e.clock }
	e.svc.client = func(context.Context, *models.GmailAccount) (*gmail.Client, error) {
		if e.clientEr != nil {
			return nil, e.clientEr
		}
		return &gmail.Client{HTTP: e.mail.srv.Client(), BaseURL: e.mail.srv.URL}, nil
	}
	e.svc.onAuth = func(id uuid.UUID) { e.authed = append(e.authed, id) }
	e.acct = e.addAccount(t, "me@example.com", true)
	return e
}

func (e *mailEnv) addAccount(t *testing.T, email string, primary bool) *models.GmailAccount {
	t.Helper()
	ctx := context.Background()
	c, err := queries.GetConnectorByName(ctx, e.pool, e.user, "gmail")
	if errors.Is(err, queries.ErrNotFound) {
		c = &models.Connector{UserID: e.user, Name: "gmail", Kind: models.ConnectorGmail, URL: "x", Status: models.ConnectorConnected}
		if err := queries.CreateConnector(ctx, e.pool, c); err != nil {
			t.Fatal(err)
		}
	}
	a := &models.GmailAccount{UserID: e.user, ConnectorID: c.ID, Email: email, Status: models.ConnectorConnected, Primary: primary}
	if err := queries.CreateGmailAccount(ctx, e.pool, a); err != nil {
		t.Fatal(err)
	}
	return a
}

func (e *mailEnv) routine(t *testing.T, rule EmailRule, mutate ...func(*ScheduleFields)) *models.Schedule {
	t.Helper()
	raw, _ := json.Marshal(rule)
	f := ScheduleFields{Title: "Mail", Prompt: "Handle this mail", Mode: "quality", Enabled: true, Trigger: "email", Rule: raw}
	for _, m := range mutate {
		m(&f)
	}
	sc, err := e.schedule.Create(context.Background(), e.user, f)
	if err != nil {
		t.Fatal(err)
	}
	return sc
}

// poll runs one pass of the poller three minutes after the last.
func (e *mailEnv) poll(t *testing.T) int {
	t.Helper()
	e.clock = e.clock.Add(3 * time.Minute)
	n, err := e.svc.RunMail(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	return n
}

func (e *mailEnv) history(t *testing.T) string {
	t.Helper()
	p, err := queries.GetMailboxPoll(context.Background(), e.pool, e.acct.ID)
	if err != nil {
		t.Fatal(err)
	}
	return p.HistoryID
}

// baseline runs the first poll, which only records where the mailbox is.
func (e *mailEnv) baseline(t *testing.T) {
	t.Helper()
	e.poll(t)
	if h := e.history(t); h != "100" {
		t.Fatalf("baseline history %q", h)
	}
}

func TestMailPollRowsFollowRoutines(t *testing.T) {
	e := newMailEnv(t)
	ctx := context.Background()
	count := func() (n int) {
		e.pool.QueryRow(ctx, `SELECT count(*) FROM mailbox_polls`).Scan(&n)
		return n
	}
	if count() != 0 {
		t.Fatal("poll row without a routine")
	}
	other := e.addAccount(t, "other@example.com", false)
	r := e.routine(t, EmailRule{From: "bank", Account: "other@example.com"})
	if _, err := queries.GetMailboxPoll(ctx, e.pool, e.acct.ID); !IsNotFound(err) {
		t.Fatalf("poll for an account the rule excludes: %v", err)
	}
	if _, err := queries.GetMailboxPoll(ctx, e.pool, other.ID); err != nil {
		t.Fatal(err)
	}
	f := ScheduleFields{Title: r.Title, Prompt: r.Prompt, Mode: r.Mode, Enabled: false, Trigger: "email", Rule: r.Rule}
	if _, err := e.schedule.Update(ctx, e.user, r.ID, f); err != nil {
		t.Fatal(err)
	}
	if count() != 0 {
		t.Fatal("poll row survives a disabled routine")
	}
	f.Enabled = true
	if _, err := e.schedule.Update(ctx, e.user, r.ID, f); err != nil || count() != 1 {
		t.Fatalf("re-enable: %v count=%d", err, count())
	}
	if err := e.schedule.Delete(ctx, e.user, r.ID); err != nil || count() != 0 {
		t.Fatalf("delete: %v count=%d", err, count())
	}
}

func TestMailFiresOnceAndReplayIsIdempotent(t *testing.T) {
	e := newMailEnv(t)
	ctx := context.Background()
	r := e.routine(t, EmailRule{From: "BANK@x.com"})
	e.baseline(t)
	if e.tasks.count() != 0 {
		t.Fatal("baseline fired a routine")
	}

	e.mail.put("m1", "Bank <bank@x.com>", "Card payment")
	e.mail.advance("101")
	e.mail.mu.Lock()
	e.mail.added = []string{"m1"}
	e.mail.mu.Unlock()
	e.poll(t)
	if e.tasks.count() != 1 {
		t.Fatalf("tasks: %d", e.tasks.count())
	}
	p := e.tasks.last()
	for _, want := range []string{"Handle this mail", "untrusted", "me@example.com", "m1", "Bank <bank@x.com>", "Card payment", "snippet of m1"} {
		if !strings.Contains(p, want) {
			t.Errorf("prompt lacks %q:\n%s", want, p)
		}
	}
	if e.history(t) != "101" {
		t.Fatalf("history %q", e.history(t))
	}
	evs, _ := queries.ListTriggerEvents(ctx, e.pool, e.user, r.ID, 10)
	if len(evs) != 1 || evs[0].TaskID == nil || !strings.Contains(evs[0].Summary, "Card payment") {
		t.Fatalf("events: %+v", evs)
	}

	// A crash after the commit but before the lease ended replays the same history.
	if _, err := e.pool.Exec(ctx, `UPDATE mailbox_polls SET history_id = '100'`); err != nil {
		t.Fatal(err)
	}
	e.poll(t)
	if e.tasks.count() != 1 {
		t.Fatalf("replay created %d tasks", e.tasks.count())
	}
	if evs, _ := queries.ListTriggerEvents(ctx, e.pool, e.user, r.ID, 10); len(evs) != 1 {
		t.Fatalf("replay events: %d", len(evs))
	}
}

func TestMailBurstCoalescesIntoOneTask(t *testing.T) {
	e := newMailEnv(t)
	e.routine(t, EmailRule{Subject: "invoice"})
	e.baseline(t)
	for _, id := range []string{"a", "b", "c", "d", "e"} {
		e.mail.put(id, "ap@vendor.com", "Invoice "+id)
	}
	e.mail.put("z", "friend@x.com", "lunch")
	e.mail.mu.Lock()
	e.mail.next = "110"
	e.mail.mu.Unlock()
	e.poll(t)
	if e.tasks.count() != 1 {
		t.Fatalf("tasks: %d", e.tasks.count())
	}
	p := e.tasks.last()
	for _, id := range []string{"a", "b", "c", "d", "e"} {
		if !strings.Contains(p, "id: "+id+"\n") {
			t.Errorf("prompt lacks %s", id)
		}
	}
	if strings.Contains(p, "lunch") {
		t.Error("prompt carries a message the rule does not match")
	}
}

func TestMailIgnoresSentDraftSelfAndKeen(t *testing.T) {
	e := newMailEnv(t)
	e.routine(t, EmailRule{Subject: "report"})
	e.baseline(t)
	e.mail.put("sent", "me@example.com", "report sent", "INBOX", "SENT")
	e.mail.put("draft", "x@x.com", "report draft", "DRAFT")
	e.mail.put("self", "Me <ME@example.com>", "report to self")
	e.mail.put("keen", "x@x.com", "report from keen")
	e.mail.setHeader("keen", "X-Keen-Sent", "1")
	e.mail.put("real", "x@x.com", "report real")
	e.mail.mu.Lock()
	e.mail.next = "105"
	e.mail.mu.Unlock()
	e.poll(t)
	p := e.tasks.last()
	if !strings.Contains(p, "id: real\n") || strings.Count(p, "- account:") != 1 {
		t.Fatalf("prompt:\n%s", p)
	}
}

func TestMailRuleMatching(t *testing.T) {
	yes, no := true, false
	cases := []struct {
		name string
		rule EmailRule
		a    gmail.Arrival
		want bool
	}{
		{"from substring any case", EmailRule{From: "BANK"}, gmail.Arrival{From: "Bank <b@bank.com>"}, true},
		{"from miss", EmailRule{From: "bank"}, gmail.Arrival{From: "x@y.com"}, false},
		{"subject any case", EmailRule{Subject: "urgent"}, gmail.Arrival{Subject: "URGENT: pay"}, true},
		{"label", EmailRule{Label: "important"}, gmail.Arrival{Labels: []string{"INBOX", "IMPORTANT"}}, true},
		{"label miss", EmailRule{Label: "important"}, gmail.Arrival{Labels: []string{"INBOX"}}, false},
		{"attachment wanted", EmailRule{HasAttachment: &yes}, gmail.Arrival{HasAttachment: true}, true},
		{"attachment wanted, none", EmailRule{HasAttachment: &yes}, gmail.Arrival{}, false},
		{"no attachment wanted", EmailRule{HasAttachment: &no}, gmail.Arrival{HasAttachment: true}, false},
		{"all fields", EmailRule{From: "a", Subject: "b", Label: "c"}, gmail.Arrival{From: "a", Subject: "b", Labels: []string{"C"}}, true},
		{"one of several fails", EmailRule{From: "a", Subject: "b"}, gmail.Arrival{From: "a", Subject: "x"}, false},
	}
	for _, c := range cases {
		if got := c.rule.matches(&c.a); got != c.want {
			t.Errorf("%s: got %v", c.name, got)
		}
	}
}

func TestMailSmartRuleOneModelCallPerRoutinePerBatch(t *testing.T) {
	e := newMailEnv(t)
	e.routine(t, EmailRule{Smart: "looks urgent"})
	e.baseline(t)
	e.mail.put("m1", "a@x.com", "newsletter")
	e.mail.put("m2", "b@x.com", "server is down")
	e.mail.put("m3", "c@x.com", "promo")
	e.mail.mu.Lock()
	e.mail.next = "103"
	e.mail.mu.Unlock()
	e.model.reply = `Sure: ["m2"]`
	e.poll(t)
	if e.model.calls != 1 {
		t.Fatalf("model calls: %d", e.model.calls)
	}
	if !strings.Contains(e.model.prompts[0], "looks urgent") || !strings.Contains(e.model.prompts[0], "untrusted") {
		t.Fatalf("model prompt:\n%s", e.model.prompts[0])
	}
	p := e.tasks.last()
	if e.tasks.count() != 1 || !strings.Contains(p, "id: m2\n") || strings.Contains(p, "id: m1\n") || strings.Contains(p, "id: m3\n") {
		t.Fatalf("tasks=%d prompt:\n%s", e.tasks.count(), p)
	}
}

func TestMailSmartFailureKeepsHistoryAndRetries(t *testing.T) {
	e := newMailEnv(t)
	e.routine(t, EmailRule{Smart: "looks urgent"})
	e.baseline(t)
	e.mail.put("m1", "a@x.com", "down")
	e.mail.mu.Lock()
	e.mail.next = "101"
	e.mail.mu.Unlock()
	e.model.err = errors.New("model down")
	e.poll(t)
	if e.history(t) != "100" || e.tasks.count() != 0 {
		t.Fatalf("history=%s tasks=%d", e.history(t), e.tasks.count())
	}
	p, _ := queries.GetMailboxPoll(context.Background(), e.pool, e.acct.ID)
	if p.Failures != 1 || p.LastError == "" {
		t.Fatalf("poll: %+v", p)
	}
	e.model.err, e.model.reply = nil, `["m1"]`
	e.poll(t)
	if e.tasks.count() != 1 {
		t.Fatalf("tasks after recovery: %d", e.tasks.count())
	}
}

func TestMailMaxRunsPerHour(t *testing.T) {
	e := newMailEnv(t)
	ctx := context.Background()
	r := e.routine(t, EmailRule{Subject: "alert"}, func(f *ScheduleFields) { f.MaxRunsPerHour = 2 })
	e.baseline(t)
	for i, id := range []string{"a", "b", "c", "d"} {
		e.mail.advance(string(rune('1'+i)) + "01")
		e.mail.put(id, "x@x.com", "alert "+id)
		e.poll(t)
	}
	if e.tasks.count() != 2 {
		t.Fatalf("tasks: %d", e.tasks.count())
	}
	var notes int
	e.pool.QueryRow(ctx, `SELECT count(*) FROM updates WHERE schedule_id = $1 AND task_id IS NULL`, r.ID).Scan(&notes)
	if notes != 1 {
		t.Fatalf("limit notes: %d", notes)
	}
	// An hour later the routine runs again.
	e.clock = e.clock.Add(time.Hour)
	e.mail.advance("999")
	e.mail.put("e", "x@x.com", "alert e")
	e.poll(t)
	if e.tasks.count() != 3 {
		t.Fatalf("tasks after the hour: %d", e.tasks.count())
	}
}

func TestMailHistoryExpiredResyncs(t *testing.T) {
	e := newMailEnv(t)
	e.routine(t, EmailRule{From: "x"})
	e.baseline(t)
	e.mail.mu.Lock()
	e.mail.historyCode, e.mail.profile = http.StatusNotFound, "900"
	e.mail.mu.Unlock()
	e.poll(t)
	if e.history(t) != "900" {
		t.Fatalf("history %q", e.history(t))
	}
	var notes int
	e.pool.QueryRow(context.Background(), `SELECT count(*) FROM updates WHERE user_id = $1`, e.user).Scan(&notes)
	if notes != 1 || e.tasks.count() != 0 {
		t.Fatalf("notes=%d tasks=%d", notes, e.tasks.count())
	}
}

func TestMailUnauthorizedStopsPolling(t *testing.T) {
	e := newMailEnv(t)
	e.routine(t, EmailRule{From: "x"})
	e.baseline(t)
	e.clientEr = gmail.ErrNeedsAuth
	e.poll(t)
	if len(e.authed) != 1 || e.authed[0] != e.acct.ID {
		t.Fatalf("needs_auth calls: %v", e.authed)
	}
	// The real hook marks the account needs_auth, which drops its poll row.
	if err := queries.UpdateGmailAccountStatus(context.Background(), e.pool, e.acct.ID, models.ConnectorNeedsAuth, nil); err != nil {
		t.Fatal(err)
	}
	e.clientEr = nil
	e.poll(t)
	if _, err := queries.GetMailboxPoll(context.Background(), e.pool, e.acct.ID); !IsNotFound(err) {
		t.Fatalf("poll row of a needs_auth account: %v", err)
	}
}

func TestMailServerErrorsBackOff(t *testing.T) {
	e := newMailEnv(t)
	e.routine(t, EmailRule{From: "x"})
	e.baseline(t)
	e.mail.mu.Lock()
	e.mail.historyCode = http.StatusServiceUnavailable
	e.mail.mu.Unlock()
	for i, want := range []time.Duration{time.Minute, 2 * time.Minute, 4 * time.Minute, 15 * time.Minute, 15 * time.Minute} {
		e.poll(t)
		p, _ := queries.GetMailboxPoll(context.Background(), e.pool, e.acct.ID)
		if p.Failures != i+1 || !p.NextCheckAt.Equal(e.clock.Add(want).Truncate(time.Microsecond)) && p.NextCheckAt.Sub(e.clock.Add(want)).Abs() > time.Millisecond {
			t.Fatalf("failure %d: failures=%d next in %v, want %v", i+1, p.Failures, p.NextCheckAt.Sub(e.clock), want)
		}
		e.clock = p.NextCheckAt.Add(-3 * time.Minute)
	}
	if e.history(t) != "100" {
		t.Fatalf("history moved to %s", e.history(t))
	}
}

func TestMailConcurrentClaimersShareNothing(t *testing.T) {
	pool, _ := database.NewTestDatabase(t)
	ctx := context.Background()
	now := time.Now().UTC()
	user := testUser(t, pool)
	c := &models.Connector{UserID: user, Name: "gmail", Kind: models.ConnectorGmail, URL: "x", Status: models.ConnectorConnected}
	if err := queries.CreateConnector(ctx, pool, c); err != nil {
		t.Fatal(err)
	}
	for i := 0; i < 40; i++ {
		a := &models.GmailAccount{UserID: user, ConnectorID: c.ID, Email: uuid.NewString() + "@x.com", Status: models.ConnectorConnected, Primary: i == 0}
		if err := queries.CreateGmailAccount(ctx, pool, a); err != nil {
			t.Fatal(err)
		}
		if err := queries.EnsureMailboxPoll(ctx, pool, a.ID, user, "1", now.Add(-time.Second)); err != nil {
			t.Fatal(err)
		}
	}
	var mu sync.Mutex
	seen := map[uuid.UUID]int{}
	var wg sync.WaitGroup
	for i := 0; i < 8; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			got, err := queries.ClaimMailboxPolls(ctx, pool, now, 10, time.Minute)
			if err != nil {
				t.Error(err)
			}
			mu.Lock()
			for _, p := range got {
				seen[p.AccountID]++
			}
			mu.Unlock()
		}()
	}
	wg.Wait()
	if len(seen) != 40 {
		t.Fatalf("claimed %d of 40", len(seen))
	}
	for id, n := range seen {
		if n != 1 {
			t.Fatalf("%s claimed %d times", id, n)
		}
	}
	if got, _ := queries.ClaimMailboxPolls(ctx, pool, now.Add(time.Second), 10, time.Minute); len(got) != 0 {
		t.Fatalf("leased rows claimed again: %d", len(got))
	}
	if got, _ := queries.ClaimMailboxPolls(ctx, pool, now.Add(2*time.Minute), 10, time.Minute); len(got) != 10 {
		t.Fatalf("expired leases: %d", len(got))
	}
}

func TestMailBackoffAndJitter(t *testing.T) {
	for i, want := range map[int]time.Duration{1: time.Minute, 2: 2 * time.Minute, 3: 4 * time.Minute, 4: 15 * time.Minute, 9: 15 * time.Minute} {
		if got := mailBackoff(i); got != want {
			t.Errorf("backoff(%d) = %v, want %v", i, got, want)
		}
	}
	u := uuid.New()
	if jitter(u) != jitter(u) || jitter(u) >= time.Minute {
		t.Errorf("jitter %v", jitter(u))
	}
}
