/*
 * Copyright (c) 2026. Papermark - All Rights Reserved
 * Unauthorized copying of this file, via any medium is strictly prohibited
 * Proprietary and confidential
 * Written by Ebot Tabi <ebot.tabi@gmail.com>, 18/10/2026
 */

package queries

import (
	"context"
	"encoding/json"
	"strings"
	"time"

	"papermark/internal/models"

	"github.com/google/uuid"
)

const searchColumns = `id, user_id, task_id, route, dates, passengers, cabin, offers::text, created_at, expires_at`

func scanSearch(row scannable) (*models.FlightSearch, error) {
	s, offers := &models.FlightSearch{}, ""
	if err := row.Scan(&s.ID, &s.UserID, &s.TaskID, &s.Route, &s.Dates, &s.Passengers, &s.Cabin, &offers, &s.CreatedAt, &s.ExpiresAt); err != nil {
		return nil, notFound(err)
	}
	s.Offers = json.RawMessage(offers)
	return s, nil
}

// InsertFlightSearch stores s, assigning its id and creation time.
func InsertFlightSearch(ctx context.Context, db DBTX, s *models.FlightSearch) error {
	s.ID, s.CreatedAt = uuid.New(), time.Now().UTC()
	_, err := db.Exec(ctx, `INSERT INTO flight_searches (id, user_id, task_id, route, dates, passengers, cabin, offers, created_at, expires_at)
		VALUES ($1, $2, $3, $4, $5, $6, $7, $8::jsonb, $9, $10)`,
		s.ID, s.UserID, s.TaskID, s.Route, s.Dates, s.Passengers, s.Cabin, string(s.Offers), s.CreatedAt, s.ExpiresAt)
	return err
}

// GetFlightSearch returns one of userID's searches.
func GetFlightSearch(ctx context.Context, db DBTX, userID, id uuid.UUID) (*models.FlightSearch, error) {
	return scanSearch(db.QueryRow(ctx, `SELECT `+searchColumns+` FROM flight_searches WHERE id = $1 AND user_id = $2`, id, userID))
}

const travellerColumns = `id, user_id, is_self, data, created_at, updated_at`

func scanTraveller(row scannable) (*models.TravellerProfile, error) {
	p := &models.TravellerProfile{}
	if err := row.Scan(&p.ID, &p.UserID, &p.IsSelf, &p.Data, &p.CreatedAt, &p.UpdatedAt); err != nil {
		return nil, notFound(err)
	}
	return p, nil
}

// ClearSelfTraveller unmarks userID's other self profile, so at most one is self.
func ClearSelfTraveller(ctx context.Context, db DBTX, userID, except uuid.UUID) error {
	_, err := db.Exec(ctx, `UPDATE traveller_profiles SET is_self = false WHERE user_id = $1 AND id <> $2 AND is_self`, userID, except)
	return err
}

// InsertTraveller stores p, assigning its id and timestamps.
func InsertTraveller(ctx context.Context, db DBTX, p *models.TravellerProfile) error {
	p.ID, p.CreatedAt = uuid.New(), time.Now().UTC()
	p.UpdatedAt = p.CreatedAt
	_, err := db.Exec(ctx, `INSERT INTO traveller_profiles (`+travellerColumns+`) VALUES ($1, $2, $3, $4, $5, $6)`,
		p.ID, p.UserID, p.IsSelf, p.Data, p.CreatedAt, p.UpdatedAt)
	return err
}

// ListTravellers returns userID's profiles, oldest first.
func ListTravellers(ctx context.Context, db DBTX, userID uuid.UUID) ([]*models.TravellerProfile, error) {
	return list(ctx, db, scanTraveller, `SELECT `+travellerColumns+` FROM traveller_profiles WHERE user_id = $1 ORDER BY created_at, id`, userID)
}

// GetTraveller returns one of userID's profiles.
func GetTraveller(ctx context.Context, db DBTX, userID, id uuid.UUID) (*models.TravellerProfile, error) {
	return scanTraveller(db.QueryRow(ctx, `SELECT `+travellerColumns+` FROM traveller_profiles WHERE id = $1 AND user_id = $2`, id, userID))
}

