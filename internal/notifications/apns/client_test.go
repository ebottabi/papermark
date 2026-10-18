/*
 * Copyright (c) 2026. Papermark - All Rights Reserved
 * Unauthorized copying of this file, via any medium is strictly prohibited
 * Proprietary and confidential
 * Written by Ebot Tabi <ebot.tabi@gmail.com>, 15/10/2026
 */
package apns

import (
	"context"
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/sha256"
	"crypto/x509"
	"encoding/base64"
	"encoding/json"
	"encoding/pem"
	"errors"
	"io"
	"math/big"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"
)

type seen struct {
	host, path, auth, topic, pushType, body string
	proto                                   int
}

// newTestClient returns a client whose sandbox and production hosts are two
// HTTP/2 TLS test servers that record requests and answer with reply(token).
func newTestClient(t *testing.T, reply func(token string) (int, string)) (*Client, *ecdsa.PublicKey, func() []seen) {
	t.Helper()
	key, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	der, _ := x509.MarshalPKCS8PrivateKey(key)
	keyPath := filepath.Join(t.TempDir(), "AuthKey.p8")
	if err := os.WriteFile(keyPath, pem.EncodeToMemory(&pem.Block{Type: "PRIVATE KEY", Bytes: der}), 0o600); err != nil {
		t.Fatal(err)
	}
	c, err := NewClient(Config{KeyPath: keyPath, KeyID: "KEY123", TeamID: "TEAM456", Topic: "com.example.keen"})
	if err != nil {
		t.Fatal(err)
	}

	var mu sync.Mutex
	var log []seen
	tr := c.http.Transport.(*http.Transport)
	for _, env := range []string{EnvSandbox, EnvProduction} {
		srv := httptest.NewUnstartedServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			b, _ := io.ReadAll(r.Body)
			mu.Lock()
			log = append(log, seen{env, r.URL.Path, r.Header.Get("authorization"), r.Header.Get("apns-topic"), r.Header.Get("apns-push-type"), string(b), r.ProtoMajor})
			mu.Unlock()
			code, reason := reply(strings.TrimPrefix(r.URL.Path, "/3/device/"))
			w.WriteHeader(code)
			if reason != "" {
				_ = json.NewEncoder(w).Encode(map[string]string{"reason": reason})
			}
		}))
		srv.EnableHTTP2 = true
		srv.StartTLS()
		t.Cleanup(srv.Close)
		if tr.TLSClientConfig == nil {
			tr.TLSClientConfig = srv.Client().Transport.(*http.Transport).TLSClientConfig.Clone()
		}
		tr.TLSClientConfig.RootCAs.AddCert(srv.Certificate())
		c.hosts[env] = srv.URL
	}
	return c, &key.PublicKey, func() []seen { mu.Lock(); defer mu.Unlock(); return append([]seen(nil), log...) }
}

func verifyJWT(t *testing.T, pub *ecdsa.PublicKey, auth string) {
	t.Helper()
	tok, ok := strings.CutPrefix(auth, "bearer ")
	if !ok {
		t.Fatalf("authorization %q", auth)
	}
	parts := strings.Split(tok, ".")
	if len(parts) != 3 {
		t.Fatalf("jwt parts: %q", tok)
	}
	dec := base64.RawURLEncoding
	var header, claims map[string]any
	h, _ := dec.DecodeString(parts[0])
	cl, _ := dec.DecodeString(parts[1])
	_ = json.Unmarshal(h, &header)
	_ = json.Unmarshal(cl, &claims)
	if header["alg"] != "ES256" || header["kid"] != "KEY123" {
		t.Fatalf("jwt header %v", header)
	}
	if claims["iss"] != "TEAM456" || claims["iat"] == nil {
		t.Fatalf("jwt claims %v", claims)
	}
	sig, _ := dec.DecodeString(parts[2])
	if len(sig) != 64 {
		t.Fatalf("signature length %d", len(sig))
	}
	sum := sha256.Sum256([]byte(parts[0] + "." + parts[1]))
	r, s := new(big.Int).SetBytes(sig[:32]), new(big.Int).SetBytes(sig[32:])
	if !ecdsa.Verify(pub, sum[:], r, s) {
		t.Fatal("jwt signature does not verify")
	}
}

