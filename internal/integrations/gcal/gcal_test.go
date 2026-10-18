/*
 * Copyright (c) 2026. Papermark - All Rights Reserved
 * Unauthorized copying of this file, via any medium is strictly prohibited
 * Proprietary and confidential
 * Written by Ebot Tabi <ebot.tabi@gmail.com>, 17/10/2026
 */
package gcal

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strconv"
	"strings"
	"sync"
	"testing"
	"time"

	"golang.org/x/oauth2"
)

// fakeCal mimics the Calendar API v3 endpoints the tools call. The bearer
// token is the account's address; "bad@..." tokens answer 401. Event lists
// serve at most two items per page to exercise pagination.
type fakeCal struct {
	srv       *httptest.Server
	mu        sync.Mutex
	calendars map[string][]CalendarEntry
	events    map[string][]map[string]any // account|calendarId
	busy      map[string][]busy           // account|calendarId
	reqs      []rec
	reply     map[string]any // body returned by insert and patch; nil echoes the request
}

type rec struct {
	Method, Path, Account string
	Query                 url.Values
	Body                  map[string]any
}

func newFakeCal(t *testing.T) *fakeCal {
	f := &fakeCal{calendars: map[string][]CalendarEntry{}, events: map[string][]map[string]any{}, busy: map[string][]busy{}}
	f.srv = httptest.NewServer(http.HandlerFunc(f.serve))
	t.Cleanup(f.srv.Close)
	return f
}

func (f *fakeCal) serve(w http.ResponseWriter, r *http.Request) {
	acct := strings.TrimPrefix(r.Header.Get("Authorization"), "Bearer ")
	if strings.HasPrefix(acct, "bad") {
		http.Error(w, `{"error":{"code":401,"message":"Invalid Credentials"}}`, http.StatusUnauthorized)
		return
	}
	var body map[string]any
	if raw, _ := io.ReadAll(r.Body); len(raw) > 0 {
		json.Unmarshal(raw, &body)
	}
	path := strings.TrimPrefix(r.URL.EscapedPath(), "/calendar/v3/")
	f.mu.Lock()
	f.reqs = append(f.reqs, rec{r.Method, path, acct, r.URL.Query(), body})
	f.mu.Unlock()
	write := func(v any) { json.NewEncoder(w).Encode(v) }
	parts := strings.Split(path, "/")
	switch {
	case path == "users/me/calendarList/primary":
		write(CalendarEntry{ID: acct, Primary: true})
	case path == "users/me/calendarList":
		write(map[string]any{"items": f.calendars[acct]})
	case path == "freeBusy":
		cals := map[string]any{}
		for _, it := range body["items"].([]any) {
			id := it.(map[string]any)["id"].(string)
			if b, ok := f.busy[acct+"|"+id]; ok {
				cals[id] = map[string]any{"busy": b}
			} else {
				cals[id] = map[string]any{"errors": []map[string]string{{"reason": "notFound"}}}
			}
		}
		write(map[string]any{"calendars": cals})
	case len(parts) == 3 && parts[0] == "calendars" && parts[2] == "events" && r.Method == http.MethodGet:
		cal := f.cal(acct, parts[1])
		all, ok := f.events[acct+"|"+cal]
		if !ok {
			http.Error(w, `{"error":{"code":404,"message":"Calendar not found"}}`, http.StatusNotFound)
			return
		}
		off, _ := strconv.Atoi(r.URL.Query().Get("pageToken"))
		n := min(2, len(all)-off)
		page := map[string]any{"items": all[off : off+n]}
		if off+n < len(all) {
			page["nextPageToken"] = strconv.Itoa(off + n)
		}
		write(page)
	case len(parts) == 3 && parts[0] == "calendars" && parts[2] == "events" && r.Method == http.MethodPost:
		if f.reply != nil {
			write(f.reply)
			return
		}
		body["id"] = "new1"
		write(body)
	case len(parts) == 4 && parts[0] == "calendars" && parts[2] == "events":
		cal := f.cal(acct, parts[1])
		id, _ := url.PathUnescape(parts[3])
		var found map[string]any
		for _, e := range f.events[acct+"|"+cal] {
			if e["id"] == id {
				found = e
			}
		}
		if found == nil {
			http.Error(w, `{"error":{"code":404,"message":"Not Found"}}`, http.StatusNotFound)
			return
		}
		switch r.Method {
		case http.MethodGet:
			write(found)
		case http.MethodPatch:
			for k, v := range body {
				found[k] = v
			}
			write(found)
		case http.MethodDelete:
			w.WriteHeader(http.StatusNoContent)
		}
	default:
		http.Error(w, `{"error":{"code":404,"message":"no route `+path+`"}}`, http.StatusNotFound)
	}
}

