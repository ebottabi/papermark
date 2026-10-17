/*
 * Copyright (c) 2026. Papermark - All Rights Reserved
 * Unauthorized copying of this file, via any medium is strictly prohibited
 * Proprietary and confidential
 * Written by Ebot Tabi <ebot.tabi@gmail.com>, 16/10/2026
 */

package queries

import (
	"context"

	"papermark/internal/models"

	"github.com/google/uuid"
)

const userColumns = `id, email, google_sub, name, picture, created_at, updated_at`

func scanUser(row scannable) (*models.User, error) {
	u := &models.User{}
	if err := row.Scan(&u.ID, &u.Email, &u.GoogleSub, &u.Name, &u.Picture, &u.CreatedAt, &u.UpdatedAt); err != nil {
		return nil, notFound(err)
	}
	return u, nil
}

// UpsertGoogleUser creates the user with googleSub, or refreshes its email,
// name and picture; created reports a new account. ErrDuplicate when another
// account already has the email.
func UpsertGoogleUser(ctx context.Context, db DBTX, googleSub, email, name, picture string) (u *models.User, created bool, err error) {
	u = &models.User{}
	err = db.QueryRow(ctx, `
		INSERT INTO users (id, email, google_sub, name, picture, created_at, updated_at)
		VALUES ($1, $2, $3, $4, $5, now(), now())
		ON CONFLICT (google_sub) DO UPDATE SET email = EXCLUDED.email, name = EXCLUDED.name,
			picture = EXCLUDED.picture, updated_at = now()
		RETURNING `+userColumns+`, xmax = 0`, uuid.New(), email, googleSub, name, picture).
		Scan(&u.ID, &u.Email, &u.GoogleSub, &u.Name, &u.Picture, &u.CreatedAt, &u.UpdatedAt, &created)
	if err != nil {
		return nil, false, duplicate(err)
	}
	return u, created, nil
}

// GetUser returns one live user.
func GetUser(ctx context.Context, db DBTX, id uuid.UUID) (*models.User, error) {
	return scanUser(db.QueryRow(ctx, `SELECT `+userColumns+` FROM users WHERE id = $1 AND deleted_at IS NULL`, id))
}

// GetUserAuthMethods reports whether userID has a passkey and a live Face ID key.
func GetUserAuthMethods(ctx context.Context, db DBTX, userID uuid.UUID) (passkey, faceID bool, err error) {
	err = db.QueryRow(ctx, `SELECT
		EXISTS (SELECT 1 FROM passkeys WHERE user_id = $1),
		EXISTS (SELECT 1 FROM device_keys WHERE user_id = $1 AND revoked_at IS NULL)`, userID).Scan(&passkey, &faceID)
	return passkey, faceID, err
}

// GetPreferences returns userID's assistant preferences.
func GetPreferences(ctx context.Context, db DBTX, userID uuid.UUID) (*models.Preferences, error) {
	p := &models.Preferences{}
	err := db.QueryRow(ctx, `SELECT timezone, quiet_start, quiet_end, push_limit, news_topics FROM users
		WHERE id = $1 AND deleted_at IS NULL`, userID).Scan(&p.Timezone, &p.QuietStart, &p.QuietEnd, &p.PushLimit, &p.NewsTopics)
	if err != nil {
		return nil, notFound(err)
	}
	if p.NewsTopics == nil {
		p.NewsTopics = []string{}
	}
	return p, nil
}

// SetPreferences stores p as userID's preferences.
func SetPreferences(ctx context.Context, db DBTX, userID uuid.UUID, p *models.Preferences) error {
	if p.NewsTopics == nil {
		p.NewsTopics = []string{}
	}
	tag, err := db.Exec(ctx, `UPDATE users SET timezone = $2, quiet_start = $3, quiet_end = $4, push_limit = $5,
		news_topics = $6, updated_at = now() WHERE id = $1 AND deleted_at IS NULL`,
		userID, p.Timezone, p.QuietStart, p.QuietEnd, p.PushLimit, p.NewsTopics)
	if err == nil && tag.RowsAffected() == 0 {
		return ErrNotFound
	}
	return err
}
