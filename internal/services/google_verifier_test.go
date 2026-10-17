/*
 * Copyright (c) 2026. Papermark - All Rights Reserved
 * Unauthorized copying of this file, via any medium is strictly prohibited
 * Proprietary and confidential
 * Written by Ebot Tabi <ebot.tabi@gmail.com>, 16/10/2026
 */

package services

import (
	"context"
	"crypto/rand"
	"crypto/rsa"
	"encoding/base64"
	"encoding/json"
	"errors"
	"math/big"
	"net/http"
	"net/http/httptest"
	"sync/atomic"
	"testing"
	"time"

	"github.com/golang-jwt/jwt/v5"
)

// googleStub serves a JWKS for a locally generated RSA key, standing in for
// Google's certs endpoint; no test talks to Google.
type googleStub struct {
	key     *rsa.PrivateKey
	kid     string
	fetches atomic.Int32
	srv     *httptest.Server
}

func newGoogleStub(t *testing.T) *googleStub {
	t.Helper()
	key, err := rsa.GenerateKey(rand.Reader, 2048)
	if err != nil {
		t.Fatal(err)
	}
	g := &googleStub{key: key, kid: "test-kid"}
	g.srv = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		g.fetches.Add(1)
		w.Header().Set("Cache-Control", "public, max-age=600, must-revalidate")
		json.NewEncoder(w).Encode(map[string]any{"keys": []map[string]string{{
			"kty": "RSA", "alg": "RS256", "use": "sig", "kid": g.kid,
			"n": base64.RawURLEncoding.EncodeToString(key.N.Bytes()),
			"e": base64.RawURLEncoding.EncodeToString(big.NewInt(int64(key.E)).Bytes()),
		}}})
	}))
	t.Cleanup(g.srv.Close)
	return g
}

// idClaims are a valid ID token's claims for audience "ios-client"; edit them per case.
func idClaims(sub string) jwt.MapClaims {
	now := time.Now()
	return jwt.MapClaims{
		"iss": "https://accounts.google.com", "aud": "ios-client", "sub": sub,
		"email": sub + "@gmail.com", "email_verified": true, "name": "Ada", "picture": "https://example.com/p.png",
		"iat": now.Unix(), "exp": now.Add(time.Hour).Unix(),
	}
}

func (g *googleStub) sign(t *testing.T, claims jwt.MapClaims) string {
	t.Helper()
	tok := jwt.NewWithClaims(jwt.SigningMethodRS256, claims)
	tok.Header["kid"] = g.kid
	s, err := tok.SignedString(g.key)
	if err != nil {
		t.Fatal(err)
	}
	return s
}

func TestGoogleVerifierChecksSignatureAndClaims(t *testing.T) {
	g := newGoogleStub(t)
	v := NewGoogleVerifier(g.srv.URL, []string{"web-client", "ios-client"})
	ctx := context.Background()

	id, err := v.Verify(ctx, g.sign(t, idClaims("sub-1")))
	if err != nil || id.Sub != "sub-1" || id.Email != "sub-1@gmail.com" || id.Name != "Ada" {
		t.Fatalf("valid token: %+v %v", id, err)
	}
	legacy := idClaims("sub-2")
	legacy["iss"], legacy["email_verified"] = "accounts.google.com", "true"
	if _, err := v.Verify(ctx, g.sign(t, legacy)); err != nil {
		t.Fatalf("legacy iss and string email_verified: %v", err)
	}

	other, _ := rsa.GenerateKey(rand.Reader, 2048)
	forged := jwt.NewWithClaims(jwt.SigningMethodRS256, idClaims("sub-1"))
	forged.Header["kid"] = g.kid
	forgedRaw, _ := forged.SignedString(other)
	hs := jwt.NewWithClaims(jwt.SigningMethodHS256, idClaims("sub-1"))
	hs.Header["kid"] = g.kid
	hsRaw, _ := hs.SignedString([]byte("secret"))

	bad := map[string]func(c jwt.MapClaims){
		"wrong audience":   func(c jwt.MapClaims) { c["aud"] = "someone-else" },
		"wrong issuer":     func(c jwt.MapClaims) { c["iss"] = "https://evil.example.com" },
		"expired":          func(c jwt.MapClaims) { c["exp"] = time.Now().Add(-2 * time.Hour).Unix() },
		"no expiry":        func(c jwt.MapClaims) { delete(c, "exp") },
		"unverified email": func(c jwt.MapClaims) { c["email_verified"] = false },
		"no email":         func(c jwt.MapClaims) { delete(c, "email") },
		"no subject":       func(c jwt.MapClaims) { delete(c, "sub") },
	}
	for name, edit := range bad {
		c := idClaims("sub-1")
		edit(c)
		if _, err := v.Verify(ctx, g.sign(t, c)); !errors.Is(err, errGoogleToken) {
			t.Errorf("%s: want errGoogleToken, got %v", name, err)
		}
	}
	for name, raw := range map[string]string{"other key": forgedRaw, "HS256": hsRaw, "garbage": "a.b.c"} {
		if _, err := v.Verify(ctx, raw); !errors.Is(err, errGoogleToken) {
			t.Errorf("%s: want errGoogleToken, got %v", name, err)
		}
	}

	if n := g.fetches.Load(); n != 1 {
		t.Fatalf("JWKS fetched %d times; max-age=600 should cache it", n)
	}
	v.now = func() time.Time { return time.Now().Add(11 * time.Minute) }
	c := idClaims("sub-1")
	c["exp"] = time.Now().Add(2 * time.Hour).Unix()
	if _, err := v.Verify(ctx, g.sign(t, c)); err != nil || g.fetches.Load() != 2 {
		t.Fatalf("after max-age: err %v, fetches %d", err, g.fetches.Load())
	}
}

func TestGoogleVerifierRequiresClientIDs(t *testing.T) {
	g := newGoogleStub(t)
	_, err := NewGoogleVerifier(g.srv.URL, nil).Verify(context.Background(), g.sign(t, idClaims("s")))
	if err == nil || errors.Is(err, errGoogleToken) {
		t.Fatalf("want a configuration error, got %v", err)
	}
}