// UpdateTraveller replaces a profile's flag and sealed data.
func UpdateTraveller(ctx context.Context, db DBTX, userID, id uuid.UUID, isSelf bool, data string) (*models.TravellerProfile, error) {
	return scanTraveller(db.QueryRow(ctx, `UPDATE traveller_profiles SET is_self = $3, data = $4, updated_at = now()
		WHERE id = $1 AND user_id = $2 RETURNING `+travellerColumns, id, userID, isSelf, data))
}

// DeleteTraveller deletes one of userID's profiles.
func DeleteTraveller(ctx context.Context, db DBTX, userID, id uuid.UUID) error {
	return deleteByID(ctx, db, "traveller_profiles", userID, id)
}

const checkoutColumns = `id, user_id, task_id, search_id, ref, status, mode, travellers::text, offer::text, extras::text, catalogue::text,
	booking_ref, pay_by, paid_at, departs_at, change, duffel_order_id, payment_id, payment_intent, reminded_pay_at, reminded_checkin_at,
	handoff_url, error, expires_at, created_at, updated_at`

func scanCheckout(row scannable) (*models.Checkout, error) {
	c, travellers, offer, extras, catalogue := &models.Checkout{}, "", "", "", ""
	if err := row.Scan(&c.ID, &c.UserID, &c.TaskID, &c.SearchID, &c.Ref, &c.Status, &c.Mode, &travellers, &offer, &extras, &catalogue,
		&c.BookingRef, &c.PayBy, &c.PaidAt, &c.DepartsAt, &c.Change, &c.DuffelOrderID, &c.PaymentID, &c.PaymentIntent, &c.RemindedPayAt, &c.RemindedCheckinAt,
		&c.HandoffURL, &c.Error, &c.ExpiresAt, &c.CreatedAt, &c.UpdatedAt); err != nil {
		return nil, notFound(err)
	}
	c.Travellers, c.Offer, c.Extras, c.Catalogue = json.RawMessage(travellers), json.RawMessage(offer), json.RawMessage(extras), json.RawMessage(catalogue)
	return c, nil
}

// InsertCheckout stores c, assigning its id and timestamps.
func InsertCheckout(ctx context.Context, db DBTX, c *models.Checkout) error {
	c.ID, c.CreatedAt = uuid.New(), time.Now().UTC()
	c.UpdatedAt = c.CreatedAt
	_, err := db.Exec(ctx, `INSERT INTO checkouts (`+strings.ReplaceAll(checkoutColumns, "::text", "")+`)
		VALUES ($1, $2, $3, $4, $5, $6, $7, $8::jsonb, $9::jsonb, $10::jsonb, $11::jsonb, $12, $13, $14, $15, $16, $17, $18, $19, $20, $21, $22, $23, $24, $25, $26)`,
		c.ID, c.UserID, c.TaskID, c.SearchID, c.Ref, c.Status, c.Mode, string(c.Travellers), string(c.Offer), jsonOr(c.Extras, "[]"), jsonOr(c.Catalogue, "{}"),
		c.BookingRef, c.PayBy, c.PaidAt, c.DepartsAt, c.Change, c.DuffelOrderID, c.PaymentID, c.PaymentIntent, c.RemindedPayAt, c.RemindedCheckinAt,
		c.HandoffURL, c.Error, c.ExpiresAt, c.CreatedAt, c.UpdatedAt)
	return err
}

func jsonOr(raw json.RawMessage, empty string) string {
	if len(raw) == 0 {
		return empty
	}
	return string(raw)
}

// GetCheckout returns one of userID's checkouts.
func GetCheckout(ctx context.Context, db DBTX, userID, id uuid.UUID) (*models.Checkout, error) {
	return scanCheckout(db.QueryRow(ctx, `SELECT `+checkoutColumns+` FROM checkouts WHERE id = $1 AND user_id = $2`, id, userID))
}

// GetCheckoutAny returns a checkout by id without an owner, for webhooks and the sweeper.
func GetCheckoutAny(ctx context.Context, db DBTX, id uuid.UUID) (*models.Checkout, error) {
	return scanCheckout(db.QueryRow(ctx, `SELECT `+checkoutColumns+` FROM checkouts WHERE id = $1`, id))
}

