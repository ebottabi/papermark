/*
 * Copyright (c) 2026. Papermark - All Rights Reserved
 * Unauthorized copying of this file, via any medium is strictly prohibited
 * Proprietary and confidential
 * Written by Ebot Tabi <ebot.tabi@gmail.com>, 15/10/2026
 */
package services

import (
	"context"
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"log"
	"net/http"
	"net/url"
	"regexp"
	"sort"
	"strings"
	"sync"
	"time"

	"papermark/internal/config"
	"papermark/internal/integrations/mcp"
	"papermark/internal/models"
	"papermark/internal/models/queries"

	"github.com/jackc/pgx/v5/pgxpool"
	"golang.org/x/oauth2"
)

const (
	connectTimeout = 20 * time.Second
	oauthStateTTL  = 10 * time.Minute
)

// ErrInvalidConnector wraps connector validation failures.
var ErrInvalidConnector = errors.New("invalid connector")

// ErrInvalidOAuthState is returned by Callback for an unknown, used or expired state.
var ErrInvalidOAuthState = errors.New("invalid or expired oauth state")

var connectorNamePattern = regexp.MustCompile(`^[a-zA-Z0-9_-]{1,32}$`)

// ConnectorService manages MCP connectors, their OAuth authorization and the
// per-task tool snapshot.
type ConnectorService struct {
	pool        *pgxpool.Pool
	http        *http.Client
	redirectURI string
	catalog     map[string]config.ConnectorConfig
}

// NewConnectorService constructs a ConnectorService whose OAuth redirect URI
// is publicURL + "/v1/oauth/callback". Catalog connectors are listed before
// they are added and are added by their first Authorize.
func NewConnectorService(pool *pgxpool.Pool, publicURL string, catalog map[string]config.ConnectorConfig) *ConnectorService {
	return &ConnectorService{
		pool:        pool,
		http:        &http.Client{Timeout: 30 * time.Second},
		redirectURI: strings.TrimRight(publicURL, "/") + "/v1/oauth/callback",
		catalog:     catalog,
	}
}

// List returns every connector with the status of its latest connection,
// plus each catalog connector not added yet: needs_auth, or error when the
// server has no URL for it.
func (s *ConnectorService) List(ctx context.Context) ([]*models.Connector, error) {
	cs, err := queries.ListConnectors(ctx, s.pool)
	if err != nil {
		return nil, err
	}
	added := map[string]bool{}
	for _, c := range cs {
		added[c.Name] = true
	}
	names := make([]string, 0, len(s.catalog))
	for name := range s.catalog {
		names = append(names, name)
	}
	sort.Strings(names)
	for _, name := range names {
		if added[name] {
			continue
		}
		c := &models.Connector{Name: name, URL: s.catalog[name].URL, Status: models.ConnectorNeedsAuth}
		if c.URL == "" {
			msg := "Not set up on this server"
			c.Status, c.Error = models.ConnectorError, &msg
		}
		cs = append(cs, c)
	}
	return cs, nil
}

// Add stores a connector and tries to connect, so the result carries
// connected, needs_auth or error with its tool count.
func (s *ConnectorService) Add(ctx context.Context, name, rawURL string) (*models.Connector, error) {
	if !connectorNamePattern.MatchString(name) {
		return nil, fmt.Errorf("%w: name must be 1-32 letters, digits, _ or -", ErrInvalidConnector)
	}
	if u, err := url.Parse(rawURL); err != nil || (u.Scheme != "https" && u.Scheme != "http") || u.Host == "" {
		return nil, fmt.Errorf("%w: url must be an http(s) URL", ErrInvalidConnector)
	}
	c := &models.Connector{Name: name, URL: rawURL, Status: models.ConnectorNeedsAuth}
	if err := queries.CreateConnector(ctx, s.pool, c); err != nil {
		return nil, err
	}
	if sess, _, err := s.connect(ctx, c); err == nil {
		sess.Close()
	}
	return c, nil
}

// Delete removes a connector and its OAuth client and tokens.
func (s *ConnectorService) Delete(ctx context.Context, name string) error {
	tx, err := s.pool.Begin(ctx)
	if err != nil {
		return err
	}
	defer tx.Rollback(ctx)
	c, err := queries.GetConnectorByName(ctx, tx, name)
	if err != nil {
		return err
	}
	if err := queries.SoftDeleteConnector(ctx, tx, c.ID); err != nil {
		return err
	}
	if err := queries.DeleteOAuthCredential(ctx, tx, c.ID); err != nil {
		return err
	}
	return tx.Commit(ctx)
}

