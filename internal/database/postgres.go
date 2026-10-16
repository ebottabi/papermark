/*
 * Copyright (c) 2026. Papermark - All Rights Reserved
 * Unauthorized copying of this file, via any medium is strictly prohibited
 * Proprietary and confidential
 * Written by Ebot Tabi <ebot.tabi@gmail.com>, 15/10/2026
 */

// This file owns the GORM handle, which is used SOLELY to run schema migrations
// (AutoMigrate). GORM is NOT used for querying anywhere in the app — all runtime
// queries go through the pgx connection pool (see pool.go) using raw SQL.
package database

import (
	"fmt"
	"log"
	"os"

	"papermark/internal/models"

	"gorm.io/driver/postgres"
	"gorm.io/gorm"
	"gorm.io/gorm/logger"
)

// PostgresConfig holds database connection configuration shared by the GORM
// migrator and the pgx query pool.
type PostgresConfig struct {
	Host     string
	Port     string
	User     string
	Password string
	DBName   string
	SSLMode  string

	// databaseURL, when set (e.g. by `fly postgres attach` via DATABASE_URL),
	// takes precedence over the individual fields above. Both GORM's postgres
	// driver and pgx accept a `postgres://` URL directly.
	databaseURL string
}

// LoadPostgresConfig loads PostgreSQL configuration from environment variables.
// It prefers DATABASE_URL (the convention used by fly.io, Heroku, Render, etc.)
// and falls back to individual DB_* variables for local development.
func LoadPostgresConfig() *PostgresConfig {
	if url := getEnv("DATABASE_URL", ""); url != "" {
		return &PostgresConfig{databaseURL: url}
	}
	return &PostgresConfig{
		Host:     getEnv("DB_HOST", "localhost"),
		Port:     getEnv("DB_PORT", "5432"),
		User:     getEnv("DB_USER", "papermark"),
		Password: getEnv("DB_PASSWORD", ""),
		DBName:   getEnv("DB_NAME", "papermark"),
		SSLMode:  getEnv("DB_SSLMODE", "disable"),
	}
}

// DSN returns a connection string understood by both GORM's postgres driver
// and pgx's pool parser. It returns DATABASE_URL verbatim when set, otherwise
// a keyword/value DSN built from the individual fields.
func (c *PostgresConfig) DSN() string {
	if c.databaseURL != "" {
		return c.databaseURL
	}
	return fmt.Sprintf(
		"host=%s port=%s user=%s password=%s dbname=%s sslmode=%s",
		c.Host, c.Port, c.User, c.Password, c.DBName, c.SSLMode,
	)
}

// InitMigrator opens a short-lived GORM connection used ONLY for migrations.
// Close it (CloseMigrator) right after RunMigrations — it must not serve traffic.
func InitMigrator(cfg *PostgresConfig) (*gorm.DB, error) {
	db, err := gorm.Open(postgres.Open(cfg.DSN()), &gorm.Config{
		Logger: logger.New(
			log.New(os.Stdout, "\r\n", log.LstdFlags),
			logger.Config{LogLevel: logger.Warn, Colorful: true},
		),
		SkipDefaultTransaction: true,
	})
	if err != nil {
		return nil, fmt.Errorf("failed to open migrator connection: %w", err)
	}
	return db, nil
}

// RunMigrations runs GORM auto-migration for all registered models.
func RunMigrations(db *gorm.DB) error {
	log.Println("Running database migrations...")
	if err := models.AutoMigrate(db); err != nil {
		return fmt.Errorf("failed to run migrations: %w", err)
	}
	log.Println("Database migrations completed")
	return nil
}

// CloseMigrator closes the GORM migrator connection. Call this immediately after
// migrations complete; the app serves traffic from the pgx pool, not GORM.
func CloseMigrator(db *gorm.DB) error {
	if db == nil {
		return nil
	}
	sqlDB, err := db.DB()
	if err != nil {
		return err
	}
	return sqlDB.Close()
}

func getEnv(key, def string) string {
	if v := os.Getenv(key); v != "" {
		return v
	}
	return def
}
