/*
 * Copyright (c) 2026. Papermark - All Rights Reserved
 * Unauthorized copying of this file, via any medium is strictly prohibited
 * Proprietary and confidential
 * Written by Ebot Tabi <ebot.tabi@gmail.com>, 21/10/2026
 */
package services

import (
	"context"
	"encoding/json"
	"testing"

	"papermark/internal/harness"
	"papermark/internal/integrations/mcp"
	"papermark/internal/models"
)

// offerMCP turns cached tool specs into an offered toolset without dialing, so a
// task starts with the connector's tools ready. Approval is gated on ReadOnly and
// a name already offered is skipped.
func TestOfferMCPFromCachedSpecs(t *testing.T) {
	set := &harness.ConnectorToolset{Handlers: map[string]harness.ConnectorHandler{}, NeedsApproval: map[string]bool{}}
	c := &models.Connector{Name: "slack"}
	tools := []mcp.Tool{
		{Name: "search", Description: "find messages", InputSchema: json.RawMessage(`{"type":"object"}`), ReadOnly: true},
		{Name: "post", Description: "send a message", ReadOnly: false},
		{Name: "search", Description: "dup", ReadOnly: true}, // same name, must be skipped
	}
	called := false
	getSession := func(context.Context) (*mcp.Session, error) { called = true; return nil, nil }

	(&ConnectorService{}).offerMCP(set, c, tools, getSession)

	if called {
		t.Fatal("offerMCP dialed the session; it must stay lazy until a tool is called")
	}
	if len(set.Tools) != 2 {
		t.Fatalf("got %d tools, want 2 (duplicate name skipped)", len(set.Tools))
	}
	search, post := mcp.ToolName("slack", "search"), mcp.ToolName("slack", "post")
	if _, ok := set.Handlers[search]; !ok {
		t.Errorf("no handler registered for %q", search)
	}
	if set.NeedsApproval[search] {
		t.Error("read-only tool must not require approval")
	}
	if !set.NeedsApproval[post] {
		t.Error("write tool must require approval")
	}
	if set.Tools[0].Description != "[slack] find messages" {
		t.Errorf("description not prefixed with connector name: %q", set.Tools[0].Description)
	}
}
