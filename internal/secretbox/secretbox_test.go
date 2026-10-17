/*
 * Copyright (c) 2026. Papermark - All Rights Reserved
 * Unauthorized copying of this file, via any medium is strictly prohibited
 * Proprietary and confidential
 * Written by Ebot Tabi <ebot.tabi@gmail.com>, 15/10/2026
 */
package secretbox

import (
	"bytes"
	"encoding/base64"
	"strings"
	"testing"
)

func TestRoundTripAndPlaintextFallback(t *testing.T) {
	box, err := New(bytes.Repeat([]byte{7}, 32))
	if err != nil {
		t.Fatal(err)
	}
	sealed, err := box.Seal("ya29.token")
	if err != nil || !strings.HasPrefix(sealed, prefix) || strings.Contains(sealed, "ya29") {
		t.Fatalf("seal: %q %v", sealed, err)
	}
	if again, _ := box.Seal("ya29.token"); again == sealed {
		t.Fatal("nonce reused")
	}
	if got, err := box.Open(sealed); got != "ya29.token" || err != nil {
		t.Fatalf("open: %q %v", got, err)
	}
	if got, err := box.Open("legacy-plaintext"); got != "legacy-plaintext" || err != nil {
		t.Fatalf("plaintext row: %q %v", got, err)
	}
	if got, _ := box.Seal(""); got != "" {
		t.Fatalf("empty: %q", got)
	}
	other, _ := New(bytes.Repeat([]byte{8}, 32))
	if _, err := other.Open(sealed); err == nil {
		t.Fatal("wrong key opened the value")
	}
	var none *Box
	if _, err := none.Open(sealed); err == nil {
		t.Fatal("keyless box opened a sealed value")
	}
	if got, _ := none.Seal("x"); got != "x" {
		t.Fatalf("keyless seal: %q", got)
	}
}

func TestParseKey(t *testing.T) {
	good := base64.StdEncoding.EncodeToString(bytes.Repeat([]byte{1}, 32))
	if k, err := ParseKey(good); len(k) != 32 || err != nil {
		t.Fatalf("good: %v", err)
	}
	for _, bad := range []string{"!!", base64.StdEncoding.EncodeToString([]byte("short"))} {
		if _, err := ParseKey(bad); err == nil {
			t.Errorf("%q accepted", bad)
		}
	}
	if k, err := ParseKey(""); k != nil || err != nil {
		t.Fatalf("empty: %v %v", k, err)
	}
}