func TestSendRoutesByEnvironmentWithSignedJWTOverHTTP2(t *testing.T) {
	c, pub, requests := newTestClient(t, func(string) (int, string) { return 200, "" })
	ctx := context.Background()
	if err := c.Send(ctx, "tok-sb", EnvSandbox, Alert{Text: "Need input", TaskID: "t1", Type: "ask"}); err != nil {
		t.Fatal(err)
	}
	if err := c.Send(ctx, "tok-pr", EnvProduction, Alert{Text: "Done", TaskID: "t2", Type: "idle"}); err != nil {
		t.Fatal(err)
	}
	got := requests()
	if len(got) != 2 {
		t.Fatalf("requests: %+v", got)
	}
	want := []struct{ host, path, body string }{
		{EnvSandbox, "/3/device/tok-sb", `{"aps":{"alert":"Need input","sound":"default","category":"KEEN_ASK"},"taskId":"t1","type":"ask"}`},
		{EnvProduction, "/3/device/tok-pr", `{"aps":{"alert":"Done","sound":"default"},"taskId":"t2","type":"idle"}`},
	}
	for i, w := range want {
		g := got[i]
		if g.host != w.host || g.path != w.path || g.body != w.body {
			t.Errorf("request %d: got %s %s %s, want %s %s %s", i, g.host, g.path, g.body, w.host, w.path, w.body)
		}
		if g.proto != 2 || g.topic != "com.example.keen" || g.pushType != "alert" {
			t.Errorf("request %d: proto=%d topic=%q push-type=%q", i, g.proto, g.topic, g.pushType)
		}
		verifyJWT(t, pub, g.auth)
	}
	if got[0].auth != got[1].auth {
		t.Error("JWT not reused between sends")
	}
	if err := c.Send(ctx, "x", "staging", Alert{}); err == nil {
		t.Error("unknown environment accepted")
	}
}

func TestTokenRefreshesAfterTTL(t *testing.T) {
	c, pub, _ := newTestClient(t, func(string) (int, string) { return 200, "" })
	now := time.Now()
	a, _ := c.token(now)
	b, _ := c.token(now.Add(tokenTTL - time.Second))
	d, _ := c.token(now.Add(tokenTTL))
	if a != b || a == d {
		t.Fatalf("cache: a==b %v, a!=d %v", a == b, a != d)
	}
	verifyJWT(t, pub, "bearer "+d)
}

func TestErrorsClassifyDeadTokens(t *testing.T) {
	replies := map[string][2]any{
		"gone":       {410, "Unregistered"},
		"bad":        {400, "BadDeviceToken"},
		"unreg":      {400, "Unregistered"},
		"too-big":    {413, "PayloadTooLarge"},
		"throttled":  {429, "TooManyRequests"},
		"wrongtopic": {400, "DeviceTokenNotForTopic"},
	}
	c, _, _ := newTestClient(t, func(tok string) (int, string) { r := replies[tok]; return r[0].(int), r[1].(string) })
	for tok, r := range replies {
		err := c.Send(context.Background(), tok, EnvSandbox, Alert{Text: "x"})
		var ae *Error
		if !errors.As(err, &ae) || ae.Status != r[0].(int) || ae.Reason != r[1].(string) {
			t.Errorf("%s: err %v", tok, err)
		}
		wantDead := tok == "gone" || tok == "bad" || tok == "unreg"
		if IsUnregistered(err) != wantDead {
			t.Errorf("%s: IsUnregistered=%v want %v", tok, !wantDead, wantDead)
		}
	}
}

func TestConfigured(t *testing.T) {
	if (Config{KeyPath: "k", KeyID: "i", TeamID: "t"}).Configured() {
		t.Error("missing topic counted as configured")
	}
	if _, err := NewClient(Config{}); err == nil {
		t.Error("NewClient accepted an empty config")
	}
}
