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

const deviceColumns = `id, token, platform, environment, created_at, updated_at`

const (
	upsertDevice = `
		INSERT INTO devices (id, token, platform, environment, created_at, updated_at)
		VALUES ($1, $2, $3, $4, $5, $5)
		ON CONFLICT (token) WHERE deleted_at IS NULL
		DO UPDATE SET platform = EXCLUDED.platform, environment = EXCLUDED.environment, updated_at = EXCLUDED.updated_at
		RETURNING ` + deviceColumns

	listDevices = `SELECT ` + deviceColumns + ` FROM devices WHERE deleted_at IS NULL ORDER BY created_at ASC`

	softDeleteDeviceByToken = `UPDATE devices SET deleted_at = $2 WHERE token = $1 AND deleted_at IS NULL`
)

func scanDevice(row scannable) (*models.Device, error) {
	d := &models.Device{}
	if err := row.Scan(&d.ID, &d.Token, &d.Platform, &d.Environment, &d.CreatedAt, &d.UpdatedAt); err != nil {
		return nil, notFound(err)
	}
	return d, nil
}

// UpsertDevice registers a token, or updates the live row that already has it.
func UpsertDevice(ctx context.Context, db DBTX, token, platform, environment string) (*models.Device, error) {
	return scanDevice(db.QueryRow(ctx, upsertDevice, uuid.New(), token, platform, environment, time.Now().UTC()))
}

// ListDevices returns every live device, oldest first.
func ListDevices(ctx context.Context, db DBTX) ([]*models.Device, error) {
	rows, err := db.Query(ctx, listDevices)
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

// SoftDeleteDeviceByToken removes a token. It reports whether a live row matched.
func SoftDeleteDeviceByToken(ctx context.Context, db DBTX, token string) (bool, error) {
	tag, err := db.Exec(ctx, softDeleteDeviceByToken, token, time.Now().UTC())
	return tag.RowsAffected() > 0, err
}
