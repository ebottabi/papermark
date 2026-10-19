/*
 * Copyright (c) 2026. Papermark - All Rights Reserved
 * Unauthorized copying of this file, via any medium is strictly prohibited
 * Proprietary and confidential
 * Written by Ebot Tabi <ebot.tabi@gmail.com>, 19/10/2026
 */

package duffel

import (
	"context"
	"errors"
	"net/http"
	"net/url"
	"regexp"
	"strings"
)

// Service is a paid extra (a bag) of an offer, or one seat for one passenger.
type Service struct {
	ID           string   `json:"id"`
	Type         string   `json:"type"`
	Total        Money    `json:"total"`
	PassengerIDs []string `json:"passengerIds"`
	SegmentIDs   []string `json:"segmentIds"`
	MaxQuantity  int      `json:"maxQuantity"`
	MaxWeightKg  *float64 `json:"maxWeightKg"`
}

// Selected is a service to book, with its quantity.
type Selected struct {
	ID       string `json:"id"`
	Quantity int    `json:"quantity"`
}

// OrderRequest creates an order. Type is "hold" (no payment) or "instant"
// (paid from the Duffel balance with Payment, which must equal the total).
type OrderRequest struct {
	Type       string
	OfferID    string
	Passengers []Passenger
	Services   []Selected
	Payment    *Money
	Metadata   map[string]string
}

// Order is a Duffel order.
type Order struct {
	ID               string
	OfferID          string
	BookingReference string
	PayBy            *string // RFC 3339
	PaidAt           *string
	CancelledAt      *string
	AwaitingPayment  bool
	Total            Money
	Metadata         map[string]string
	Slices           []Slice
	Segments         [][]Segment
	Changes          int // airline-initiated changes
}

type rawOrder struct {
	rawOffer
	OfferID          string            `json:"offer_id"`
	BookingReference string            `json:"booking_reference"`
	CancelledAt      *string           `json:"cancelled_at"`
	Metadata         map[string]string `json:"metadata"`
	PaymentStatus    struct {
		AwaitingPayment   bool    `json:"awaiting_payment"`
		PaymentRequiredBy *string `json:"payment_required_by"`
		PaidAt            *string `json:"paid_at"`
	} `json:"payment_status"`
	Changes []struct {
		ID string `json:"id"`
	} `json:"airline_initiated_changes"`
}

func (r rawOrder) order() Order {
	o := r.normalise()
	return Order{ID: r.ID, OfferID: r.OfferID, BookingReference: r.BookingReference, PayBy: r.PaymentStatus.PaymentRequiredBy,
		PaidAt: r.PaymentStatus.PaidAt, CancelledAt: r.CancelledAt, AwaitingPayment: r.PaymentStatus.AwaitingPayment,
		Total: o.Price, Metadata: r.Metadata, Slices: o.Slices, Segments: o.Segments, Changes: len(r.Changes)}
}

// CreateOrder books or holds the offer.
func (c *Client) CreateOrder(ctx context.Context, req OrderRequest) (*Order, error) {
	data := map[string]any{"type": req.Type, "selected_offers": []string{req.OfferID}, "passengers": req.Passengers}
	if len(req.Services) > 0 {
		data["services"] = req.Services
	}
	if req.Payment != nil {
		data["payments"] = []map[string]string{{"type": "balance", "amount": req.Payment.Amount, "currency": req.Payment.Currency}}
	}
	if len(req.Metadata) > 0 {
		data["metadata"] = req.Metadata
	}
	var r rawOrder
	if err := c.call(ctx, http.MethodPost, "/air/orders", nil, map[string]any{"data": data}, &r); err != nil {
		return nil, err
	}
	o := r.order()
	return &o, nil
}

// GetOrder returns an order as it is now.
func (c *Client) GetOrder(ctx context.Context, id string) (*Order, error) {
	var r rawOrder
	if err := c.call(ctx, http.MethodGet, "/air/orders/"+url.PathEscape(id), nil, nil, &r); err != nil {
		return nil, err
	}
	o := r.order()
	return &o, nil
}

// FindOrder returns the order of a passenger whose metadata key has value, or
// nil. It recovers an order whose create call timed out.
// shortcut: reads the first page (50 newest orders) for the name; page with after when one name books more.
func (c *Client) FindOrder(ctx context.Context, passengerName, key, value string) (*Order, error) {
	var rs []rawOrder
	q := url.Values{"passenger_name[]": {passengerName}, "limit": {"50"}}
	if err := c.call(ctx, http.MethodGet, "/air/orders", q, nil, &rs); err != nil {
		return nil, err
	}
	for _, r := range rs {
		if r.Metadata[key] == value {
			o := r.order()
			return &o, nil
		}
	}
	return nil, nil
}

// ErrPaymentFailed is a payment Duffel declined.
var ErrPaymentFailed = errors.New("duffel: payment failed")