// cal resolves a path segment to a calendar id, "primary" being the account's own.
func (f *fakeCal) cal(acct, seg string) string {
	if id, _ := url.PathUnescape(seg); id != "primary" {
		return id
	}
	return acct
}

func (f *fakeCal) account(email string, primary bool) Account {
	ts := oauth2.StaticTokenSource(&oauth2.Token{AccessToken: email})
	return Account{Email: email, Primary: primary, Client: &Client{HTTP: &http.Client{Transport: &oauth2.Transport{Source: ts}}, BaseURL: f.srv.URL}}
}

func (f *fakeCal) find(method, prefix string) []rec {
	f.mu.Lock()
	defer f.mu.Unlock()
	var out []rec
	for _, r := range f.reqs {
		if r.Method == method && strings.HasPrefix(r.Path, prefix) {
			out = append(out, r)
		}
	}
	return out
}

func ev(id, start, end string, extra ...any) map[string]any {
	e := map[string]any{"id": id, "summary": "Event " + id, "status": "confirmed", "htmlLink": "https://cal/" + id}
	if len(start) == 10 {
		e["start"], e["end"] = map[string]string{"date": start}, map[string]string{"date": end}
	} else {
		e["start"], e["end"] = map[string]string{"dateTime": start}, map[string]string{"dateTime": end}
	}
	for i := 0; i+1 < len(extra); i += 2 {
		e[extra[i].(string)] = extra[i+1]
	}
	return e
}

func toolMap(accounts []Account, tz string, onAuth func(string)) map[string]Tool {
	m := map[string]Tool{}
	for _, t := range Tools(accounts, tz, onAuth) {
		m[t.Name] = t
	}
	return m
}

func run(t *testing.T, tool Tool, in string) map[string]any {
	t.Helper()
	out, err := tool.Run(context.Background(), json.RawMessage(in))
	if err != nil {
		t.Fatalf("%s: %v", tool.Name, err)
	}
	var m map[string]any
	if err := json.Unmarshal([]byte(out), &m); err != nil {
		t.Fatalf("%s: %v %s", tool.Name, err, out)
	}
	return m
}

