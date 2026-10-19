/*
 * Copyright (c) 2026. Papermark - All Rights Reserved
 * Unauthorized copying of this file, via any medium is strictly prohibited
 * Proprietary and confidential
 * Written by Ebot Tabi <ebot.tabi@gmail.com>, 18/10/2026
 */

package models

import (
	"encoding/json"
	"time"

	"github.com/google/uuid"
)

// Checkout statuses.
const (
	CheckoutTravellers = "travellers"
	CheckoutReview     = "review"
	CheckoutHolding    = "holding"
	CheckoutHeld       = "held"
	CheckoutPaying     = "paying"
	CheckoutBooked     = "booked"
	CheckoutCancelled  = "cancelled"
	CheckoutHandoff    = "handoff"
	CheckoutFailed     = "failed"
	CheckoutExpired    = "expired"
)

// FlightSearch is one flight_search result. Offers is the normalised, ranked
// offer list (duffel.Offer), including Duffel ids that never reach clients.
type FlightSearch struct {
	ID         uuid.UUID       `gorm:"type:uuid;primary_key"`
	UserID     uuid.UUID       `gorm:"type:uuid;not null;index"`
	TaskID     uuid.UUID       `gorm:"type:uuid;not null"`
	Route      string          `gorm:"type:text;not null"`
	Dates      string          `gorm:"type:text;not null"`
	Passengers int             `gorm:"not null"`
	Cabin      string          `gorm:"type:varchar(16);not null"`
	Offers     json.RawMessage `gorm:"type:jsonb;not null"`
	CreatedAt  time.Time       `gorm:"not null"`
	ExpiresAt  time.Time       `gorm:"not null"`
}

// TableName for GORM.
func (FlightSearch) TableName() string { return "flight_searches" }

// TravellerProfile is a saved traveller. Data is the whole profile sealed with
// secretbox (plaintext without a key).
type TravellerProfile struct {
	ID        uuid.UUID `gorm:"type:uuid;primary_key"`
	UserID    uuid.UUID `gorm:"type:uuid;not null;index"`
	IsSelf    bool      `gorm:"not null;default:false"`
	Data      string    `gorm:"type:text;not null"`
	CreatedAt time.Time `gorm:"not null"`
	UpdatedAt time.Time `gorm:"not null"`
}

// TableName for GORM.
func (TravellerProfile) TableName() string { return "traveller_profiles" }

// Checkout is one booking attempt for an offer of a search. Travellers is a
// JSON array with one profile id (or "") per passenger; Offer is the latest
// OfferDetail shown to the user.
type Checkout struct {
	ID                uuid.UUID       `gorm:"type:uuid;primary_key"`
	UserID            uuid.UUID       `gorm:"type:uuid;not null;index"`
	TaskID            uuid.UUID       `gorm:"type:uuid;not null"`
	SearchID          uuid.UUID       `gorm:"type:uuid;not null"`
	Ref               string          `gorm:"type:varchar(8);not null"`
	Status            string          `gorm:"type:varchar(16);not null"`
	Mode              string          `gorm:"type:varchar(8);not null"`
	Travellers        json.RawMessage `gorm:"type:jsonb;not null"`
	Offer             json.RawMessage `gorm:"type:jsonb;not null"`
	Extras            json.RawMessage `gorm:"type:jsonb;not null;default:'[]'"`
	Catalogue         json.RawMessage `gorm:"type:jsonb;not null;default:'{}'"`
	BookingRef        string          `gorm:"type:varchar(32);not null;default:''"`
	PayBy             *time.Time
	PaidAt            *time.Time
	DepartsAt         *time.Time
	Change            string `gorm:"type:text;not null;default:''"`
	DuffelOrderID     string `gorm:"type:varchar(64);not null;default:''"`
	PaymentID         string `gorm:"type:varchar(128);not null;default:''"`
	PaymentIntent     string `gorm:"type:varchar(128);not null;default:''"`
	RemindedPayAt     *time.Time
	RemindedCheckinAt *time.Time
	HandoffURL        string    `gorm:"type:text;not null;default:''"`
	Error             string    `gorm:"type:text;not null;default:''"`
	ExpiresAt         time.Time `gorm:"not null"`
	CreatedAt         time.Time `gorm:"not null"`
	UpdatedAt         time.Time `gorm:"not null"`
}

// TableName for GORM.
func (Checkout) TableName() string { return "checkouts" }
