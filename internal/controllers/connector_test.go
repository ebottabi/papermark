/*
 * Copyright (c) 2026. Papermark - All Rights Reserved
 * Unauthorized copying of this file, via any medium is strictly prohibited
 * Proprietary and confidential
 * Written by Ebot Tabi <ebot.tabi@gmail.com>, 15/10/2026
 */
package controllers_test

import (
	"context"
	"crypto/sha256"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"sync"
	"testing"

	"papermark/internal/config"
	"papermark/internal/controllers"
	"papermark/internal/database"
	"papermark/internal/models"
	"papermark/internal/models/queries"
	"papermark/internal/router"
	"papermark/internal/services"

	"github.com/jackc/pgx/v5/pgxpool"
	sdkmcp "github.com/modelcontextprotocol/go-sdk/mcp"
)

const publicURL = "https://keen.example"

// provider is an MCP server guarded by its own OAuth authorization server
// (metadata discovery, dynamic registration, PKCE code grant, refresh).
type provider struct {
	t         *testing.T
	srv       *httptest.Server
	mu        sync.Mutex
	access    map[string]bool
	refresh   map[string]bool
	codes     map[string]string // code -> PKCE challenge
	redirects []string
	issued    int
	refreshes int
}

func newProvider(t *testing.T) *provider {
	p := &provider{t: t, access: map[string]bool{}, refresh: map[string]bool{}, codes: map[string]string{}}
	server := sdkmcp.NewServer(&sdkmcp.Implementation{Name: "notes", Version: "1"}, nil)
	type query struct {
		Q string `json:"q"`
	}
	sdkmcp.AddTool(server, &sdkmcp.Tool{Name: "search", Description: "Search pages", Annotations: &sdkmcp.ToolAnnotations{ReadOnlyHint: true}},
		func(_ context.Context, _ *sdkmcp.CallToolRequest, in query) (*sdkmcp.CallToolResult, any, error) {
			return &sdkmcp.CallToolResult{Content: []sdkmcp.Content{&sdkmcp.TextContent{Text: "found: " + in.Q}}}, nil, nil
		})
	server.AddTool(&sdkmcp.Tool{Name: "create.page", Description: "Create a page", InputSchema: map[string]any{"type": "object"}},
		func(context.Context, *sdkmcp.CallToolRequest) (*sdkmcp.CallToolResult, error) {
			return &sdkmcp.CallToolResult{Content: []sdkmcp.Content{
				&sdkmcp.TextContent{Text: "created"}, &sdkmcp.ImageContent{Data: []byte("x"), MIMEType: "image/png"},
			}}, nil
		})
	server.AddTool(&sdkmcp.Tool{Name: "boom", InputSchema: map[string]any{"type": "object"}},
		func(context.Context, *sdkmcp.CallToolRequest) (*sdkmcp.CallToolResult, error) {
			return &sdkmcp.CallToolResult{IsError: true, Content: []sdkmcp.Content{&sdkmcp.TextContent{Text: "nope"}}}, nil
		})
	mcpHandler := sdkmcp.NewStreamableHTTPHandler(func(*http.Request) *sdkmcp.Server { return server }, nil)

	mux := http.NewServeMux()
	mux.HandleFunc("/mcp", func(w http.ResponseWriter, r *http.Request) {
		p.mu.Lock()
		ok := p.access[strings.TrimPrefix(r.Header.Get("Authorization"), "Bearer ")]
		p.mu.Unlock()
		if !ok {
			w.Header().Set("WWW-Authenticate", `Bearer resource_metadata="`+p.srv.URL+`/.well-known/oauth-protected-resource/mcp", scope="notes.read"`)
			http.Error(w, "unauthorized", http.StatusUnauthorized)
			return
		}
		mcpHandler.ServeHTTP(w, r)
	})
	mux.HandleFunc("/.well-known/oauth-protected-resource/mcp", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(map[string]any{"resource": p.srv.URL + "/mcp", "authorization_servers": []string{p.srv.URL + "/as"}})
	})
	mux.HandleFunc("/.well-known/oauth-authorization-server/as", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(map[string]any{
			"issuer": p.srv.URL + "/as", "authorization_endpoint": p.srv.URL + "/as/authorize",
			"token_endpoint": p.srv.URL + "/as/token", "registration_endpoint": p.srv.URL + "/as/register",
			"response_types_supported": []string{"code"}, "code_challenge_methods_supported": []string{"S256"},
		})
	})
	mux.HandleFunc("/as/register", func(w http.ResponseWriter, r *http.Request) {
		var body struct {
			RedirectURIs []string `json:"redirect_uris"`
		}
		json.NewDecoder(r.Body).Decode(&body)
		p.mu.Lock()
		p.redirects = append(p.redirects, body.RedirectURIs...)
		p.mu.Unlock()
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusCreated)
		json.NewEncoder(w).Encode(map[string]any{"client_id": "client-1", "redirect_uris": body.RedirectURIs})
	})
	mux.HandleFunc("/as/token", p.token)
	p.srv = httptest.NewServer(mux)
	t.Cleanup(p.srv.Close)
	return p
}