func twoAccounts(t *testing.T) (*fakeCal, []Account) {
	f := newFakeCal(t)
	f.calendars["a@x.com"] = []CalendarEntry{{ID: "a@x.com", Summary: "A", Primary: true, AccessRole: "owner", TimeZone: "Europe/Berlin", Selected: true},
		{ID: "team@group", Summary: "Team", SummaryOverride: "My team", AccessRole: "reader", Selected: true},
		{ID: "hidden@group", Summary: "Hidden", AccessRole: "reader"}}
	f.calendars["b@x.com"] = []CalendarEntry{{ID: "b@x.com", Summary: "B", Primary: true, AccessRole: "owner"}}
	long := strings.Repeat("d", 900)
	f.events["a@x.com|a@x.com"] = []map[string]any{
		ev("a1", "2026-10-20T09:00:00+02:00", "2026-10-20T10:00:00+02:00", "hangoutLink", "https://meet.google.com/abc",
			"attendees", []map[string]any{{"email": "a@x.com", "self": true, "responseStatus": "accepted"}, {"email": "z@y.com", "responseStatus": "needsAction"}, {"email": "room@resource", "resource": true}},
			"organizer", map[string]any{"email": "z@y.com"}, "description", long),
		ev("a2", "2026-10-20T12:00:00+02:00", "2026-10-20T13:00:00+02:00", "status", "cancelled"),
		ev("a3", "2026-10-22T08:00:00Z", "2026-10-22T09:00:00Z"),
		ev("a4", "2026-10-25T08:00:00Z", "2026-10-25T09:00:00Z"),
	}
	f.events["a@x.com|team@group"] = []map[string]any{ev("t1", "2026-10-21", "2026-10-22",
		"conferenceData", map[string]any{"entryPoints": []map[string]string{{"entryPointType": "phone", "uri": "tel:1"}, {"entryPointType": "video", "uri": "https://zoom/x"}}})}
	f.events["a@x.com|hidden@group"] = []map[string]any{ev("h1", "2026-10-20T00:00:00Z", "2026-10-20T01:00:00Z")}
	f.events["b@x.com|b@x.com"] = []map[string]any{ev("b1", "2026-10-20T06:00:00Z", "2026-10-20T06:30:00Z")}
	return f, []Account{f.account("a@x.com", true), f.account("b@x.com", false)}
}

func ids(events []any) string {
	var s []string
	for _, e := range events {
		s = append(s, e.(map[string]any)["id"].(string))
	}
	return strings.Join(s, ",")
}

func TestListEventsMergesAccountsAndCalendars(t *testing.T) {
	f, accts := twoAccounts(t)
	tools := toolMap(accts, "Europe/Berlin", nil)
	out := run(t, tools["list_events"], `{"timeMin":"2026-10-20","timeMax":"2026-10-27"}`)
	evs := out["events"].([]any)
	// b1 06:00Z < a1 07:00Z < t1 (21st, all-day) < a3 < a4; a2 cancelled, h1 not selected.
	if got := ids(evs); got != "b1,a1,t1,a3,a4" {
		t.Fatalf("order %s", got)
	}
	if out["count"] != float64(5) || out["more"] != nil || out["timeZone"] != "Europe/Berlin" || out["timeMin"] != "2026-10-20T00:00:00+02:00" {
		t.Fatalf("meta %v", out)
	}
	a1 := evs[1].(map[string]any)
	if a1["account"] != "a@x.com" || a1["calendarId"] != "a@x.com" || a1["meetingLink"] != "https://meet.google.com/abc" ||
		a1["responseStatus"] != "accepted" || a1["organizer"] != "z@y.com" || a1["allDay"] != false || len(a1["attendees"].([]any)) != 2 ||
		len([]rune(a1["description"].(string))) != listDescLen+1 {
		t.Fatalf("a1 %v", a1)
	}
	t1 := evs[2].(map[string]any)
	if t1["allDay"] != true || t1["start"] != "2026-10-21" || t1["meetingLink"] != "https://zoom/x" || t1["calendarId"] != "team@group" {
		t.Fatalf("t1 %v", t1)
	}
	lists := f.find("GET", "calendars/a@x.com/events")
	if len(lists) != 2 || lists[1].Query.Get("pageToken") != "2" {
		t.Fatalf("pagination: %+v", lists)
	}
	q := lists[0].Query
	if q.Get("singleEvents") != "true" || q.Get("orderBy") != "startTime" || q.Get("timeZone") != "Europe/Berlin" ||
		q.Get("timeMin") != "2026-10-20T00:00:00+02:00" || q.Get("timeMax") != "2026-10-27T00:00:00+01:00" || q.Get("maxResults") != "50" {
		t.Fatalf("query %v", q)
	}
	if len(f.find("GET", "calendars/hidden@group")) != 0 {
		t.Fatal("unselected calendar was read")
	}

	// One account only, and a cap.
	out = run(t, tools["list_events"], `{"account":"A@X.com","timeMin":"2026-10-20T00:00:00Z","timeMax":"2026-10-27T00:00:00Z","maxResults":2}`)
	if got := ids(out["events"].([]any)); got != "a1,t1" || out["more"] != true {
		t.Fatalf("capped %s %v", got, out)
	}
	// An explicit calendar on an explicit account.
	out = run(t, tools["list_events"], `{"account":"a@x.com","calendarId":"hidden@group","timeMin":"2026-10-19T00:00:00Z","q":"x"}`)
	if got := ids(out["events"].([]any)); got != "h1" {
		t.Fatalf("calendarId %s", got)
	}
	if r := f.find("GET", "calendars/hidden@group"); r[0].Query.Get("q") != "x" || r[0].Query.Get("timeMax") != "2026-10-26T00:00:00Z" {
		t.Fatalf("query %v", r[0].Query)
	}
}