// Authorize discovers the connector's authorization server, registers a
// client unless the catalog supplies one, and returns the URL the user opens
// to grant access (PKCE S256). A catalog connector is added on first use.
func (s *ConnectorService) Authorize(ctx context.Context, name string) (string, error) {
	entry, inCatalog := s.catalog[name]
	c, err := queries.GetConnectorByName(ctx, s.pool, name)
	if errors.Is(err, queries.ErrNotFound) && inCatalog && entry.URL != "" {
		c = &models.Connector{Name: name, URL: entry.URL, Status: models.ConnectorNeedsAuth}
		err = queries.CreateConnector(ctx, s.pool, c)
	}
	if err != nil {
		return "", err
	}
	as, err := mcp.Discover(ctx, c.URL, s.http)
	if err != nil {
		return "", fmt.Errorf("discover authorization server: %w", err)
	}
	if len(entry.Scopes) > 0 {
		as.Scopes = entry.Scopes
	}
	clientID, secret := entry.ClientID, entry.ClientSecret
	if clientID == "" {
		if clientID, secret, err = mcp.Register(ctx, as, s.redirectURI, s.http); err != nil {
			return "", err
		}
	}
	raw := make([]byte, 32)
	if _, err := rand.Read(raw); err != nil {
		return "", err
	}
	state := base64.RawURLEncoding.EncodeToString(raw)
	expires := time.Now().Add(oauthStateTTL).UTC()
	cred := &models.OAuthCredential{
		ConnectorID: c.ID, Resource: as.Resource, AuthURL: as.AuthURL, TokenURL: as.TokenURL,
		Scopes: strings.Join(as.Scopes, " "), ClientID: clientID, ClientSecret: secret,
		Verifier: oauth2.GenerateVerifier(), StateHash: hashState(state), StateExpiresAt: &expires,
	}
	if err := queries.UpsertOAuthPending(ctx, s.pool, cred); err != nil {
		return "", err
	}
	opts := []oauth2.AuthCodeOption{oauth2.S256ChallengeOption(cred.Verifier), oauth2.SetAuthURLParam("resource", cred.Resource)}
	for k, v := range entry.AuthParams {
		opts = append(opts, oauth2.SetAuthURLParam(k, v))
	}
	return s.oauthConfig(cred).AuthCodeURL(state, opts...), nil
}

// Callback completes an authorization: it consumes state (single use,
// expiring), exchanges code for tokens and reconnects. It returns the
// connector whenever state matched one, even when a later step failed.
func (s *ConnectorService) Callback(ctx context.Context, state, code, oauthError string) (*models.Connector, error) {
	if state == "" {
		return nil, ErrInvalidOAuthState
	}
	cred, err := queries.ConsumeOAuthState(ctx, s.pool, hashState(state), time.Now())
	if errors.Is(err, queries.ErrNotFound) {
		return nil, ErrInvalidOAuthState
	}
	if err != nil {
		return nil, err
	}
	c, err := queries.GetConnector(ctx, s.pool, cred.ConnectorID)
	if err != nil {
		return nil, err
	}
	if oauthError != "" || code == "" {
		return c, fmt.Errorf("authorization denied: %q", oauthError)
	}
	tok, err := s.oauthConfig(cred).Exchange(context.WithValue(ctx, oauth2.HTTPClient, s.http), code,
		oauth2.VerifierOption(cred.Verifier), oauth2.SetAuthURLParam("resource", cred.Resource))
	if err != nil {
		return c, fmt.Errorf("token exchange: %w", err)
	}
	if err := s.saveToken(ctx, c, tok); err != nil {
		return c, err
	}
	sess, _, err := s.connect(ctx, c)
	if err != nil {
		return c, err
	}
	sess.Close()
	return c, nil
}

// Snapshot connects to every connector and returns their tools for one task,
// named <connector>_<tool>. Connectors that fail are skipped and their status
// records why. A name that collides after sanitizing keeps its first tool.
// shortcut: connects one connector at a time, each bounded by connectTimeout; connect concurrently if task start latency matters.
func (s *ConnectorService) Snapshot(ctx context.Context) (*ConnectorToolset, error) {
	cs, err := queries.ListConnectors(ctx, s.pool)
	if err != nil {
		return nil, err
	}
	set := &ConnectorToolset{Handlers: map[string]ConnectorHandler{}, NeedsApproval: map[string]bool{}}
	var sessions []*mcp.Session
	set.Close = func() {
		for _, sess := range sessions {
			sess.Close()
		}
	}
	for _, c := range cs {
		sess, tools, err := s.connect(ctx, c)
		if err != nil {
			continue
		}
		sessions = append(sessions, sess)
		for _, t := range tools {
			name := mcp.ToolName(c.Name, t.Name)
			if _, dup := set.Handlers[name]; dup {
				continue
			}
			set.Tools = append(set.Tools, ConnectorTool{Name: name, Description: "[" + c.Name + "] " + t.Description, InputSchema: t.InputSchema})
			toolName := t.Name
			set.Handlers[name] = func(ctx context.Context, input json.RawMessage) (string, bool, error) {
				return sess.Call(ctx, toolName, input)
			}
			if !t.ReadOnly {
				set.NeedsApproval[name] = true
			}
		}
	}
	sort.Slice(set.Tools, func(i, j int) bool { return set.Tools[i].Name < set.Tools[j].Name })
	return set, nil
}

