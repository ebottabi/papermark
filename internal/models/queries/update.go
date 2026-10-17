/*
 * Copyright (c) 2026. Papermark - All Rights Reserved
 * Unauthorized copying of this file, via any medium is strictly prohibited
 * Proprietary and confidential
 * Written by Ebot Tabi <ebot.tabi@gmail.com>, 17/10/2026
 */

package queries

import (
	"context"
	"time"

	"papermark/internal/models"

	"github.com/google/uuid"
)

const updateColumns = `id, user_id, task_id, schedule_id, importance, title, body, sections, read_at, created_at`

func scanUpdate(row scannable) (*models.Update, error) {
	u := &models.Update{}
	if err := row.Scan(&u.ID, &u.UserID, &u.TaskID, &u.ScheduleID, &u.Importance, &u.Title, &u.Body, &u.Sections, &u.ReadAt, &u.CreatedAt); err != nil {
		return nil, notFound(err)
	}
	return u, nil
}

// InsertUpdate stores u, assigning its id and creation time (to the
// millisecond, the precision of the API's cursor).
func InsertUpdate(ctx context.Context, db DBTX, u *models.Update) error {
	u.ID = uuid.New()
	u.CreatedAt = time.Now().UTC().Truncate(time.Millisecond)
	if u.Sections == nil {
		u.Sections = []models.Section{}
	}
	_, err := db.Exec(ctx, `INSERT INTO updates (`+updateColumns+`) VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10)`,
		u.ID, u.UserID, u.TaskID, u.ScheduleID, u.Importance, u.Title, u.Body, u.Sections, u.ReadAt, u.CreatedAt)
	return err
}

// ListUpdates returns userID's feed (everything but quiet), newest first,
// strictly older than before when it is non-nil.
func ListUpdates(ctx context.Context, db DBTX, userID uuid.UUID, before *time.Time, limit int) ([]*models.Update, error) {
	return list(ctx, db, scanUpdate, `SELECT `+updateColumns+` FROM updates
		WHERE user_id = $1 AND importance <> 'quiet' AND ($2::timestamptz IS NULL OR created_at < $2)
		ORDER BY created_at DESC, id DESC LIMIT $3`, userID, before, limit)
}

// CountPushUpdatesSince counts the push updates userID got in [from, to).
func CountPushUpdatesSince(ctx context.Context, db DBTX, userID uuid.UUID, from, to time.Time) (n int, err error) {
	err = db.QueryRow(ctx, `SELECT count(*) FROM updates WHERE user_id = $1 AND importance = 'push'
		AND created_at >= $2 AND created_at < $3`, userID, from, to).Scan(&n)
	return n, err
}

// MarkUpdateRead marks one of userID's updates read; ErrNotFound when it is not theirs.
func MarkUpdateRead(ctx context.Context, db DBTX, userID, id uuid.UUID) error {
	tag, err := db.Exec(ctx, `UPDATE updates SET read_at = COALESCE(read_at, now()) WHERE id = $1 AND user_id = $2`, id, userID)
	if err == nil && tag.RowsAffected() == 0 {
		return ErrNotFound
	}
	return err
}

// MarkAllUpdatesRead marks every unread update of userID read.
func MarkAllUpdatesRead(ctx context.Context, db DBTX, userID uuid.UUID) error {
	_, err := db.Exec(ctx, `UPDATE updates SET read_at = now() WHERE user_id = $1 AND read_at IS NULL`, userID)
	return err
}