func TestListEventsDefaultsAndValidation(t *testing.T) {
	f, accts := twoAccounts(t)
	k := toolMap(accts, "", nil)
	out := run(t, k["list_events"], `{}`)
	from, err1 := time.Parse(time.RFC3339, out["timeMin"].(string))
	to, err2 := time.Parse(time.RFC3339, out["timeMax"].(string))
	if err1 != nil || err2 != nil || time.Since(from) > time.Minute || to.Sub(from) != defaultRange || out["timeZone"] != nil {
		t.Fatalf("default window %v", out)
	}
	if q := f.find("GET", "calendars/")[0].Query; q.Has("timeZone") {
		t.Fatalf("timeZone sent without a preference: %v", q)
	}
	for in, want := range map[string]string{
		`{"timeMin":"2026-10-20T09:00"}`:                                      "needs a UTC offset",
		`{"timeMin":"2026-10-20T09:00:00Z","timeMax":"2026-10-19T09:00:00Z"}`: "after timeMin",
		`{"account":"nobody@x.com"}`:                                          "no connected Calendar account",
	} {
		if _, err := k["list_events"].Run(context.Background(), json.RawMessage(in)); err == nil || !strings.Contains(err.Error(), want) {
			t.Errorf("%s: %v", in, err)
		}
	}
}

func TestNeedsAuthAndAPIErrors(t *testing.T) {
	f, accts := twoAccounts(t)
	f.calendars["bad@x.com"] = nil
	accts = append(accts, f.account("bad@x.com", false))
	var authed []string
	tools := toolMap(accts, "UTC", func(e string) { authed = append(authed, e) })

	out := run(t, tools["list_events"], `{"timeMin":"2026-10-20T00:00:00Z","timeMax":"2026-10-27T00:00:00Z"}`)
	errs := out["errors"].([]any)
	if len(errs) != 1 || errs[0].(map[string]any)["account"] != "bad@x.com" || !strings.Contains(errs[0].(map[string]any)["error"].(string), "needs reconnecting") ||
		len(out["events"].([]any)) != 5 {
		t.Fatalf("partial failure %v", out)
	}
	if len(authed) != 1 || authed[0] != "bad@x.com" {
		t.Fatalf("onAuth %v", authed)
	}

	_, err := tools["get_event"].Run(context.Background(), json.RawMessage(`{"account":"bad@x.com","eventId":"x"}`))
	var ae *AccountAuthError
	if !errors.Is(err, ErrNeedsAuth) || !errors.As(err, &ae) || err.Error() != "Calendar account bad@x.com needs reconnecting in Connectors" {
		t.Fatalf("needs auth: %v", err)
	}
	_, err = tools["list_events"].Run(context.Background(), json.RawMessage(`{"account":"bad@x.com"}`))
	if !errors.Is(err, ErrNeedsAuth) {
		t.Fatalf("only account failing: %v", err)
	}

	_, err = tools["get_event"].Run(context.Background(), json.RawMessage(`{"account":"a@x.com","eventId":"missing"}`))
	var api *APIError
	if !errors.As(err, &api) || api.Status != 404 || err.Error() != "calendar: 404 Not Found" {
		t.Fatalf("api error: %v", err)
	}
	if _, err := tools["get_event"].Run(context.Background(), json.RawMessage(`{"eventId":"a1"}`)); err == nil || !strings.Contains(err.Error(), "account is required") {
		t.Fatalf("ambiguous account: %v", err)
	}
	// A refused token refresh is needs_auth too.
	refused := &Client{HTTP: &http.Client{Transport: &oauth2.Transport{Source: oauth2.ReuseTokenSource(nil, failingSource{})}}, BaseURL: f.srv.URL}
	if _, err := refused.PrimaryCalendar(context.Background()); !errors.Is(err, ErrNeedsAuth) {
		t.Fatalf("invalid_grant: %v", err)
	}
}

