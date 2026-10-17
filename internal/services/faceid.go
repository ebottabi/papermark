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
	"crypto/sha256"
	"crypto/x509"
	"encoding/base64"
	"encoding/json"
	"errors"
	"time"

	"papermark/internal/models"
	"papermark/internal/models/queries"

	"github.com/google/uuid"
)

const (
	faceIDChallengeTTL = 2 * time.Minute
	kindFaceID         = "faceid"
	deviceNameMaxLen   = 64
)

// faceIDChallenge is what a Face ID challenge stores server-side.
type faceIDChallenge struct {
	KeyID     uuid.UUID `json:"keyId"`
	Challenge []byte    `json:"challenge"`
}

// parseP256 accepts a base64 P-256 public key as SPKI (DER) or X9.63
// (uncompressed point, as SecKeyCopyExternalRepresentation returns it).
func parseP256(b64 string) (*ecdsa.PublicKey, error) {
	raw, err := base64.StdEncoding.DecodeString(b64)
	if err != nil {
		return nil, errors.New("publicKey must be base64")
	}
	if pub, err := ecdsa.ParseUncompressedPublicKey(elliptic.P256(), raw); err == nil {
		return pub, nil
	}
	parsed, err := x509.ParsePKIXPublicKey(raw)
	if err != nil {
		return nil, errors.New("publicKey must be a P-256 key in X9.63 or SPKI form")
	}
	pub, ok := parsed.(*ecdsa.PublicKey)
	if !ok || pub.Curve != elliptic.P256() {
		return nil, errors.New("publicKey must be a P-256 key")
	}
	return pub, nil
}

// FaceIDRegister enrolls a signed-in user's Secure Enclave key.
func (s *AuthService) FaceIDRegister(ctx context.Context, userID uuid.UUID, name, publicKey string) (*models.DeviceKey, error) {
	if len(name) > deviceNameMaxLen {
		return nil, badRequest("deviceName must be at most %d characters", deviceNameMaxLen)
	}
	pub, err := parseP256(publicKey)
	if err != nil {
		return nil, badRequest("%v", err)
	}
	der, err := x509.MarshalPKIXPublicKey(pub)
	if err != nil {
		return nil, err
	}
	return queries.InsertDeviceKey(ctx, s.pool, userID, name, der)
}

// FaceIDChallenge issues a single-use challenge for a live key. The response
// does not reveal the key's owner.
func (s *AuthService) FaceIDChallenge(ctx context.Context, keyID uuid.UUID) (uuid.UUID, []byte, time.Time, error) {
	if _, err := queries.GetLiveDeviceKey(ctx, s.pool, keyID); err != nil {
		return uuid.Nil, nil, time.Time{}, err
	}
	ch := faceIDChallenge{KeyID: keyID, Challenge: randomBytes(32)}
	data, err := json.Marshal(ch)
	if err != nil {
		return uuid.Nil, nil, time.Time{}, err
	}
	now := s.now()
	expires := now.Add(faceIDChallengeTTL)
	id, err := queries.InsertAuthChallenge(ctx, s.pool, kindFaceID, nil, data, now, expires)
	return id, ch.Challenge, expires, err
}

// FaceIDLogin checks an ECDSA-SHA256 (DER) signature over the challenge bytes
// by the key, consumes the challenge and signs the key's owner in.
func (s *AuthService) FaceIDLogin(ctx context.Context, keyID, challengeID uuid.UUID, signature string) (*Session, error) {
	failed := unauthorized("Face ID sign-in failed")
	c, err := queries.TakeAuthChallenge(ctx, s.pool, challengeID, kindFaceID, s.now())
	if errors.Is(err, queries.ErrNotFound) {
		return nil, failed
	}
	if err != nil {
		return nil, err
	}
	var ch faceIDChallenge
	if err := json.Unmarshal(c.Data, &ch); err != nil {
		return nil, err
	}
	if ch.KeyID != keyID {
		return nil, failed
	}
	key, err := queries.GetLiveDeviceKey(ctx, s.pool, keyID)
	if errors.Is(err, queries.ErrNotFound) {
		return nil, failed
	}
	if err != nil {
		return nil, err
	}
	sig, err := base64.StdEncoding.DecodeString(signature)
	if err != nil {
		return nil, failed
	}
	parsed, err := x509.ParsePKIXPublicKey(key.PublicKey)
	if err != nil {
		return nil, err
	}
	digest := sha256.Sum256(ch.Challenge)
	if !ecdsa.VerifyASN1(parsed.(*ecdsa.PublicKey), digest[:], sig) {
		return nil, failed
	}
	if err := queries.MarkDeviceKeyUsed(ctx, s.pool, key.ID, s.now()); err != nil {
		return nil, err
	}
	user, err := queries.GetUser(ctx, s.pool, key.UserID)
	if err != nil {
		return nil, err
	}
	return s.newSession(ctx, user)
}

// DeviceKeys lists the user's live Face ID keys.
func (s *AuthService) DeviceKeys(ctx context.Context, userID uuid.UUID) ([]*models.DeviceKey, error) {
	return queries.ListDeviceKeys(ctx, s.pool, userID)
}

// RevokeDeviceKey revokes one of the user's Face ID keys.
func (s *AuthService) RevokeDeviceKey(ctx context.Context, userID, id uuid.UUID) error {
	return queries.RevokeDeviceKey(ctx, s.pool, userID, id, s.now())
}
