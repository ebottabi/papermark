/*
 * Copyright (c) 2026. Papermark - All Rights Reserved
 * Unauthorized copying of this file, via any medium is strictly prohibited
 * Proprietary and confidential
 * Written by Ebot Tabi <ebot.tabi@gmail.com>, 18/10/2026
 */
package tools

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"time"

	"papermark/internal/integrations/duffel"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
)

// FlightSearchResult is a stored search.
type FlightSearchResult struct {
	ID         uuid.UUID
	Route      string // London → Lisbon
	Dates      string // Fri 23 Oct – Sun 25 Oct
	Passengers int
	Cabin      string
	Offers     []duffel.Offer // ranked; Ref is the position
	ExpiresAt  time.Time
}

// BookingStatus is what booking_get tells the model about one checkout.
type BookingStatus struct {
	ID        uuid.UUID
	Route     string
	Status    string
	Reference string
	PayBy     *time.Time
	Error     string
}

// Flights is the flight search and booking service. Origin and Destination of
// the query may be city names or IATA codes.
type Flights interface {
	Search(ctx context.Context, userID, taskID uuid.UUID, q duffel.Query) (*FlightSearchResult, error)
	// Offer returns the stored offer with its current price and rules.
	Offer(ctx context.Context, userID, searchID uuid.UUID, ref string) (*duffel.Offer, error)
	// Checkout creates the checkout and shows its card in the task.
	Checkout(ctx context.Context, userID, searchID uuid.UUID, ref string) error
	Bookings(ctx context.Context, userID uuid.UUID, checkoutID *uuid.UUID) ([]BookingStatus, error)
	// Book pays the checkout from the operator's Duffel balance and books the
	// airline, with no card from the user. Only when balance booking is on.
	Book(ctx context.Context, userID, checkoutID uuid.UUID) (BookingStatus, error)
	// CanBook reports whether balance booking is enabled.
	CanBook() bool
}

// CardEvent is a rich card in the task: flight_options, flight_offer or checkout. A
// checkout card is emitted again on every status change with the same id,
// and the app replaces the earlier one.
type CardEvent struct {
	Type string `json:"type"`
	ID   string `json:"id"`
	Kind string `json:"kind"`
	Data any    `json:"data"`
}

// FlightOfferCard is the data of a flight_offer card: one offer in full.
type FlightOfferCard struct {
	SearchID string        `json:"searchId"`
	Offer    duffel.Detail `json:"offer"`
}

// FlightOptionsCard is the data of a flight_options card.
type FlightOptionsCard struct {
	SearchID   string           `json:"searchId"`
	Route      string           `json:"route"`
	Dates      string           `json:"dates"`
	Passengers int              `json:"passengers"`
	Cabin      string           `json:"cabin"`
	Options    []duffel.Summary `json:"options"`
	Total      int              `json:"total"`
	ExpiresAt  string           `json:"expiresAt"`
}

const (
	maxPassengers   = 9
	maxListedOffers = 8
	maxCardOptions  = 3
)

var cabins = []string{"economy", "premium_economy", "business", "first"}

// FlightSearch searches flights and shows the options card.
var FlightSearch = Tool{
	Name:        "flight_search",
	Description: "Search flights. origin and destination are city names or IATA codes; dates are YYYY-MM-DD. Shows an options card to the user and returns the ranked offers. Quote prices only from this result.",
	Schema:      json.RawMessage(`{"type":"object","properties":{"origin":{"type":"string"},"destination":{"type":"string"},"departDate":{"type":"string","description":"YYYY-MM-DD"},"returnDate":{"type":"string","description":"YYYY-MM-DD, for a return trip"},"adults":{"type":"integer","minimum":1,"maximum":9,"description":"default 1"},"childAges":{"type":"array","items":{"type":"integer","minimum":0,"maximum":17}},"cabin":{"type":"string","enum":["economy","premium_economy","business","first"]}},"required":["origin","destination","departDate"],"additionalProperties":false}`),
	Run:         flightSearch,
}

