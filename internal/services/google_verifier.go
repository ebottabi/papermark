/*
 * Copyright (c) 2026. Papermark - All Rights Reserved
 * Unauthorized copying of this file, via any medium is strictly prohibited
 * Proprietary and confidential
 * Written by Ebot Tabi <ebot.tabi@gmail.com>, 16/10/2026
 */

package services

import (
	"context"
	"crypto/rsa"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"math/big"
	"net/http"
	"regexp"
	"strconv"
	"sync"
	"time"

	"github.com/golang-jwt/jwt/v5"
)

// GoogleCertsURL is Google's JWKS for ID tokens.
const GoogleCertsURL = "https://www.googleapis.com/oauth2/v3/certs"

// GoogleIdentity is what a verified Google ID token says about its user.
type GoogleIdentity struct {
	Sub, Email, Name, Picture string
}

// GoogleVerifier checks Google ID tokens: an RS256 signature by a key in
// Google's JWKS (cached for the response's Cache-Control max-age), the
// issuer, one of the configured audiences, expiry and a verified email.
type GoogleVerifier struct {
	certsURL  string
	audiences []string
	http      *http.Client
	now       func() time.Time

	mu        sync.Mutex
	keys      map[string]*rsa.PublicKey
	expires   time.Time
	lastFetch time.Time
}

// NewGoogleVerifier accepts tokens issued to any of audiences (OAuth client ids).
func NewGoogleVerifier(certsURL string, audiences []string) *GoogleVerifier {
	return &GoogleVerifier{certsURL: certsURL, audiences: audiences, http: &http.Client{Timeout: 10 * time.Second}, now: time.Now}
}

var errGoogleToken = errors.New("invalid google id token")

type googleClaims struct {
	jwt.RegisteredClaims
	Email         string `json:"email"`
	EmailVerified any    `json:"email_verified"` // a bool, or "true" in older tokens
	Name          string `json:"name"`
	Picture       string `json:"picture"`
}

// Verify returns the identity in idToken, or an error wrapping errGoogleToken.
func (v *GoogleVerifier) Verify(ctx context.Context, idToken string) (*GoogleIdentity, error) {
	if len(v.audiences) == 0 {
		return nil, errors.New("google sign-in is not configured (GOOGLE_CLIENT_IDS)")
	}
	var claims googleClaims
	parser := jwt.NewParser(jwt.WithValidMethods([]string{"RS256"}), jwt.WithExpirationRequired(),
		jwt.WithAudience(v.audiences...), jwt.WithTimeFunc(v.now), jwt.WithLeeway(time.Minute))
	_, err := parser.ParseWithClaims(idToken, &claims, func(t *jwt.Token) (any, error) {
		kid, _ := t.Header["kid"].(string)
		return v.key(ctx, kid)
	})
	if err != nil {
		return nil, fmt.Errorf("%w: %v", errGoogleToken, err)
	}
	if claims.Issuer != "accounts.google.com" && claims.Issuer != "https://accounts.google.com" {
		return nil, fmt.Errorf("%w: issuer %q", errGoogleToken, claims.Issuer)
	}
	if claims.Subject == "" || claims.Email == "" {
		return nil, fmt.Errorf("%w: missing sub or email", errGoogleToken)
	}
	if claims.EmailVerified != true && claims.EmailVerified != "true" {
		return nil, fmt.Errorf("%w: email not verified", errGoogleToken)
	}
	return &GoogleIdentity{Sub: claims.Subject, Email: claims.Email, Name: claims.Name, Picture: claims.Picture}, nil
}

// key returns the signing key kid, refetching the JWKS when the cache is
// stale or (at most every 30s, for a rotation) when kid is unknown.
func (v *GoogleVerifier) key(ctx context.Context, kid string) (*rsa.PublicKey, error) {
	v.mu.Lock()
	defer v.mu.Unlock()
	now := v.now()
	fresh := now.Before(v.expires)
	if k, ok := v.keys[kid]; ok && fresh {
		return k, nil
	}
	if !fresh || now.Sub(v.lastFetch) >= 30*time.Second {
		if err := v.fetch(ctx, now); err != nil {
			return nil, err
		}
	}
	if k, ok := v.keys[kid]; ok {
		return k, nil
	}
	return nil, fmt.Errorf("unknown key id %q", kid)
}

var maxAgeRe = regexp.MustCompile(`(?i)(?:^|[,\s])max-age=(\d+)`)

func (v *GoogleVerifier) fetch(ctx context.Context, now time.Time) error {
	v.lastFetch = now
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, v.certsURL, nil)
	if err != nil {
		return err
	}
	resp, err := v.http.Do(req)
	if err != nil {
		return fmt.Errorf("google jwks: %w", err)
	}
	defer resp.Body.Close()
	raw, err := io.ReadAll(io.LimitReader(resp.Body, 1<<20))
	if err != nil {
		return fmt.Errorf("google jwks: %w", err)
	}
	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("google jwks: status %d", resp.StatusCode)
	}
	var doc struct {
		Keys []struct {
			Kty, Kid, N, E string
		} `json:"keys"`
	}
	if err := json.Unmarshal(raw, &doc); err != nil {
		return fmt.Errorf("google jwks: %w", err)
	}
	keys := map[string]*rsa.PublicKey{}
	for _, k := range doc.Keys {
		if k.Kty != "RSA" || k.Kid == "" {
			continue
		}
		n, errN := base64.RawURLEncoding.DecodeString(k.N)
		e, errE := base64.RawURLEncoding.DecodeString(k.E)
		if errN != nil || errE != nil || len(e) == 0 || len(e) > 4 {
			continue
		}
		keys[k.Kid] = &rsa.PublicKey{N: new(big.Int).SetBytes(n), E: int(new(big.Int).SetBytes(e).Int64())}
	}
	if len(keys) == 0 {
		return errors.New("google jwks: no RSA keys")
	}
	maxAge := time.Hour
	if m := maxAgeRe.FindStringSubmatch(resp.Header.Get("Cache-Control")); m != nil {
		if s, err := strconv.Atoi(m[1]); err == nil {
			maxAge = time.Duration(s) * time.Second
		}
	}
	v.keys, v.expires = keys, now.Add(maxAge)
	return nil
}
