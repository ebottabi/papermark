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

const contactColumns = `id, user_id, account_id, resource_name, saved, name, emails, phones, email_keys, phone_keys,
	organization, title, notes, relations, birthday, groups, etag, updated_at`

func scanContact(row scannable) (*models.Contact, error) {
	c := &models.Contact{}
	if err := row.Scan(&c.ID, &c.UserID, &c.AccountID, &c.ResourceName, &c.Saved, &c.Name, &c.Emails, &c.Phones, &c.EmailKeys, &c.PhoneKeys,
		&c.Organization, &c.Title, &c.Notes, &c.Relations, &c.Birthday, &c.Groups, &c.ETag, &c.UpdatedAt); err != nil {
		return nil, notFound(err)
	}
	return c, nil
}

// nonNil keeps a nil slice from being stored as JSON null.
func nonNil[T any](s []T) []T {
	if s == nil {
		return []T{}
	}
	return s
}

// UpsertContact inserts a contact or replaces the account's copy of the same resource.
func UpsertContact(ctx context.Context, db DBTX, c *models.Contact) error {
	if c.ID == uuid.Nil {
		c.ID = uuid.New()
	}
	_, err := db.Exec(ctx, `INSERT INTO contacts (`+contactColumns+`)
		VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10, $11, $12, $13, $14, $15, $16, $17, $18)
		ON CONFLICT (account_id, resource_name) DO UPDATE SET saved = EXCLUDED.saved, name = EXCLUDED.name,
			emails = EXCLUDED.emails, phones = EXCLUDED.phones, email_keys = EXCLUDED.email_keys, phone_keys = EXCLUDED.phone_keys,
			organization = EXCLUDED.organization, title = EXCLUDED.title, notes = EXCLUDED.notes, relations = EXCLUDED.relations,
			birthday = EXCLUDED.birthday, groups = EXCLUDED.groups, etag = EXCLUDED.etag, updated_at = EXCLUDED.updated_at`,
		c.ID, c.UserID, c.AccountID, c.ResourceName, c.Saved, c.Name, nonNil(c.Emails), nonNil(c.Phones), nonNil(c.EmailKeys), nonNil(c.PhoneKeys),
		c.Organization, c.Title, c.Notes, nonNil(c.Relations), c.Birthday, nonNil(c.Groups), c.ETag, c.UpdatedAt)
	return err
}

// DeleteContact removes one contact of an account.
func DeleteContact(ctx context.Context, db DBTX, accountID uuid.UUID, resourceName string) error {
	_, err := db.Exec(ctx, `DELETE FROM contacts WHERE account_id = $1 AND resource_name = $2`, accountID, resourceName)
	return err
}

// DeleteAccountContactsKind removes an account's saved (or other) contacts before a full sync rewrites them.
func DeleteAccountContactsKind(ctx context.Context, db DBTX, accountID uuid.UUID, saved bool) error {
	_, err := db.Exec(ctx, `DELETE FROM contacts WHERE account_id = $1 AND saved = $2`, accountID, saved)
	return err
}

// DeleteAccountContacts removes an account's contacts and its sync state.
func DeleteAccountContacts(ctx context.Context, db DBTX, accountID uuid.UUID) error {
	if _, err := db.Exec(ctx, `DELETE FROM contacts WHERE account_id = $1`, accountID); err != nil {
		return err
	}
	_, err := db.Exec(ctx, `DELETE FROM contact_syncs WHERE account_id = $1`, accountID)
	return err
}

// contactHaystack is the lowercased text a search token must occur in.
const contactHaystack = `lower(name || ' ' || organization || ' ' || title || ' ' || notes || ' ' || emails::text || ' ' || phones::text || ' ' || phone_keys::text)`

// SearchContacts returns up to limit contacts of user's accounts in which every
// token (lowercased) occurs in the name, an address, a number, the company,
// the title or the notes. Saved contacts and name matches come first.
// shortcut: substring scan over the user's rows; add a trigram index if contact lists grow past tens of thousands.
func SearchContacts(ctx context.Context, db DBTX, userID uuid.UUID, accounts []uuid.UUID, tokens []string, limit int) ([]*models.Contact, error) {
	return list(ctx, db, scanContact, `SELECT `+contactColumns+` FROM contacts
		WHERE user_id = $1 AND account_id = ANY($2)
			AND NOT EXISTS (SELECT 1 FROM unnest($3::text[]) t WHERE position(t IN `+contactHaystack+`) = 0)
		ORDER BY saved DESC, (lower(name) LIKE '%' || ($3::text[])[1] || '%') DESC, lower(name), resource_name
		LIMIT $4`, userID, accounts, tokens, limit)
}

