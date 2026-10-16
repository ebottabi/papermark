/*
 * Copyright (c) 2026. Papermark - All Rights Reserved
 * Unauthorized copying of this file, via any medium is strictly prohibited
 * Proprietary and confidential
 * Written by Ebot Tabi <ebot.tabi@gmail.com>, 15/10/2026
 */

package database

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"errors"
	"fmt"
	"net/url"
	"os"
	"testing"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/redis/go-redis/v9"
)

// NewTestDatabase creates a throwaway, fully migrated database for one test
// and drops it on cleanup. The server comes from TEST_DATABASE_URL (default
// postgres://localhost:5432/postgres). It returns a pool on the new database
// and the database's URL for subprocesses. CREATE DATABASE costs seconds on
// Postgres 13; packages with many tests share one through RunWithTestDatabase.
func NewTestDatabase(t testing.TB) (*pgxpool.Pool, string) {
	t.Helper()
	pool, dsn, drop, err := createTestDatabase()
	if err != nil {
		t.Fatalf("test database: %v", err)
	}
	t.Cleanup(func() {
		pool.Close()
		if err := drop(); err != nil {
			t.Errorf("test database: %v", err)
		}
	})
	return pool, dsn
}

// RunWithTestDatabase runs a package's tests (call it from TestMain) against
// one throwaway database, passed to setup before the tests run.
func RunWithTestDatabase(m *testing.M, setup func(pool *pgxpool.Pool, dsn string)) int {
	pool, dsn, drop, err := createTestDatabase()
	if err != nil {
		fmt.Fprintf(os.Stderr, "test database: %v\n", err)
		return 1
	}
	setup(pool, dsn)
	code := m.Run()
	pool.Close()
	if err := drop(); err != nil {
		fmt.Fprintf(os.Stderr, "test database: %v\n", err)
	}
	return code
}

func createTestDatabase() (*pgxpool.Pool, string, func() error, error) {
	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	defer cancel()

	admin := os.Getenv("TEST_DATABASE_URL")
	if admin == "" {
		admin = "postgres://localhost:5432/postgres?sslmode=disable"
	}
	conn, err := pgx.Connect(ctx, admin)
	if err != nil {
		return nil, "", nil, fmt.Errorf("connect %s: %w", admin, err)
	}
	defer conn.Close(ctx)

	b := make([]byte, 6)
	_, _ = rand.Read(b)
	name := "papermark_test_" + hex.EncodeToString(b)
	if _, err := conn.Exec(ctx, "CREATE DATABASE "+name); err != nil {
		return nil, "", nil, fmt.Errorf("create: %w", err)
	}
	drop := func() error {
		ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
		defer cancel()
		conn, err := pgx.Connect(ctx, admin)
		if err != nil {
			return fmt.Errorf("drop connect: %w", err)
		}
		defer conn.Close(ctx)
		if _, err := conn.Exec(ctx, "DROP DATABASE IF EXISTS "+name+" WITH (FORCE)"); err != nil {
			return fmt.Errorf("drop %s: %w", name, err)
		}
		return nil
	}
	u, err := url.Parse(admin)
	if err != nil {
		return nil, "", nil, errors.Join(fmt.Errorf("parse url: %w", err), drop())
	}
	u.Path = "/" + name
	dsn := u.String()

	migrator, err := InitMigrator(&PostgresConfig{databaseURL: dsn})
	if err != nil {
		return nil, "", nil, errors.Join(fmt.Errorf("migrator: %w", err), drop())
	}
	err = RunMigrations(migrator)
	_ = CloseMigrator(migrator)
	if err != nil {
		return nil, "", nil, errors.Join(fmt.Errorf("migrate: %w", err), drop())
	}
	pool, err := pgxpool.New(ctx, dsn)
	if err != nil {
		return nil, "", nil, errors.Join(fmt.Errorf("pool: %w", err), drop())
	}
	return pool, dsn, drop, nil
}

// NewTestRedis connects to TEST_REDIS_URL (default redis://localhost:6379/0).
// Tests share the server, so callers namespace keys and channels by random ids.
func NewTestRedis(t testing.TB) *redis.Client {
	t.Helper()
	raw := os.Getenv("TEST_REDIS_URL")
	if raw == "" {
		raw = "redis://localhost:6379/0"
	}
	opt, err := redis.ParseURL(raw)
	if err != nil {
		t.Fatalf("test redis: %v", err)
	}
	client := redis.NewClient(opt)
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	if err := client.Ping(ctx).Err(); err != nil {
		t.Fatalf("test redis: ping %s: %v", raw, err)
	}
	t.Cleanup(func() { _ = client.Close() })
	return client
}