type failingSource struct{}

func (failingSource) Token() (*oauth2.Token, error) {
	return nil, &oauth2.RetrieveError{Response: &http.Response{StatusCode: http.StatusBadRequest}, ErrorCode: "invalid_grant"}
}

func TestGetEventAndListCalendars(t *testing.T) {
	_, accts := twoAccounts(t)
	tools := toolMap(accts[:1], "UTC", nil)
	e := run(t, tools["get_event"], `{"eventId":"a1"}`)
	if e["id"] != "a1" || e["account"] != "a@x.com" || e["calendarId"] != "primary" || len(e["description"].(string)) != 900 {
		t.Fatalf("get %v", e)
	}
	tools = toolMap(accts, "UTC", nil)
	cals := run(t, tools["list_calendars"], `{}`)["calendars"].([]any)
	if len(cals) != 4 {
		t.Fatalf("calendars %v", cals)
	}
	team := cals[1].(map[string]any)
	if team["summary"] != "My team" || team["account"] != "a@x.com" || team["selected"] != true || team["accessRole"] != "reader" {
		t.Fatalf("team %v", team)
	}
	if cals[3].(map[string]any)["account"] != "b@x.com" {
		t.Fatalf("b %v", cals[3])
	}
}

func TestFreeBusyMergesBusyPeriods(t *testing.T) {
	f, accts := twoAccounts(t)
	f.busy["a@x.com|a@x.com"] = []busy{{"2026-10-20T09:00:00Z", "2026-10-20T10:00:00Z"}, {"2026-10-20T14:00:00Z", "2026-10-20T15:00:00Z"}}
	f.busy["a@x.com|team@group"] = []busy{{"2026-10-20T09:30:00Z", "2026-10-20T11:00:00Z"}}
	f.busy["b@x.com|b@x.com"] = []busy{}
	tools := toolMap(accts, "UTC", nil)
	out := run(t, tools["free_busy"], `{"timeMin":"2026-10-20T00:00:00Z","timeMax":"2026-10-21T00:00:00Z"}`)
	b, _ := json.Marshal(out["busy"])
	if string(b) != `[{"end":"2026-10-20T11:00:00Z","start":"2026-10-20T09:00:00Z"},{"end":"2026-10-20T15:00:00Z","start":"2026-10-20T14:00:00Z"}]` {
		t.Fatalf("union %s", b)
	}
	if len(out["calendars"].([]any)) != 3 || out["errors"] != nil {
		t.Fatalf("calendars %v", out)
	}
	reqs := f.find("POST", "freeBusy")
	if len(reqs) != 2 || reqs[0].Body["timeZone"] != "UTC" || reqs[0].Body["timeMin"] != "2026-10-20T00:00:00Z" {
		t.Fatalf("requests %+v", reqs)
	}
	out = run(t, tools["free_busy"], `{"calendarIds":["a@x.com","gone@group"]}`)
	if errs := out["errors"].([]any); len(errs) != 1 || errs[0].(map[string]any)["calendarId"] != "gone@group" {
		t.Fatalf("calendar error %v", out)
	}
}

