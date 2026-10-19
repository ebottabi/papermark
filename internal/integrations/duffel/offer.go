/*
 * Copyright (c) 2026. Papermark - All Rights Reserved
 * Unauthorized copying of this file, via any medium is strictly prohibited
 * Proprietary and confidential
 * Written by Ebot Tabi <ebot.tabi@gmail.com>, 18/10/2026
 */

package duffel

import (
	"math"
	"math/big"
	"regexp"
	"slices"
	"strconv"
	"time"
)

// Money is an amount in a currency; the amount is a decimal string, never a float.
type Money struct {
	Amount   string `json:"amount"`
	Currency string `json:"currency"`
}

var symbols = map[string]string{"EUR": "€", "USD": "$", "GBP": "£"}

// String formats the amount for a person: €226.40, or CHF 226.40 for a currency without a symbol here.
func (m Money) String() string {
	if s, ok := symbols[m.Currency]; ok {
		return s + m.Amount
	}
	return m.Currency + " " + m.Amount
}

// Owner is the airline selling the offer.
type Owner struct {
	Name string  `json:"name"`
	IATA string  `json:"iata"`
	Logo *string `json:"logo"`
}

// Slice is one direction of the trip. Times are local to their airport.
type Slice struct {
	Origin          string   `json:"origin"`
	OriginCity      string   `json:"originCity"`
	Destination     string   `json:"destination"`
	DestinationCity string   `json:"destinationCity"`
	DepartAt        string   `json:"departAt"`
	ArriveAt        string   `json:"arriveAt"`
	Minutes         int      `json:"minutes"`
	Stops           int      `json:"stops"`
	Carriers        []string `json:"carriers"`
}

// Segment is one flight of a slice.
type Segment struct {
	Flight         string `json:"flight"`
	Carrier        string `json:"carrier"`
	From           string `json:"from"`
	FromTerminal   string `json:"fromTerminal"`
	To             string `json:"to"`
	ToTerminal     string `json:"toTerminal"`
	DepartAt       string `json:"departAt"`
	ArriveAt       string `json:"arriveAt"`
	Minutes        int    `json:"minutes"`
	Aircraft       string `json:"aircraft"`
	LayoverMinutes *int   `json:"layoverMinutes"`
	AirportChange  bool   `json:"airportChange"`
}

// Bag is the baggage allowance per passenger.
type Bag struct {
	Type     string `json:"type"`
	Quantity int    `json:"quantity"`
}

// Conditions are the refund and change rules before departure.
type Conditions struct {
	Refundable bool   `json:"refundable"`
	RefundFee  *Money `json:"refundFee"`
	Changeable bool   `json:"changeable"`
	ChangeFee  *Money `json:"changeFee"`
}

// Breakdown splits the total price.
type Breakdown struct {
	Base  Money `json:"base"`
	Tax   Money `json:"tax"`
	Total Money `json:"total"`
}

// Offer is a normalised Duffel offer. Stored searches keep it whole;
// DuffelID and PassengerIDs never leave the server (see Summary and Detail).
type Offer struct {
	DuffelID     string   `json:"duffelId"`
	PassengerIDs []string `json:"passengerIds"`
	// PassengerTypes is Duffel's type of each passenger (adult, child, infant_without_seat), in PassengerIDs order.
	PassengerTypes []string `json:"passengerTypes"`
	// SegmentIDs are Duffel's segment ids in slice order, for seat maps and bags.
	SegmentIDs []string `json:"segmentIds"`
	// DepartsUTC is the first departure as an instant, for reminders.
	DepartsUTC   *time.Time  `json:"departsUtc"`
	Ref          string      `json:"ref"`
	Label        string      `json:"label"`
	Price        Money       `json:"price"`
	Owner        Owner       `json:"owner"`
	Slices       []Slice     `json:"slices"`
	Segments     [][]Segment `json:"segments"`
	Baggage      []Bag       `json:"baggage"`
	Conditions   Conditions  `json:"conditions"`
	Breakdown    Breakdown   `json:"breakdown"`
	Passengers   int         `json:"passengers"`
	Cabin        string      `json:"cabin"`
	Holdable     bool        `json:"holdable"`
	PayBy        *string     `json:"payBy"`
	GuaranteeEnd *string     `json:"priceGuaranteeUntil"`
	ExpiresAt    string      `json:"expiresAt"`
	DocsRequired bool        `json:"documentsRequired"`
}

