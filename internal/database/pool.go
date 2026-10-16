/*
 * Copyright (c) 2026. Papermark - All Rights Reserved
 * Unauthorized copying of this file, via any medium is strictly prohibited
 * Proprietary and confidential
 * Written by Ebot Tabi <ebot.tabi@gmail.com>, 15/10/2026
 */

// This file owns the pgx connection pool — the single source of database
// connections for ALL runtime queries. Models run raw SQL against this pool
// (see internal/models/*_queries.go). GORM is migrations-only (see postgres.go).
package database

import (
	"context"
	"fmt"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
)

// Pool is the package-level pgx pool, set by InitPool.
var Pool *pgxpool.Pool

// InitPool creates and verifies the pgx connection pool from the given config.
func InitPool(ctx context.Context, cfg *PostgresConfig) (*pgxpool.Pool, error) {
	poolCfg, err := pgxpool.ParseConfig(cfg.DSN())
	if err != nil {
		return nil, fmt.Errorf("failed to parse pool config: %w", err)
	}

	// Connection pool tuning — adjust to the host's resources / Postgres max_connections.
	poolCfg.MaxConns = 125
	poolCfg.MinConns = 5
	poolCfg.MaxConnLifetime = time.Hour
	poolCfg.MaxConnIdleTime = 30 * time.Minute
	poolCfg.HealthCheckPeriod = time.Minute

	pool, err := pgxpool.NewWithConfig(ctx, poolCfg)
	if err != nil {
		return nil, fmt.Errorf("failed to create connection pool: %w", err)
	}

	pingCtx, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()
	if err := pool.Ping(pingCtx); err != nil {
		pool.Close()
		return nil, fmt.Errorf("failed to ping database pool: %w", err)
	}

	Pool = pool
	return pool, nil
}

// ClosePool drains and closes the pool.
func ClosePool(pool *pgxpool.Pool) {
	if pool != nil {
		pool.Close()
	}
}

// PoolHealthCheck pings the pool (used by the readiness probe).
func PoolHealthCheck(ctx context.Context, pool *pgxpool.Pool) error {
	if pool == nil {
		return fmt.Errorf("pool is nil")
	}
	c, cancel := context.WithTimeout(ctx, 2*time.Second)
	defer cancel()
	return pool.Ping(c)
}
