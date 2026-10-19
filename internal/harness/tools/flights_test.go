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
	"strings"
	"testing"
	"time"

	"papermark/internal/harness/skills"
	"papermark/internal/integrations/duffel"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
)

type fakeFlights struct {
	q        duffel.Query
	checkout string
	booked   *uuid.UUID
	canBook  bool
}

func (f *fakeFlights) Search(_ context.Context, _, _ uuid.UUID, q duffel.Query) (*FlightSearchResult, error) {
	f.q = q
	mk := func(ref, label string) duffel.Offer {
		return duffel.Offer{DuffelID: "off_secret", Ref: ref, Label: label, Price: duffel.Money{Amount: "212.40", Currency: "EUR"}, Owner: duffel.Owner{Name: "TAP", IATA: "TP"},
			Slices: []duffel.Slice{{Origin: "LHR", Destination: "LIS", DepartAt: "2026-10-23T18:05", ArriveAt: "2026-10-23T21:10", Minutes: 125}}}
	}
	offers := []duffel.Offer{mk("1", "best"), mk("2", "cheapest"), mk("3", "fastest"), mk("4", ""), mk("5", "")}
	return &FlightSearchResult{ID: uuid.MustParse("11111111-1111-1111-1111-111111111111"), Route: "London → Lisbon", Dates: "Fri 23 Oct", Passengers: 1, Cabin: "economy",
		Offers: offers, ExpiresAt: time.Date(2026, 10, 18, 12, 0, 0, 0, time.UTC)}, nil
}
func (f *fakeFlights) Offer(_ context.Context, _, _ uuid.UUID, ref string) (*duffel.Offer, error) {
	return &duffel.Offer{DuffelID: "off_secret", PassengerIDs: []string{"pas_secret"}, Ref: ref, Price: duffel.Money{Amount: "212.40", Currency: "EUR"},
		Owner: duffel.Owner{Name: "TAP", IATA: "TP"}, Holdable: true}, nil
}
func (f *fakeFlights) Checkout(_ context.Context, _, _ uuid.UUID, ref string) error {
	f.checkout = ref
	return nil
}
func (f *fakeFlights) Bookings(context.Context, uuid.UUID, *uuid.UUID) ([]BookingStatus, error) {
	return nil, nil
}
func (f *fakeFlights) Book(_ context.Context, _, checkoutID uuid.UUID) (BookingStatus, error) {
	f.booked = &checkoutID
	return BookingStatus{ID: checkoutID, Route: "London → Lisbon", Status: "booked", Reference: "K7Q2XM"}, nil
}
func (f *fakeFlights) CanBook() bool { return f.canBook }

func flightEnv(f Flights, events *[]any) *Env {
	return &Env{Flights: f, UserID: uuid.New(), TaskID: uuid.New(),
		TaskTx: func(_ context.Context, fn func(pgx.Tx, func(any) error) error) error {
			return fn(nil, func(e any) error { *events = append(*events, e); return nil })
		}}
}

func TestFlightSearchEmitsTheCard(t *testing.T) {
	var events []any
	f := &fakeFlights{}
	depart := time.Now().AddDate(0, 0, 10).Format("2006-01-02")
	out, err := flightSearch(context.Background(), flightEnv(f, &events), json.RawMessage(`{"origin":"London","destination":"LIS","departDate":"`+depart+`"}`))
	if err != nil || f.q.Adults != 1 || f.q.Cabin != "economy" || f.q.Origin != "London" {
		t.Fatalf("%v %+v", err, f.q)
	}
	if len(events) != 1 {
		t.Fatalf("events: %v", events)
	}
	raw, _ := json.Marshal(events[0])
	var card struct {
		Type, ID, Kind string
		Data           struct {
			SearchID, Route string
			Options         []map[string]any
			Total           int
			ExpiresAt       string
		}
	}
	json.Unmarshal(raw, &card)
	if card.Type != "card" || card.Kind != "flight_options" || card.ID != card.Data.SearchID || card.Data.Route != "London → Lisbon" ||
		len(card.Data.Options) != 3 || card.Data.Total != 5 || card.Data.ExpiresAt != "2026-10-18T12:00:00Z" || card.Data.Options[1]["label"] != "cheapest" {
		t.Errorf("card: %s", raw)
	}
	if strings.Contains(string(raw), "off_secret") || strings.Contains(out.Text, "off_secret") {
		t.Error("a Duffel id leaked")
	}
	if !strings.Contains(out.Text, "11111111-1111-1111-1111-111111111111") || !strings.Contains(out.Text, "2. [cheapest] €212.40") {
		t.Errorf("text: %s", out.Text)
	}
}