func (p *provider) token(w http.ResponseWriter, r *http.Request) {
	r.ParseForm()
	p.mu.Lock()
	defer p.mu.Unlock()
	fail := func(msg string) {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusBadRequest)
		fmt.Fprintf(w, `{"error":"invalid_grant","error_description":%q}`, msg)
	}
	if r.Form.Get("client_id") != "client-1" {
		fail("client")
		return
	}
	switch r.Form.Get("grant_type") {
	case "authorization_code":
		challenge, ok := p.codes[r.Form.Get("code")]
		delete(p.codes, r.Form.Get("code"))
		sum := sha256.Sum256([]byte(r.Form.Get("code_verifier")))
		switch {
		case !ok:
			fail("code")
			return
		case base64.RawURLEncoding.EncodeToString(sum[:]) != challenge:
			fail("pkce")
			return
		case r.Form.Get("redirect_uri") != publicURL+"/v1/oauth/callback" || r.Form.Get("resource") != p.srv.URL+"/mcp":
			fail("redirect or resource")
			return
		}
	case "refresh_token":
		if !p.refresh[r.Form.Get("refresh_token")] {
			fail("refresh")
			return
		}
		delete(p.refresh, r.Form.Get("refresh_token"))
		p.refreshes++
	default:
		fail("grant")
		return
	}
	p.issued++
	at, rt := fmt.Sprintf("at-%d", p.issued), fmt.Sprintf("rt-%d", p.issued)
	p.access[at], p.refresh[rt] = true, true
	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(map[string]any{"access_token": at, "refresh_token": rt, "token_type": "Bearer", "expires_in": 3600})
}

// approve plays the user granting access at authURL and returns the callback query.
func (p *provider) approve(t *testing.T, authURL string) url.Values {
	u, err := url.Parse(authURL)
	if err != nil {
		t.Fatal(err)
	}
	q := u.Query()
	if u.Path != "/as/authorize" || q.Get("client_id") != "client-1" || q.Get("response_type") != "code" ||
		q.Get("code_challenge_method") != "S256" || q.Get("code_challenge") == "" || q.Get("state") == "" ||
		q.Get("redirect_uri") != publicURL+"/v1/oauth/callback" || q.Get("resource") != p.srv.URL+"/mcp" || q.Get("scope") != "notes.read" {
		t.Fatalf("authorization URL: %s", authURL)
	}
	p.mu.Lock()
	defer p.mu.Unlock()
	code := fmt.Sprintf("code-%d", len(p.codes)+p.issued)
	p.codes[code] = q.Get("code_challenge")
	return url.Values{"code": {code}, "state": {q.Get("state")}}
}

// revoke invalidates every token, as when the user disconnects the app.
func (p *provider) revoke() {
	p.mu.Lock()
	defer p.mu.Unlock()
	p.access, p.refresh = map[string]bool{}, map[string]bool{}
}

// callback hits the public callback without a bearer token and returns the app redirect.
func callback(t *testing.T, api *testAPI, q url.Values) url.Values {
	t.Helper()
	req := httptest.NewRequest("GET", "/v1/oauth/callback?"+q.Encode(), nil)
	res, err := api.app.Test(req, -1)
	if err != nil {
		t.Fatal(err)
	}
	loc, _ := url.Parse(res.Header.Get("Location"))
	if res.StatusCode != http.StatusFound || loc == nil || loc.Scheme != "keen" || loc.Host != "oauth" {
		t.Fatalf("callback: %d %q", res.StatusCode, res.Header.Get("Location"))
	}
	return loc.Query()
}

