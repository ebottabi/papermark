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
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"errors"
	"log"
	"time"

	"papermark/internal/config"
	"papermark/internal/models"
	"papermark/internal/models/queries"

	"github.com/go-webauthn/webauthn/webauthn"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"
)

// AuthService signs users in and resolves session tokens. Google creates the
// account and is the only first sign-in; a signed-in user may then enroll
// passkeys and Face ID keys, which sign in to that account and never create one.
type AuthService struct {
	pool       *pgxpool.Pool
	google     *GoogleVerifier
	webauthn   *webauthn.WebAuthn // nil when the WEBAUTHN_* config is unusable
	sessionTTL time.Duration
	now        func() time.Time
}

// NewAuthService builds the service; passkeys are off (with a warning) when
// the WebAuthn relying party config is invalid.
func NewAuthService(pool *pgxpool.Pool, cfg config.AuthConfig, google *GoogleVerifier) *AuthService {
	s := &AuthService{pool: pool, google: google, sessionTTL: cfg.SessionTTL, now: time.Now}
	wa, err := webauthn.New(&webauthn.Config{RPID: cfg.WebAuthnRPID, RPDisplayName: cfg.WebAuthnRPName, RPOrigins: cfg.WebAuthnOrigins})
	if err != nil {
		log.Printf("WARNING: passkeys are off: %v", err)
	} else {
		s.webauthn = wa
	}
	return s
}

// Session is a new sign-in: the bearer token (shown once) and its user.
type Session struct {
	Token     string
	ExpiresAt time.Time
	User      *models.User
	Created   bool // the sign-in created the account (Google only)
}

func unauthorized(msg string) error { return &StatusError{401, msg} }

func hashToken(token string) string {
	sum := sha256.Sum256([]byte(token))
	return hex.EncodeToString(sum[:])
}

func randomBytes(n int) []byte {
	b := make([]byte, n)
	rand.Read(b) // never fails (crypto/rand panics instead)
	return b
}

// newSession issues an opaque token for user and stores only its hash.
func (s *AuthService) newSession(ctx context.Context, user *models.User) (*Session, error) {
	now := s.now()
	if err := queries.DeleteExpiredSessions(ctx, s.pool, now); err != nil {
		return nil, err
	}
	token := base64.RawURLEncoding.EncodeToString(randomBytes(32))
	sess, err := queries.InsertSession(ctx, s.pool, user.ID, hashToken(token), now.Add(s.sessionTTL))
	if err != nil {
		return nil, err
	}
	return &Session{Token: token, ExpiresAt: sess.ExpiresAt, User: user}, nil
}

// GoogleSignIn verifies a Google ID token, creates its account on first use
// (or refreshes it) and signs the user in.
func (s *AuthService) GoogleSignIn(ctx context.Context, idToken string) (*Session, error) {
	id, err := s.google.Verify(ctx, idToken)
	if errors.Is(err, errGoogleToken) {
		return nil, unauthorized("invalid Google ID token")
	}
	if err != nil {
		return nil, err
	}
	user, created, err := queries.UpsertGoogleUser(ctx, s.pool, id.Sub, id.Email, id.Name, id.Picture)
	if errors.Is(err, queries.ErrDuplicate) {
		return nil, conflict("another account already uses %s", id.Email)
	}
	if err != nil {
		return nil, err
	}
	sess, err := s.newSession(ctx, user)
	if err != nil {
		return nil, err
	}
	sess.Created = created
	return sess, nil
}

// Authenticate returns the user of a live session token.
func (s *AuthService) Authenticate(ctx context.Context, token string) (uuid.UUID, error) {
	return queries.TouchSession(ctx, s.pool, hashToken(token), s.now())
}

// Logout revokes the session token.
func (s *AuthService) Logout(ctx context.Context, token string) error {
	return queries.DeleteSession(ctx, s.pool, hashToken(token))
}

// Me returns the signed-in user.
func (s *AuthService) Me(ctx context.Context, user uuid.UUID) (*models.User, error) {
	return queries.GetUser(ctx, s.pool, user)
}

// Methods reports which optional sign-in methods user has enrolled.
func (s *AuthService) Methods(ctx context.Context, user uuid.UUID) (passkey, faceID bool, err error) {
	return queries.GetUserAuthMethods(ctx, s.pool, user)
}
