/*
 * Copyright (c) 2026. Papermark - All Rights Reserved
 * Unauthorized copying of this file, via any medium is strictly prohibited
 * Proprietary and confidential
 * Written by Ebot Tabi <ebot.tabi@gmail.com>, 15/10/2026
 */

package services

import (
	"context"

	"papermark/internal/models"
	"papermark/internal/models/queries"

	"github.com/jackc/pgx/v5/pgxpool"
)

// DeviceService registers the APNs tokens push alerts go to.
type DeviceService struct {
	pool *pgxpool.Pool
}

// NewDeviceService constructs a DeviceService.
func NewDeviceService(pool *pgxpool.Pool) *DeviceService {
	return &DeviceService{pool: pool}
}

// Register stores token, or updates its platform and environment (idempotent by token).
func (s *DeviceService) Register(ctx context.Context, token, platform, environment string) (*models.Device, error) {
	return queries.UpsertDevice(ctx, s.pool, token, platform, environment)
}

// Unregister removes token. Removing an unknown token is not an error.
func (s *DeviceService) Unregister(ctx context.Context, token string) error {
	_, err := queries.SoftDeleteDeviceByToken(ctx, s.pool, token)
	return err
}
