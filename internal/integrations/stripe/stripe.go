/*
 * Copyright (c) 2026. Papermark - All Rights Reserved
 * Unauthorized copying of this file, via any medium is strictly prohibited
 * Proprietary and confidential
 * Written by Ebot Tabi <ebot.tabi@gmail.com>, 19/10/2026
 */

// Package stripe is the small part of the Stripe API flights payments use:
// hosted Checkout with a manual-capture PaymentIntent.
package stripe

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"math/big"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"time"
)

// Error is an error response from Stripe.
type Error struct {
	Status  int
	Type    string
	Code    string
	Message string
}

func (e *Error) Error() string { return fmt.Sprintf("stripe: %d %s %s", e.Status, e.Code, e.Message) }

// Client calls Stripe with one secret key.
type Client struct {
	HTTP    *http.Client
	BaseURL string
	Key     string
}

func (c *Client) call(ctx context.Context, method, path string, form url.Values, idem string, out any) error {
	var body io.Reader
	if form != nil {
		body = strings.NewReader(form.Encode())
	}
	req, err := http.NewRequestWithContext(ctx, method, strings.TrimRight(c.BaseURL, "/")+path, body)
	if err != nil {
		return err
	}
	req.Header.Set("Authorization", "Bearer "+c.Key)
	if form != nil {
		req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	}
	if idem != "" {
		req.Header.Set("Idempotency-Key", idem)
	}
	resp, err := c.HTTP.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	raw, err := io.ReadAll(io.LimitReader(resp.Body, 1<<20))
	if err != nil {
		return err
	}
	if resp.StatusCode >= 300 {
		var e struct {
			Error struct{ Type, Code, Message string }
		}
		json.Unmarshal(raw, &e)
		return &Error{resp.StatusCode, e.Error.Type, e.Error.Code, e.Error.Message}
	}
	return json.Unmarshal(raw, out)
}

// zeroDecimal are the currencies Stripe charges in whole units.
var zeroDecimal = map[string]bool{"BIF": true, "CLP": true, "DJF": true, "GNF": true, "JPY": true, "KMF": true, "KRW": true, "MGA": true,
	"PYG": true, "RWF": true, "UGX": true, "VND": true, "VUV": true, "XAF": true, "XOF": true, "XPF": true}

// threeDecimal are currencies Stripe needs special rounding for; they are refused.
var threeDecimal = map[string]bool{"BHD": true, "JOD": true, "KWD": true, "OMR": true, "TND": true}

// MinorUnits turns a decimal amount such as "212.40" into Stripe's integer
// amount in the smallest unit of currency. An amount it can't charge exactly is an error.
func MinorUnits(amount, currency string) (int64, error) {
	currency = strings.ToUpper(currency)
	if threeDecimal[currency] || len(currency) != 3 {
		return 0, fmt.Errorf("stripe: currency %q is not supported", currency)
	}
	r, ok := new(big.Rat).SetString(amount)
	if !ok || r.Sign() <= 0 {
		return 0, fmt.Errorf("stripe: bad amount %q", amount)
	}
	if !zeroDecimal[currency] {
		r.Mul(r, big.NewRat(100, 1))
	}
	if !r.IsInt() || !r.Num().IsInt64() {
		return 0, fmt.Errorf("stripe: %s %s is not a whole number of minor units", amount, currency)
	}
	return r.Num().Int64(), nil
}

// SessionRequest opens a hosted Checkout page for one manual-capture payment.
type SessionRequest struct {
	Amount      int64 // minor units
	Currency    string
	Name        string // shown on the page
	Email       string
	Metadata    map[string]string // copied to the PaymentIntent too
	SuccessURL  string
	CancelURL   string
	ExpiresAt   time.Time // at least 30 minutes ahead
	Idempotency string
}

// Session is a Checkout Session.
type Session struct {
	ID            string            `json:"id"`
	URL           string            `json:"url"`
	Status        string            `json:"status"`         // open, complete, expired
	PaymentStatus string            `json:"payment_status"` // paid, unpaid, no_payment_required
	PaymentIntent string            `json:"payment_intent"`
	AmountTotal   int64             `json:"amount_total"`
	Currency      string            `json:"currency"`
	Metadata      map[string]string `json:"metadata"`
}

