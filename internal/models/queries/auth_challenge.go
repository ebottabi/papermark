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

// InsertAuthChallenge stores a begun ceremony and returns its id. It also
// sweeps ceremonies nobody finished.
func InsertAuthChallenge(ctx context.Context, db DBTX, kind string, userID *uuid.UUID, data []byte, now, expiresAt time.Time) (uuid.UUID, error) {
	if _, err := db.Exec(ctx, `DELETE FROM auth_challenges WHERE expires_at <= $1`, now); err != nil {
		return uuid.Nil, err
	}
	id := uuid.New()
	_, err := db.Exec(ctx, `INSERT INTO auth_challenges (id, kind, user_id, data, expires_at) VALUES ($1, $2, $3, $4, $5)`,
		id, kind, userID, string(data), expiresAt)
	return id, err
}

// TakeAuthChallenge deletes and returns the unexpired ceremony of kind with
// id, so each one is used at most once; ErrNotFound otherwise.
func TakeAuthChallenge(ctx context.Context, db DBTX, id uuid.UUID, kind string, now time.Time) (*models.AuthChallenge, error) {
	c := &models.AuthChallenge{}
	var data string
	err := db.QueryRow(ctx, `DELETE FROM auth_challenges WHERE id = $1 AND kind = $2 AND expires_at > $3
		RETURNING id, kind, user_id, data::text, expires_at`, id, kind, now).Scan(&c.ID, &c.Kind, &c.UserID, &data, &c.ExpiresAt)
	if err != nil {
		return nil, notFound(err)
	}
	c.Data = []byte(data)
	return c, nil
}