// GetCheckoutByOrder returns the checkout holding a Duffel order, for its webhook.
func GetCheckoutByOrder(ctx context.Context, db DBTX, orderID string) (*models.Checkout, error) {
	return scanCheckout(db.QueryRow(ctx, `SELECT `+checkoutColumns+` FROM checkouts WHERE duffel_order_id = $1`, orderID))
}

// ListRecentCheckouts returns userID's newest checkouts.
func ListRecentCheckouts(ctx context.Context, db DBTX, userID uuid.UUID, limit int) ([]*models.Checkout, error) {
	return list(ctx, db, scanCheckout, `SELECT `+checkoutColumns+` FROM checkouts WHERE user_id = $1 ORDER BY created_at DESC, id LIMIT $2`, userID, limit)
}

// ListTripCheckouts returns userID's live and finished trips: upcoming first by
// departure, then past ones newest first.
func ListTripCheckouts(ctx context.Context, db DBTX, userID uuid.UUID) ([]*models.Checkout, error) {
	return list(ctx, db, scanCheckout, `SELECT `+checkoutColumns+` FROM checkouts
		WHERE user_id = $1 AND status IN ('holding','held','paying','booked','cancelled')
		ORDER BY (departs_at IS NOT NULL AND departs_at >= now()) DESC,
			CASE WHEN departs_at >= now() THEN departs_at END ASC,
			COALESCE(departs_at, created_at) DESC, id`, userID)
}

// UpdateCheckout writes every mutable field of c.
func UpdateCheckout(ctx context.Context, db DBTX, c *models.Checkout) error {
	c.UpdatedAt = time.Now().UTC()
	tag, err := db.Exec(ctx, `UPDATE checkouts SET status = $3, mode = $4, travellers = $5::jsonb, offer = $6::jsonb, extras = $7::jsonb, catalogue = $8::jsonb,
		booking_ref = $9, pay_by = $10, paid_at = $11, departs_at = $12, change = $13, duffel_order_id = $14, payment_id = $15, payment_intent = $16,
		reminded_pay_at = $17, reminded_checkin_at = $18, handoff_url = $19, error = $20, expires_at = $21, updated_at = $22 WHERE id = $1 AND user_id = $2`,
		c.ID, c.UserID, c.Status, c.Mode, string(c.Travellers), string(c.Offer), jsonOr(c.Extras, "[]"), jsonOr(c.Catalogue, "{}"),
		c.BookingRef, c.PayBy, c.PaidAt, c.DepartsAt, c.Change, c.DuffelOrderID, c.PaymentID, c.PaymentIntent,
		c.RemindedPayAt, c.RemindedCheckinAt, c.HandoffURL, c.Error, c.ExpiresAt, c.UpdatedAt)
	if err == nil && tag.RowsAffected() == 0 {
		return ErrNotFound
	}
	return err
}

// SettleCheckout writes every mutable field of c, but only when its status is
// still "from": the compare-and-set that makes payment and booking run once.
func SettleCheckout(ctx context.Context, db DBTX, c *models.Checkout, from string) (bool, error) {
	c.UpdatedAt = time.Now().UTC()
	tag, err := db.Exec(ctx, `UPDATE checkouts SET status = $4, mode = $5, travellers = $6::jsonb, offer = $7::jsonb, extras = $8::jsonb, catalogue = $9::jsonb,
		booking_ref = $10, pay_by = $11, paid_at = $12, departs_at = $13, change = $14, duffel_order_id = $15, payment_id = $16, payment_intent = $17,
		reminded_pay_at = $18, reminded_checkin_at = $19, handoff_url = $20, error = $21, expires_at = $22, updated_at = $23
		WHERE id = $1 AND user_id = $2 AND status = $3`,
		c.ID, c.UserID, from, c.Status, c.Mode, string(c.Travellers), string(c.Offer), jsonOr(c.Extras, "[]"), jsonOr(c.Catalogue, "{}"),
		c.BookingRef, c.PayBy, c.PaidAt, c.DepartsAt, c.Change, c.DuffelOrderID, c.PaymentID, c.PaymentIntent,
		c.RemindedPayAt, c.RemindedCheckinAt, c.HandoffURL, c.Error, c.ExpiresAt, c.UpdatedAt)
	return err == nil && tag.RowsAffected() == 1, err
}