// PayOrder pays a held order from the balance. amount must equal its total.
func (c *Client) PayOrder(ctx context.Context, orderID string, amount Money) error {
	body := map[string]any{"data": map[string]any{"order_id": orderID,
		"payment": map[string]string{"type": "balance", "amount": amount.Amount, "currency": amount.Currency}}}
	var r struct {
		Status string `json:"status"`
	}
	if err := c.call(ctx, http.MethodPost, "/air/payments", nil, body, &r); err != nil {
		return err
	}
	if r.Status != "succeeded" && r.Status != "pending" {
		return ErrPaymentFailed
	}
	return nil
}

// Services returns the paid bags of an offer.
func (c *Client) Services(ctx context.Context, offerID string) ([]Service, error) {
	var r struct {
		Available []struct {
			ID            string   `json:"id"`
			Type          string   `json:"type"`
			TotalAmount   string   `json:"total_amount"`
			TotalCurrency string   `json:"total_currency"`
			PassengerIDs  []string `json:"passenger_ids"`
			SegmentIDs    []string `json:"segment_ids"`
			MaxQuantity   int      `json:"maximum_quantity"`
			Metadata      *struct {
				MaxWeightKg *float64 `json:"maximum_weight_kg"`
			} `json:"metadata"`
		} `json:"available_services"`
	}
	q := url.Values{"return_available_services": {"true"}}
	if err := c.call(ctx, http.MethodGet, "/air/offers/"+url.PathEscape(offerID), q, nil, &r); err != nil {
		return nil, err
	}
	out := []Service{}
	for _, a := range r.Available {
		if a.Type != "baggage" {
			continue
		}
		s := Service{ID: a.ID, Type: a.Type, Total: Money{a.TotalAmount, a.TotalCurrency}, PassengerIDs: a.PassengerIDs, SegmentIDs: a.SegmentIDs, MaxQuantity: a.MaxQuantity}
		if a.Metadata != nil {
			s.MaxWeightKg = a.Metadata.MaxWeightKg
		}
		out = append(out, s)
	}
	return out, nil
}

// SeatOption is the service that books a seat for one passenger.
type SeatOption struct {
	ServiceID   string
	PassengerID string
	Total       Money
}

// Seat is one element of a seat map row: a seat, or an exit, galley and so on.
type Seat struct {
	Kind       string // seat, exit_row, lavatory, galley, bassinet, empty
	Designator string
	Options    []SeatOption
}

// SeatRow is a row of blocks of elements between aisles.
type SeatRow struct {
	Number   string
	Sections [][]Seat
}

// SeatMap is the cabin layout of one segment.
type SeatMap struct {
	SegmentID string
	Rows      []SeatRow
}

var rowNumber = regexp.MustCompile(`^\d+`)

// SeatMaps returns the seat maps of an offer; an airline without them returns none.
func (c *Client) SeatMaps(ctx context.Context, offerID string) ([]SeatMap, error) {
	var raw []struct {
		SegmentID string `json:"segment_id"`
		Cabins    []struct {
			Rows []struct {
				Sections []struct {
					Elements []struct {
						Type       string `json:"type"`
						Designator string `json:"designator"`
						Services   []struct {
							ID            string `json:"id"`
							PassengerID   string `json:"passenger_id"`
							TotalAmount   string `json:"total_amount"`
							TotalCurrency string `json:"total_currency"`
						} `json:"available_services"`
					} `json:"elements"`
				} `json:"sections"`
			} `json:"rows"`
		} `json:"cabins"`
	}
	if err := c.call(ctx, http.MethodGet, "/air/seat_maps", url.Values{"offer_id": {offerID}}, nil, &raw); err != nil {
		return nil, err
	}
	out := make([]SeatMap, 0, len(raw))
	for _, m := range raw {
		sm := SeatMap{SegmentID: m.SegmentID, Rows: []SeatRow{}}
		for _, cabin := range m.Cabins {
			for _, row := range cabin.Rows {
				r := SeatRow{Sections: [][]Seat{}}
				for _, sec := range row.Sections {
					block := []Seat{}
					for _, e := range sec.Elements {
						s := Seat{Kind: e.Type, Designator: e.Designator, Options: []SeatOption{}}
						for _, sv := range e.Services {
							s.Options = append(s.Options, SeatOption{sv.ID, sv.PassengerID, Money{sv.TotalAmount, sv.TotalCurrency}})
						}
						if r.Number == "" && e.Type == "seat" {
							r.Number = rowNumber.FindString(e.Designator)
						}
						block = append(block, s)
					}
					r.Sections = append(r.Sections, block)
				}
				if strings.TrimSpace(r.Number) != "" || len(r.Sections) > 0 {
					sm.Rows = append(sm.Rows, r)
				}
			}
		}
		out = append(out, sm)
	}
	return out, nil
}