// Summary is the OfferSummary wire shape.
type Summary struct {
	Ref    string  `json:"ref"`
	Label  string  `json:"label"`
	Price  Money   `json:"price"`
	Owner  Owner   `json:"owner"`
	Slices []Slice `json:"slices"`
}

// Detail is the OfferDetail wire shape.
type Detail struct {
	Summary
	Segments     [][]Segment `json:"segments"`
	Baggage      []Bag       `json:"baggage"`
	Conditions   Conditions  `json:"conditions"`
	Breakdown    Breakdown   `json:"breakdown"`
	Passengers   int         `json:"passengers"`
	Cabin        string      `json:"cabin"`
	Holdable     bool        `json:"holdable"`
	PayBy        *string     `json:"payBy"`
	GuaranteeEnd *string     `json:"priceGuaranteeUntil"`
	ExpiresAt    string      `json:"expiresAt"`
	DocsRequired bool        `json:"documentsRequired"`
}

func (o Offer) Summary() Summary {
	return Summary{o.Ref, o.Label, o.Price, o.Owner, o.Slices}
}

func (o Offer) Detail() Detail {
	return Detail{o.Summary(), o.Segments, o.Baggage, o.Conditions, o.Breakdown, o.Passengers, o.Cabin, o.Holdable, o.PayBy, o.GuaranteeEnd, o.ExpiresAt, o.DocsRequired}
}

// TotalMinutes is the travel time of all slices.
func (o Offer) TotalMinutes() int {
	n := 0
	for _, s := range o.Slices {
		n += s.Minutes
	}
	return n
}

// PriceUp reports whether now costs more than before (same currency assumed).
func PriceUp(before, now Money) bool {
	a, ok1 := new(big.Rat).SetString(before.Amount)
	b, ok2 := new(big.Rat).SetString(now.Amount)
	return ok1 && ok2 && b.Cmp(a) > 0
}

// Rank orders offers best first and labels up to three distinct ones: best
// (price plus 20 per travel hour), then the cheapest and fastest of the rest.
func Rank(offers []Offer) []Offer {
	price := func(o Offer) float64 {
		r, ok := new(big.Rat).SetString(o.Price.Amount)
		if !ok {
			return math.MaxFloat64 / 4
		}
		f, _ := r.Float64()
		return f
	}
	score := func(o Offer) float64 { return price(o) + float64(o.TotalMinutes())/60*20 }
	slices.SortStableFunc(offers, func(a, b Offer) int {
		switch x, y := score(a), score(b); {
		case x < y:
			return -1
		case x > y:
			return 1
		}
		return 0
	})
	picks := []struct {
		label string
		less  func(a, b Offer) bool
	}{
		{"best", func(a, b Offer) bool { return false }},
		{"cheapest", func(a, b Offer) bool { return price(a) < price(b) }},
		{"fastest", func(a, b Offer) bool { return a.TotalMinutes() < b.TotalMinutes() }},
	}
	for _, p := range picks {
		win := -1
		for i := range offers {
			if offers[i].Label == "" && (win < 0 || p.less(offers[i], offers[win])) {
				win = i
			}
		}
		if win >= 0 {
			offers[win].Label = p.label
		}
	}
	for i := range offers {
		offers[i].Ref = strconv.Itoa(i + 1)
	}
	return offers
}

type rawPlace struct {
	IataCode string `json:"iata_code"`
	CityName string `json:"city_name"`
	TimeZone string `json:"time_zone"`
}