// MoveCheckout sets the status to "to" only when it is still "from", and
// reports whether it did: the compare-and-set that makes a hold run once.
func MoveCheckout(ctx context.Context, db DBTX, userID, id uuid.UUID, from, to string) (bool, error) {
	tag, err := db.Exec(ctx, `UPDATE checkouts SET status = $4, error = '', updated_at = now() WHERE id = $1 AND user_id = $2 AND status = $3`,
		id, userID, from, to)
	return err == nil && tag.RowsAffected() == 1, err
}

// ClaimStuckCheckouts returns checkouts stuck in holding or paying for longer
// than age, bumping updated_at so one sweeper claims each. SKIP LOCKED keeps
// two workers off the same row.
func ClaimStuckCheckouts(ctx context.Context, db DBTX, age time.Duration, limit int) ([]*models.Checkout, error) {
	return list(ctx, db, scanCheckout, `UPDATE checkouts SET updated_at = now() WHERE id IN (
		SELECT id FROM checkouts WHERE status IN ('holding','paying') AND updated_at < now() - $1::interval
		ORDER BY updated_at FOR UPDATE SKIP LOCKED LIMIT $2) RETURNING `+checkoutColumns,
		age.String(), limit)
}

// ClaimUncapturedCheckouts returns booked checkouts whose payment was not yet
// captured, so the sweeper retries the capture.
func ClaimUncapturedCheckouts(ctx context.Context, db DBTX, limit int) ([]*models.Checkout, error) {
	return list(ctx, db, scanCheckout, `UPDATE checkouts SET updated_at = now() WHERE id IN (
		SELECT id FROM checkouts WHERE status = 'booked' AND payment_intent <> '' AND paid_at IS NULL
		ORDER BY updated_at FOR UPDATE SKIP LOCKED LIMIT $1) RETURNING `+checkoutColumns, limit)
}

// ClaimExpiredHolds returns held checkouts past their pay-by, marking them
// expired so each is swept once.
func ClaimExpiredHolds(ctx context.Context, db DBTX, limit int) ([]*models.Checkout, error) {
	return list(ctx, db, scanCheckout, `UPDATE checkouts SET status = 'expired', updated_at = now() WHERE id IN (
		SELECT id FROM checkouts WHERE status = 'held' AND pay_by IS NOT NULL AND pay_by < now()
		ORDER BY pay_by FOR UPDATE SKIP LOCKED LIMIT $1) RETURNING `+checkoutColumns, limit)
}

// ClaimPayReminders returns held checkouts whose pay-by is within window and
// whose reminder has not been sent, setting reminded_pay_at so each fires once.
func ClaimPayReminders(ctx context.Context, db DBTX, window time.Duration, limit int) ([]*models.Checkout, error) {
	return list(ctx, db, scanCheckout, `UPDATE checkouts SET reminded_pay_at = now(), updated_at = now() WHERE id IN (
		SELECT id FROM checkouts WHERE status = 'held' AND reminded_pay_at IS NULL
		AND pay_by IS NOT NULL AND pay_by > now() AND pay_by < now() + $1::interval
		ORDER BY pay_by FOR UPDATE SKIP LOCKED LIMIT $2) RETURNING `+checkoutColumns, window.String(), limit)
}

// ClaimCheckinReminders returns booked checkouts departing within window whose
// check-in reminder has not been sent, setting reminded_checkin_at once.
func ClaimCheckinReminders(ctx context.Context, db DBTX, window time.Duration, limit int) ([]*models.Checkout, error) {
	return list(ctx, db, scanCheckout, `UPDATE checkouts SET reminded_checkin_at = now(), updated_at = now() WHERE id IN (
		SELECT id FROM checkouts WHERE status = 'booked' AND reminded_checkin_at IS NULL
		AND departs_at IS NOT NULL AND departs_at > now() AND departs_at < now() + $1::interval
		ORDER BY departs_at FOR UPDATE SKIP LOCKED LIMIT $2) RETURNING `+checkoutColumns, window.String(), limit)
}
