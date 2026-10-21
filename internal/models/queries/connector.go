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

const connectorColumns = `id, user_id, name, kind, url, status, error, tools, account, history_id, created_at, updated_at, tools_spec`

const (
	insertConnector = `
		INSERT INTO connectors (id, user_id, name, kind, url, status, error, tools, created_at, updated_at)
		VALUES ($1, $8, $2, $9, $3, $4, $5, $6, $7, $7)`

	selectConnectorByName = `SELECT ` + connectorColumns + ` FROM connectors WHERE user_id = $1 AND name = $2 AND deleted_at IS NULL`

	selectConnectorByID = `SELECT ` + connectorColumns + ` FROM connectors WHERE id = $1 AND deleted_at IS NULL`

	listConnectors = `SELECT ` + connectorColumns + ` FROM connectors WHERE user_id = $1 AND deleted_at IS NULL ORDER BY name ASC`

	updateConnectorStatus = `UPDATE connectors SET status = $2, error = $3, tools = $4, updated_at = $5, tools_spec = $6 WHERE id = $1`

	updateConnectorAccount = `UPDATE connectors SET account = $2, history_id = $3, updated_at = $4 WHERE id = $1`

	updateConnectorHistory = `UPDATE connectors SET history_id = $2, updated_at = $3 WHERE id = $1`

	softDeleteConnector = `UPDATE connectors SET deleted_at = $2 WHERE id = $1 AND deleted_at IS NULL`
)

func scanConnector(row scannable) (*models.Connector, error) {
	c := &models.Connector{}
	if err := row.Scan(&c.ID, &c.UserID, &c.Name, &c.Kind, &c.URL, &c.Status, &c.Error, &c.Tools, &c.Account, &c.HistoryID, &c.CreatedAt, &c.UpdatedAt, &c.ToolsSpec); err != nil {
		return nil, notFound(err)
	}
	return c, nil
}

// CreateConnector inserts c; ErrConflict when the user has a live connector with its name.
func CreateConnector(ctx context.Context, db DBTX, c *models.Connector) error {
	c.ID = uuid.New()
	if c.Kind == "" {
		c.Kind = models.ConnectorMCP
	}
	c.CreatedAt = time.Now().UTC()
	c.UpdatedAt = c.CreatedAt
	_, err := db.Exec(ctx, insertConnector, c.ID, c.Name, c.URL, c.Status, c.Error, c.Tools, c.CreatedAt, c.UserID, c.Kind)
	var pgErr *pgconn.PgError
	if errors.As(err, &pgErr) && pgErr.Code == "23505" {
		return ErrConflict
	}
	return err
}

// GetConnectorByName returns one of userID's live connectors.
func GetConnectorByName(ctx context.Context, db DBTX, userID uuid.UUID, name string) (*models.Connector, error) {
	return scanConnector(db.QueryRow(ctx, selectConnectorByName, userID, name))
}

// GetConnector returns one live connector by id whatever its owner; the OAuth
// callback reaches it through a state only its owner was given.
func GetConnector(ctx context.Context, db DBTX, id uuid.UUID) (*models.Connector, error) {
	return scanConnector(db.QueryRow(ctx, selectConnectorByID, id))
}

// ListConnectors returns userID's live connectors by name.
func ListConnectors(ctx context.Context, db DBTX, userID uuid.UUID) ([]*models.Connector, error) {
	rows, err := db.Query(ctx, listConnectors, userID)
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
	_, err := db.Exec(ctx, updateConnectorStatus, c.ID, c.Status, c.Error, c.Tools, c.UpdatedAt, c.ToolsSpec)
	return err
}

// UpdateConnectorAccount records a gmail connector's address and the history id its digests start from.
func UpdateConnectorAccount(ctx context.Context, db DBTX, c *models.Connector) error {
	c.UpdatedAt = time.Now().UTC()
	_, err := db.Exec(ctx, updateConnectorAccount, c.ID, c.Account, c.HistoryID, c.UpdatedAt)
	return err
}

// UpdateConnectorHistory stores the Gmail history id a digest reached.
func UpdateConnectorHistory(ctx context.Context, db DBTX, id uuid.UUID, historyID string) error {
	_, err := db.Exec(ctx, updateConnectorHistory, id, historyID, time.Now().UTC())
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