// FlightOffer shows one offer's fresh price and rules.
var FlightOffer = Tool{
	Name:        "flight_offer",
	Description: "Get the current price, baggage, refund and change rules of one offer of a search, and show them to the user as a details card. Call it whenever the user asks about one flight.",
	Schema:      json.RawMessage(`{"type":"object","properties":{"searchId":{"type":"string"},"ref":{"type":"string"}},"required":["searchId","ref"],"additionalProperties":false}`),
	Run:         flightOffer,
}

// FlightCheckout starts a checkout for the offer the user picked.
var FlightCheckout = Tool{
	Name:        "flight_checkout",
	Description: "Start the booking of the offer the user picked. Shows a checkout card where the user adds travellers, any bags, and holds or pays. Nothing is booked or paid by this tool.",
	Schema:      json.RawMessage(`{"type":"object","properties":{"searchId":{"type":"string"},"ref":{"type":"string"}},"required":["searchId","ref"],"additionalProperties":false}`),
	Run:         flightCheckout,
}

// BookingGet reports checkout and booking status.
var BookingGet = Tool{
	Name:        "booking_get",
	Description: "Get the status of one checkout, or the user's 10 most recent. Say a flight is held or booked only when this says held or booked.",
	Schema:      json.RawMessage(`{"type":"object","properties":{"checkoutId":{"type":"string"}},"additionalProperties":false}`),
	Run:         bookingGet,
}

// FlightBook books and pays a confirmed checkout from the Duffel balance.
var FlightBook = Tool{
	Name:        "flight_book",
	Description: "Book and pay for a checkout the user confirmed, charging the Duffel balance. The traveller must be set on the checkout first. This books and pays with no card from the user. Say a flight is booked only when the result says booked.",
	Schema:      json.RawMessage(`{"type":"object","properties":{"checkoutId":{"type":"string"}},"required":["checkoutId"],"additionalProperties":false}`),
	Run:         flightBook,
}

func parseQuery(input json.RawMessage, now time.Time) (duffel.Query, error) {
	var in struct {
		Origin, Destination, DepartDate, ReturnDate, Cabin string
		Adults                                             int
		ChildAges                                          []int
	}
	if err := json.Unmarshal(input, &in); err != nil {
		return duffel.Query{}, err
	}
	if in.Adults == 0 {
		in.Adults = 1
	}
	if in.Cabin == "" {
		in.Cabin = "economy"
	}
	oldest := now.UTC().AddDate(0, 0, -1).Format("2006-01-02") // tolerates a user ahead of UTC
	date := func(name, v string) error {
		if _, err := time.Parse("2006-01-02", v); err != nil {
			return fmt.Errorf("%s must be a date like 2026-10-23", name)
		}
		if v < oldest {
			return fmt.Errorf("%s is in the past", name)
		}
		return nil
	}
	switch {
	case strings.TrimSpace(in.Origin) == "" || strings.TrimSpace(in.Destination) == "":
		return duffel.Query{}, errors.New("origin and destination are required")
	case date("departDate", in.DepartDate) != nil:
		return duffel.Query{}, date("departDate", in.DepartDate)
	case in.ReturnDate != "" && date("returnDate", in.ReturnDate) != nil:
		return duffel.Query{}, date("returnDate", in.ReturnDate)
	case in.ReturnDate != "" && in.ReturnDate < in.DepartDate:
		return duffel.Query{}, errors.New("returnDate is before departDate")
	case in.Adults < 1 || in.Adults > maxPassengers:
		return duffel.Query{}, errors.New("adults must be 1 to 9")
	case in.Adults+len(in.ChildAges) > maxPassengers:
		return duffel.Query{}, errors.New("at most 9 passengers")
	case !contains(cabins, in.Cabin):
		return duffel.Query{}, errors.New("cabin must be economy, premium_economy, business or first")
	}
	for _, a := range in.ChildAges {
		if a < 0 || a > 17 {
			return duffel.Query{}, errors.New("child ages must be 0 to 17")
		}
	}
	return duffel.Query{Origin: strings.TrimSpace(in.Origin), Destination: strings.TrimSpace(in.Destination), DepartDate: in.DepartDate,
		ReturnDate: in.ReturnDate, Adults: in.Adults, ChildAges: in.ChildAges, Cabin: in.Cabin}, nil
}

