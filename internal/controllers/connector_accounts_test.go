/*
 * Copyright (c) 2026. Papermark - All Rights Reserved
 * Unauthorized copying of this file, via any medium is strictly prohibited
 * Proprietary and confidential
 * Written by Ebot Tabi <ebot.tabi@gmail.com>, 17/10/2026
 */
package controllers_test

import (
	"context"
	"testing"

	"papermark/internal/models"
	"papermark/internal/models/queries"

	"github.com/google/uuid"
)

func seedGmail(t *testing.T, api *testAPI, emails ...string) []*models.GmailAccount {
	t.Helper()
	return seedAccounts(t, api, "gmail", emails...)
}

// seedAccounts adds a connector of kind (gmail or calendar, also its name) with emails signed in.
func seedAccounts(t *testing.T, api *testAPI, kind string, emails ...string) []*models.GmailAccount {
	t.Helper()
	ctx := context.Background()
	primary := emails[0]
	c := &models.Connector{UserID: api.user, Name: kind, Kind: kind, URL: "https://www.googleapis.com", Status: "connected", Account: &primary}
	if err := queries.CreateConnector(ctx, api.pool, c); err != nil {
		t.Fatal(err)
	}
	var out []*models.GmailAccount
	for i, e := range emails {
		a := &models.GmailAccount{UserID: api.user, ConnectorID: c.ID, Email: e, Status: "connected", Primary: i == 0}
		if err := queries.CreateGmailAccount(ctx, api.pool, a); err != nil {
			t.Fatal(err)
		}
		out = append(out, a)
	}
	return out
}

func gmailEntry(t *testing.T, api *testAPI) map[string]any {
	t.Helper()
	return connectorEntry(t, api, "gmail")
}

func connectorEntry(t *testing.T, api *testAPI, name string) map[string]any {
	t.Helper()
	_, body := api.do("GET", "/v1/connectors", "")
	for _, c := range body.data()["connectors"].([]any) {
		if m := c.(map[string]any); m["name"] == name {
			return m
		}
	}
	return nil
}

func TestGmailAccountRoutes(t *testing.T) {
	api, _, _ := newConnectorAPI(t)
	accts := seedGmail(t, api, "a@example.com", "b@example.com")

	g := gmailEntry(t, api)
	list, _ := g["accounts"].([]any)
	if g["account"] != "a@example.com" || g["status"] != "connected" || len(list) != 2 {
		t.Fatalf("connector: %v", g)
	}
	first := list[0].(map[string]any)
	if first["email"] != "a@example.com" || first["primary"] != true || first["status"] != "connected" || first["error"] != nil || first["id"] != accts[0].ID.String() {
		t.Fatalf("account: %v", first)
	}

	if res := api.raw("POST", "/v1/connectors/gmail/accounts/"+accts[1].ID.String()+"/primary", ""); res.StatusCode != 204 {
		t.Fatalf("primary: %d", res.StatusCode)
	}
	if g := gmailEntry(t, api); g["account"] != "b@example.com" {
		t.Fatalf("primary did not move: %v", g)
	}
	if res := api.raw("DELETE", "/v1/connectors/gmail/accounts/"+accts[1].ID.String(), ""); res.StatusCode != 204 {
		t.Fatalf("delete primary: %d", res.StatusCode)
	}
	if g := gmailEntry(t, api); g["account"] != "a@example.com" || len(g["accounts"].([]any)) != 1 {
		t.Fatalf("oldest not promoted: %v", g)
	}
	if res := api.raw("DELETE", "/v1/connectors/gmail/accounts/"+accts[0].ID.String(), ""); res.StatusCode != 204 {
		t.Fatalf("delete last: %d", res.StatusCode)
	}
	if gmailEntry(t, api) != nil {
		t.Fatal("connector survives its last account")
	}
}

func TestGmailAccountRoutesAreOwned(t *testing.T) {
	api, _, pool := newConnectorAPI(t)
	accts := seedGmail(t, api, "a@example.com")
	uid, token := newUser(t, pool)
	other := api.as(uid, token)
	id := accts[0].ID.String()
	for _, r := range [][2]string{{"POST", "/primary"}, {"POST", "/authorize"}, {"DELETE", ""}} {
		if code, _ := other.do(r[0], "/v1/connectors/gmail/accounts/"+id+r[1], ""); code != 404 {
			t.Errorf("%s %s: %d", r[0], r[1], code)
		}
	}
	if code, _ := api.do("POST", "/v1/connectors/gmail/accounts/"+uuid.NewString()+"/primary", ""); code != 404 {
		t.Errorf("unknown id: %d", code)
	}
	if code, _ := api.do("POST", "/v1/connectors/gmail/accounts/not-a-uuid/primary", ""); code != 404 && code != 400 {
		t.Errorf("bad id: %d", code)
	}
	if g := gmailEntry(t, api); len(g["accounts"].([]any)) != 1 {
		t.Fatal("a stranger changed the account")
	}
}

func TestDeleteGmailRemovesEveryAccount(t *testing.T) {
	api, _, pool := newConnectorAPI(t)
	seedGmail(t, api, "a@example.com", "b@example.com")
	if res := api.raw("DELETE", "/v1/connectors/gmail", ""); res.StatusCode >= 300 {
		t.Fatalf("delete: %d", res.StatusCode)
	}
	var n int
	pool.QueryRow(context.Background(), `SELECT count(*) FROM gmail_accounts`).Scan(&n)
	if n != 0 || gmailEntry(t, api) != nil {
		t.Fatalf("accounts left: %d", n)
	}
}

