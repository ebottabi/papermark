/*
 * Copyright (c) 2026. Papermark - All Rights Reserved
 * Unauthorized copying of this file, via any medium is strictly prohibited
 * Proprietary and confidential
 * Written by Ebot Tabi <ebot.tabi@gmail.com>, 18/10/2026
 */
package controllers

import (
	"errors"
	"testing"
)

func TestCallbackReasonNamesADisabledGoogleAPI(t *testing.T) {
	err := errors.New("contacts: 403 People API has not been used in project 1 before or it is disabled.")
	if got := callbackReason(err); got != "Google hasn't switched this service on for Keen yet. Try again later." {
		t.Fatalf("%q", got)
	}
	if got := callbackReason(errors.New("token exchange: timeout")); got != "" {
		t.Fatalf("an unknown failure keeps the app's own message: %q", got)
	}
}