func contains(list []string, v string) bool {
	for _, s := range list {
		if s == v {
			return true
		}
	}
	return false
}

func flightSearch(ctx context.Context, env *Env, input json.RawMessage) (Output, error) {
	q, err := parseQuery(input, time.Now())
	if err != nil {
		return Output{}, err
	}
	res, err := env.Flights.Search(ctx, env.UserID, env.TaskID, q)
	if err != nil {
		return Output{}, err
	}
	card := FlightOptionsCard{SearchID: res.ID.String(), Route: res.Route, Dates: res.Dates, Passengers: res.Passengers, Cabin: res.Cabin,
		Options: []duffel.Summary{}, Total: len(res.Offers), ExpiresAt: res.ExpiresAt.Format(time.RFC3339)}
	for _, o := range res.Offers {
		if o.Label != "" && len(card.Options) < maxCardOptions {
			card.Options = append(card.Options, o.Summary())
		}
	}
	err = env.TaskTx(ctx, func(_ pgx.Tx, emit func(any) error) error {
		return emit(CardEvent{"card", card.SearchID, "flight_options", card})
	})
	if err != nil {
		return Output{}, err
	}
	var b strings.Builder
	fmt.Fprintf(&b, "Search %s: %s, %s, %d passenger(s), %s. Options are shown to the user as a card, so don't list them; recommend one in a sentence. %d offers found.\n", res.ID, res.Route, res.Dates, res.Passengers, res.Cabin, len(res.Offers))
	for i, o := range res.Offers {
		if i == maxListedOffers {
			break
		}
		fmt.Fprintf(&b, "%s. %s %s\n", o.Ref, labelOf(o), offerLine(o))
	}
	return Output{Text: b.String()}, nil
}

func labelOf(o duffel.Offer) string {
	if o.Label == "" {
		return ""
	}
	return "[" + o.Label + "]"
}

func hm(minutes int) string { return fmt.Sprintf("%dh%02dm", minutes/60, minutes%60) }

// offerLine is one offer in a sentence: price, airline, each slice's times, duration and stops.
func offerLine(o duffel.Offer) string {
	parts := make([]string, len(o.Slices))
	for i, s := range o.Slices {
		parts[i] = fmt.Sprintf("%s %s→%s %s (%s, %d stops)", s.Origin, strings.Replace(s.DepartAt, "T", " ", 1), s.Destination, s.ArriveAt[min(11, len(s.ArriveAt)):], hm(s.Minutes), s.Stops)
	}
	return fmt.Sprintf("%s, %s, %s", o.Price, o.Owner.Name, strings.Join(parts, "; "))
}

func offerID(input json.RawMessage) (uuid.UUID, string, error) {
	var in struct{ SearchID, Ref string }
	if err := json.Unmarshal(input, &in); err != nil {
		return uuid.Nil, "", err
	}
	id, err := uuid.Parse(in.SearchID)
	if err != nil || in.Ref == "" {
		return uuid.Nil, "", errors.New("searchId and ref are required")
	}
	return id, in.Ref, nil
}

