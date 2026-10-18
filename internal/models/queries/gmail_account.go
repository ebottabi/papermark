/*
 * Copyright (c) 2026. Papermark - All Rights Reserved
 * Unauthorized copying of this file, via any medium is strictly prohibited
 * Proprietary and confidential
 * Written by Ebot Tabi <ebot.tabi@gmail.com>, 17/10/2026
 */
package queries

import (
	"context"
	"time"

	"papermark/internal/models"

	"github.com/google/uuid"
)

const gmailAccountColumns = `id, user_id, connector_id, email, status, error, is_primary, history_id, created_at, updated_at`

func scanGmailAccount(row scannable) (*models.GmailAccount, error) {
	a := &models.GmailAccount{}
	if err := row.Scan(&a.ID, &a.UserID, &a.ConnectorID, &a.Email, &a.Status, &a.Error, &a.Primary, &a.HistoryID, &a.CreatedAt, &a.UpdatedAt); err != nil {
		return nil, notFound(err)
	}
	return a, nil
}

// CreateGmailAccount inserts a; ErrDuplicate when the connector already has the address.
func CreateGmailAccount(ctx context.Context, db DBTX, a *models.GmailAccount) error {
	a.ID = uuid.New()
	a.CreatedAt = time.Now().UTC()
	a.UpdatedAt = a.CreatedAt
	_, err := db.Exec(ctx, `INSERT INTO gmail_accounts (`+gmailAccountColumns+`) VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $9)`,
		a.ID, a.UserID, a.ConnectorID, a.Email, a.Status, a.Error, a.Primary, a.HistoryID, a.CreatedAt)
	if err != nil {
		return duplicate(err)
	}
	return nil
}

// GetGmailAccount returns one of userID's accounts.
func GetGmailAccount(ctx context.Context, db DBTX, userID, id uuid.UUID) (*models.GmailAccount, error) {
	return scanGmailAccount(db.QueryRow(ctx, `SELECT `+gmailAccountColumns+` FROM gmail_accounts WHERE id = $1 AND user_id = $2`, id, userID))
}

// GetGmailAccountByID returns an account without an owner check; for workers.
func GetGmailAccountByID(ctx context.Context, db DBTX, id uuid.UUID) (*models.GmailAccount, error) {
	return scanGmailAccount(db.QueryRow(ctx, `SELECT `+gmailAccountColumns+` FROM gmail_accounts WHERE id = $1`, id))
}

// GetGmailAccountByEmail returns a connector's account for an address, ignoring case.
func GetGmailAccountByEmail(ctx context.Context, db DBTX, connectorID uuid.UUID, email string) (*models.GmailAccount, error) {
	return scanGmailAccount(db.QueryRow(ctx, `SELECT `+gmailAccountColumns+` FROM gmail_accounts WHERE connector_id = $1 AND lower(email) = lower($2)`, connectorID, email))
}

// AccountEmailElsewhere reports whether userID has the address on a connector other than connectorID.
func AccountEmailElsewhere(ctx context.Context, db DBTX, userID, connectorID uuid.UUID, email string) (bool, error) {
	var ok bool
	err := db.QueryRow(ctx, `SELECT EXISTS (SELECT 1 FROM gmail_accounts WHERE user_id = $1 AND connector_id <> $2 AND lower(email) = lower($3))`,
		userID, connectorID, email).Scan(&ok)
	return ok, err
}

// ListGmailAccounts returns a connector's accounts, oldest first.
func ListGmailAccounts(ctx context.Context, db DBTX, connectorID uuid.UUID) ([]*models.GmailAccount, error) {
	return list(ctx, db, scanGmailAccount, `SELECT `+gmailAccountColumns+` FROM gmail_accounts WHERE connector_id = $1 ORDER BY created_at, id`, connectorID)
}

// ListUserGmailAccounts returns userID's mailboxes (accounts of a live gmail
// connector, never calendar ones), oldest first.
func ListUserGmailAccounts(ctx context.Context, db DBTX, userID uuid.UUID) ([]*models.GmailAccount, error) {
	return list(ctx, db, scanGmailAccount, `SELECT `+gmailAccountColumns+` FROM gmail_accounts
		WHERE user_id = $1 AND connector_id IN (SELECT id FROM connectors WHERE user_id = $1 AND kind = 'gmail' AND deleted_at IS NULL)
		ORDER BY created_at, id`, userID)
}

// UpdateGmailAccountStatus records the outcome of the latest use of an account.
func UpdateGmailAccountStatus(ctx context.Context, db DBTX, id uuid.UUID, status string, errText *string) error {
	_, err := db.Exec(ctx, `UPDATE gmail_accounts SET status = $2, error = $3, updated_at = $4 WHERE id = $1`, id, status, errText, time.Now().UTC())
	return err
}

// UpdateGmailAccountHistory stores the Gmail history id a digest reached.
func UpdateGmailAccountHistory(ctx context.Context, db DBTX, id uuid.UUID, historyID string) error {
	_, err := db.Exec(ctx, `UPDATE gmail_accounts SET history_id = $2, updated_at = $3 WHERE id = $1`, id, historyID, time.Now().UTC())
	return err
}

// SetPrimaryGmailAccount makes id the connector's only primary account. It
// needs a transaction: the unique index allows one primary at a time.
func SetPrimaryGmailAccount(ctx context.Context, db DBTX, connectorID, id uuid.UUID) error {
	if _, err := db.Exec(ctx, `UPDATE gmail_accounts SET is_primary = false WHERE connector_id = $1 AND is_primary AND id <> $2`, connectorID, id); err != nil {
		return err
	}
	_, err := db.Exec(ctx, `UPDATE gmail_accounts SET is_primary = true, updated_at = $3 WHERE id = $2 AND connector_id = $1`, connectorID, id, time.Now().UTC())
	return err
}

// DeleteGmailAccount removes an account.
func DeleteGmailAccount(ctx context.Context, db DBTX, id uuid.UUID) error {
	_, err := db.Exec(ctx, `DELETE FROM gmail_accounts WHERE id = $1`, id)
	return err
}

// DeleteConnectorGmailAccounts removes every account of a connector.
func DeleteConnectorGmailAccounts(ctx context.Context, db DBTX, connectorID uuid.UUID) error {
	_, err := db.Exec(ctx, `DELETE FROM gmail_accounts WHERE connector_id = $1`, connectorID)
	return err
}