func TestWriteToolsNeedApprovalAndResolve(t *testing.T) {
	_, accts := twoAccounts(t)
	tools := toolMap(accts, "Europe/Berlin", nil)
	for name, tool := range tools {
		write := map[string]bool{"create_event": true, "update_event": true, "delete_event": true, "respond": true}[name]
		if tool.Approval != write || (tool.Resolve != nil) != write {
			t.Errorf("%s: approval=%v resolve=%v", name, tool.Approval, tool.Resolve != nil)
		}
	}
	if n := len(Tools([]Account{{}}, "", nil)); n != 8 {
		t.Fatalf("tool count %d", n)
	}
	got, err := tools["create_event"].Resolve(json.RawMessage(`{"summary":"S","start":"2026-10-20T09:00","end":"2026-10-20T10:00"}`))
	if err != nil || string(got) != `{"account":"a@x.com","calendarId":"primary","end":"2026-10-20T10:00","sendUpdates":"all","start":"2026-10-20T09:00","summary":"S","timeZone":"Europe/Berlin"}` {
		t.Fatalf("resolve create: %s %v", got, err)
	}
	got, err = tools["respond"].Resolve(json.RawMessage(`{"eventId":"e","response":"accepted","account":"b@x.com"}`))
	if err != nil || string(got) != `{"account":"b@x.com","calendarId":"primary","eventId":"e","response":"accepted"}` {
		t.Fatalf("resolve respond: %s %v", got, err)
	}
	if _, err := tools["delete_event"].Resolve(json.RawMessage(`{"eventId":"e"}`)); err == nil || !strings.Contains(err.Error(), "account is required") {
		t.Fatalf("resolve delete without account: %v", err)
	}
}

func TestCreateEvent(t *testing.T) {
	f, accts := twoAccounts(t)
	tools := toolMap(accts, "Europe/Berlin", nil)
	e := run(t, tools["create_event"], `{"summary":"Lunch","start":"2026-10-20T12:00","end":"2026-10-20T13:00","attendees":["z@y.com"," "],"addMeet":true,"location":"Cafe"}`)
	if e["id"] != "new1" || e["account"] != "a@x.com" || e["summary"] != "Lunch" {
		t.Fatalf("created %v", e)
	}
	r := f.find("POST", "calendars/primary/events")[0]
	if r.Account != "a@x.com" || r.Query.Get("sendUpdates") != "all" || r.Query.Get("conferenceDataVersion") != "1" {
		t.Fatalf("request %+v", r)
	}
	start := r.Body["start"].(map[string]any)
	att := r.Body["attendees"].([]any)
	cr := r.Body["conferenceData"].(map[string]any)["createRequest"].(map[string]any)
	if start["dateTime"] != "2026-10-20T12:00:00" || start["timeZone"] != "Europe/Berlin" || len(att) != 1 || r.Body["location"] != "Cafe" ||
		cr["requestId"] == "" || cr["conferenceSolutionKey"].(map[string]any)["type"] != "hangoutsMeet" {
		t.Fatalf("body %v", r.Body)
	}

	run(t, tools["create_event"], `{"summary":"Trip","start":"2026-10-24","account":"b@x.com","calendarId":"b@x.com","sendUpdates":"none"}`)
	r = f.find("POST", "calendars/b@x.com/events")[0]
	if r.Account != "b@x.com" || r.Query.Get("sendUpdates") != "none" || r.Query.Has("conferenceDataVersion") ||
		r.Body["start"].(map[string]any)["date"] != "2026-10-24" || r.Body["end"].(map[string]any)["date"] != "2026-10-25" {
		t.Fatalf("all-day %+v", r)
	}

	noTZ := toolMap(accts, "", nil)
	for in, want := range map[string]string{
		`{"summary":"S","start":"2026-10-20T09:00","end":"2026-10-20T10:00"}`:                     "no UTC offset",
		`{"summary":"S","start":"2026-10-20T09:00:00Z"}`:                                          "end is required",
		`{"summary":"","start":"2026-10-20"}`:                                                     "summary and start are required",
		`{"summary":"S","start":"tomorrow"}`:                                                      "invalid time",
		`{"summary":"S","start":"2026-10-20","sendUpdates":"everyone"}`:                           "sendUpdates must be",
		`{"summary":"S","start":"2026-10-20T09:00","end":"2026-10-20T10:00","timeZone":"Mars/X"}`: "unknown timeZone",
	} {
		if _, err := noTZ["create_event"].Run(context.Background(), json.RawMessage(in)); err == nil || !strings.Contains(err.Error(), want) {
			t.Errorf("%s: %v", in, err)
		}
	}
	run(t, noTZ["create_event"], `{"summary":"S","start":"2026-10-20T09:00:00+02:00","end":"2026-10-20T10:00:00+02:00"}`)
}