func TestFlightOfferShowsTheDetailCard(t *testing.T) {
	var events []any
	out, err := flightOffer(context.Background(), flightEnv(&fakeFlights{}, &events),
		json.RawMessage(`{"searchId":"11111111-1111-1111-1111-111111111111","ref":"4"}`))
	if err != nil || len(events) != 1 {
		t.Fatalf("%v %v", err, events)
	}
	raw, _ := json.Marshal(events[0])
	var card struct {
		Type, ID, Kind string
		Data           struct {
			SearchID string
			Offer    struct{ Ref string }
		}
	}
	json.Unmarshal(raw, &card)
	if card.Type != "card" || card.Kind != "flight_offer" || card.ID != "11111111-1111-1111-1111-111111111111/4" ||
		card.Data.SearchID != "11111111-1111-1111-1111-111111111111" || card.Data.Offer.Ref != "4" {
		t.Errorf("card: %s", raw)
	}
	if strings.Contains(string(raw), "_secret") || strings.Contains(out.Text, "_secret") {
		t.Error("a Duffel id leaked")
	}
	if !strings.Contains(out.Text, "details card") {
		t.Errorf("text: %s", out.Text)
	}
}

func TestSkillReadShowsTheSkillInUse(t *testing.T) {
	var events []any
	env := flightEnv(nil, &events)
	env.Skills = []skills.Skill{{Name: "book-flight", Description: "Find flights.", Instructions: "Do it."}}
	out, err := skillRead(context.Background(), env, json.RawMessage(`{"name":"book-flight"}`))
	if err != nil || out.Text != "Do it." || len(events) != 1 {
		t.Fatalf("%v %+v %v", err, out, events)
	}
	if raw, _ := json.Marshal(events[0]); string(raw) != `{"type":"skills","skills":[{"name":"book-flight","title":"Book flight","description":"Find flights."}]}` {
		t.Errorf("event: %s", raw)
	}
	if out, _ := skillRead(context.Background(), env, json.RawMessage(`{"name":"nope"}`)); !out.IsError || len(events) != 1 {
		t.Error("an unknown skill showed as in use")
	}
}

func TestFlightSearchValidatesInput(t *testing.T) {
	var events []any
	env := flightEnv(&fakeFlights{}, &events)
	future := time.Now().AddDate(0, 0, 10).Format("2006-01-02")
	for _, in := range []string{
		`{"origin":"","destination":"LIS","departDate":"` + future + `"}`,
		`{"origin":"LHR","destination":"LIS","departDate":"2020-01-01"}`,
		`{"origin":"LHR","destination":"LIS","departDate":"tomorrow"}`,
		`{"origin":"LHR","destination":"LIS","departDate":"` + future + `","returnDate":"2020-01-01"}`,
		`{"origin":"LHR","destination":"LIS","departDate":"` + future + `","adults":10}`,
		`{"origin":"LHR","destination":"LIS","departDate":"` + future + `","adults":-1}`,
		`{"origin":"LHR","destination":"LIS","departDate":"` + future + `","adults":8,"childAges":[3,4]}`,
		`{"origin":"LHR","destination":"LIS","departDate":"` + future + `","childAges":[18]}`,
		`{"origin":"LHR","destination":"LIS","departDate":"` + future + `","cabin":"coach"}`,
	} {
		if _, err := flightSearch(context.Background(), env, json.RawMessage(in)); err == nil {
			t.Errorf("accepted %s", in)
		}
	}
	if len(events) != 0 {
		t.Errorf("events on invalid input: %v", events)
	}
}

func TestFlightToolsAreOfferedOnlyWithFlights(t *testing.T) {
	names := func(env *Env) string {
		var n []string
		for _, tool := range Offered(env, false) {
			n = append(n, tool.Name)
		}
		return strings.Join(n, ",")
	}
	if strings.Contains(names(&Env{}), "flight_search") {
		t.Error("offered without Flights")
	}
	if got := names(&Env{Flights: &fakeFlights{}}); !strings.Contains(got, "flight_search,flight_offer,flight_checkout,booking_get") || strings.Contains(got, "flight_book") {
		t.Errorf("tools: %s", got)
	}
	if got := names(&Env{Flights: &fakeFlights{canBook: true}}); !strings.Contains(got, "booking_get,flight_book") {
		t.Errorf("flight_book not offered: %s", got)
	}
}

func TestFlightBookReportsTheBooking(t *testing.T) {
	f := &fakeFlights{}
	id := uuid.New()
	out, err := flightBook(context.Background(), &Env{Flights: f}, json.RawMessage(`{"checkoutId":"`+id.String()+`"}`))
	if err != nil || f.booked == nil || *f.booked != id || !strings.Contains(out.Text, "booked") || !strings.Contains(out.Text, "K7Q2XM") {
		t.Fatalf("book: %v %q", err, out.Text)
	}
	if _, err := flightBook(context.Background(), &Env{Flights: f}, json.RawMessage(`{}`)); err == nil {
		t.Error("missing checkoutId accepted")
	}
	if _, err := flightBook(context.Background(), &Env{Flights: f}, json.RawMessage(`{"checkoutId":"nope"}`)); err == nil {
		t.Error("non-uuid checkoutId accepted")
	}
}

func TestFlightCheckoutTellsTheModelToKeepDetailsOutOfChat(t *testing.T) {
	f := &fakeFlights{}
	id := uuid.NewString()
	out, err := flightCheckout(context.Background(), &Env{Flights: f}, json.RawMessage(`{"searchId":"`+id+`","ref":"2"}`))
	if err != nil || f.checkout != "2" || !strings.Contains(out.Text, "Never ask for passport") {
		t.Errorf("%v %q %q", err, f.checkout, out.Text)
	}
}
