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

const deviceKeyColumns = `id, user_id, name, public_key, created_at, last_used_at, revoked_at`

func scanDeviceKey(row scannable) (*models.DeviceKey, error) {
	k := &models.DeviceKey{}
	if err := row.Scan(&k.ID, &k.UserID, &k.Name, &k.PublicKey, &k.CreatedAt, &k.LastUsedAt, &k.RevokedAt); err != nil {
		return nil, notFound(err)
	}
	return k, nil
}

// InsertDeviceKey stores a Face ID key for userID.
func InsertDeviceKey(ctx context.Context, db DBTX, userID uuid.UUID, name string, publicKey []byte) (*models.DeviceKey, error) {
	return scanDeviceKey(db.QueryRow(ctx, `INSERT INTO device_keys (id, user_id, name, public_key, created_at)
		VALUES ($1, $2, $3, $4, now()) RETURNING `+deviceKeyColumns, uuid.New(), userID, name, publicKey))
}

// GetLiveDeviceKey returns an unrevoked key whatever its owner; login proves ownership by signing.
func GetLiveDeviceKey(ctx context.Context, db DBTX, id uuid.UUID) (*models.DeviceKey, error) {
	return scanDeviceKey(db.QueryRow(ctx, `SELECT `+deviceKeyColumns+` FROM device_keys WHERE id = $1 AND revoked_at IS NULL`, id))
}

// ListDeviceKeys returns userID's unrevoked keys, oldest first.
func ListDeviceKeys(ctx context.Context, db DBTX, userID uuid.UUID) ([]*models.DeviceKey, error) {
	return list(ctx, db, scanDeviceKey, `SELECT `+deviceKeyColumns+` FROM device_keys
		WHERE user_id = $1 AND revoked_at IS NULL ORDER BY created_at, id`, userID)
}

// MarkDeviceKeyUsed records a login.
func MarkDeviceKeyUsed(ctx context.Context, db DBTX, id uuid.UUID, now time.Time) error {
	_, err := db.Exec(ctx, `UPDATE device_keys SET last_used_at = $2 WHERE id = $1`, id, now)
	return err
}

// RevokeDeviceKey revokes one of userID's keys; ErrNotFound when none is live.
func RevokeDeviceKey(ctx context.Context, db DBTX, userID, id uuid.UUID, now time.Time) error {
	tag, err := db.Exec(ctx, `UPDATE device_keys SET revoked_at = $3 WHERE id = $1 AND user_id = $2 AND revoked_at IS NULL`, id, userID, now)
	if err == nil && tag.RowsAffected() == 0 {
		return ErrNotFound
	}
	return err
}