func TestMCPConnectorHasEmptyAccounts(t *testing.T) {
	api, _, pool := newConnectorAPI(t)
	c := &models.Connector{UserID: api.user, Name: "notion", Kind: "mcp", URL: "https://mcp.example.com", Status: "connected"}
	if err := queries.CreateConnector(context.Background(), pool, c); err != nil {
		t.Fatal(err)
	}
	_, body := api.do("GET", "/v1/connectors", "")
	for _, c := range body.data()["connectors"].([]any) {
		if a, ok := c.(map[string]any)["accounts"].([]any); !ok || len(a) != 0 {
			t.Fatalf("accounts must be [] not %v", c)
		}
	}
}

func TestCalendarAccountRoutes(t *testing.T) {
	api, _, _ := newConnectorAPI(t)
	cal := seedAccounts(t, api, "calendar", "a@example.com", "b@example.com")
	mail := seedGmail(t, api, "a@example.com")

	c := connectorEntry(t, api, "calendar")
	list, _ := c["accounts"].([]any)
	if c["kind"] != "calendar" || c["account"] != "a@example.com" || len(list) != 2 || list[1].(map[string]any)["email"] != "b@example.com" {
		t.Fatalf("connector: %v", c)
	}
	// An account answers only under its own connector's name.
	for _, r := range []struct{ method, path string }{
		{"POST", "/v1/connectors/gmail/accounts/" + cal[0].ID.String() + "/primary"},
		{"DELETE", "/v1/connectors/gmail/accounts/" + cal[0].ID.String()},
		{"POST", "/v1/connectors/gmail/accounts/" + cal[0].ID.String() + "/authorize"},
		{"POST", "/v1/connectors/calendar/accounts/" + mail[0].ID.String() + "/primary"},
		{"DELETE", "/v1/connectors/calendar/accounts/" + mail[0].ID.String()},
		{"POST", "/v1/connectors/notion/accounts/" + cal[0].ID.String() + "/primary"},
	} {
		if code, _ := api.do(r.method, r.path, ""); code != 404 {
			t.Errorf("%s %s: %d", r.method, r.path, code)
		}
	}

	if res := api.raw("POST", "/v1/connectors/calendar/accounts/"+cal[1].ID.String()+"/primary", ""); res.StatusCode != 204 {
		t.Fatalf("primary: %d", res.StatusCode)
	}
	if c := connectorEntry(t, api, "calendar"); c["account"] != "b@example.com" {
		t.Fatalf("primary did not move: %v", c)
	}
	if res := api.raw("DELETE", "/v1/connectors/calendar/accounts/"+cal[1].ID.String(), ""); res.StatusCode != 204 {
		t.Fatalf("delete: %d", res.StatusCode)
	}
	if c := connectorEntry(t, api, "calendar"); c["account"] != "a@example.com" || len(c["accounts"].([]any)) != 1 {
		t.Fatalf("after delete: %v", c)
	}
	if g := gmailEntry(t, api); g["account"] != "a@example.com" || len(g["accounts"].([]any)) != 1 {
		t.Fatalf("gmail changed: %v", g)
	}
}

func TestContactsAndDriveAccountRoutes(t *testing.T) {
	api, _, _ := newConnectorAPI(t)
	people := seedAccounts(t, api, "contacts", "a@example.com", "b@example.com")
	drive := seedAccounts(t, api, "drive", "a@example.com")
	mail := seedGmail(t, api, "a@example.com")
	cal := seedAccounts(t, api, "calendar", "a@example.com")

	for _, name := range []string{"contacts", "drive"} {
		c := connectorEntry(t, api, name)
		if list, _ := c["accounts"].([]any); c["kind"] != name || c["account"] != "a@example.com" || len(list) == 0 || list[0].(map[string]any)["primary"] != true {
			t.Fatalf("%s: %v", name, c)
		}
	}
	for _, r := range []struct{ method, path string }{
		{"POST", "/v1/connectors/gmail/accounts/" + people[0].ID.String() + "/primary"},
		{"DELETE", "/v1/connectors/calendar/accounts/" + people[0].ID.String()},
		{"DELETE", "/v1/connectors/drive/accounts/" + people[0].ID.String()},
		{"POST", "/v1/connectors/contacts/accounts/" + drive[0].ID.String() + "/authorize"},
		{"DELETE", "/v1/connectors/contacts/accounts/" + mail[0].ID.String()},
		{"POST", "/v1/connectors/drive/accounts/" + cal[0].ID.String() + "/primary"},
		{"DELETE", "/v1/connectors/drive/accounts/" + mail[0].ID.String()},
	} {
		if code, _ := api.do(r.method, r.path, ""); code != 404 {
			t.Errorf("%s %s: %d", r.method, r.path, code)
		}
	}
	if res := api.raw("DELETE", "/v1/connectors/contacts/accounts/"+people[0].ID.String(), ""); res.StatusCode != 204 {
		t.Fatalf("delete: %d", res.StatusCode)
	}
	if c := connectorEntry(t, api, "contacts"); c["account"] != "b@example.com" || len(c["accounts"].([]any)) != 1 {
		t.Fatalf("after delete: %v", c)
	}
}
