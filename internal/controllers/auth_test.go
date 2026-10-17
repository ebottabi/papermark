/*
 * Copyright (c) 2026. Papermark - All Rights Reserved
 * Unauthorized copying of this file, via any medium is strictly prohibited
 * Proprietary and confidential
 * Written by Ebot Tabi <ebot.tabi@gmail.com>, 16/10/2026
 */

package controllers_test

import (
	"context"
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/rsa"
	"crypto/sha256"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"math/big"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"papermark/internal/config"
	"papermark/internal/controllers"
	"papermark/internal/database"
	"papermark/internal/router"
	"papermark/internal/services"

	"github.com/golang-jwt/jwt/v5"
)

// fakeGoogleJWKS serves the public half of a locally generated RSA key as
// Google's certs endpoint and returns a signer for ID tokens.
func fakeGoogleJWKS(t *testing.T) (url string, sign func(claims jwt.MapClaims) string) {
	t.Helper()
	key, err := rsa.GenerateKey(rand.Reader, 2048)
	if err != nil {
		t.Fatal(err)
	}
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		json.NewEncoder(w).Encode(map[string]any{"keys": []map[string]string{{
			"kty": "RSA", "kid": "k1",
			"n": base64.RawURLEncoding.EncodeToString(key.N.Bytes()),
			"e": base64.RawURLEncoding.EncodeToString(big.NewInt(int64(key.E)).Bytes()),
		}}})
	}))
	t.Cleanup(srv.Close)
	return srv.URL, func(claims jwt.MapClaims) string {
		tok := jwt.NewWithClaims(jwt.SigningMethodRS256, claims)
		tok.Header["kid"] = "k1"
		s, err := tok.SignedString(key)
		if err != nil {
			t.Fatal(err)
		}
		return s
	}
}

func idToken(sub string) jwt.MapClaims {
	return jwt.MapClaims{
		"iss": "https://accounts.google.com", "aud": "ios-client", "sub": sub,
		"email": sub + "@gmail.com", "email_verified": true, "name": "Ada",
		"exp": time.Now().Add(time.Hour).Unix(),
	}
}

func newAuthAPI(t *testing.T) (*testAPI, func(jwt.MapClaims) string) {
	pool, _ := database.NewTestDatabase(t)
	certs, sign := fakeGoogleJWKS(t)
	cfg := config.AuthConfig{
		GoogleClientIDs: []string{"ios-client"}, SessionTTL: time.Hour,
		WebAuthnRPID: "keen.example.com", WebAuthnRPName: "Keen", WebAuthnOrigins: []string{"https://keen.example.com"},
	}
	svc := services.NewAuthService(pool, cfg, services.NewGoogleVerifier(certs, cfg.GoogleClientIDs))
	return newAPI(t, &router.Config{Pool: pool, Auth: controllers.NewAuthController(svc)}), sign
}

func TestGoogleSignInCreatesThenSignsIn(t *testing.T) {
	api, sign := newAuthAPI(t)
	anon := api.as(api.user, "")

	code, body := anon.do("POST", "/v1/auth/google", fmt.Sprintf(`{"idToken":%q}`, sign(idToken("g-1"))))
	d := body.data()
	user, _ := d["user"].(map[string]any)
	if code != 200 || d["created"] != true || d["token"] == "" || d["expiresAt"] == nil || user["email"] != "g-1@gmail.com" || user["name"] != "Ada" {
		t.Fatalf("first sign-in: %d %v", code, body)
	}
	first := d["token"].(string)
	code, body = anon.do("POST", "/v1/auth/google", fmt.Sprintf(`{"idToken":%q}`, sign(idToken("g-1"))))
	if again, _ := body.data()["user"].(map[string]any); code != 200 || body.data()["created"] != false || again["id"] != user["id"] {
		t.Fatalf("second sign-in: %d %v", code, body)
	}

	me := api.as(api.user, first)
	if code, body := me.do("GET", "/v1/me", ""); code != 200 || body.data()["id"] != user["id"] {
		t.Fatalf("me: %d %v", code, body)
	}
	if code, body := me.do("GET", "/v1/auth/methods", ""); code != 200 || body.data()["passkey"] != false || body.data()["faceId"] != false {
		t.Fatalf("methods: %d %v", code, body)
	}

	bad := idToken("g-2")
	bad["aud"] = "web-only"
	if code, body := anon.do("POST", "/v1/auth/google", fmt.Sprintf(`{"idToken":%q}`, sign(bad))); code != 401 || body["code"] != "UNAUTHORIZED" {
		t.Fatalf("bad audience: %d %v", code, body)
	}
	if code, _ := anon.do("POST", "/v1/auth/google", `{}`); code != 400 {
		t.Fatalf("missing idToken: %d", code)
	}
}