type rawSegment struct {
	ID               string `json:"id"`
	FlightNumber     string `json:"marketing_carrier_flight_number"`
	MarketingCarrier struct {
		Name     string `json:"name"`
		IataCode string `json:"iata_code"`
	} `json:"marketing_carrier"`
	Origin              rawPlace `json:"origin"`
	Destination         rawPlace `json:"destination"`
	OriginTerminal      *string  `json:"origin_terminal"`
	DestinationTerminal *string  `json:"destination_terminal"`
	DepartingAt         string   `json:"departing_at"`
	ArrivingAt          string   `json:"arriving_at"`
	Duration            string   `json:"duration"`
	Aircraft            *struct {
		Name string `json:"name"`
	} `json:"aircraft"`
	Passengers []struct {
		CabinClass string `json:"cabin_class"`
		Baggages   []struct {
			Type     string `json:"type"`
			Quantity int    `json:"quantity"`
		} `json:"baggages"`
	} `json:"passengers"`
}

type rawCondition struct {
	Allowed        bool    `json:"allowed"`
	PenaltyAmount  *string `json:"penalty_amount"`
	PenaltyCurency *string `json:"penalty_currency"`
}

type rawOffer struct {
	ID            string  `json:"id"`
	TotalAmount   string  `json:"total_amount"`
	TotalCurrency string  `json:"total_currency"`
	BaseAmount    string  `json:"base_amount"`
	TaxAmount     *string `json:"tax_amount"`
	Owner         struct {
		Name          string  `json:"name"`
		IataCode      string  `json:"iata_code"`
		LogoSymbolURL *string `json:"logo_symbol_url"`
	} `json:"owner"`
	Slices []struct {
		Duration string       `json:"duration"`
		Segments []rawSegment `json:"segments"`
	} `json:"slices"`
	Passengers []struct {
		ID   string `json:"id"`
		Type string `json:"type"`
	} `json:"passengers"`
	Conditions struct {
		Refund *rawCondition `json:"refund_before_departure"`
		Change *rawCondition `json:"change_before_departure"`
	} `json:"conditions"`
	PaymentRequirements struct {
		RequiresInstantPayment  bool    `json:"requires_instant_payment"`
		PaymentRequiredBy       *string `json:"payment_required_by"`
		PriceGuaranteeExpiresAt *string `json:"price_guarantee_expires_at"`
	} `json:"payment_requirements"`
	IdentityDocsRequired bool   `json:"passenger_identity_documents_required"`
	ExpiresAt            string `json:"expires_at"`
}

var isoDuration = regexp.MustCompile(`^P(?:(\d+)D)?(?:T(?:(\d+)H)?(?:(\d+)M)?)?$`)

// minutes parses an ISO 8601 duration such as PT02H26M; 0 when malformed.
func minutes(d string) int {
	m := isoDuration.FindStringSubmatch(d)
	if m == nil {
		return 0
	}
	n := func(s string) int { v, _ := strconv.Atoi(s); return v }
	return n(m[1])*1440 + n(m[2])*60 + n(m[3])
}

// local cuts Duffel's local timestamp to minutes: 2026-10-23T18:05.
func local(t string) string {
	if len(t) > 16 {
		return t[:16]
	}
	return t
}

func at(t, zone string) (time.Time, bool) {
	loc, err := time.LoadLocation(zone)
	if err != nil {
		loc = time.UTC
	}
	v, err := time.ParseInLocation("2006-01-02T15:04", local(t), loc)
	return v, err == nil
}

func money(amount *string, currency string) *Money {
	if amount == nil || *amount == "" {
		return nil
	}
	return &Money{*amount, currency}
}

