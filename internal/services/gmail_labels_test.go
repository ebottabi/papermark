/*
 * Copyright (c) 2026. Papermark - All Rights Reserved
 * Unauthorized copying of this file, via any medium is strictly prohibited
 * Proprietary and confidential
 * Written by Ebot Tabi <ebot.tabi@gmail.com>, 17/10/2026
 */
package services

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"testing"

	"papermark/internal/database"
	"papermark/internal/integrations/gmail"
	"papermark/internal/models"
	"papermark/internal/models/queries"

	"github.com/google/uuid"
)

func TestGmailLabelsList(t *testing.T) {
	pool, _ := database.NewTestDatabase(t)
	ctx := context.Background()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet || r.URL.Path != "/gmail/v1/users/me/labels" {
			http.NotFound(w, r)
			return
		}
		w.Write([]byte(`{"labels":[{"id":"INBOX","name":"INBOX","type":"system"},{"id":"Label_12","name":"Receipts","type":"user","messagesTotal":4}]}`))
	}))
	t.Cleanup(srv.Close)
	var asked []uuid.UUID
	svc := &GmailLabelService{pool: pool, client: func(_ context.Context, a *models.GmailAccount) (*gmail.Client, error) {
		asked = append(asked, a.ID)
		return &gmail.Client{HTTP: srv.Client(), BaseURL: srv.URL}, nil
	}}

	account := func(user uuid.UUID, kind string) *models.GmailAccount {
		t.Helper()
		c := &models.Connector{UserID: user, Name: kind, Kind: kind, URL: "x", Status: models.ConnectorConnected}
		if err := queries.CreateConnector(ctx, pool, c); err != nil {
			t.Fatal(err)
		}
		a := &models.GmailAccount{UserID: user, ConnectorID: c.ID, Email: kind + "@example.com", Status: models.ConnectorConnected, Primary: true}
		if err := queries.CreateGmailAccount(ctx, pool, a); err != nil {
			t.Fatal(err)
		}
		return a
	}
	me, other := testUser(t, pool), testUser(t, pool)
	mine, theirs, cal := account(me, models.ConnectorGmail), account(other, models.ConnectorGmail), account(me, models.ConnectorCalendar)

	labels, err := svc.List(ctx, me, mine.ID)
	if err != nil || len(labels) != 2 || labels[1] != (gmail.Label{ID: "Label_12", Name: "Receipts", Type: "user"}) {
		t.Fatalf("labels: %v %v", labels, err)
	}
	for name, id := range map[string]uuid.UUID{"other user": theirs.ID, "calendar": cal.ID, "missing": uuid.New()} {
		if _, err := svc.List(ctx, me, id); !errors.Is(err, queries.ErrNotFound) {
			t.Errorf("%s: %v", name, err)
		}
	}
	if len(asked) != 1 || asked[0] != mine.ID {
		t.Fatalf("gmail reached for %v", asked)
	}
}
