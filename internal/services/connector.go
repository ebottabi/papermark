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
	"papermark/internal/harness"
	"papermark/internal/integrations/gcal"
	"papermark/internal/integrations/gdrive"
	"papermark/internal/integrations/gmail"
	"papermark/internal/integrations/mcp"
	"papermark/internal/models"
	"papermark/internal/models/queries"
	"papermark/internal/secretbox"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
	"golang.org/x/oauth2"
)

const gmailName = "gmail"

const (
	connectTimeout = 20 * time.Second
	oauthStateTTL  = 10 * time.Minute
)

// ErrInvalidConnector wraps connector validation failures.
var ErrInvalidConnector = errors.New("invalid connector")

// ErrDifferentAccount is returned by Callback when a reconnect was signed in
// with another address than the account being reconnected.
var ErrDifferentAccount = errors.New("signed in as a different account")

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
	box         *secretbox.Box // seals stored tokens; nil keeps them in plaintext
}

// NewConnectorService constructs a ConnectorService whose OAuth redirect URI
// is publicURL + "/v1/oauth/callback". Catalog connectors are listed before
// they are added and are added by their first Authorize. box seals the OAuth
// tokens at rest; nil stores them in plaintext.
func NewConnectorService(pool *pgxpool.Pool, publicURL string, catalog map[string]config.ConnectorConfig, box *secretbox.Box) *ConnectorService {
	return &ConnectorService{
		pool:        pool,
		http:        &http.Client{Timeout: 30 * time.Second},
		redirectURI: strings.TrimRight(publicURL, "/") + "/v1/oauth/callback",
		catalog:     catalog,
		box:         box,
	}
}

func (s *ConnectorService) kind(name string) string {
	if k := s.catalog[name].Kind; k != "" {
		return k
	}
	return models.ConnectorMCP
}