// CreateSession opens a Checkout Session.
func (c *Client) CreateSession(ctx context.Context, r SessionRequest) (*Session, error) {
	f := url.Values{
		"mode":                                   {"payment"},
		"success_url":                            {r.SuccessURL},
		"cancel_url":                             {r.CancelURL},
		"expires_at":                             {strconv.FormatInt(r.ExpiresAt.Unix(), 10)},
		"line_items[0][quantity]":                {"1"},
		"line_items[0][price_data][currency]":    {strings.ToLower(r.Currency)},
		"line_items[0][price_data][unit_amount]": {strconv.FormatInt(r.Amount, 10)},
		"line_items[0][price_data][product_data][name]": {r.Name},
		"payment_intent_data[capture_method]":           {"manual"},
	}
	if r.Email != "" {
		f.Set("customer_email", r.Email)
	}
	for k, v := range r.Metadata {
		f.Set("metadata["+k+"]", v)
		f.Set("payment_intent_data[metadata]["+k+"]", v)
	}
	var s Session
	return &s, c.call(ctx, http.MethodPost, "/v1/checkout/sessions", f, r.Idempotency, &s)
}

// GetSession reads a Checkout Session.
func (c *Client) GetSession(ctx context.Context, id string) (*Session, error) {
	var s Session
	return &s, c.call(ctx, http.MethodGet, "/v1/checkout/sessions/"+url.PathEscape(id), nil, "", &s)
}

// ExpireSession closes an open Checkout Session so it can't be paid.
func (c *Client) ExpireSession(ctx context.Context, id string) error {
	var s Session
	return c.call(ctx, http.MethodPost, "/v1/checkout/sessions/"+url.PathEscape(id)+"/expire", url.Values{}, "", &s)
}

// PaymentIntent is the payment behind a session.
type PaymentIntent struct {
	ID       string `json:"id"`
	Status   string `json:"status"` // requires_capture, succeeded, canceled, ...
	Amount   int64  `json:"amount"`
	Currency string `json:"currency"`
}

// GetPaymentIntent reads a PaymentIntent.
func (c *Client) GetPaymentIntent(ctx context.Context, id string) (*PaymentIntent, error) {
	var p PaymentIntent
	return &p, c.call(ctx, http.MethodGet, "/v1/payment_intents/"+url.PathEscape(id), nil, "", &p)
}

// Capture takes the authorised amount. Capturing a captured payment is not an error.
func (c *Client) Capture(ctx context.Context, id string) error {
	var p PaymentIntent
	err := c.call(ctx, http.MethodPost, "/v1/payment_intents/"+url.PathEscape(id)+"/capture", url.Values{}, "capture-"+id, &p)
	if err != nil {
		if cur, gerr := c.GetPaymentIntent(ctx, id); gerr == nil && cur.Status == "succeeded" {
			return nil
		}
	}
	return err
}

// ErrCaptured means a payment was already taken and can't be released.
var ErrCaptured = errors.New("stripe: payment already captured")

// Release cancels an authorisation so the customer is not charged. A
// cancelled payment is not an error; a captured one is ErrCaptured.
func (c *Client) Release(ctx context.Context, id string) error {
	var p PaymentIntent
	err := c.call(ctx, http.MethodPost, "/v1/payment_intents/"+url.PathEscape(id)+"/cancel", url.Values{}, "cancel-"+id, &p)
	if err != nil {
		if cur, gerr := c.GetPaymentIntent(ctx, id); gerr == nil {
			switch cur.Status {
			case "canceled":
				return nil
			case "succeeded":
				return ErrCaptured
			}
		}
	}
	return err
}

// Event is a webhook event; Object is the event's data.object.
type Event struct {
	ID   string `json:"id"`
	Type string `json:"type"`
	Data struct {
		Object json.RawMessage `json:"object"`
	} `json:"data"`
}