func expireToken(t *testing.T, pool *pgxpool.Pool) {
	if _, err := pool.Exec(context.Background(), "UPDATE oauth_credentials SET expiry = now() - interval '1 minute'"); err != nil {
		t.Fatal(err)
	}
}

func newConnectorAPI(t *testing.T, catalog ...map[string]config.ConnectorConfig) (*testAPI, *services.ConnectorService, *pgxpool.Pool) {
	pool, _ := database.NewTestDatabase(t)
	svc := services.NewConnectorService(pool, publicURL, append(catalog, nil)[0], nil)
	return newAPI(t, &router.Config{Pool: pool, Connectors: controllers.NewConnectorController(svc)}), svc, pool
}

func TestConnectorOAuthLifecycle(t *testing.T) {
	p := newProvider(t)
	api, svc, pool := newConnectorAPI(t)
	ctx := context.Background()

	code, body := api.do("POST", "/v1/connectors", `{"name":"notes","url":"`+p.srv.URL+`/mcp"}`)
	if c := body.data(); code != 201 || c["status"] != "needs_auth" || c["tools"] != float64(0) || c["error"] != nil {
		t.Fatalf("add: %d %v", code, body)
	}
	for bad, want := range map[string]int{
		`{"name":"notes","url":"` + p.srv.URL + `/mcp"}`:     409,
		`{"name":"has space","url":"https://x.example/mcp"}`: 400,
		`{"name":"ok","url":"ftp://x.example"}`:              400,
	} {
		if code, body := api.do("POST", "/v1/connectors", bad); code != want {
			t.Errorf("%s: %d %v", bad, code, body)
		}
	}

	_, body = api.do("POST", "/v1/connectors/notes/authorize", "")
	authURL, _ := body.data()["authorizationUrl"].(string)
	q := p.approve(t, authURL)
	if len(p.redirects) != 1 || p.redirects[0] != publicURL+"/v1/oauth/callback" {
		t.Fatalf("registered redirects: %v", p.redirects)
	}
	if got := callback(t, api, q); got.Get("connector") != "notes" || got.Get("status") != "connected" {
		t.Fatalf("callback: %v", got)
	}
	if got := callback(t, api, q); got.Get("status") != "error" || got.Has("connector") {
		t.Fatalf("replayed state accepted: %v", got)
	}

	_, body = api.do("GET", "/v1/connectors", "")
	list := body.data()["connectors"].([]any)
	if c := list[0].(map[string]any); len(list) != 1 || c["status"] != "connected" || c["tools"] != float64(3) || c["error"] != nil {
		t.Fatalf("list: %v", body)
	}

	cs, _ := queries.ListConnectors(ctx, pool, api.user)
	cred, err := queries.GetOAuthCredential(ctx, pool, cs[0].ID)
	if err != nil || cred.AccessToken != "at-1" || cred.RefreshToken != "rt-1" || cred.Verifier != "" || cred.StateHash != "" {
		t.Fatalf("stored credential: %+v %v", cred, err)
	}
	cred.ClientSecret = "client-secret"
	leaked, _ := json.Marshal(cred)
	for _, secret := range []string{"at-1", "rt-1", "client-secret", "state", "verifier"} {
		if strings.Contains(string(leaked), secret) {
			t.Errorf("credential JSON exposes %s: %s", secret, leaked)
		}
	}

	set, err := svc.Snapshot(ctx, api.user)
	if err != nil {
		t.Fatal(err)
	}
	var names []string
	for _, tool := range set.Tools {
		names = append(names, tool.Name)
	}
	if strings.Join(names, ",") != "notes_boom,notes_create_page,notes_search" {
		t.Fatalf("tools: %v", names)
	}
	if set.Tools[2].Description != "[notes] Search pages" || !strings.Contains(string(set.Tools[2].InputSchema), `"q"`) {
		t.Fatalf("search tool: %+v", set.Tools[2])
	}
	if set.NeedsApproval["notes_search"] || !set.NeedsApproval["notes_create_page"] || !set.NeedsApproval["notes_boom"] {
		t.Fatalf("approval: %v", set.NeedsApproval)
	}
	if text, isErr, err := set.Handlers["notes_search"](ctx, json.RawMessage(`{"q":"go"}`)); text != "found: go" || isErr || err != nil {
		t.Fatalf("search: %q %v %v", text, isErr, err)
	}
	if text, isErr, err := set.Handlers["notes_create_page"](ctx, nil); !strings.HasPrefix(text, "created\n{") || !strings.Contains(text, `"type":"image"`) || isErr || err != nil {
		t.Fatalf("create page: %q %v %v", text, isErr, err)
	}
	if text, isErr, err := set.Handlers["notes_boom"](ctx, json.RawMessage(`{}`)); text != "nope" || !isErr || err != nil {
		t.Fatalf("boom: %q %v %v", text, isErr, err)
	}
	set.Close()

	expireToken(t, pool)
	set, err = svc.Snapshot(ctx, api.user)
	if err != nil || len(set.Tools) != 3 || p.refreshes != 1 {
		t.Fatalf("after expiry: %v tools=%d refreshes=%d", err, len(set.Tools), p.refreshes)
	}
	set.Close()
	if cred, _ := queries.GetOAuthCredential(ctx, pool, cs[0].ID); cred.AccessToken != "at-2" || cred.RefreshToken != "rt-2" {
		t.Fatalf("refreshed token not persisted: %q %q", cred.AccessToken, cred.RefreshToken)
	}

	p.revoke()
	expireToken(t, pool)
	set, _ = svc.Snapshot(ctx, api.user)
	set.Close()
	if c, _ := queries.GetConnectorByName(ctx, pool, api.user, "notes"); len(set.Tools) != 0 || c.Status != models.ConnectorNeedsAuth {
		t.Fatalf("after revoke: tools=%d status=%s", len(set.Tools), c.Status)
	}

	if code, _ := api.do("DELETE", "/v1/connectors/notes", ""); code != 200 {
		t.Fatalf("delete: %d", code)
	}
	if _, err := queries.GetOAuthCredential(ctx, pool, cs[0].ID); err != queries.ErrNotFound {
		t.Fatalf("credential survives delete: %v", err)
	}
	if code, _ := api.do("DELETE", "/v1/connectors/notes", ""); code != 404 {
		t.Fatalf("second delete: %d", code)
	}
}