func (r rawOffer) normalise() Offer {
	cur := r.TotalCurrency
	o := Offer{
		DuffelID: r.ID, Price: Money{r.TotalAmount, cur},
		Owner:      Owner{r.Owner.Name, r.Owner.IataCode, r.Owner.LogoSymbolURL},
		Passengers: len(r.Passengers), Holdable: !r.PaymentRequirements.RequiresInstantPayment,
		PayBy: r.PaymentRequirements.PaymentRequiredBy, GuaranteeEnd: r.PaymentRequirements.PriceGuaranteeExpiresAt,
		ExpiresAt: r.ExpiresAt, DocsRequired: r.IdentityDocsRequired,
		Slices: []Slice{}, Segments: [][]Segment{}, Baggage: []Bag{}, PassengerIDs: []string{}, PassengerTypes: []string{}, SegmentIDs: []string{},
	}
	for _, p := range r.Passengers {
		o.PassengerIDs = append(o.PassengerIDs, p.ID)
		o.PassengerTypes = append(o.PassengerTypes, p.Type)
	}
	tax := "0.00"
	if r.TaxAmount != nil {
		tax = *r.TaxAmount
	}
	o.Breakdown = Breakdown{Money{r.BaseAmount, cur}, Money{tax, cur}, o.Price}
	if c := r.Conditions.Refund; c != nil {
		o.Conditions.Refundable, o.Conditions.RefundFee = c.Allowed, money(c.PenaltyAmount, deref(c.PenaltyCurency))
	}
	if c := r.Conditions.Change; c != nil {
		o.Conditions.Changeable, o.Conditions.ChangeFee = c.Allowed, money(c.PenaltyAmount, deref(c.PenaltyCurency))
	}
	for si, s := range r.Slices {
		if len(s.Segments) == 0 {
			continue
		}
		first, last := s.Segments[0], s.Segments[len(s.Segments)-1]
		if si == 0 {
			if t, ok := at(first.DepartingAt, first.Origin.TimeZone); ok && first.Origin.TimeZone != "" {
				u := t.UTC()
				o.DepartsUTC = &u
			}
		}
		sl := Slice{Origin: first.Origin.IataCode, OriginCity: first.Origin.CityName, Destination: last.Destination.IataCode,
			DestinationCity: last.Destination.CityName, DepartAt: local(first.DepartingAt), ArriveAt: local(last.ArrivingAt),
			Minutes: minutes(s.Duration), Stops: len(s.Segments) - 1, Carriers: []string{}}
		segs := make([]Segment, len(s.Segments))
		total := 0
		for i, g := range s.Segments {
			seg := Segment{Flight: g.MarketingCarrier.IataCode + g.FlightNumber, Carrier: g.MarketingCarrier.Name,
				From: g.Origin.IataCode, FromTerminal: deref(g.OriginTerminal), To: g.Destination.IataCode, ToTerminal: deref(g.DestinationTerminal),
				DepartAt: local(g.DepartingAt), ArriveAt: local(g.ArrivingAt), Minutes: minutes(g.Duration)}
			o.SegmentIDs = append(o.SegmentIDs, g.ID)
			if g.Aircraft != nil {
				seg.Aircraft = g.Aircraft.Name
			}
			total += seg.Minutes
			if i+1 < len(s.Segments) {
				next := s.Segments[i+1]
				seg.AirportChange = next.Origin.IataCode != g.Destination.IataCode
				arr, ok1 := at(g.ArrivingAt, g.Destination.TimeZone)
				dep, ok2 := at(next.DepartingAt, next.Origin.TimeZone)
				if ok1 && ok2 {
					m := int(dep.Sub(arr).Minutes())
					seg.LayoverMinutes = &m
				}
			}
			if !slices.Contains(sl.Carriers, g.MarketingCarrier.IataCode) {
				sl.Carriers = append(sl.Carriers, g.MarketingCarrier.IataCode)
			}
			segs[i] = seg
		}
		if sl.Minutes == 0 {
			sl.Minutes = total
		}
		o.Slices = append(o.Slices, sl)
		o.Segments = append(o.Segments, segs)
		if si == 0 && len(first.Passengers) > 0 {
			o.Cabin = first.Passengers[0].CabinClass
			for _, b := range first.Passengers[0].Baggages {
				o.Baggage = append(o.Baggage, Bag{b.Type, b.Quantity})
			}
		}
	}
	return o
}

func deref(s *string) string {
	if s == nil {
		return ""
	}
	return *s
}
