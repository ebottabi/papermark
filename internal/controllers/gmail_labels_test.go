/*
 * Copyright (c) 2026. Papermark - All Rights Reserved
 * Unauthorized copying of this file, via any medium is strictly prohibited
 * Proprietary and confidential
 * Written by Ebot Tabi <ebot.tabi@gmail.com>, 17/10/2026
 */
package controllers_test

import (
	"testing"

	"papermark/internal/controllers"
	"papermark/internal/database"
	"papermark/internal/router"
	"papermark/internal/services"

	"github.com/google/uuid"
)

func TestGmailLabelsRoute(t *testing.T) {
	pool, _ := database.NewTestDatabase(t)
	cs := services.NewConnectorService(pool, publicURL, nil, nil)
	api := newAPI(t, &router.Config{Pool: pool, Connectors: controllers.NewConnectorController(cs),
		GmailLabels: controllers.NewGmailLabelController(services.NewGmailLabelService(pool, cs))})
	mine := seedGmail(t, api, "me@example.com")[0]
	other, token := newUser(t, pool)
	theirs := seedGmail(t, api.as(other, token), "them@example.com")[0]
	cal := seedAccounts(t, api, "calendar", "cal@example.com")[0]

	for path, want := range map[string]int{
		// Signed in without tokens, so Gmail is never reached and the app is told to reconnect.
		mine.ID.String():   409,
		theirs.ID.String(): 404,
		cal.ID.String():    404,
		uuid.NewString():   404,
		"not-a-uuid":       404,
	} {
		if code, body := api.do("GET", "/v1/connectors/gmail/accounts/"+path+"/labels", ""); code != want {
			t.Errorf("%s: %d %v", path, code, body)
		}
	}
}