func flightOffer(ctx context.Context, env *Env, input json.RawMessage) (Output, error) {
	id, ref, err := offerID(input)
	if err != nil {
		return Output{}, err
	}
	o, err := env.Flights.Offer(ctx, env.UserID, id, ref)
	if err != nil {
		return Output{}, err
	}
	err = env.TaskTx(ctx, func(_ pgx.Tx, emit func(any) error) error {
		return emit(CardEvent{"card", id.String() + "/" + o.Ref, "flight_offer", FlightOfferCard{id.String(), o.Detail()}})
	})
	if err != nil {
		return Output{}, err
	}
	var b strings.Builder
	fmt.Fprintf(&b, "Shown to the user as a details card, so don't restate it; add only what matters for their choice.\nOffer %s: %s\n", o.Ref, offerLine(*o))
	for i, segs := range o.Segments {
		for _, s := range segs {
			fmt.Fprintf(&b, "Slice %d: %s %s→%s %s to %s", i+1, s.Flight, s.From, s.To, s.DepartAt, s.ArriveAt)
			if s.LayoverMinutes != nil {
				fmt.Fprintf(&b, ", then a %s wait", hm(*s.LayoverMinutes))
				if s.AirportChange {
					b.WriteString(" with an airport change")
				}
			}
			b.WriteString("\n")
		}
	}
	for _, bag := range o.Baggage {
		fmt.Fprintf(&b, "Baggage per passenger: %d %s\n", bag.Quantity, strings.ReplaceAll(bag.Type, "_", " "))
	}
	c := o.Conditions
	fmt.Fprintf(&b, "Refundable: %s. Changeable: %s.\n", rule(c.Refundable, c.RefundFee), rule(c.Changeable, c.ChangeFee))
	if o.Holdable {
		b.WriteString("It can be held without paying")
		if o.PayBy != nil {
			b.WriteString(", pay by " + *o.PayBy)
		}
		b.WriteString(".\n")
	} else {
		b.WriteString("It can't be held; the user books it with the airline.\n")
	}
	return Output{Text: b.String()}, nil
}

func rule(ok bool, fee *duffel.Money) string {
	switch {
	case !ok:
		return "no"
	case fee != nil:
		return "yes, fee " + fee.String()
	}
	return "yes"
}

func flightCheckout(ctx context.Context, env *Env, input json.RawMessage) (Output, error) {
	id, ref, err := offerID(input)
	if err != nil {
		return Output{}, err
	}
	if err := env.Flights.Checkout(ctx, env.UserID, id, ref); err != nil {
		return Output{}, err
	}
	return Output{Text: "Checkout shown to the user. They add travellers and confirm in the app. Never ask for passport or card details in chat."}, nil
}

func bookingGet(ctx context.Context, env *Env, input json.RawMessage) (Output, error) {
	var in struct{ CheckoutID string }
	if err := json.Unmarshal(input, &in); err != nil {
		return Output{}, err
	}
	var id *uuid.UUID
	if in.CheckoutID != "" {
		v, err := uuid.Parse(in.CheckoutID)
		if err != nil {
			return Output{}, errors.New("checkoutId must be a UUID")
		}
		id = &v
	}
	list, err := env.Flights.Bookings(ctx, env.UserID, id)
	if err != nil {
		return Output{}, err
	}
	if len(list) == 0 {
		return Output{Text: "No bookings."}, nil
	}
	var b strings.Builder
	for _, s := range list {
		bookingLine(&b, s)
	}
	return Output{Text: b.String()}, nil
}

func bookingLine(b *strings.Builder, s BookingStatus) {
	fmt.Fprintf(b, "%s: %s, %s", s.ID, s.Route, s.Status)
	if s.Reference != "" {
		b.WriteString(", reference " + s.Reference)
	}
	if s.PayBy != nil {
		b.WriteString(", pay by " + s.PayBy.Format(time.RFC3339))
	}
	if s.Error != "" {
		b.WriteString(", " + s.Error)
	}
	b.WriteString("\n")
}

func flightBook(ctx context.Context, env *Env, input json.RawMessage) (Output, error) {
	var in struct{ CheckoutID string }
	if err := json.Unmarshal(input, &in); err != nil {
		return Output{}, err
	}
	if in.CheckoutID == "" {
		return Output{}, errors.New("checkoutId is required")
	}
	id, err := uuid.Parse(in.CheckoutID)
	if err != nil {
		return Output{}, errors.New("checkoutId must be a UUID")
	}
	s, err := env.Flights.Book(ctx, env.UserID, id)
	if err != nil {
		return Output{}, err
	}
	var b strings.Builder
	bookingLine(&b, s)
	return Output{Text: b.String()}, nil
}