func TestUpdateDeleteRespond(t *testing.T) {
	f, accts := twoAccounts(t)
	tools := toolMap(accts, "UTC", nil)
	e := run(t, tools["update_event"], `{"account":"a@x.com","eventId":"a3","summary":"Renamed"}`)
	if e["summary"] != "Renamed" {
		t.Fatalf("updated %v", e)
	}
	r := f.find("PATCH", "calendars/primary/events/a3")[0]
	if len(r.Body) != 1 || r.Body["summary"] != "Renamed" || r.Query.Get("sendUpdates") != "all" {
		t.Fatalf("patch %+v", r)
	}
	if _, err := tools["update_event"].Run(context.Background(), json.RawMessage(`{"account":"a@x.com","eventId":"a3"}`)); err == nil || !strings.Contains(err.Error(), "nothing to change") {
		t.Fatalf("empty patch: %v", err)
	}

	d := run(t, tools["delete_event"], `{"account":"a@x.com","eventId":"a4","sendUpdates":"none"}`)
	if d["deleted"] != true || d["id"] != "a4" {
		t.Fatalf("delete %v", d)
	}
	if r := f.find("DELETE", "calendars/primary/events/a4"); len(r) != 1 || r[0].Query.Get("sendUpdates") != "none" {
		t.Fatalf("delete request %+v", r)
	}

	out := run(t, tools["respond"], `{"account":"a@x.com","eventId":"a1","response":"declined"}`)
	if out["responseStatus"] != "declined" {
		t.Fatalf("respond %v", out)
	}
	r = f.find("PATCH", "calendars/primary/events/a1")[0]
	att := r.Body["attendees"].([]any)
	if r.Query.Get("sendUpdates") != "all" || len(att) != 3 || att[0].(map[string]any)["responseStatus"] != "declined" || att[1].(map[string]any)["responseStatus"] != "needsAction" {
		t.Fatalf("respond patch %+v", r)
	}
	if _, err := tools["respond"].Run(context.Background(), json.RawMessage(`{"account":"a@x.com","eventId":"a3","response":"accepted"}`)); err == nil || !strings.Contains(err.Error(), "not a guest") {
		t.Fatalf("respond without invitation: %v", err)
	}
	if _, err := tools["respond"].Run(context.Background(), json.RawMessage(`{"account":"a@x.com","eventId":"a1","response":"maybe"}`)); err == nil {
		t.Fatal("bad response accepted")
	}
}

func TestUnion(t *testing.T) {
	got := union([]busy{{"2026-01-01T03:00:00Z", "2026-01-01T04:00:00Z"}, {"2026-01-01T01:00:00Z", "2026-01-01T02:00:00Z"}, {"2026-01-01T02:00:00Z", "2026-01-01T02:30:00Z"}, {"bad", "x"}})
	if fmt.Sprint(got) != "[{2026-01-01T01:00:00Z 2026-01-01T02:30:00Z} {2026-01-01T03:00:00Z 2026-01-01T04:00:00Z}]" {
		t.Fatalf("%v", got)
	}
}
