/*
 * Copyright (c) 2026. Papermark - All Rights Reserved
 * Unauthorized copying of this file, via any medium is strictly prohibited
 * Proprietary and confidential
 * Written by Ebot Tabi <ebot.tabi@gmail.com>, 15/10/2026
 */
package queries

import (
	"context"
	"errors"
	"time"

	"papermark/internal/models"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgconn"
)

// ErrConflict is returned when a unique constraint rejects a write.
var ErrConflict = errors.New("record already exists")

const connectorColumns = `id, name, url, status, error, tools, created_at, updated_at`

const (
	insertConnector = `
		INSERT INTO connectors (id, name, url, status, error, tools, created_at, updated_at)
		VALUES ($1, $2, $3, $4, $5, $6, $7, $7)`

	selectConnectorByName = `SELECT ` + connectorColumns + ` FROM connectors WHERE name = $1 AND deleted_at IS NULL`

	selectConnectorByID = `SELECT ` + connectorColumns + ` FROM connectors WHERE id = $1 AND deleted_at IS NULL`

	listConnectors = `SELECT ` + connectorColumns + ` FROM connectors WHERE deleted_at IS NULL ORDER BY name ASC`

	updateConnectorStatus = `UPDATE connectors SET status = $2, error = $3, tools = $4, updated_at = $5 WHERE id = $1`

	softDeleteConnector = `UPDATE connectors SET deleted_at = $2 WHERE id = $1 AND deleted_at IS NULL`
)

func scanConnector(row scannable) (*models.Connector, error) {
	c := &models.Connector{}
	if err := row.Scan(&c.ID, &c.Name, &c.URL, &c.Status, &c.Error, &c.Tools, &c.CreatedAt, &c.UpdatedAt); err != nil {
		return nil, notFound(err)
	}
	return c, nil
}

// CreateConnector inserts c; ErrConflict when a live connector has its name.
func CreateConnector(ctx context.Context, db DBTX, c *models.Connector) error {
	c.ID = uuid.New()
	c.CreatedAt = time.Now().UTC()
	c.UpdatedAt = c.CreatedAt
	_, err := db.Exec(ctx, insertConnector, c.ID, c.Name, c.URL, c.Status, c.Error, c.Tools, c.CreatedAt)
	var pgErr *pgconn.PgError
	if errors.As(err, &pgErr) && pgErr.Code == "23505" {
		return ErrConflict
	}
	return err
}

// GetConnectorByName returns one live connector.
func GetConnectorByName(ctx context.Context, db DBTX, name string) (*models.Connector, error) {
	return scanConnector(db.QueryRow(ctx, selectConnectorByName, name))
}

// GetConnector returns one live connector by id.
func GetConnector(ctx context.Context, db DBTX, id uuid.UUID) (*models.Connector, error) {
	return scanConnector(db.QueryRow(ctx, selectConnectorByID, id))
}

// ListConnectors returns every live connector by name.
func ListConnectors(ctx context.Context, db DBTX) ([]*models.Connector, error) {
	rows, err := db.Query(ctx, listConnectors)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []*models.Connector
	for rows.Next() {
		c, err := scanConnector(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, c)
	}
	return out, rows.Err()
}

// UpdateConnectorStatus records the outcome of a connection attempt on c.
func UpdateConnectorStatus(ctx context.Context, db DBTX, c *models.Connector) error {
	c.UpdatedAt = time.Now().UTC()
	_, err := db.Exec(ctx, updateConnectorStatus, c.ID, c.Status, c.Error, c.Tools, c.UpdatedAt)
	return err
}

// SoftDeleteConnector deletes connector id; ErrNotFound when it is not live.
func SoftDeleteConnector(ctx context.Context, db DBTX, id uuid.UUID) error {
	tag, err := db.Exec(ctx, softDeleteConnector, id, time.Now().UTC())
	if err == nil && tag.RowsAffected() == 0 {
		return ErrNotFound
	}
	return err
}
