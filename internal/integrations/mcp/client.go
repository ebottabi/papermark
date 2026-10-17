/*
 * Copyright (c) 2026. Papermark - All Rights Reserved
 * Unauthorized copying of this file, via any medium is strictly prohibited
 * Proprietary and confidential
 * Written by Ebot Tabi <ebot.tabi@gmail.com>, 15/10/2026
 */
// Package mcp connects to remote MCP servers over streamable HTTP and runs the
// OAuth discovery and registration steps their authorization needs.
package mcp

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log"
	"net/http"
	"regexp"
	"sort"
	"strings"
	"sync/atomic"

	sdkmcp "github.com/modelcontextprotocol/go-sdk/mcp"
	"golang.org/x/oauth2"
)

// ErrNeedsAuth means the server rejected the request for lack of a valid token.
var ErrNeedsAuth = errors.New("mcp: server requires authorization")

// Tool is a tool an MCP server offers.
type Tool struct {
	Name        string
	Description string
	InputSchema json.RawMessage
	ReadOnly    bool
}

// Session is an open connection to one MCP server.
type Session struct {
	s    *sdkmcp.ClientSession
	flag *authFlag
}

// authFlag is the transport's OAuth handler. It never runs an authorization
// flow itself (that needs the user's browser); it records that one is needed.
type authFlag struct {
	ts        oauth2.TokenSource
	needsAuth atomic.Bool
}

func (a *authFlag) TokenSource(context.Context) (oauth2.TokenSource, error) {
	if a.ts == nil {
		return nil, nil
	}
	return flaggingSource{a}, nil
}

func (a *authFlag) Authorize(_ context.Context, req *http.Request, resp *http.Response) error {
	log.Printf("mcp: %s requires authorization: status=%d www-authenticate=%q", req.URL.Host, resp.StatusCode, resp.Header.Get("WWW-Authenticate"))
	io.Copy(io.Discard, resp.Body)
	resp.Body.Close()
	a.needsAuth.Store(true)
	return ErrNeedsAuth
}

// flaggingSource treats a failed refresh (a revoked or expired refresh token) as needing authorization.
type flaggingSource struct{ a *authFlag }

func (f flaggingSource) Token() (*oauth2.Token, error) {
	t, err := f.a.ts.Token()
	var re *oauth2.RetrieveError
	if errors.As(err, &re) {
		f.a.needsAuth.Store(true)
	}
	return t, err
}

// Connect opens a session to the MCP server at url, sending tokens from ts
// (nil for none). It returns an error wrapping ErrNeedsAuth when the server
// demands authorization.
func Connect(ctx context.Context, url string, ts oauth2.TokenSource, hc *http.Client) (*Session, error) {
	flag := &authFlag{ts: ts}
	client := sdkmcp.NewClient(&sdkmcp.Implementation{Name: "keen", Version: "1.0.0"}, nil)
	s, err := client.Connect(ctx, &sdkmcp.StreamableClientTransport{
		Endpoint:             url,
		HTTPClient:           hc,
		MaxRetries:           -1,
		DisableStandaloneSSE: true,
		OAuthHandler:         flag,
	}, nil)
	if err != nil {
		if flag.needsAuth.Load() {
			return nil, fmt.Errorf("%w: %v", ErrNeedsAuth, err)
		}
		return nil, err
	}
	return &Session{s: s, flag: flag}, nil
}

// Tools lists every tool the server offers, sorted by name.
func (s *Session) Tools(ctx context.Context) ([]Tool, error) {
	var out []Tool
	for t, err := range s.s.Tools(ctx, nil) {
		if err != nil {
			return nil, err
		}
		schema, err := json.Marshal(t.InputSchema)
		if err != nil {
			return nil, fmt.Errorf("tool %s: schema: %w", t.Name, err)
		}
		out = append(out, Tool{
			Name: t.Name, Description: t.Description, InputSchema: schema,
			ReadOnly: t.Annotations != nil && t.Annotations.ReadOnlyHint,
		})
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Name < out[j].Name })
	return out, nil
}

// Call runs tool name with JSON arguments and flattens its content to text:
// text blocks verbatim, other blocks as JSON, joined by newlines.
func (s *Session) Call(ctx context.Context, name string, args json.RawMessage) (text string, isError bool, err error) {
	var arguments any = map[string]any{}
	if len(args) > 0 && string(args) != "null" {
		arguments = args
	}
	res, err := s.s.CallTool(ctx, &sdkmcp.CallToolParams{Name: name, Arguments: arguments})
	if err != nil {
		if s.flag.needsAuth.Load() {
			return "", false, fmt.Errorf("%w: %v", ErrNeedsAuth, err)
		}
		return "", false, err
	}
	parts := make([]string, 0, len(res.Content))
	for _, c := range res.Content {
		if t, ok := c.(*sdkmcp.TextContent); ok {
			parts = append(parts, t.Text)
			continue
		}
		b, err := json.Marshal(c)
		if err != nil {
			return "", false, err
		}
		parts = append(parts, string(b))
	}
	return strings.Join(parts, "\n"), res.IsError, nil
}

// Close ends the session.
func (s *Session) Close() error { return s.s.Close() }

var invalidToolChars = regexp.MustCompile(`[^a-zA-Z0-9_-]`)

// ToolName is the name a connector tool gets in a task: <connector>_<tool>,
// with characters outside [a-zA-Z0-9_-] replaced by "_" and cut to the
// 64-byte limit of Anthropic tool names.
func ToolName(connector, tool string) string {
	n := invalidToolChars.ReplaceAllString(connector+"_"+tool, "_")
	if len(n) > 64 {
		n = n[:64]
	}
	return n
}