// List returns every connector with the status of its latest connection,
// plus each catalog connector not added yet: needs_auth, or error when the
// server has no URL for it.
func (s *ConnectorService) List(ctx context.Context, user uuid.UUID) ([]*models.Connector, error) {
	cs, err := queries.ListConnectors(ctx, s.pool, user)
	if err != nil {
		return nil, err
	}
	added := map[string]bool{}
	for _, c := range cs {
		added[c.Name] = true
		if models.HasAccounts(c.Kind) {
			accounts, err := queries.ListGmailAccounts(ctx, s.pool, c.ID)
			if err != nil {
				return nil, err
			}
			applyAccounts(c, accounts)
		}
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
		c := &models.Connector{Name: name, Kind: s.kind(name), URL: s.catalog[name].URL, Status: models.ConnectorNeedsAuth}
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
func (s *ConnectorService) Add(ctx context.Context, user uuid.UUID, name, rawURL string) (*models.Connector, error) {
	if !connectorNamePattern.MatchString(name) {
		return nil, fmt.Errorf("%w: name must be 1-32 letters, digits, _ or -", ErrInvalidConnector)
	}
	if u, err := url.Parse(rawURL); err != nil || (u.Scheme != "https" && u.Scheme != "http") || u.Host == "" {
		return nil, fmt.Errorf("%w: url must be an http(s) URL", ErrInvalidConnector)
	}
	c := &models.Connector{UserID: user, Name: name, URL: rawURL, Status: models.ConnectorNeedsAuth}
	if err := queries.CreateConnector(ctx, s.pool, c); err != nil {
		return nil, err
	}
	if sess, _, err := s.connect(ctx, c); err == nil {
		sess.Close()
	}
	return c, nil
}

// Delete removes a connector and its OAuth client and tokens. A Google
// connector's accounts are revoked at Google first, on a best-effort basis,
// and a contacts account's stored contacts are deleted.
func (s *ConnectorService) Delete(ctx context.Context, user uuid.UUID, name string) error {
	tx, err := s.pool.Begin(ctx)
	if err != nil {
		return err
	}
	defer tx.Rollback(ctx)
	c, err := queries.GetConnectorByName(ctx, tx, user, name)
	if err != nil {
		return err
	}
	if models.HasAccounts(c.Kind) {
		accounts, err := queries.ListGmailAccounts(ctx, tx, c.ID)
		if err != nil {
			return err
		}
		for _, a := range accounts {
			s.revokeAccount(ctx, c, a)
			if err := queries.DeleteAccountContacts(ctx, tx, a.ID); err != nil {
				return err
			}
		}
		if err := queries.DeleteConnectorGmailAccounts(ctx, tx, c.ID); err != nil {
			return err
		}
	}
	if err := queries.SoftDeleteConnector(ctx, tx, c.ID); err != nil {
		return err
	}
	if err := queries.DeleteOAuthCredential(ctx, tx, c.ID); err != nil {
		return err
	}
	if c.Kind == models.ConnectorGmail {
		if err := syncMailboxPolls(ctx, tx, user); err != nil {
			return err
		}
	}
	return tx.Commit(ctx)
}

// revokeAccount asks Google to invalidate an account's token. Failures are
// logged and ignored: the account is removed either way. Google revokes the
// whole grant of the OAuth client, so an address still signed in to the
// other Google connector keeps its grant and only loses this token locally.
func (s *ConnectorService) revokeAccount(ctx context.Context, c *models.Connector, a *models.GmailAccount) {
	if shared, err := queries.AccountEmailElsewhere(ctx, s.pool, a.UserID, c.ID, a.Email); err != nil || shared {
		return
	}
	cred, err := queries.GetAccountOAuthCredential(ctx, s.pool, a.ID)
	if err != nil {
		return
	}
	if err := s.unseal(cred); err != nil {
		log.Printf("connector %s: revoke: %v", c.Name, err)
		return
	}
	tok := cred.RefreshToken
	if tok == "" {
		tok = cred.AccessToken
	}
	endpoint := s.catalog[c.Name].RevokeURL
	if tok == "" || endpoint == "" {
		return
	}
	rctx, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()
	req, err := http.NewRequestWithContext(rctx, http.MethodPost, endpoint, strings.NewReader(url.Values{"token": {tok}}.Encode()))
	if err != nil {
		return
	}
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	resp, err := s.http.Do(req)
	if err != nil {
		log.Printf("connector %s: revoke: %v", c.Name, err)
		return
	}
	resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		log.Printf("connector %s: revoke: status %d", c.Name, resp.StatusCode)
	}
}

// account returns one of user's accounts and its connector, ErrNotFound
// unless the connector is of kind.
func (s *ConnectorService) account(ctx context.Context, user uuid.UUID, kind string, id uuid.UUID) (*models.GmailAccount, *models.Connector, error) {
	a, err := queries.GetGmailAccount(ctx, s.pool, user, id)
	if err != nil {
		return nil, nil, err
	}
	c, err := queries.GetConnector(ctx, s.pool, a.ConnectorID)
	if err != nil {
		return nil, nil, err
	}
	if c.Kind != kind {
		return nil, nil, queries.ErrNotFound
	}
	return a, c, nil
}

// DeleteAccount removes one account of a Google connector of kind
// after revoking its token. Removing the primary promotes the oldest
// remaining account; removing the last one removes the connector.
func (s *ConnectorService) DeleteAccount(ctx context.Context, user uuid.UUID, kind string, id uuid.UUID) error {
	a, c, err := s.account(ctx, user, kind, id)
	if err != nil {
		return err
	}
	s.revokeAccount(ctx, c, a)
	tx, err := s.pool.Begin(ctx)
	if err != nil {
		return err
	}
	defer tx.Rollback(ctx)
	if err := queries.DeleteAccountOAuthCredential(ctx, tx, a.ID); err != nil {
		return err
	}
	if err := queries.DeleteAccountContacts(ctx, tx, a.ID); err != nil {
		return err
	}
	if err := queries.DeleteGmailAccount(ctx, tx, a.ID); err != nil {
		return err
	}
	rest, err := queries.ListGmailAccounts(ctx, tx, c.ID)
	if err != nil {
		return err
	}
	if len(rest) == 0 {
		if err := queries.SoftDeleteConnector(ctx, tx, c.ID); err != nil {
			return err
		}
		if err := queries.DeleteOAuthCredential(ctx, tx, c.ID); err != nil {
			return err
		}
	} else {
		if a.Primary {
			if err := queries.SetPrimaryGmailAccount(ctx, tx, c.ID, rest[0].ID); err != nil {
				return err
			}
		}
		if err := s.refreshAccounts(ctx, tx, c); err != nil {
			return err
		}
	}
	if err := syncMailboxPolls(ctx, tx, user); err != nil {
		return err
	}
	return tx.Commit(ctx)
}

// SetPrimary makes id the account Keen uses by default (sends from, or
// creates events in) on its connector of kind.
func (s *ConnectorService) SetPrimary(ctx context.Context, user uuid.UUID, kind string, id uuid.UUID) error {
	a, c, err := s.account(ctx, user, kind, id)
	if err != nil {
		return err
	}
	tx, err := s.pool.Begin(ctx)
	if err != nil {
		return err
	}
	defer tx.Rollback(ctx)
	if err := queries.SetPrimaryGmailAccount(ctx, tx, c.ID, a.ID); err != nil {
		return err
	}
	if err := s.refreshAccounts(ctx, tx, c); err != nil {
		return err
	}
	return tx.Commit(ctx)
}

// Authorize discovers the connector's authorization server (gmail and
// calendar use Google's endpoints from the catalog), registers a
// client unless the catalog supplies one, and returns the URL the user opens
// to grant access (PKCE S256). A catalog connector is added on first use, and
// authorizing gmail or calendar again adds another account.
func (s *ConnectorService) Authorize(ctx context.Context, user uuid.UUID, name string) (string, error) {
	entry, inCatalog := s.catalog[name]
	c, err := queries.GetConnectorByName(ctx, s.pool, user, name)
	if errors.Is(err, queries.ErrNotFound) && inCatalog && entry.URL != "" {
		c = &models.Connector{UserID: user, Name: name, Kind: s.kind(name), URL: entry.URL, Status: models.ConnectorNeedsAuth}
		err = queries.CreateConnector(ctx, s.pool, c)
		if errors.Is(err, queries.ErrConflict) { // a concurrent first authorize created it
			c, err = queries.GetConnectorByName(ctx, s.pool, user, name)
		}
	}
	if err != nil {
		return "", err
	}
	return s.authorize(ctx, c, entry, nil)
}

// AuthorizeAccount returns the URL that reconnects one account of a gmail or calendar connector (kind).
func (s *ConnectorService) AuthorizeAccount(ctx context.Context, user uuid.UUID, kind string, id uuid.UUID) (string, error) {
	a, c, err := s.account(ctx, user, kind, id)
	if err != nil {
		return "", err
	}
	return s.authorize(ctx, c, s.catalog[c.Name], &a.ID)
}

// authorize stores a pending authorization for c, or for one of its accounts.
func (s *ConnectorService) authorize(ctx context.Context, c *models.Connector, entry config.ConnectorConfig, account *uuid.UUID) (string, error) {
	var as *mcp.AuthServer
	var err error
	if models.HasAccounts(c.Kind) {
		as = &mcp.AuthServer{AuthURL: entry.AuthURL, TokenURL: entry.TokenURL}
	} else if as, err = mcp.Discover(ctx, c.URL, s.http); err != nil {
		return "", fmt.Errorf("discover authorization server: %w", err)
	}
	if len(entry.Scopes) > 0 {
		as.Scopes = entry.Scopes
	}
	clientID, secret := entry.ClientID, entry.ClientSecret
	if clientID == "" && models.HasAccounts(c.Kind) {
		return "", fmt.Errorf("%s: no OAuth client is configured (%s_CLIENT_ID)", c.Name, strings.ToUpper(c.Kind))
	}
	if clientID == "" {
		if clientID, secret, err = mcp.Register(ctx, as, s.redirectURI, s.http); err != nil {
			return "", err
		}
	}
	sealedSecret, err := s.box.Seal(secret)
	if err != nil {
		return "", err
	}
	raw := make([]byte, 32)
	if _, err := rand.Read(raw); err != nil {
		return "", err
	}
	state := base64.RawURLEncoding.EncodeToString(raw)
	expires := time.Now().Add(oauthStateTTL).UTC()
	cred := &models.OAuthCredential{
		UserID: c.UserID, ConnectorID: c.ID, AccountID: account, Resource: as.Resource, AuthURL: as.AuthURL, TokenURL: as.TokenURL,
		Scopes: strings.Join(as.Scopes, " "), ClientID: clientID, ClientSecret: sealedSecret,
		Verifier: oauth2.GenerateVerifier(), StateHash: hashState(state), StateExpiresAt: &expires,
	}
	if err := queries.UpsertOAuthPending(ctx, s.pool, cred); err != nil {
		return "", err
	}
	opts := []oauth2.AuthCodeOption{oauth2.S256ChallengeOption(cred.Verifier)}
	if cred.Resource != "" { // Google rejects RFC 8707 resource indicators
		opts = append(opts, oauth2.SetAuthURLParam("resource", cred.Resource))
	}
	for k, v := range entry.AuthParams {
		opts = append(opts, oauth2.SetAuthURLParam(k, v))
	}
	return s.oauthConfig(cred).AuthCodeURL(state, opts...), nil
}

// Callback completes an authorization: it consumes state (single use,
// expiring, bound to the user who started it through the connector's
// credential row), exchanges code for tokens and reconnects. It returns the
// connector whenever state matched one, even when a later step failed, and
// for gmail and calendar the address that signed in.
func (s *ConnectorService) Callback(ctx context.Context, state, code, oauthError string) (*models.Connector, string, error) {
	if state == "" {
		return nil, "", ErrInvalidOAuthState
	}
	cred, err := queries.ConsumeOAuthState(ctx, s.pool, hashState(state), time.Now())
	if errors.Is(err, queries.ErrNotFound) {
		return nil, "", ErrInvalidOAuthState
	}
	if err != nil {
		return nil, "", err
	}
	c, err := queries.GetConnector(ctx, s.pool, cred.ConnectorID)
	if err != nil {
		return nil, "", err
	}
	if oauthError != "" || code == "" {
		return c, "", fmt.Errorf("authorization denied: %q", oauthError)
	}
	if err := s.unseal(cred); err != nil {
		return c, "", err
	}
	opts := []oauth2.AuthCodeOption{oauth2.VerifierOption(cred.Verifier)}
	if cred.Resource != "" {
		opts = append(opts, oauth2.SetAuthURLParam("resource", cred.Resource))
	}
	tok, err := s.oauthConfig(cred).Exchange(context.WithValue(ctx, oauth2.HTTPClient, s.http), code, opts...)
	if err != nil {
		return c, "", fmt.Errorf("token exchange: %w", err)
	}
	if models.HasAccounts(c.Kind) {
		email, err := s.connectAccount(ctx, c, cred, tok)
		return c, email, err
	}
	if err := s.saveToken(ctx, c, tok); err != nil {
		return c, "", err
	}
	sess, _, err := s.connect(ctx, c)
	if err != nil {
		return c, "", err
	}
	sess.Close()
	return c, "", nil
}

// Snapshot connects to every one of the user's connectors and returns their tools for one task,
// named <connector>_<tool>. Connectors that fail are skipped and their status
// records why. A name that collides after sanitizing keeps its first tool.
// shortcut: connects one connector at a time, each bounded by connectTimeout; connect concurrently if task start latency matters.
func (s *ConnectorService) Snapshot(ctx context.Context, userID uuid.UUID) (*harness.ConnectorToolset, error) {
	cs, err := queries.ListConnectors(ctx, s.pool, userID)
	if err != nil {
		return nil, err
	}
	set := &harness.ConnectorToolset{Handlers: map[string]harness.ConnectorHandler{}, NeedsApproval: map[string]bool{}, NoAlways: map[string]bool{}, Resolve: map[string]func(json.RawMessage) (json.RawMessage, error){}}
	var sessions []*mcp.Session
	set.Close = func() {
		for _, sess := range sessions {
			sess.Close()
		}
	}
	for _, c := range cs {
		switch c.Kind {
		case models.ConnectorGmail:
			s.addGmail(ctx, set, c)
		case models.ConnectorCalendar:
			s.addCalendar(ctx, set, c)
		case models.ConnectorContacts:
			s.addContacts(ctx, set, c)
		case models.ConnectorDrive:
			s.addDrive(ctx, set, c)
		default:
			s.addMCP(ctx, set, c, &sessions)
		}
	}
	sort.Slice(set.Tools, func(i, j int) bool { return set.Tools[i].Name < set.Tools[j].Name })
	return set, nil
}

// addMCP offers an MCP connector's tools. When the connector is connected and
// its tool list is cached, the tools are offered from the cache and the live
// session is dialed only on first use (opened sessions are appended to sessions
// for the caller to close); otherwise it dials now, as it always did, which
// also warms the cache for next time.
// shortcut: the cache refreshes on reconnect, on the Connectors page, and on a
// task's first call into the connector; a tool added server-side is offered on
// the next task that dials. Persist tool changes eagerly if that lag matters.
func (s *ConnectorService) addMCP(ctx context.Context, set *harness.ConnectorToolset, c *models.Connector, sessions *[]*mcp.Session) {
	var cached []mcp.Tool
	if c.Status == models.ConnectorConnected && len(c.ToolsSpec) > 0 {
		if err := json.Unmarshal(c.ToolsSpec, &cached); err != nil {
			cached = nil
		}
	}
	if cached == nil {
		sess, tools, err := s.connect(ctx, c)
		if err != nil {
			return
		}
		*sessions = append(*sessions, sess)
		s.offerMCP(set, c, tools, func(context.Context) (*mcp.Session, error) { return sess, nil })
		return
	}
	var (
		once sync.Once
		sess *mcp.Session
		derr error
	)
	dial := func(ctx context.Context) (*mcp.Session, error) {
		once.Do(func() {
			if sess, _, derr = s.connect(ctx, c); derr == nil {
				*sessions = append(*sessions, sess)
			}
		})
		return sess, derr
	}
	s.offerMCP(set, c, cached, dial)
}

// offerMCP registers tools on set, each call routed through getSession so the
// live connection can be opened lazily. Duplicate tool names are skipped.
func (s *ConnectorService) offerMCP(set *harness.ConnectorToolset, c *models.Connector, tools []mcp.Tool, getSession func(context.Context) (*mcp.Session, error)) {
	for _, t := range tools {
		name := mcp.ToolName(c.Name, t.Name)
		if _, dup := set.Handlers[name]; dup {
			continue
		}
		set.Tools = append(set.Tools, harness.ConnectorTool{Name: name, Description: "[" + c.Name + "] " + t.Description, InputSchema: t.InputSchema})
		toolName, c := t.Name, c
		set.Handlers[name] = func(ctx context.Context, input json.RawMessage) (string, bool, error) {
			sess, err := getSession(ctx)
			if err != nil {
				if errors.Is(err, mcp.ErrNeedsAuth) {
					return s.needsReconnect(c), true, nil
				}
				return "", true, err
			}
			text, isErr, err := sess.Call(ctx, toolName, input)
			if errors.Is(err, mcp.ErrNeedsAuth) {
				return s.needsReconnect(c), true, nil
			}
			return text, isErr, err
		}
		if !t.ReadOnly {
			set.NeedsApproval[name] = true
		}
	}
}

// addGmail adds one set of Gmail tools over c's connected accounts. An
// account without a usable sign-in is marked needs_auth and left out, as a
// failed MCP connection is.
func (s *ConnectorService) addGmail(ctx context.Context, set *harness.ConnectorToolset, c *models.Connector) {
	rows, err := queries.ListGmailAccounts(ctx, s.pool, c.ID)
	if err != nil {
		log.Printf("connector %s: %v", c.Name, err)
		return
	}
	var accounts []gmail.Account
	for _, a := range rows {
		if a.Status != models.ConnectorConnected {
			continue
		}
		ts, err := s.accountTokenSource(ctx, a)
		if err != nil {
			log.Printf("connector %s: %v", c.Name, err)
			continue
		}
		if ts == nil {
			s.markAccountNeedsAuth(a.ID)
			continue
		}
		accounts = append(accounts, gmail.Account{Email: a.Email, Primary: a.Primary, Client: s.gmailClient(c.Name, ts, 30*time.Second), Cursor: accountCursor{s.pool, a.ID}})
	}
	byEmail := map[string]uuid.UUID{}
	for _, a := range rows {
		byEmail[strings.ToLower(a.Email)] = a.ID
	}
	onAuth := func(email string) { s.markAccountNeedsAuth(byEmail[strings.ToLower(email)]) }
	for _, t := range gmail.Tools(accounts, onAuth) {
		name := mcp.ToolName(c.Name, t.Name)
		set.Tools = append(set.Tools, harness.ConnectorTool{Name: name, Description: "[" + c.Name + "] " + t.Description, InputSchema: t.Schema})
		run := t.Run
		set.Handlers[name] = func(ctx context.Context, input json.RawMessage) (string, bool, error) {
			text, err := run(ctx, harness.WorkspaceFrom(ctx), input)
			if errors.Is(err, gmail.ErrNeedsAuth) {
				return err.Error(), true, nil
			}
			return text, false, err
		}
		set.NeedsApproval[name] = t.Approval
		set.NoAlways[name] = t.NoAlways
		if t.Resolve != nil {
			set.Resolve[name] = t.Resolve
		}
	}
}

// addCalendar adds one set of Calendar tools over c's connected accounts, as
// addGmail does. New events default to the user's preferred time zone.
func (s *ConnectorService) addCalendar(ctx context.Context, set *harness.ConnectorToolset, c *models.Connector) {
	rows, err := queries.ListGmailAccounts(ctx, s.pool, c.ID)
	if err != nil {
		log.Printf("connector %s: %v", c.Name, err)
		return
	}
	tz := ""
	if prefs, err := queries.GetPreferences(ctx, s.pool, c.UserID); err == nil {
		tz = prefs.Timezone
	}
	var accounts []gcal.Account
	byEmail := map[string]uuid.UUID{}
	for _, a := range rows {
		byEmail[strings.ToLower(a.Email)] = a.ID
		if a.Status != models.ConnectorConnected {
			continue
		}
		ts, err := s.accountTokenSource(ctx, a)
		if err != nil {
			log.Printf("connector %s: %v", c.Name, err)
			continue
		}
		if ts == nil {
			s.markAccountNeedsAuth(a.ID)
			continue
		}
		accounts = append(accounts, gcal.Account{Email: a.Email, Primary: a.Primary, Client: s.calendarClient(c.Name, ts, 30*time.Second)})
	}
	onAuth := func(email string) { s.markAccountNeedsAuth(byEmail[strings.ToLower(email)]) }
	for _, t := range gcal.Tools(accounts, tz, onAuth) {
		name := mcp.ToolName(c.Name, t.Name)
		set.Tools = append(set.Tools, harness.ConnectorTool{Name: name, Description: "[" + c.Name + "] " + t.Description, InputSchema: t.Schema})
		run := t.Run
		set.Handlers[name] = func(ctx context.Context, input json.RawMessage) (string, bool, error) {
			text, err := run(ctx, input)
			if errors.Is(err, gcal.ErrNeedsAuth) {
				return err.Error(), true, nil
			}
			return text, false, err
		}
		set.NeedsApproval[name] = t.Approval
		if t.Resolve != nil {
			set.Resolve[name] = t.Resolve
		}
	}
}

// connectedAccounts returns c's connected accounts with a token source. An
// account without a usable sign-in is marked needs_auth and left out. byEmail
// maps every account's lowercased address to its id.
func (s *ConnectorService) connectedAccounts(ctx context.Context, c *models.Connector) (accounts []*models.GmailAccount, sources []oauth2.TokenSource, byEmail map[string]uuid.UUID) {
	rows, err := queries.ListGmailAccounts(ctx, s.pool, c.ID)
	if err != nil {
		log.Printf("connector %s: %v", c.Name, err)
		return nil, nil, nil
	}
	byEmail = map[string]uuid.UUID{}
	for _, a := range rows {
		byEmail[strings.ToLower(a.Email)] = a.ID
		if a.Status != models.ConnectorConnected {
			continue
		}
		ts, err := s.accountTokenSource(ctx, a)
		if err != nil {
			log.Printf("connector %s: %v", c.Name, err)
			continue
		}
		if ts == nil {
			s.markAccountNeedsAuth(a.ID)
			continue
		}
		accounts, sources = append(accounts, a), append(sources, ts)
	}
	return accounts, sources, byEmail
}

// addContacts adds the read-only contacts tools over the stored copies of
// c's connected accounts' contacts.
func (s *ConnectorService) addContacts(ctx context.Context, set *harness.ConnectorToolset, c *models.Connector) {
	rows, _, _ := s.connectedAccounts(ctx, c)
	accounts := make([]contactAccount, len(rows))
	for i, a := range rows {
		accounts[i] = contactAccount{a.ID, a.Email}
	}
	for _, t := range contactTools(s.pool, c.UserID, accounts) {
		name := mcp.ToolName(c.Name, t.Name)
		set.Tools = append(set.Tools, harness.ConnectorTool{Name: name, Description: "[" + c.Name + "] " + t.Description, InputSchema: t.Schema})
		run := t.Run
		set.Handlers[name] = func(ctx context.Context, input json.RawMessage) (string, bool, error) {
			text, err := run(ctx, input)
			return text, false, err
		}
	}
}

// addDrive adds the read-only Drive tools over c's connected accounts.
func (s *ConnectorService) addDrive(ctx context.Context, set *harness.ConnectorToolset, c *models.Connector) {
	rows, sources, byEmail := s.connectedAccounts(ctx, c)
	accounts := make([]gdrive.Account, len(rows))
	for i, a := range rows {
		accounts[i] = gdrive.Account{Email: a.Email, Primary: a.Primary, Client: s.driveClient(c.Name, sources[i], 60*time.Second)}
	}
	onAuth := func(email string) { s.markAccountNeedsAuth(byEmail[strings.ToLower(email)]) }
	for _, t := range gdrive.Tools(accounts, onAuth) {
		name := mcp.ToolName(c.Name, t.Name)
		set.Tools = append(set.Tools, harness.ConnectorTool{Name: name, Description: "[" + c.Name + "] " + t.Description, InputSchema: t.Schema})
		run := t.Run
		set.Handlers[name] = func(ctx context.Context, input json.RawMessage) (string, bool, error) {
			text, err := run(ctx, harness.WorkspaceFrom(ctx), input)
			if errors.Is(err, gdrive.ErrNeedsAuth) {
				return err.Error(), true, nil
			}
			return text, false, err
		}
	}
}

func (s *ConnectorService) driveClient(name string, ts oauth2.TokenSource, timeout time.Duration) *gdrive.Client {
	return &gdrive.Client{
		HTTP:    &http.Client{Transport: &oauth2.Transport{Source: ts, Base: s.http.Transport}, Timeout: timeout},
		BaseURL: s.catalog[name].URL,
	}
}

func (s *ConnectorService) calendarClient(name string, ts oauth2.TokenSource, timeout time.Duration) *gcal.Client {
	return &gcal.Client{
		HTTP:    &http.Client{Transport: &oauth2.Transport{Source: ts, Base: s.http.Transport}, Timeout: timeout},
		BaseURL: s.catalog[name].URL,
	}
}

// MailClient returns a Gmail client for an account's stored sign-in, or
// gmail.ErrNeedsAuth when it has none.
func (s *ConnectorService) MailClient(ctx context.Context, a *models.GmailAccount) (*gmail.Client, error) {
	ts, err := s.accountTokenSource(ctx, a)
	if err != nil {
		return nil, err
	}
	if ts == nil {
		return nil, gmail.ErrNeedsAuth
	}
	return s.gmailClient(gmailName, ts, 30*time.Second), nil
}

// accountCursor keeps a gmail account's digest position in its row.
type accountCursor struct {
	pool *pgxpool.Pool
	id   uuid.UUID
}

func (h accountCursor) Get(ctx context.Context) (string, error) {
	a, err := queries.GetGmailAccountByID(ctx, h.pool, h.id)
	if err != nil {
		return "", err
	}
	return a.HistoryID, nil
}

func (h accountCursor) Set(ctx context.Context, id string) error {
	return queries.UpdateGmailAccountHistory(ctx, h.pool, h.id, id)
}

func (s *ConnectorService) gmailClient(name string, ts oauth2.TokenSource, timeout time.Duration) *gmail.Client {
	return &gmail.Client{
		HTTP:    &http.Client{Transport: &oauth2.Transport{Source: ts, Base: s.http.Transport}, Timeout: timeout},
		BaseURL: s.catalog[name].URL,
	}
}

// identify checks tokens just granted to a Google connector
// against Google and returns the address that signed in, plus a gmail
// mailbox's current history id.
func (s *ConnectorService) identify(ctx context.Context, c *models.Connector, tok *oauth2.Token) (email, historyID string, err error) {
	ts := oauth2.StaticTokenSource(tok)
	switch c.Kind {
	case models.ConnectorContacts:
		email, err := s.contactsClient(c.Name, ts, connectTimeout).Me(ctx)
		return email, "", err
	case models.ConnectorDrive:
		email, err := s.driveClient(c.Name, ts, connectTimeout).About(ctx)
		return email, "", err
	}
	if c.Kind == models.ConnectorCalendar {
		cal, err := s.calendarClient(c.Name, ts, connectTimeout).PrimaryCalendar(ctx)
		if err != nil {
			return "", "", err
		}
		if !strings.Contains(cal.ID, "@") {
			return "", "", fmt.Errorf("calendar: primary calendar %q is not an address", cal.ID)
		}
		return cal.ID, "", nil
	}
	prof, err := s.gmailClient(c.Name, ts, connectTimeout).Profile(ctx)
	if err != nil {
		return "", "", err
	}
	return prof.Email, prof.HistoryID, nil
}

// connectAccount checks the tokens just granted against Google, then either
// reconnects the account the authorization was for, reconnects the account
// already holding the address that signed in, or adds a new account. It
// returns the address that signed in. Reconnecting as another address leaves
// the account in error.
func (s *ConnectorService) connectAccount(ctx context.Context, c *models.Connector, cred *models.OAuthCredential, tok *oauth2.Token) (string, error) {
	email, historyID, err := s.identify(ctx, c, tok)
	if err != nil {
		return "", err
	}
	tx, err := s.pool.Begin(ctx)
	if err != nil {
		return email, err
	}
	defer tx.Rollback(ctx)

	var acct *models.GmailAccount
	if cred.AccountID != nil {
		if acct, err = queries.GetGmailAccountByID(ctx, tx, *cred.AccountID); err != nil {
			return email, err
		}
		if !strings.EqualFold(acct.Email, email) {
			msg := ErrDifferentAccount.Error()
			if err := queries.UpdateGmailAccountStatus(ctx, tx, acct.ID, models.ConnectorError, &msg); err != nil {
				return email, err
			}
			if err := s.refreshAccounts(ctx, tx, c); err != nil {
				return email, err
			}
			if err := syncMailboxPolls(ctx, tx, c.UserID); err != nil {
				return email, err
			}
			if err := tx.Commit(ctx); err != nil {
				return email, err
			}
			return email, ErrDifferentAccount
		}
	} else if acct, err = queries.GetGmailAccountByEmail(ctx, tx, c.ID, email); err != nil && !errors.Is(err, queries.ErrNotFound) {
		return email, err
	}
	access, refresh, expiry, err := s.sealToken(tok)
	if err != nil {
		return email, err
	}
	if cred.AccountID != nil {
		err = queries.SaveAccountOAuthToken(ctx, tx, acct.ID, access, refresh, tok.TokenType, expiry)
	} else {
		// The pending row becomes the account's credential, replacing the old one.
		if err = queries.SaveOAuthToken(ctx, tx, c.ID, access, refresh, tok.TokenType, expiry); err != nil {
			return email, err
		}
		if acct == nil {
			existing, lerr := queries.ListGmailAccounts(ctx, tx, c.ID)
			if lerr != nil {
				return email, lerr
			}
			acct = &models.GmailAccount{UserID: c.UserID, ConnectorID: c.ID, Email: email, Status: models.ConnectorConnected, Primary: len(existing) == 0, HistoryID: historyID}
			if err = queries.CreateGmailAccount(ctx, tx, acct); err != nil {
				return email, err
			}
		} else if err = queries.DeleteAccountOAuthCredential(ctx, tx, acct.ID); err != nil {
			return email, err
		}
		err = queries.AttachOAuthCredential(ctx, tx, cred.ID, acct.ID)
	}
	if err != nil {
		return email, err
	}
	if err := queries.UpdateGmailAccountStatus(ctx, tx, acct.ID, models.ConnectorConnected, nil); err != nil {
		return email, err
	}
	if c.Kind == models.ConnectorContacts {
		// The first sync runs on the next trigger tick.
		if err := queries.ScheduleContactSync(ctx, tx, acct.ID, c.UserID, time.Now()); err != nil {
			return email, err
		}
	}
	if err := s.refreshAccounts(ctx, tx, c); err != nil {
		return email, err
	}
	if err := syncMailboxPolls(ctx, tx, c.UserID); err != nil {
		return email, err
	}
	return email, tx.Commit(ctx)
}

// refreshAccounts recomputes a Google connector's status, primary
// address and tool count from its accounts.
func (s *ConnectorService) refreshAccounts(ctx context.Context, db queries.DBTX, c *models.Connector) error {
	accounts, err := queries.ListGmailAccounts(ctx, db, c.ID)
	if err != nil {
		return err
	}
	applyAccounts(c, accounts)
	switch {
	case len(accounts) == 0:
		c.Tools = 0
	case c.Kind == models.ConnectorCalendar:
		c.Tools = len(gcal.Tools([]gcal.Account{{}}, "", nil))
	case c.Kind == models.ConnectorContacts:
		c.Tools = len(contactTools(nil, uuid.Nil, []contactAccount{{}}))
	case c.Kind == models.ConnectorDrive:
		c.Tools = len(gdrive.Tools([]gdrive.Account{{}}, nil))
	default:
		c.Tools = len(gmail.Tools([]gmail.Account{{}}, nil))
	}
	if err := queries.UpdateConnectorStatus(ctx, db, c); err != nil {
		return err
	}
	return queries.UpdateConnectorAccount(ctx, db, c)
}

// markAccountNeedsAuth records that an account's sign-in stopped working.
func (s *ConnectorService) markAccountNeedsAuth(id uuid.UUID) {
	ctx := context.Background()
	a, err := queries.GetGmailAccountByID(ctx, s.pool, id)
	if err != nil {
		return
	}
	err = pgx.BeginFunc(ctx, s.pool, func(tx pgx.Tx) error {
		if err := queries.UpdateGmailAccountStatus(ctx, tx, id, models.ConnectorNeedsAuth, nil); err != nil {
			return err
		}
		c, err := queries.GetConnector(ctx, tx, a.ConnectorID)
		if err != nil {
			return err
		}
		if err := s.refreshAccounts(ctx, tx, c); err != nil {
			return err
		}
		return syncMailboxPolls(ctx, tx, a.UserID)
	})
	if err != nil {
		log.Printf("account %s: %v", id, err)
	}
}

// applyAccounts sets a gmail or calendar connector's fields from its accounts: connected
// if any account is, needs_auth if every one is, error otherwise.
func applyAccounts(c *models.Connector, accounts []*models.GmailAccount) {
	c.Accounts = accounts
	if len(accounts) == 0 {
		return
	}
	status, errText := models.ConnectorNeedsAuth, (*string)(nil)
	connected, needsAuth := false, 0
	for _, a := range accounts {
		switch a.Status {
		case models.ConnectorConnected:
			connected = true
		case models.ConnectorNeedsAuth:
			needsAuth++
		case models.ConnectorError:
			if errText == nil {
				errText = a.Error
			}
		}
	}
	switch {
	case connected:
		status, errText = models.ConnectorConnected, nil
	case needsAuth == len(accounts):
		errText = nil
	default:
		status = models.ConnectorError
	}
	c.Status, c.Error = status, errText
	primary := accounts[0]
	for _, a := range accounts {
		if a.Primary {
			primary = a
		}
	}
	email := primary.Email
	c.Account = &email
}

// needsReconnect records that c's sign-in stopped working and returns what
// the model is told.
func (s *ConnectorService) needsReconnect(c *models.Connector) string {
	s.markNeedsAuth(c)
	return strings.ToUpper(c.Name[:1]) + c.Name[1:] + " needs reconnecting in Connectors"
}

func (s *ConnectorService) markNeedsAuth(c *models.Connector) {
	c.Status, c.Error = models.ConnectorNeedsAuth, nil
	if err := queries.UpdateConnectorStatus(context.Background(), s.pool, c); err != nil {
		log.Printf("connector %s: %v", c.Name, err)
	}
}

// connect opens a session to c and lists its tools, recording the outcome
// as c's status. The caller closes the session.
func (s *ConnectorService) connect(ctx context.Context, c *models.Connector) (*mcp.Session, []mcp.Tool, error) {
	sess, tools, err := s.dial(ctx, c)
	c.Status, c.Error, c.Tools = models.ConnectorConnected, nil, len(tools)
	if err == nil {
		c.ToolsSpec, _ = json.Marshal(tools) // cache for cache-only task starts; keep the old cache on a failed dial
	}
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
	if err != nil {
		return nil, err
	}
	return s.sourceFrom(cred, func(t *oauth2.Token) error { return s.saveToken(context.Background(), c, t) })
}

// accountTokenSource is tokenSource for a gmail or calendar account.
func (s *ConnectorService) accountTokenSource(ctx context.Context, a *models.GmailAccount) (oauth2.TokenSource, error) {
	cred, err := queries.GetAccountOAuthCredential(ctx, s.pool, a.ID)
	if errors.Is(err, queries.ErrNotFound) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	return s.sourceFrom(cred, func(t *oauth2.Token) error {
		access, refresh, expiry, err := s.sealToken(t)
		if err != nil {
			return err
		}
		return queries.SaveAccountOAuthToken(context.Background(), s.pool, a.ID, access, refresh, t.TokenType, expiry)
	})
}

func (s *ConnectorService) sourceFrom(cred *models.OAuthCredential, save func(*oauth2.Token) error) (oauth2.TokenSource, error) {
	if err := s.unseal(cred); err != nil {
		return nil, err
	}
	if cred.AccessToken == "" {
		return nil, nil
	}
	tok := &oauth2.Token{AccessToken: cred.AccessToken, RefreshToken: cred.RefreshToken, TokenType: cred.TokenType}
	if cred.Expiry != nil {
		tok.Expiry = *cred.Expiry
	}
	// The refresh context outlives this call: the token source lives as long as the session.
	refreshCtx := context.WithValue(context.Background(), oauth2.HTTPClient, s.http)
	return &savingTokenSource{inner: s.oauthConfig(cred).TokenSource(refreshCtx, tok), last: tok.AccessToken, save: save}, nil
}

// sealToken encrypts t's secrets for storage.
func (s *ConnectorService) sealToken(t *oauth2.Token) (access, refresh string, expiry *time.Time, err error) {
	if !t.Expiry.IsZero() {
		e := t.Expiry.UTC()
		expiry = &e
	}
	if access, err = s.box.Seal(t.AccessToken); err != nil {
		return
	}
	refresh, err = s.box.Seal(t.RefreshToken)
	return
}

func (s *ConnectorService) saveToken(ctx context.Context, c *models.Connector, t *oauth2.Token) error {
	access, refresh, expiry, err := s.sealToken(t)
	if err != nil {
		return err
	}
	return queries.SaveOAuthToken(ctx, s.pool, c.ID, access, refresh, t.TokenType, expiry)
}

// unseal decrypts cred's secrets in place; plaintext rows from before
// encryption pass through and are sealed by their next save.
func (s *ConnectorService) unseal(cred *models.OAuthCredential) (err error) {
	for _, f := range []*string{&cred.AccessToken, &cred.RefreshToken, &cred.ClientSecret} {
		if *f, err = s.box.Open(*f); err != nil {
			return err
		}
	}
	return nil
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
