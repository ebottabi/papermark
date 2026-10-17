/*
 * Copyright (c) 2026. Papermark - All Rights Reserved
 * Unauthorized copying of this file, via any medium is strictly prohibited
 * Proprietary and confidential
 * Written by Ebot Tabi <ebot.tabi@gmail.com>, 15/10/2026
 */

package queries

import (
	"context"
	"time"

	"papermark/internal/models"

	"github.com/google/uuid"
)

const deviceColumns = `id, user_id, token, platform, environment, created_at, updated_at`

const (
	upsertDevice = `
		INSERT INTO devices (id, user_id, token, platform, environment, created_at, updated_at)
		VALUES ($1, $6, $2, $3, $4, $5, $5)
		ON CONFLICT (token) WHERE deleted_at IS NULL
		DO UPDATE SET user_id = EXCLUDED.user_id, platform = EXCLUDED.platform, environment = EXCLUDED.environment, updated_at = EXCLUDED.updated_at
		RETURNING ` + deviceColumns

	listDevices = `SELECT ` + deviceColumns + ` FROM devices WHERE user_id = $1 AND deleted_at IS NULL ORDER BY created_at ASC`

	softDeleteDeviceByToken = `UPDATE devices SET deleted_at = $3 WHERE user_id = $1 AND token = $2 AND deleted_at IS NULL`
)

func scanDevice(row scannable) (*models.Device, error) {
	d := &models.Device{}
	if err := row.Scan(&d.ID, &d.UserID, &d.Token, &d.Platform, &d.Environment, &d.CreatedAt, &d.UpdatedAt); err != nil {
		return nil, notFound(err)
	}
	return d, nil
}

// UpsertDevice registers a token for userID, or updates the live row that
// already has it. A phone that signs in as someone else moves to them.
func UpsertDevice(ctx context.Context, db DBTX, userID uuid.UUID, token, platform, environment string) (*models.Device, error) {
	return scanDevice(db.QueryRow(ctx, upsertDevice, uuid.New(), token, platform, environment, time.Now().UTC(), userID))
}

// ListDevices returns userID's live devices, oldest first.
func ListDevices(ctx context.Context, db DBTX, userID uuid.UUID) ([]*models.Device, error) {
	rows, err := db.Query(ctx, listDevices, userID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []*models.Device
	for rows.Next() {
		d, err := scanDevice(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, d)
	}
	return out, rows.Err()
}

// ListTaskOwnerDevices returns the live devices of the user who owns taskID.
func ListTaskOwnerDevices(ctx context.Context, db DBTX, taskID uuid.UUID) ([]*models.Device, error) {
	var userID uuid.UUID
	if err := db.QueryRow(ctx, `SELECT user_id FROM tasks WHERE id = $1`, taskID).Scan(&userID); err != nil {
		return nil, notFound(err)
	}
	return ListDevices(ctx, db, userID)
}

// TaskOwnerAndTitle returns the user who owns taskID and the title of the
// routine that started it ("" for any other task).
func TaskOwnerAndTitle(ctx context.Context, db DBTX, taskID uuid.UUID) (user uuid.UUID, title string, err error) {
	err = db.QueryRow(ctx, `SELECT t.user_id, COALESCE(s.title, '') FROM tasks t
		LEFT JOIN schedules s ON s.id = t.schedule_id WHERE t.id = $1`, taskID).Scan(&user, &title)
	return user, title, notFound(err)
}

// SoftDeleteDeviceByToken removes one of userID's tokens. It reports whether a live row matched.
func SoftDeleteDeviceByToken(ctx context.Context, db DBTX, userID uuid.UUID, token string) (bool, error) {
	tag, err := db.Exec(ctx, softDeleteDeviceByToken, userID, token, time.Now().UTC())
	return tag.RowsAffected() > 0, err
}