// LookupContacts returns user's contacts with the address email (lowercased)
// or a number whose normalised key is one of phoneKeys. Saved contacts come first.
func LookupContacts(ctx context.Context, db DBTX, userID uuid.UUID, accounts []uuid.UUID, email string, phoneKeys []string) ([]*models.Contact, error) {
	return list(ctx, db, scanContact, `SELECT `+contactColumns+` FROM contacts
		WHERE user_id = $1 AND account_id = ANY($2)
			AND (($3 <> '' AND email_keys @> jsonb_build_array($3::text))
				OR EXISTS (SELECT 1 FROM unnest($4::text[]) k WHERE phone_keys @> jsonb_build_array(k)))
		ORDER BY saved DESC, lower(name), resource_name
		LIMIT 20`, userID, accounts, email, phoneKeys)
}

// GetContacts returns the copies of one resource in user's accounts.
func GetContacts(ctx context.Context, db DBTX, userID uuid.UUID, accounts []uuid.UUID, resourceName string) ([]*models.Contact, error) {
	return list(ctx, db, scanContact, `SELECT `+contactColumns+` FROM contacts
		WHERE user_id = $1 AND account_id = ANY($2) AND resource_name = $3 ORDER BY saved DESC`, userID, accounts, resourceName)
}

const contactSyncColumns = `account_id, user_id, connections_token, other_token, next_sync_at, locked_until, failures, last_error, synced_at`

func scanContactSync(row scannable) (*models.ContactSync, error) {
	s := &models.ContactSync{}
	if err := row.Scan(&s.AccountID, &s.UserID, &s.ConnectionsToken, &s.OtherToken, &s.NextSyncAt, &s.LockedUntil, &s.Failures, &s.LastError, &s.SyncedAt); err != nil {
		return nil, notFound(err)
	}
	return s, nil
}

// ScheduleContactSync makes an account's next sync due at at, creating its
// sync state on first use. A reconnect keeps the sync tokens and clears the failure streak.
func ScheduleContactSync(ctx context.Context, db DBTX, accountID, userID uuid.UUID, at time.Time) error {
	_, err := db.Exec(ctx, `INSERT INTO contact_syncs (account_id, user_id, next_sync_at) VALUES ($1, $2, $3)
		ON CONFLICT (account_id) DO UPDATE SET next_sync_at = LEAST(contact_syncs.next_sync_at, EXCLUDED.next_sync_at), failures = 0, last_error = ''`,
		accountID, userID, at)
	return err
}

// GetContactSync returns an account's sync state.
func GetContactSync(ctx context.Context, db DBTX, accountID uuid.UUID) (*models.ContactSync, error) {
	return scanContactSync(db.QueryRow(ctx, `SELECT `+contactSyncColumns+` FROM contact_syncs WHERE account_id = $1`, accountID))
}

// ClaimContactSyncs leases up to limit due accounts to the caller until
// now+lease. Rows another claimer holds are skipped, so two claimers never share one.
func ClaimContactSyncs(ctx context.Context, db DBTX, now time.Time, limit int, lease time.Duration) ([]*models.ContactSync, error) {
	return list(ctx, db, scanContactSync, `UPDATE contact_syncs SET locked_until = $2
		WHERE account_id IN (SELECT account_id FROM contact_syncs
			WHERE next_sync_at <= $1 AND (locked_until IS NULL OR locked_until < $1)
			ORDER BY next_sync_at LIMIT $3 FOR UPDATE SKIP LOCKED)
		RETURNING `+contactSyncColumns, now, now.Add(lease), limit)
}

// ReleaseContactSync ends a sync that stored nothing: it drops the lease,
// schedules the next sync and records the failure streak and error.
func ReleaseContactSync(ctx context.Context, db DBTX, accountID uuid.UUID, next time.Time, failures int, lastError string) error {
	_, err := db.Exec(ctx, `UPDATE contact_syncs SET locked_until = NULL, next_sync_at = $2, failures = $3, last_error = $4 WHERE account_id = $1`,
		accountID, next, failures, lastError)
	return err
}

// AdvanceContactSync is ReleaseContactSync for a sync that stored its changes and reached the given tokens.
func AdvanceContactSync(ctx context.Context, db DBTX, accountID uuid.UUID, connectionsToken, otherToken string, next, now time.Time) error {
	_, err := db.Exec(ctx, `UPDATE contact_syncs SET connections_token = $2, other_token = $3, locked_until = NULL, next_sync_at = $4,
		failures = 0, last_error = '', synced_at = $5 WHERE account_id = $1`, accountID, connectionsToken, otherToken, next, now)
	return err
}
