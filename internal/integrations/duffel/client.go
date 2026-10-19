/*
 * Copyright (c) 2026. Papermark - All Rights Reserved
 * Unauthorized copying of this file, via any medium is strictly prohibited
 * Proprietary and confidential
 * Written by Ebot Tabi <ebot.tabi@gmail.com>, 18/10/2026
 */

// Package duffel talks to the Duffel flights API and normalises its offers.
package duffel

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"regexp"
	"strings"
)

// APIError is an error response from Duffel. Type and Code are Duffel's
// (for example airline_error / offer_no_longer_available).
type APIError struct {
	Status  int
	Type    string
	Code    string
	Message string
}

func (e *APIError) Error() string {
	return fmt.Sprintf("duffel: %d %s %s", e.Status, e.Code, e.Message)
}

// Client calls Duffel with one access token.
type Client struct {
	HTTP    *http.Client
	BaseURL string
	Token   string
}

const maxResponse = 32 << 20

func (c *Client) call(ctx context.Context, method, path string, q url.Values, body, out any) error {
	var rd io.Reader
	if body != nil {
		b, err := json.Marshal(body)
		if err != nil {
			return err
		}
		rd = bytes.NewReader(b)
	}
	u := strings.TrimRight(c.BaseURL, "/") + path
	if len(q) > 0 {
		u += "?" + q.Encode()
	}
	req, err := http.NewRequestWithContext(ctx, method, u, rd)
	if err != nil {
		return err
	}
	req.Header.Set("Authorization", "Bearer "+c.Token)
	req.Header.Set("Duffel-Version", "v2")
	req.Header.Set("Accept", "application/json")
	if body != nil {
		req.Header.Set("Content-Type", "application/json")
	}
	resp, err := c.HTTP.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	raw, err := io.ReadAll(io.LimitReader(resp.Body, maxResponse))
	if err != nil {
		return err
	}
	if resp.StatusCode >= 300 {
		var e struct {
			Errors []struct{ Type, Code, Message string }
		}
		json.Unmarshal(raw, &e)
		ae := &APIError{Status: resp.StatusCode, Message: http.StatusText(resp.StatusCode)}
		if len(e.Errors) > 0 {
			ae.Type, ae.Code, ae.Message = e.Errors[0].Type, e.Errors[0].Code, e.Errors[0].Message
		}
		return ae
	}
	if out == nil {
		return nil
	}
	var env struct {
		Data json.RawMessage `json:"data"`
	}
	if err := json.Unmarshal(raw, &env); err != nil {
		return err
	}
	return json.Unmarshal(env.Data, out)
}

// ErrNoPlace means no airport or city matched a name.
var ErrNoPlace = errors.New("duffel: no such place")

var iataCode = regexp.MustCompile(`^[A-Za-z]{3}$`)

// Resolve turns a city name or IATA code into an IATA airport or city code.
func (c *Client) Resolve(ctx context.Context, text string) (string, error) {
	text = strings.TrimSpace(text)
	if iataCode.MatchString(text) {
		return strings.ToUpper(text), nil
	}
	if text == "" {
		return "", ErrNoPlace
	}
	var places []struct {
		Type     string `json:"type"`
		IataCode string `json:"iata_code"`
	}
	if err := c.call(ctx, http.MethodGet, "/places/suggestions", url.Values{"query": {text}}, nil, &places); err != nil {
		return "", err
	}
	for _, want := range []string{"city", "airport"} {
		for _, p := range places {
			if p.Type == want && iataCode.MatchString(p.IataCode) {
				return p.IataCode, nil
			}
		}
	}
	return "", ErrNoPlace
}

// Query is a flight search; Origin and Destination are IATA codes.
type Query struct {
	Origin, Destination string
	DepartDate          string // YYYY-MM-DD
	ReturnDate          string // optional
	Adults              int
	ChildAges           []int
	Cabin               string
}

// Search returns the offers for q, normalised, ranked and labelled. Refs are
// 1-based positions in the returned order.
func (c *Client) Search(ctx context.Context, q Query) ([]Offer, error) {
	slices := []map[string]string{{"origin": q.Origin, "destination": q.Destination, "departure_date": q.DepartDate}}
	if q.ReturnDate != "" {
		slices = append(slices, map[string]string{"origin": q.Destination, "destination": q.Origin, "departure_date": q.ReturnDate})
	}
	var pax []map[string]any
	for range q.Adults {
		pax = append(pax, map[string]any{"type": "adult"})
	}
	for _, age := range q.ChildAges {
		pax = append(pax, map[string]any{"age": age})
	}
	req := map[string]any{"data": map[string]any{"slices": slices, "passengers": pax, "cabin_class": q.Cabin}}
	var res struct {
		Offers []rawOffer `json:"offers"`
	}
	if err := c.call(ctx, http.MethodPost, "/air/offer_requests", url.Values{"return_offers": {"true"}}, req, &res); err != nil {
		return nil, err
	}
	offers := make([]Offer, 0, len(res.Offers))
	for _, r := range res.Offers {
		offers = append(offers, r.normalise())
	}
	return Rank(offers), nil
}

// GetOffer returns the offer with its current price, conditions and passenger ids.
func (c *Client) GetOffer(ctx context.Context, id string) (*Offer, error) {
	var r rawOffer
	if err := c.call(ctx, http.MethodGet, "/air/offers/"+url.PathEscape(id), nil, nil, &r); err != nil {
		return nil, err
	}
	o := r.normalise()
	return &o, nil
}

// Passenger is one traveller of a hold order.
type Passenger struct {
	ID          string     `json:"id"`
	Title       string     `json:"title"`
	Gender      string     `json:"gender"`
	GivenName   string     `json:"given_name"`
	FamilyName  string     `json:"family_name"`
	BornOn      string     `json:"born_on"`
	Email       string     `json:"email"`
	PhoneNumber string     `json:"phone_number"`
	Documents   []Document `json:"identity_documents,omitempty"`
	Loyalty     []Loyalty  `json:"loyalty_programme_accounts,omitempty"`
	// InfantID links an adult to the infant without a seat who travels on their lap.
	InfantID string `json:"infant_passenger_id,omitempty"`
}

// Document is a passenger's identity document.
type Document struct {
	Type               string `json:"type"`
	UniqueIdentifier   string `json:"unique_identifier"`
	IssuingCountryCode string `json:"issuing_country_code"`
	ExpiresOn          string `json:"expires_on"`
}

// Loyalty is a frequent-flyer account.
type Loyalty struct {
	Airline string `json:"airline_iata_code"`
	Number  string `json:"account_number"`
}