func TestCatalogConnectorIsListedAndAddedBySignIn(t *testing.T) {
	p := newProvider(t)
	api, _, _ := newConnectorAPI(t, map[string]config.ConnectorConfig{
		"notes": {URL: p.srv.URL + "/mcp", ClientID: "client-1", Scopes: []string{"notes.read"}, AuthParams: map[string]string{"access_type": "offline"}},
		"mail":  {},
	})

	_, body := api.do("GET", "/v1/connectors", "")
	list := body.data()["connectors"].([]any)
	mail, notes := list[0].(map[string]any), list[1].(map[string]any)
	if len(list) != 2 || mail["name"] != "mail" || mail["status"] != "error" || notes["name"] != "notes" || notes["status"] != "needs_auth" {
		t.Fatalf("catalog before sign-in: %v", body)
	}
	if code, _ := api.do("POST", "/v1/connectors/mail/authorize", ""); code != 404 {
		t.Fatalf("authorize without a URL: %d", code)
	}

	_, body = api.do("POST", "/v1/connectors/notes/authorize", "")
	authURL := body.data()["authorizationUrl"].(string)
	if u, _ := url.Parse(authURL); u.Query().Get("scope") != "notes.read" || u.Query().Get("access_type") != "offline" {
		t.Fatalf("authorization URL ignores the catalog's scopes and params: %s", authURL)
	}
	q := p.approve(t, authURL)
	if len(p.redirects) != 0 {
		t.Fatalf("registered a client despite the catalog's: %v", p.redirects)
	}
	if got := callback(t, api, q); got.Get("connector") != "notes" || got.Get("status") != "connected" {
		t.Fatalf("callback: %v", got)
	}
	_, body = api.do("GET", "/v1/connectors", "")
	list = body.data()["connectors"].([]any)
	if c := list[0].(map[string]any); len(list) != 2 || c["name"] != "notes" || c["status"] != "connected" {
		t.Fatalf("catalog after sign-in: %v", body)
	}
}