func TestSessionsExpireAndLogoutRevokes(t *testing.T) {
	api, _ := newAuthAPI(t)
	if code, _ := api.do("GET", "/v1/me", ""); code != 200 {
		t.Fatalf("live session: %d", code)
	}
	if code, _ := api.do("POST", "/v1/auth/logout", ""); code != 200 {
		t.Fatalf("logout: %d", code)
	}
	if code, _ := api.do("GET", "/v1/me", ""); code != 401 {
		t.Fatalf("revoked session: %d", code)
	}

	user, token := newUser(t, api.pool)
	other := api.as(user, token)
	if code, _ := other.do("GET", "/v1/me", ""); code != 200 {
		t.Fatalf("second session: %d", code)
	}
	if _, err := api.pool.Exec(context.Background(), `UPDATE sessions SET expires_at = now() - interval '1 second' WHERE user_id = $1`, user); err != nil {
		t.Fatal(err)
	}
	if code, body := other.do("GET", "/v1/me", ""); code != 401 || body["code"] != "UNAUTHORIZED" {
		t.Fatalf("expired session: %d %v", code, body)
	}
	if code, _ := api.as(user, "nonsense").do("GET", "/v1/me", ""); code != 401 {
		t.Fatalf("unknown token: %d", code)
	}
}

func TestPasskeyBeginOptions(t *testing.T) {
	api, _ := newAuthAPI(t)
	anon := api.as(api.user, "")

	if code, _ := anon.do("POST", "/v1/auth/passkeys/register/begin", ""); code != 401 {
		t.Fatalf("register begin without a session: %d", code)
	}
	code, body := api.do("POST", "/v1/auth/passkeys/register/begin", "")
	d := body.data()
	pk, _ := d["options"].(map[string]any)["publicKey"].(map[string]any)
	rp, _ := pk["rp"].(map[string]any)
	sel, _ := pk["authenticatorSelection"].(map[string]any)
	pkUser, _ := pk["user"].(map[string]any)
	userID, _ := base64.RawURLEncoding.DecodeString(fmt.Sprint(pkUser["id"]))
	if code != 200 || d["ceremonyId"] == nil || len(fmt.Sprint(pk["challenge"])) < 43 || rp["id"] != "keen.example.com" ||
		sel["residentKey"] != "required" || sel["userVerification"] != "required" || string(userID) != string(api.user[:]) {
		t.Fatalf("register begin: %d %v", code, body)
	}
	finish := fmt.Sprintf(`{"ceremonyId":%q,"name":"iPhone","credential":{"id":"x","type":"public-key"}}`, d["ceremonyId"])
	if code, _ := api.do("POST", "/v1/auth/passkeys/register/finish", finish); code != 400 {
		t.Fatalf("register finish with a bogus credential: %d", code)
	}
	if code, _ := api.do("POST", "/v1/auth/passkeys/register/finish", finish); code != 400 {
		t.Fatalf("register finish replay: %d", code)
	}

	code, body = anon.do("POST", "/v1/auth/passkeys/login/begin", "")
	d = body.data()
	pk, _ = d["options"].(map[string]any)["publicKey"].(map[string]any)
	if code != 200 || d["ceremonyId"] == nil || len(fmt.Sprint(pk["challenge"])) < 43 || pk["rpId"] != "keen.example.com" ||
		pk["userVerification"] != "required" || pk["allowCredentials"] != nil {
		t.Fatalf("login begin: %d %v", code, body)
	}
	login := fmt.Sprintf(`{"ceremonyId":%q,"credential":{"id":"x","type":"public-key"}}`, d["ceremonyId"])
	if code, _ := anon.do("POST", "/v1/auth/passkeys/login/finish", login); code != 401 && code != 400 {
		t.Fatalf("login finish with a bogus assertion: %d", code)
	}
	if code, _ := anon.do("POST", "/v1/auth/passkeys/login/finish", login); code != 400 {
		t.Fatalf("login finish replay: %d", code)
	}
	if code, body := api.do("GET", "/v1/auth/passkeys", ""); code != 200 || len(body["data"].([]any)) != 0 {
		t.Fatalf("list passkeys: %d %v", code, body)
	}
	if code, _ := api.do("DELETE", "/v1/auth/passkeys/00000000-0000-0000-0000-000000000000", ""); code != 404 {
		t.Fatalf("delete unknown passkey: %d", code)
	}
}

