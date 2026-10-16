/*
 * Copyright (c) 2026. Papermark - All Rights Reserved
 * Unauthorized copying of this file, via any medium is strictly prohibited
 * Proprietary and confidential
 * Written by Ebot Tabi <ebot.tabi@gmail.com>, 15/10/2026
 */
package mcp

import (
	"context"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"

	"github.com/modelcontextprotocol/go-sdk/auth"
	"github.com/modelcontextprotocol/go-sdk/oauthex"
)

// AuthServer is where and how to authorize access to one MCP server.
type AuthServer struct {
	Resource        string // RFC 8707 resource indicator sent with authorize and token requests
	AuthURL         string
	TokenURL        string
	RegistrationURL string
	Scopes          []string
}

// Discover finds the authorization server for the MCP server at serverURL,
// following the MCP authorization spec: an unauthenticated probe for the
// WWW-Authenticate challenge, protected resource metadata (challenge URL,
// then path-inserted, then root), then authorization server metadata, with
// the 2025-03-26 fallbacks when either document is missing.
func Discover(ctx context.Context, serverURL string, hc *http.Client) (*AuthServer, error) {
	challenges, err := probe(ctx, serverURL, hc)
	if err != nil {
		return nil, err
	}
	var metadataURL string
	var scopes []string
	for _, c := range challenges {
		if c.Scheme != "bearer" {
			continue
		}
		metadataURL = c.Params["resource_metadata"]
		scopes = strings.Fields(c.Params["scope"])
	}

	as := AuthServer{Resource: serverURL, Scopes: scopes}
	issuer := ""
	for _, cand := range prmCandidates(metadataURL, serverURL) {
		prm, err := oauthex.GetProtectedResourceMetadata(ctx, cand[0], cand[1], hc)
		if err != nil || prm == nil {
			continue
		}
		if len(prm.AuthorizationServers) == 0 {
			return nil, errors.New("protected resource metadata names no authorization server")
		}
		issuer, as.Resource = prm.AuthorizationServers[0], prm.Resource
		if len(as.Scopes) == 0 {
			as.Scopes = prm.ScopesSupported
		}
		break
	}
	if issuer == "" {
		u, err := url.Parse(serverURL)
		if err != nil {
			return nil, err
		}
		u.Path, u.RawQuery = "", ""
		issuer = u.String()
	}

	meta, err := auth.GetAuthServerMetadata(ctx, issuer, hc)
	if err != nil {
		return nil, fmt.Errorf("authorization server metadata: %w", err)
	}
	if meta == nil {
		base := strings.TrimRight(issuer, "/")
		meta = &oauthex.AuthServerMeta{AuthorizationEndpoint: base + "/authorize", TokenEndpoint: base + "/token", RegistrationEndpoint: base + "/register"}
	}
	as.AuthURL, as.TokenURL, as.RegistrationURL = meta.AuthorizationEndpoint, meta.TokenEndpoint, meta.RegistrationEndpoint
	return &as, nil
}

// probe makes an unauthenticated initialize request and returns the
// server's WWW-Authenticate challenges, if any.
func probe(ctx context.Context, serverURL string, hc *http.Client) ([]oauthex.Challenge, error) {
	body := `{"jsonrpc":"2.0","id":1,"method":"initialize","params":{"protocolVersion":"2025-06-18","capabilities":{},"clientInfo":{"name":"keen","version":"1.0.0"}}}`
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, serverURL, strings.NewReader(body))
	if err != nil {
		return nil, err
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Accept", "application/json, text/event-stream")
	resp, err := hc.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	io.Copy(io.Discard, io.LimitReader(resp.Body, 1<<16))
	return oauthex.ParseWWWAuthenticate(resp.Header.Values("WWW-Authenticate"))
}

// prmCandidates lists {metadata URL, expected resource} pairs to try.
func prmCandidates(metadataURL, serverURL string) [][2]string {
	var out [][2]string
	if metadataURL != "" {
		out = append(out, [2]string{metadataURL, serverURL})
	}
	u, err := url.Parse(serverURL)
	if err != nil {
		return out
	}
	m := *u
	m.RawQuery = ""
	m.Path = "/.well-known/oauth-protected-resource/" + strings.TrimLeft(u.Path, "/")
	out = append(out, [2]string{m.String(), serverURL})
	m.Path = "/.well-known/oauth-protected-resource"
	root := *u
	root.Path, root.RawQuery = "", ""
	return append(out, [2]string{m.String(), root.String()})
}

// Register registers a public client (PKCE, no secret requested) with the
// authorization server by dynamic client registration. The server may still
// issue a secret, which is returned.
func Register(ctx context.Context, as *AuthServer, redirectURI string, hc *http.Client) (clientID, clientSecret string, err error) {
	if as.RegistrationURL == "" {
		return "", "", errors.New("authorization server does not support dynamic client registration")
	}
	res, err := oauthex.RegisterClient(ctx, as.RegistrationURL, &oauthex.ClientRegistrationMetadata{
		RedirectURIs:            []string{redirectURI},
		TokenEndpointAuthMethod: "none",
		GrantTypes:              []string{"authorization_code", "refresh_token"},
		ResponseTypes:           []string{"code"},
		ClientName:              "Keen",
		Scope:                   strings.Join(as.Scopes, " "),
	}, hc)
	if err != nil {
		return "", "", fmt.Errorf("client registration: %w", err)
	}
	return res.ClientID, res.ClientSecret, nil
}
