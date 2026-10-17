/*
 * Copyright (c) 2026. Papermark - All Rights Reserved
 * Unauthorized copying of this file, via any medium is strictly prohibited
 * Proprietary and confidential
 * Written by Ebot Tabi <ebot.tabi@gmail.com>, 15/10/2026
 */
package mcp

import (
	"context"
	"net/http"
	"os"
	"testing"
	"time"
)

// TestDiscoverLive checks discovery against the real catalog servers. Run with MCP_LIVE=1.
func TestDiscoverLive(t *testing.T) {
	if os.Getenv("MCP_LIVE") == "" {
		t.Skip("set MCP_LIVE=1 to reach the real Gmail MCP server")
	}
	cases := []struct{ url, auth, token string }{
		{"https://gmailmcp.googleapis.com/mcp/v1", "https://accounts.google.com/o/oauth2/v2/auth", "https://oauth2.googleapis.com/token"},
	}
	hc := &http.Client{Timeout: 30 * time.Second}
	for _, c := range cases {
		as, err := Discover(context.Background(), c.url, hc)
		if err != nil {
			t.Errorf("%s: %v", c.url, err)
			continue
		}
		if as.AuthURL != c.auth || as.TokenURL != c.token || as.Resource != c.url {
			t.Errorf("%s: got %+v", c.url, as)
		}
	}
}