func TestFaceIDOverHTTP(t *testing.T) {
	api, _ := newAuthAPI(t)
	anon := api.as(api.user, "")
	priv, _ := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	raw, _ := priv.PublicKey.Bytes()

	code, body := api.do("POST", "/v1/auth/faceid/register", fmt.Sprintf(`{"deviceName":"iPhone","publicKey":%q}`, base64.StdEncoding.EncodeToString(raw)))
	if code != 201 || body.data()["name"] != "iPhone" {
		t.Fatalf("register: %d %v", code, body)
	}
	keyID := body.data()["id"].(string)
	if code, body := api.do("GET", "/v1/auth/methods", ""); code != 200 || body.data()["faceId"] != true {
		t.Fatalf("methods: %d %v", code, body)
	}

	code, body = anon.do("POST", "/v1/auth/faceid/challenge", fmt.Sprintf(`{"keyId":%q}`, keyID))
	if code != 200 {
		t.Fatalf("challenge: %d %v", code, body)
	}
	challenge, _ := base64.StdEncoding.DecodeString(body.data()["challenge"].(string))
	digest := sha256.Sum256(challenge)
	sig, _ := ecdsa.SignASN1(rand.Reader, priv, digest[:])
	req := fmt.Sprintf(`{"keyId":%q,"challengeId":%q,"signature":%q}`, keyID, body.data()["challengeId"], base64.StdEncoding.EncodeToString(sig))
	code, body = anon.do("POST", "/v1/auth/faceid/login", req)
	if user, _ := body.data()["user"].(map[string]any); code != 200 || body.data()["created"] != false || user["id"] != api.user.String() {
		t.Fatalf("login: %d %v", code, body)
	}
	if code, _ := anon.do("POST", "/v1/auth/faceid/login", req); code != 401 {
		t.Fatalf("replay: %d", code)
	}

	// An expired challenge is refused even with a good signature.
	_, body = anon.do("POST", "/v1/auth/faceid/challenge", fmt.Sprintf(`{"keyId":%q}`, keyID))
	challenge, _ = base64.StdEncoding.DecodeString(body.data()["challenge"].(string))
	digest = sha256.Sum256(challenge)
	sig, _ = ecdsa.SignASN1(rand.Reader, priv, digest[:])
	if _, err := api.pool.Exec(context.Background(), `UPDATE auth_challenges SET expires_at = now() - interval '1 second'`); err != nil {
		t.Fatal(err)
	}
	expired := fmt.Sprintf(`{"keyId":%q,"challengeId":%q,"signature":%q}`, keyID, body.data()["challengeId"], base64.StdEncoding.EncodeToString(sig))
	if code, _ := anon.do("POST", "/v1/auth/faceid/login", expired); code != 401 {
		t.Fatalf("expired challenge: %d", code)
	}

	other := api.as(newUser(t, api.pool))
	if code, _ := other.do("DELETE", "/v1/auth/faceid/"+keyID, ""); code != 404 {
		t.Fatalf("other user revokes: %d", code)
	}
	if code, body := other.do("GET", "/v1/auth/faceid", ""); code != 200 || len(body["data"].([]any)) != 0 {
		t.Fatalf("other user lists: %d %v", code, body)
	}
	if code, _ := api.do("DELETE", "/v1/auth/faceid/"+keyID, ""); code != 200 {
		t.Fatalf("revoke: %d", code)
	}
	if code, _ := anon.do("POST", "/v1/auth/faceid/challenge", fmt.Sprintf(`{"keyId":%q}`, keyID)); code != 404 {
		t.Fatalf("challenge for revoked key: %d", code)
	}
}
