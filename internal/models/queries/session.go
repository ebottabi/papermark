/*
 * Copyright (c) 2026. Papermark - All Rights Reserved
 * Unauthorized copying of this file, via any medium is strictly prohibited
 * Proprietary and confidential
 * Written by Ebot Tabi <ebot.tabi@gmail.com>, 16/10/2026
 */

package queries

import (
	"context"
	"time"

	"papermark/internal/models"

	"github.com/google/uuid"
)

// InsertSession stores a session for userID under the token's hash.
func InsertSession(ctx context.Context, db DBTX, userID uuid.UUID, tokenHash string, expiresAt time.Time) (*models.Session, error) {
	s := &models.Session{ID: uuid.New(), UserID: userID, TokenHash: tokenHash, ExpiresAt: expiresAt}
	err := db.QueryRow(ctx, `
		INSERT INTO sessions (id, user_id, token_hash, expires_at, last_used_at, created_at)
		VALUES ($1, $2, $3, $4, now(), now()) RETURNING last_used_at, created_at`,
		s.ID, s.UserID, s.TokenHash, s.ExpiresAt).Scan(&s.LastUsedAt, &s.CreatedAt)
	return s, err
}

// TouchSession returns the user of the unexpired session with tokenHash and
// records its use; ErrNotFound for an unknown, revoked or expired token.
func TouchSession(ctx context.Context, db DBTX, tokenHash string, now time.Time) (uuid.UUID, error) {
	var userID uuid.UUID
	err := db.QueryRow(ctx, `UPDATE sessions SET last_used_at = $2 WHERE token_hash = $1 AND expires_at > $2
		RETURNING user_id`, tokenHash, now).Scan(&userID)
	return userID, notFound(err)
}

// DeleteSession revokes the session with tokenHash.
func DeleteSession(ctx context.Context, db DBTX, tokenHash string) error {
	_, err := db.Exec(ctx, `DELETE FROM sessions WHERE token_hash = $1`, tokenHash)
	return err
}

// DeleteExpiredSessions removes sessions that expired before now.
func DeleteExpiredSessions(ctx context.Context, db DBTX, now time.Time) error {
	_, err := db.Exec(ctx, `DELETE FROM sessions WHERE expires_at <= $1`, now)
	return err
}