func TestOAuthCallbackRejectsExpiredAndDeniedStates(t *testing.T) {
	p := newProvider(t)
	api, _, pool := newConnectorAPI(t)
	api.do("POST", "/v1/connectors", `{"name":"notes","url":"`+p.srv.URL+`/mcp"}`)

	_, body := api.do("POST", "/v1/connectors/notes/authorize", "")
	q := p.approve(t, body.data()["authorizationUrl"].(string))
	if _, err := pool.Exec(context.Background(), "UPDATE oauth_credentials SET state_expires_at = now() - interval '1 second'"); err != nil {
		t.Fatal(err)
	}
	if got := callback(t, api, q); got.Get("status") != "error" || got.Has("connector") {
		t.Fatalf("expired state: %v", got)
	}

	_, body = api.do("POST", "/v1/connectors/notes/authorize", "")
	q = p.approve(t, body.data()["authorizationUrl"].(string))
	q.Del("code")
	q.Set("error", "access_denied")
	if got := callback(t, api, q); got.Get("status") != "error" || got.Get("connector") != "notes" || got.Get("reason") != "Google access wasn't allowed, so nothing was connected." {
		t.Fatalf("denied: %v", got)
	}
	if got := callback(t, api, url.Values{"code": {"x"}, "state": {"forged"}}); got.Get("status") != "error" || got.Get("reason") != "That sign-in expired. Start it again from Keen." {
		t.Fatalf("forged state: %v", got)
	}
}

func TestConnectorWithoutAuthAndUnreachable(t *testing.T) {
	server := sdkmcp.NewServer(&sdkmcp.Implementation{Name: "open", Version: "1"}, nil)
	server.AddTool(&sdkmcp.Tool{Name: "ping", InputSchema: map[string]any{"type": "object"}},
		func(context.Context, *sdkmcp.CallToolRequest) (*sdkmcp.CallToolResult, error) {
			return &sdkmcp.CallToolResult{Content: []sdkmcp.Content{&sdkmcp.TextContent{Text: "pong"}}}, nil
		})
	open := httptest.NewServer(sdkmcp.NewStreamableHTTPHandler(func(*http.Request) *sdkmcp.Server { return server }, nil))
	defer open.Close()
	dead := httptest.NewServer(http.NotFoundHandler())
	dead.Close()

	api, svc, pool := newConnectorAPI(t)
	if code, body := api.do("POST", "/v1/connectors", `{"name":"open","url":"`+open.URL+`"}`); code != 201 || body.data()["status"] != "connected" || body.data()["tools"] != float64(1) {
		t.Fatalf("open: %d %v", code, body)
	}
	code, body := api.do("POST", "/v1/connectors", `{"name":"dead","url":"`+dead.URL+`"}`)
	if c := body.data(); code != 201 || c["status"] != "error" || c["error"] == nil || c["tools"] != float64(0) {
		t.Fatalf("dead: %d %v", code, body)
	}

	// Another user sees none of them, cannot touch them, may reuse a name,
	// and a task of theirs gets only their own tools.
	other := api.as(newUser(t, pool))
	if _, body := other.do("GET", "/v1/connectors", ""); len(body.data()["connectors"].([]any)) != 0 {
		t.Fatalf("other user lists: %v", body)
	}
	if code, _ := other.do("DELETE", "/v1/connectors/open", ""); code != 404 {
		t.Fatalf("other user deletes: %d", code)
	}
	if code, _ := other.do("POST", "/v1/connectors/open/authorize", ""); code != 404 {
		t.Fatalf("other user authorizes: %d", code)
	}
	if set, err := svc.Snapshot(context.Background(), other.user); err != nil || len(set.Tools) != 0 {
		t.Fatalf("other user's snapshot: %v %v", set, err)
	}
	if code, _ := other.do("POST", "/v1/connectors", `{"name":"dead","url":"`+dead.URL+`"}`); code != 201 {
		t.Fatalf("other user reuses a name: %d", code)
	}
	set, err := svc.Snapshot(context.Background(), api.user)
	if err != nil || len(set.Tools) != 1 {
		t.Fatalf("owner's snapshot: %v %v", set, err)
	}
	if set.Close != nil {
		set.Close()
	}
}
