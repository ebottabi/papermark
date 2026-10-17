/*
 * Copyright (c) 2026. Papermark - All Rights Reserved
 * Unauthorized copying of this file, via any medium is strictly prohibited
 * Proprietary and confidential
 * Written by Ebot Tabi <ebot.tabi@gmail.com>, 15/10/2026
 */
package queries

import (
	"context"
	"fmt"
	"time"

	"papermark/internal/models"

	"github.com/google/uuid"
)

const oauthCredentialColumns = `id, user_id, connector_id, account_id, resource, auth_url, token_url, scopes, client_id, client_secret,
	verifier, state_hash, state_expires_at, access_token, refresh_token, token_type, expiry, created_at, updated_at`

const (
	// A new authorization replaces the client and pending state but keeps the
	// current tokens, so the connector keeps working until the callback lands.
	// A credential is keyed by its account, or by its connector while it has no
	// account (mcp connectors, and a gmail account before its callback).
	upsertOAuthPendingHead = `
		INSERT INTO oauth_credentials (id, user_id, connector_id, account_id, resource, auth_url, token_url, scopes, client_id, client_secret,
			verifier, state_hash, state_expires_at, created_at, updated_at)
		VALUES ($1, $13, $2, $14, $3, $4, $5, $6, $7, $8, $9, $10, $11, $12, $12)`
	upsertOAuthPendingTail = `
		DO UPDATE SET resource = EXCLUDED.resource, auth_url = EXCLUDED.auth_url,
			token_url = EXCLUDED.token_url, scopes = EXCLUDED.scopes, client_id = EXCLUDED.client_id,
			client_secret = EXCLUDED.client_secret, verifier = EXCLUDED.verifier, state_hash = EXCLUDED.state_hash,
			state_expires_at = EXCLUDED.state_expires_at, updated_at = EXCLUDED.updated_at`
	upsertOAuthPendingByConnector = upsertOAuthPendingHead + ` ON CONFLICT (connector_id) WHERE account_id IS NULL` + upsertOAuthPendingTail
	upsertOAuthPendingByAccount   = upsertOAuthPendingHead + ` ON CONFLICT (account_id) WHERE account_id IS NOT NULL` + upsertOAuthPendingTail

	// Clearing the state in the statement that reads it makes a state single use.
	consumeOAuthState = `
		UPDATE oauth_credentials SET state_hash = '', state_expires_at = NULL, updated_at = $3
		WHERE state_hash = $1 AND state_hash <> '' AND state_expires_at > $2
		RETURNING ` + oauthCredentialColumns

	selectOAuthCredential        = `SELECT ` + oauthCredentialColumns + ` FROM oauth_credentials WHERE connector_id = $1 AND account_id IS NULL`
	selectAccountOAuthCredential = `SELECT ` + oauthCredentialColumns + ` FROM oauth_credentials WHERE account_id = $1`

	saveOAuthToken = `
		UPDATE oauth_credentials SET access_token = $2, refresh_token = $3, token_type = $4, expiry = $5,
			verifier = '', updated_at = $6
		WHERE %s`

	deleteOAuthCredential = `DELETE FROM oauth_credentials WHERE connector_id = $1`
)

func scanOAuthCredential(row scannable) (*models.OAuthCredential, error) {
	o := &models.OAuthCredential{}
	err := row.Scan(&o.ID, &o.UserID, &o.ConnectorID, &o.AccountID, &o.Resource, &o.AuthURL, &o.TokenURL, &o.Scopes, &o.ClientID, &o.ClientSecret,
		&o.Verifier, &o.StateHash, &o.StateExpiresAt, &o.AccessToken, &o.RefreshToken, &o.TokenType, &o.Expiry,
		&o.CreatedAt, &o.UpdatedAt)
	if err != nil {
		return nil, notFound(err)
	}
	return o, nil
}

// UpsertOAuthPending stores o's client registration and pending authorization.
func UpsertOAuthPending(ctx context.Context, db DBTX, o *models.OAuthCredential) error {
	o.ID = uuid.New()
	o.CreatedAt = time.Now().UTC()
	o.UpdatedAt = o.CreatedAt
	sql := upsertOAuthPendingByConnector
	if o.AccountID != nil {
		sql = upsertOAuthPendingByAccount
	}
	_, err := db.Exec(ctx, sql, o.ID, o.ConnectorID, o.Resource, o.AuthURL, o.TokenURL, o.Scopes,
		o.ClientID, o.ClientSecret, o.Verifier, o.StateHash, o.StateExpiresAt, o.CreatedAt, o.UserID, o.AccountID)
	return err
}

// ConsumeOAuthState returns the credential whose unexpired state hashes to
// stateHash and clears that state; ErrNotFound when none matches.
func ConsumeOAuthState(ctx context.Context, db DBTX, stateHash string, now time.Time) (*models.OAuthCredential, error) {
	return scanOAuthCredential(db.QueryRow(ctx, consumeOAuthState, stateHash, now, now.UTC()))
}

// GetOAuthCredential returns an mcp connector's credential, or a gmail
// connector's pending new-account credential.
func GetOAuthCredential(ctx context.Context, db DBTX, connectorID uuid.UUID) (*models.OAuthCredential, error) {
	return scanOAuthCredential(db.QueryRow(ctx, selectOAuthCredential, connectorID))
}

// GetAccountOAuthCredential returns a gmail account's credential.
func GetAccountOAuthCredential(ctx context.Context, db DBTX, accountID uuid.UUID) (*models.OAuthCredential, error) {
	return scanOAuthCredential(db.QueryRow(ctx, selectAccountOAuthCredential, accountID))
}

// SaveOAuthToken stores a connector's tokens and drops the used PKCE verifier.
func SaveOAuthToken(ctx context.Context, db DBTX, connectorID uuid.UUID, access, refresh, tokenType string, expiry *time.Time) error {
	_, err := db.Exec(ctx, fmt.Sprintf(saveOAuthToken, "connector_id = $1 AND account_id IS NULL"), connectorID, access, refresh, tokenType, expiry, time.Now().UTC())
	return err
}

// SaveAccountOAuthToken is SaveOAuthToken for a gmail account.
func SaveAccountOAuthToken(ctx context.Context, db DBTX, accountID uuid.UUID, access, refresh, tokenType string, expiry *time.Time) error {
	_, err := db.Exec(ctx, fmt.Sprintf(saveOAuthToken, "account_id = $1"), accountID, access, refresh, tokenType, expiry, time.Now().UTC())
	return err
}

// DeleteOAuthCredential removes every credential of a connector, whatever its account.
func DeleteOAuthCredential(ctx context.Context, db DBTX, connectorID uuid.UUID) error {
	_, err := db.Exec(ctx, `DELETE FROM oauth_credentials WHERE connector_id = $1`, connectorID)
	return err
}

// DeleteAccountOAuthCredential removes a gmail account's credential.
func DeleteAccountOAuthCredential(ctx context.Context, db DBTX, accountID uuid.UUID) error {
	_, err := db.Exec(ctx, `DELETE FROM oauth_credentials WHERE account_id = $1`, accountID)
	return err
}

// AttachOAuthCredential gives the credential id to a gmail account.
func AttachOAuthCredential(ctx context.Context, db DBTX, id, accountID uuid.UUID) error {
	_, err := db.Exec(ctx, `UPDATE oauth_credentials SET account_id = $2, updated_at = $3 WHERE id = $1`, id, accountID, time.Now().UTC())
	return err
}
