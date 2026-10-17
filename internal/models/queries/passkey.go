/*
 * Copyright (c) 2026. Papermark - All Rights Reserved
 * Unauthorized copying of this file, via any medium is strictly prohibited
 * Proprietary and confidential
 * Written by Ebot Tabi <ebot.tabi@gmail.com>, 16/10/2026
 */

package queries

import (
	"context"
	"time"

	"papermark/internal/models"

	"github.com/google/uuid"
)

const passkeyColumns = `id, user_id, credential_id, public_key, sign_count, backup_eligible, backup_state, transports, name, created_at, last_used_at`

func scanPasskey(row scannable) (*models.Passkey, error) {
	p := &models.Passkey{}
	err := row.Scan(&p.ID, &p.UserID, &p.CredentialID, &p.PublicKey, &p.SignCount, &p.BackupEligible, &p.BackupState, &p.Transports, &p.Name,
		&p.CreatedAt, &p.LastUsedAt)
	if err != nil {
		return nil, notFound(err)
	}
	return p, nil
}

// InsertPasskey stores p; ErrDuplicate when the credential is already registered.
func InsertPasskey(ctx context.Context, db DBTX, p *models.Passkey) error {
	p.ID = uuid.New()
	p.CreatedAt = time.Now().UTC()
	if p.Transports == nil {
		p.Transports = []string{}
	}
	_, err := db.Exec(ctx, `INSERT INTO passkeys (id, user_id, credential_id, public_key, sign_count,
		backup_eligible, backup_state, transports, name, created_at)
		VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10)`,
		p.ID, p.UserID, p.CredentialID, p.PublicKey, p.SignCount, p.BackupEligible, p.BackupState, p.Transports, p.Name, p.CreatedAt)
	return duplicate(err)
}

// ListPasskeys returns userID's passkeys, oldest first.
func ListPasskeys(ctx context.Context, db DBTX, userID uuid.UUID) ([]*models.Passkey, error) {
	return list(ctx, db, scanPasskey, `SELECT `+passkeyColumns+` FROM passkeys WHERE user_id = $1 ORDER BY created_at, id`, userID)
}

// GetPasskeyByCredentialID returns the passkey with a WebAuthn credential id.
func GetPasskeyByCredentialID(ctx context.Context, db DBTX, credentialID []byte) (*models.Passkey, error) {
	return scanPasskey(db.QueryRow(ctx, `SELECT `+passkeyColumns+` FROM passkeys WHERE credential_id = $1`, credentialID))
}

// MarkPasskeyUsed records a login, the authenticator's new sign count and backup state.
func MarkPasskeyUsed(ctx context.Context, db DBTX, id uuid.UUID, signCount int64, backupState bool, now time.Time) error {
	_, err := db.Exec(ctx, `UPDATE passkeys SET sign_count = $2, backup_state = $3, last_used_at = $4 WHERE id = $1`,
		id, signCount, backupState, now)
	return err
}

// DeletePasskey removes one of userID's passkeys; ErrNotFound when none matched.
func DeletePasskey(ctx context.Context, db DBTX, userID, id uuid.UUID) error {
	return deleteByID(ctx, db, "passkeys", userID, id)
}
