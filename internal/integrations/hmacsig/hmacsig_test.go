/*
 * Copyright (c) 2026. Papermark - All Rights Reserved
 * Unauthorized copying of this file, via any medium is strictly prohibited
 * Proprietary and confidential
 * Written by Ebot Tabi <ebot.tabi@gmail.com>, 19/10/2026
 */

package hmacsig

import (
	"testing"
	"time"
)

func TestVerify(t *testing.T) {
	now := time.Unix(1_800_000_000, 0)
	body := []byte(`{"id":"evt_1"}`)
	good := Sign("whsec_x", body, now)
	if err := Verify(good, body, "whsec_x", now.Add(time.Minute)); err != nil {
		t.Fatal(err)
	}
	if err := Verify("t=1800000000,v1=00,"+good[len("t=1800000000,"):], body, "whsec_x", now); err != nil {
		t.Errorf("a second v1 value should be accepted: %v", err)
	}
	for name, c := range map[string]struct {
		header, secret string
		body           []byte
		at             time.Time
	}{
		"tampered": {good, "whsec_x", []byte(`{"id":"evt_2"}`), now},
		"secret":   {good, "whsec_y", body, now},
		"stale":    {good, "whsec_x", body, now.Add(6 * time.Minute)},
		"empty":    {"", "whsec_x", body, now},
		"nosecret": {good, "", body, now},
	} {
		if Verify(c.header, c.body, c.secret, c.at) == nil {
			t.Errorf("%s accepted", name)
		}
	}
}