// connect opens a session to c and lists its tools, recording the outcome
// as c's status. The caller closes the session.
func (s *ConnectorService) connect(ctx context.Context, c *models.Connector) (*mcp.Session, []mcp.Tool, error) {
	sess, tools, err := s.dial(ctx, c)
	c.Status, c.Error, c.Tools = models.ConnectorConnected, nil, len(tools)
	switch {
	case errors.Is(err, mcp.ErrNeedsAuth):
		c.Status = models.ConnectorNeedsAuth
	case err != nil:
		msg := err.Error()
		c.Status, c.Error = models.ConnectorError, &msg
		log.Printf("connector %s: %v", c.Name, err)
	}
	if uerr := queries.UpdateConnectorStatus(ctx, s.pool, c); uerr != nil && err == nil {
		sess.Close()
		return nil, nil, uerr
	}
	return sess, tools, err
}

func (s *ConnectorService) dial(ctx context.Context, c *models.Connector) (*mcp.Session, []mcp.Tool, error) {
	ts, err := s.tokenSource(ctx, c)
	if err != nil {
		return nil, nil, err
	}
	cctx, cancel := context.WithTimeout(ctx, connectTimeout)
	defer cancel()
	sess, err := mcp.Connect(cctx, c.URL, ts, s.http)
	if err != nil {
		return nil, nil, err
	}
	tools, err := sess.Tools(cctx)
	if err != nil {
		sess.Close()
		return nil, nil, err
	}
	return sess, tools, nil
}

// tokenSource returns c's stored tokens, refreshing and re-saving them when
// they expire; nil when c has none.
func (s *ConnectorService) tokenSource(ctx context.Context, c *models.Connector) (oauth2.TokenSource, error) {
	cred, err := queries.GetOAuthCredential(ctx, s.pool, c.ID)
	if errors.Is(err, queries.ErrNotFound) {
		return nil, nil
	}
	if err != nil || cred.AccessToken == "" {
		return nil, err
	}
	tok := &oauth2.Token{AccessToken: cred.AccessToken, RefreshToken: cred.RefreshToken, TokenType: cred.TokenType}
	if cred.Expiry != nil {
		tok.Expiry = *cred.Expiry
	}
	// The refresh context outlives this call: the token source lives as long as the session.
	refreshCtx := context.WithValue(context.Background(), oauth2.HTTPClient, s.http)
	return &savingTokenSource{
		inner: s.oauthConfig(cred).TokenSource(refreshCtx, tok),
		last:  tok.AccessToken,
		save:  func(t *oauth2.Token) error { return s.saveToken(context.Background(), c, t) },
	}, nil
}

func (s *ConnectorService) saveToken(ctx context.Context, c *models.Connector, t *oauth2.Token) error {
	var expiry *time.Time
	if !t.Expiry.IsZero() {
		e := t.Expiry.UTC()
		expiry = &e
	}
	return queries.SaveOAuthToken(ctx, s.pool, c.ID, t.AccessToken, t.RefreshToken, t.TokenType, expiry)
}

func (s *ConnectorService) oauthConfig(cred *models.OAuthCredential) *oauth2.Config {
	style := oauth2.AuthStyleInParams
	if cred.ClientSecret != "" {
		style = oauth2.AuthStyleAutoDetect
	}
	return &oauth2.Config{
		ClientID: cred.ClientID, ClientSecret: cred.ClientSecret, RedirectURL: s.redirectURI,
		Endpoint: oauth2.Endpoint{AuthURL: cred.AuthURL, TokenURL: cred.TokenURL, AuthStyle: style},
		Scopes:   strings.Fields(cred.Scopes),
	}
}

// savingTokenSource persists every token its inner source refreshes.
type savingTokenSource struct {
	mu    sync.Mutex
	inner oauth2.TokenSource
	last  string
	save  func(*oauth2.Token) error
}

func (s *savingTokenSource) Token() (*oauth2.Token, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	t, err := s.inner.Token()
	if err != nil {
		return nil, err
	}
	if t.AccessToken != s.last {
		s.last = t.AccessToken
		if err := s.save(t); err != nil {
			log.Printf("connector: save refreshed token: %v", err)
		}
	}
	return t, nil
}

func hashState(state string) string {
	h := sha256.Sum256([]byte(state))
	return hex.EncodeToString(h[:])
}
