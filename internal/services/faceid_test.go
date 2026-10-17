/*
 * Copyright (c) 2026. Papermark - All Rights Reserved
 * Unauthorized copying of this file, via any medium is strictly prohibited
 * Proprietary and confidential
 * Written by Ebot Tabi <ebot.tabi@gmail.com>, 16/10/2026
 */

package services

import (
	"context"
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/sha256"
	"crypto/x509"
	"encoding/base64"
	"errors"
	"testing"
	"time"

	"papermark/internal/config"

	"github.com/google/uuid"
)

// enclaveKey stands in for the app's Secure Enclave key.
type enclaveKey struct{ priv *ecdsa.PrivateKey }

func newEnclaveKey(t *testing.T) enclaveKey {
	t.Helper()
	priv, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	return enclaveKey{priv}
}

// x963 is the key as SecKeyCopyExternalRepresentation returns it.
func (k enclaveKey) x963(t *testing.T) string {
	raw, err := k.priv.PublicKey.Bytes()
	if err != nil {
		t.Fatal(err)
	}
	return base64.StdEncoding.EncodeToString(raw)
}

func (k enclaveKey) spki(t *testing.T) string {
	der, err := x509.MarshalPKIXPublicKey(&k.priv.PublicKey)
	if err != nil {
		t.Fatal(err)
	}
	return base64.StdEncoding.EncodeToString(der)
}

// sign is what .ecdsaSignatureMessageX962SHA256 produces: a DER signature of sha256(message).
func (k enclaveKey) sign(t *testing.T, message []byte) string {
	digest := sha256.Sum256(message)
	sig, err := ecdsa.SignASN1(rand.Reader, k.priv, digest[:])
	if err != nil {
		t.Fatal(err)
	}
	return base64.StdEncoding.EncodeToString(sig)
}

func newTestAuth(t *testing.T) *AuthService {
	return NewAuthService(testPool, config.AuthConfig{SessionTTL: time.Hour, WebAuthnRPID: "localhost", WebAuthnRPName: "Keen", WebAuthnOrigins: []string{"https://localhost"}}, nil)
}

func isStatus(err error, status int) bool {
	var se *StatusError
	return errors.As(err, &se) && se.Status == status
}

func TestFaceIDLogin(t *testing.T) {
	s := newTestAuth(t)
	ctx := context.Background()
	alice, bob := testUser(t, testPool), testUser(t, testPool)
	aliceKey, bobKey := newEnclaveKey(t), newEnclaveKey(t)

	ak, err := s.FaceIDRegister(ctx, alice, "Alice's iPhone", aliceKey.x963(t))
	if err != nil {
		t.Fatal(err)
	}
	bk, err := s.FaceIDRegister(ctx, bob, "Bob's iPhone", bobKey.spki(t))
	if err != nil {
		t.Fatal(err)
	}
	for name, pub := range map[string]string{
		"not base64": "%%%",
		"P-384": func() string {
			k, _ := ecdsa.GenerateKey(elliptic.P384(), rand.Reader)
			d, _ := x509.MarshalPKIXPublicKey(&k.PublicKey)
			return base64.StdEncoding.EncodeToString(d)
		}(),
		"garbage": base64.StdEncoding.EncodeToString([]byte("not a key")),
	} {
		if _, err := s.FaceIDRegister(ctx, alice, "x", pub); !isStatus(err, 400) {
			t.Errorf("register %s: want 400, got %v", name, err)
		}
	}

	login := func(keyID uuid.UUID, key enclaveKey) (*Session, uuid.UUID, error) {
		cid, challenge, expires, err := s.FaceIDChallenge(ctx, keyID)
		if err != nil {
			t.Fatal(err)
		}
		if len(challenge) != 32 || time.Until(expires) > faceIDChallengeTTL {
			t.Fatalf("challenge: %d bytes, expires %v", len(challenge), expires)
		}
		sess, err := s.FaceIDLogin(ctx, keyID, cid, key.sign(t, challenge))
		return sess, cid, err
	}

	sess, cid, err := login(ak.ID, aliceKey)
	if err != nil || sess.User.ID != alice || sess.Created || sess.Token == "" {
		t.Fatalf("good signature: %+v %v", sess, err)
	}
	if got, err := s.Authenticate(ctx, sess.Token); err != nil || got != alice {
		t.Fatalf("session from Face ID: %v %v", got, err)
	}
	if _, err := s.FaceIDLogin(ctx, ak.ID, cid, "AAAA"); !isStatus(err, 401) {
		t.Fatalf("replayed challenge: %v", err)
	}
	if _, _, err := login(ak.ID, bobKey); !isStatus(err, 401) {
		t.Fatalf("wrong key: %v", err)
	}

	// Another user's key: a challenge for Bob's key signed by Alice fails,
	// and a challenge issued for Bob's key cannot be spent on Alice's.
	if _, _, err := login(bk.ID, aliceKey); !isStatus(err, 401) {
		t.Fatalf("other user's key: %v", err)
	}
	bcid, bch, _, _ := s.FaceIDChallenge(ctx, bk.ID)
	if _, err := s.FaceIDLogin(ctx, ak.ID, bcid, aliceKey.sign(t, bch)); !isStatus(err, 401) {
		t.Fatalf("challenge for another key: %v", err)
	}
	if err := s.RevokeDeviceKey(ctx, bob, ak.ID); !IsNotFound(err) {
		t.Fatalf("bob revokes alice's key: %v", err)
	}
	if keys, _ := s.DeviceKeys(ctx, bob); len(keys) != 1 || keys[0].ID != bk.ID {
		t.Fatalf("bob's keys: %+v", keys)
	}

	ecid, ech, _, _ := s.FaceIDChallenge(ctx, ak.ID)
	s.now = func() time.Time { return time.Now().Add(faceIDChallengeTTL + time.Second) }
	if _, err := s.FaceIDLogin(ctx, ak.ID, ecid, aliceKey.sign(t, ech)); !isStatus(err, 401) {
		t.Fatalf("expired challenge: %v", err)
	}
	s.now = time.Now

	if passkey, faceID, _ := s.Methods(ctx, alice); passkey || !faceID {
		t.Fatalf("methods: passkey %v faceID %v", passkey, faceID)
	}
	rcid, rch, _, _ := s.FaceIDChallenge(ctx, ak.ID)
	if err := s.RevokeDeviceKey(ctx, alice, ak.ID); err != nil {
		t.Fatal(err)
	}
	if _, err := s.FaceIDLogin(ctx, ak.ID, rcid, aliceKey.sign(t, rch)); !isStatus(err, 401) {
		t.Fatalf("revoked key: %v", err)
	}
	if _, _, _, err := s.FaceIDChallenge(ctx, ak.ID); !IsNotFound(err) {
		t.Fatalf("challenge for revoked key: %v", err)
	}
	if _, faceID, _ := s.Methods(ctx, alice); faceID {
		t.Fatal("methods still report Face ID after revoke")
	}
}
