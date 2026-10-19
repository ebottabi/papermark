/*
 * Copyright (c) 2026. Papermark - All Rights Reserved
 * Unauthorized copying of this file, via any medium is strictly prohibited
 * Proprietary and confidential
 * Written by Ebot Tabi <ebot.tabi@gmail.com>, 18/10/2026
 */
package services

import "papermark/internal/integrations/duffel"

// Wire types of the flights API. They live here because responses imports services.

// Passport is a traveller's passport. Numbers are masked on every response.
type Passport struct {
	Number    string `json:"number"`
	Country   string `json:"country"`
	ExpiresOn string `json:"expiresOn"`
}

// Loyalty is a frequent-flyer account.
type Loyalty struct {
	Airline string `json:"airline"`
	Number  string `json:"number"`
}

// Traveller is a saved traveller profile. Services hold it with full
// numbers; Masked is what leaves the server.
type Traveller struct {
	ID         string    `json:"id"`
	IsSelf     bool      `json:"isSelf"`
	Title      string    `json:"title"`
	GivenName  string    `json:"givenName"`
	FamilyName string    `json:"familyName"`
	BornOn     string    `json:"bornOn"`
	Gender     string    `json:"gender"`
	Email      string    `json:"email"`
	Phone      string    `json:"phone"`
	Passport   *Passport `json:"passport"`
	Loyalty    []Loyalty `json:"loyalty"`
	Seat       string    `json:"seat"`
}

// MaskNumber keeps the last two characters: ••42.
func MaskNumber(n string) string {
	r := []rune(n)
	if len(r) <= 2 {
		return "••"
	}
	return "••" + string(r[len(r)-2:])
}

// Masked returns a copy whose passport and loyalty numbers are masked.
func (t Traveller) Masked() Traveller {
	if t.Passport != nil {
		p := *t.Passport
		p.Number = MaskNumber(p.Number)
		t.Passport = &p
	}
	loyalty := make([]Loyalty, len(t.Loyalty))
	for i, l := range t.Loyalty {
		loyalty[i] = Loyalty{l.Airline, MaskNumber(l.Number)}
	}
	t.Loyalty = loyalty
	return t
}

// TravellerSlot is one passenger place of a checkout.
type TravellerSlot struct {
	ProfileID *string  `json:"profileId"`
	Name      string   `json:"name"`
	Ready     bool     `json:"ready"`
	Missing   []string `json:"missing"`
}

// Booking is a held or booked order.
type Booking struct {
	Reference string  `json:"reference"`
	PayBy     *string `json:"payBy"`
	PaidAt    *string `json:"paidAt"`
}

// CheckoutExtra is one chosen paid extra (a bag). Price is per unit.
type CheckoutExtra struct {
	Ref      string       `json:"ref"`
	Kind     string       `json:"kind"`
	Label    string       `json:"label"`
	Quantity int          `json:"quantity"`
	Price    duffel.Money `json:"price"`
}

// Checkout is the checkout object of the API and of the checkout card.
type Checkout struct {
	ID         string          `json:"id"`
	TaskID     string          `json:"taskId"`
	SearchID   string          `json:"searchId"`
	Ref        string          `json:"ref"`
	Status     string          `json:"status"`
	Mode       string          `json:"mode"`
	Offer      duffel.Detail   `json:"offer"`
	Travellers []TravellerSlot `json:"travellers"`
	Booking    *Booking        `json:"booking"`
	HandoffURL *string         `json:"handoffUrl"`
	Error      *string         `json:"error"`
	ExpiresAt  string          `json:"expiresAt"`
	CanHold    bool            `json:"canHold"`
	CanPay     bool            `json:"canPay"`
	Extras     []CheckoutExtra `json:"extras"`
	Total      *duffel.Money   `json:"total"` // fare plus chosen extras
	DepartsAt  *string         `json:"departsAt"`
	Change     *string         `json:"change"`
}

// ExtraBag is one bag a pay-mode checkout can add. Traveller is the passenger slot index.
type ExtraBag struct {
	Ref         string       `json:"ref"`
	Traveller   int          `json:"traveller"`
	Label       string       `json:"label"`
	Price       duffel.Money `json:"price"`
	MaxQuantity int          `json:"maxQuantity"`
}

// ExtrasCatalogue is GET /checkouts/:id/extras: the extras a pay-mode checkout can add.
type ExtrasCatalogue struct {
	Bags     []ExtraBag `json:"bags"`
	SeatMaps []any      `json:"seatMaps"` // shortcut: seat selection is a follow-up; wire duffel.SeatMaps here when it ships
}

// PaymentStart is POST /checkouts/:id/pay: the checkout and the hosted page where the user pays.
type PaymentStart struct {
	Checkout   *Checkout `json:"checkout"`
	PaymentURL string    `json:"paymentUrl"`
}

// Choice is one extra the user selects (PATCH body entry).
type Choice struct {
	Ref      string `json:"ref"`
	Quantity int    `json:"quantity"`
}
