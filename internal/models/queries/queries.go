/*
 * Copyright (c) 2026. Papermark - All Rights Reserved
 * Unauthorized copying of this file, via any medium is strictly prohibited
 * Proprietary and confidential
 * Written by Ebot Tabi <ebot.tabi@gmail.com>, 15/10/2026
 */

// Package queries holds all raw SQL data access, one file per model. Every
// function runs against the pgx pool (or a pgx.Tx) via the DBTX interface.
//
// IMPORTANT: this package is the ONLY way the app reads/writes Postgres at
// runtime. GORM (internal/database) is used solely for schema migrations and is
// never used to query data.
package queries

import (
	"context"
	"errors"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
)

// DBTX is the minimal query surface every query function depends on. Both
// *pgxpool.Pool and pgx.Tx satisfy it, so the same functions work for pooled
// connections and inside transactions.
type DBTX interface {
	Exec(ctx context.Context, sql string, args ...any) (pgconn.CommandTag, error)
	Query(ctx context.Context, sql string, args ...any) (pgx.Rows, error)
	QueryRow(ctx context.Context, sql string, args ...any) pgx.Row
}

// ErrNotFound is returned when no row matches.
var ErrNotFound = errors.New("record not found")

// notFound maps pgx's no-rows sentinel onto our domain error.
func notFound(err error) error {
	if errors.Is(err, pgx.ErrNoRows) {
		return ErrNotFound
	}
	return err
}

// scannable is anything with a Scan method (pgx.Row / pgx.Rows).
type scannable interface{ Scan(...any) error }

// ErrDuplicate is returned when an insert or update hits a unique constraint.
var ErrDuplicate = errors.New("duplicate record")

// duplicate maps a unique violation (SQLSTATE 23505) onto ErrDuplicate.
func duplicate(err error) error {
	var pgErr *pgconn.PgError
	if errors.As(err, &pgErr) && pgErr.Code == "23505" {
		return ErrDuplicate
	}
	return err
}
