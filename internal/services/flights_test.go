/*
 * Copyright (c) 2026. Papermark - All Rights Reserved
 * Unauthorized copying of this file, via any medium is strictly prohibited
 * Proprietary and confidential
 * Written by Ebot Tabi <ebot.tabi@gmail.com>, 18/10/2026
 */
package services

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"papermark/internal/database"
	"papermark/internal/harness/tools"
	"papermark/internal/integrations/duffel"
	"papermark/internal/integrations/stripe"
	"papermark/internal/models"
	"papermark/internal/models/queries"
	"papermark/internal/secretbox"

	"github.com/google/uuid"
)

// duffelStub is a fake Duffel with one holdable and one instant-payment offer.
type duffelStub struct {
	price     string // current price of off_hold
	bagPrice  string // when set, offers one baggage service at this price
	offerErr  string // Duffel error code returned for GET /air/offers/*
	orderErr  string // Duffel error code returned for POST /air/orders
	payStatus string // status returned for POST /air/payments (default succeeded)
	orders    int
	lastOrder map[string]any
}

func (d *duffelStub) offer(id, price string, instant bool, extra string) string {
	return fmt.Sprintf(`{"id":%q,"total_amount":%q,"total_currency":"EUR","base_amount":"80.00","tax_amount":"20.00",
		"owner":{"name":"Air TP","iata_code":"TP","logo_symbol_url":null},
		"slices":[{"duration":"PT2H5M","segments":[{"id":"seg_1","marketing_carrier_flight_number":"1363","marketing_carrier":{"name":"TAP","iata_code":"TP"},
		"origin":{"iata_code":"LHR","city_name":"London","time_zone":"Europe/London"},"destination":{"iata_code":"LIS","city_name":"Lisbon","time_zone":"Europe/Lisbon"},
		"departing_at":"2026-10-23T18:05:00","arriving_at":"2026-10-23T21:10:00","duration":"PT2H5M","aircraft":{"name":"Airbus A320"},
		"passengers":[{"cabin_class":"economy","baggages":[{"type":"carry_on","quantity":1}]}]}]}],
		"passengers":[{"id":"pas_1"}],"conditions":{"refund_before_departure":null,"change_before_departure":null},
		"payment_requirements":{"requires_instant_payment":%t,"payment_required_by":"2026-10-25T12:00:00Z","price_guarantee_expires_at":null},
		"passenger_identity_documents_required":true,"expires_at":%q%s}`, id, price, instant, time.Now().Add(time.Hour).UTC().Format(time.RFC3339), extra)
}

// bagServices is the available_services block GET offers returns when asked.
func (d *duffelStub) bagServices() string {
	if d.bagPrice == "" {
		return ""
	}
	return fmt.Sprintf(`,"available_services":[{"id":"ase_bag","type":"baggage","total_amount":%q,"total_currency":"EUR",
		"passenger_ids":["pas_1"],"segment_ids":["seg_1"],"maximum_quantity":2,"metadata":{"maximum_weight_kg":23}}]`, d.bagPrice)
}

func (d *duffelStub) client(t *testing.T) *duffel.Client {
	mux := http.NewServeMux()
	apiErr := func(w http.ResponseWriter, code string) {
		w.WriteHeader(422)
		fmt.Fprintf(w, `{"errors":[{"type":"airline_error","code":%q,"message":"x"}]}`, code)
	}
	mux.HandleFunc("/air/offer_requests", func(w http.ResponseWriter, r *http.Request) {
		fmt.Fprintf(w, `{"data":{"offers":[%s,%s]}}`, d.offer("off_hold", d.price, false, ""), d.offer("off_instant", "500.00", true, ""))
	})
	mux.HandleFunc("/air/offers/", func(w http.ResponseWriter, r *http.Request) {
		if d.offerErr != "" {
			apiErr(w, d.offerErr)
			return
		}
		price, instant := d.price, false
		if strings.HasSuffix(r.URL.Path, "off_instant") {
			price, instant = "500.00", true
		}
		extra := ""
		if r.URL.Query().Get("return_available_services") == "true" {
			extra = d.bagServices()
		}
		fmt.Fprintf(w, `{"data":%s}`, d.offer("off_hold", price, instant, extra))
	})
	mux.HandleFunc("/air/orders", func(w http.ResponseWriter, r *http.Request) {
		if d.orderErr != "" {
			apiErr(w, d.orderErr)
			return
		}
		d.orders++
		raw, _ := io.ReadAll(r.Body)
		d.lastOrder = map[string]any{}
		json.Unmarshal(raw, &d.lastOrder)
		fmt.Fprint(w, `{"data":{"id":"ord_1","booking_reference":"K7Q2XM","payment_status":{"payment_required_by":"2026-10-25T12:00:00Z"}}}`)
	})
	mux.HandleFunc("/air/payments", func(w http.ResponseWriter, r *http.Request) {
		status := d.payStatus
		if status == "" {
			status = "succeeded"
		}
		fmt.Fprintf(w, `{"data":{"status":%q}}`, status)
	})
	srv := httptest.NewServer(mux)
	t.Cleanup(srv.Close)
	return &duffel.Client{HTTP: srv.Client(), BaseURL: srv.URL, Token: "tok"}
}

// stripeStub is a fake Stripe: a session with a manual-capture PaymentIntent.
type stripeStub struct {
	piStatus string // GetPaymentIntent status (default requires_capture)
	noPI     bool   // GetSession returns an empty payment_intent
	captures int
	releases int
}

func (s *stripeStub) client(t *testing.T) *stripe.Client {
	mux := http.NewServeMux()
	mux.HandleFunc("/v1/checkout/sessions", func(w http.ResponseWriter, r *http.Request) {
		fmt.Fprint(w, `{"id":"cs_1","url":"https://pay.stripe.test/cs_1","status":"open"}`)
	})
	mux.HandleFunc("/v1/checkout/sessions/", func(w http.ResponseWriter, r *http.Request) {
		pi := "pi_1"
		if s.noPI {
			pi = ""
		}
		fmt.Fprintf(w, `{"id":"cs_1","url":"https://pay.stripe.test/cs_1","status":"complete","payment_intent":%q}`, pi)
	})
	mux.HandleFunc("/v1/payment_intents/", func(w http.ResponseWriter, r *http.Request) {
		switch {
		case strings.HasSuffix(r.URL.Path, "/capture"):
			s.captures++
			fmt.Fprint(w, `{"id":"pi_1","status":"succeeded"}`)
		case strings.HasSuffix(r.URL.Path, "/cancel"):
			s.releases++
			fmt.Fprint(w, `{"id":"pi_1","status":"canceled"}`)
		default:
			st := s.piStatus
			if st == "" {
				st = "requires_capture"
			}
			fmt.Fprintf(w, `{"id":"pi_1","status":%q}`, st)
		}
	})
	srv := httptest.NewServer(mux)
	t.Cleanup(srv.Close)
	return &stripe.Client{HTTP: srv.Client(), BaseURL: srv.URL, Key: "sk_test"}
}

type flightEnv struct {
	svc    *FlightService
	stub   *duffelStub
	user   uuid.UUID
	cards  []tools.CardEvent
	notes  []string
	search *tools.FlightSearchResult
}

func buildEnv(t *testing.T, sc *stripe.Client, payments string, maxMinor int64) *flightEnv {
	t.Helper()
	pool, _ := database.NewTestDatabase(t)
	key := make([]byte, 32)
	box, err := secretbox.New(key)
	if err != nil {
		t.Fatal(err)
	}
	e := &flightEnv{stub: &duffelStub{price: "100.00", bagPrice: "30.00"}, user: testUser(t, pool)}
	e.svc = NewFlightService(pool, e.stub.client(t), box, nil, sc, "https://api.keen.test", payments, maxMinor)
	e.svc.emit = func(_ context.Context, _, _ uuid.UUID, c tools.CardEvent) error {
		e.cards = append(e.cards, c)
		return nil
	}
	e.svc.note = func(_ context.Context, _, _ uuid.UUID, text string) error {
		e.notes = append(e.notes, text)
		return nil
	}
	e.search, err = e.svc.Search(context.Background(), e.user, uuid.New(), duffel.Query{Origin: "LHR", Destination: "LIS", DepartDate: "2026-10-23", Adults: 1, Cabin: "economy"})
	if err != nil {
		t.Fatal(err)
	}
	return e
}

func newFlightEnv(t *testing.T) *flightEnv { return buildEnv(t, nil, "off", 0) }

// newPayEnv is an environment with payments on and a Stripe stub.
func newPayEnv(t *testing.T, maxMinor int64) (*flightEnv, *stripeStub) {
	ss := &stripeStub{}
	return buildEnv(t, ss.client(t), "pay", maxMinor), ss
}

// payReview creates a pay-mode checkout in review with a complete self traveller.
func (e *flightEnv) payReview(t *testing.T, ref string) uuid.UUID {
	t.Helper()
	ctx := context.Background()
	if ts, _ := e.svc.Travellers(ctx, e.user); len(ts) == 0 {
		if _, err := e.svc.CreateTraveller(ctx, e.user, []byte(fullTraveller)); err != nil {
			t.Fatal(err)
		}
	}
	c := e.checkout(t, ref)
	if c.Status != "review" || c.Mode != "pay" {
		t.Fatalf("payReview: %+v", c)
	}
	return uuid.MustParse(c.ID)
}

const fullTraveller = `{"isSelf":true,"title":"mr","givenName":"Ebot","familyName":"Tabi","bornOn":"1990-01-02","gender":"m","email":"e@example.com","phone":"+447700900123",
	"passport":{"number":"AB123456","country":"GB","expiresOn":"2035-01-01"},"loyalty":[{"airline":"TP","number":"TP998891"}],"seat":"aisle"}`

// holdRef is the ref of the holdable offer; the other one needs instant payment.
func (e *flightEnv) refs(t *testing.T) (hold, instant string) {
	t.Helper()
	for _, o := range e.search.Offers {
		if o.Holdable {
			hold = o.Ref
		} else {
			instant = o.Ref
		}
	}
	if hold == "" || instant == "" {
		t.Fatalf("offers: %+v", e.search.Offers)
	}
	return
}

func (e *flightEnv) checkout(t *testing.T, ref string) *Checkout {
	t.Helper()
	c, err := e.svc.CreateCheckout(context.Background(), e.user, e.search.ID, ref)
	if err != nil {
		t.Fatal(err)
	}
	return c
}

func TestModeRuleHoldVsHandoff(t *testing.T) {
	e := newFlightEnv(t)
	hold, instant := e.refs(t)
	if c := e.checkout(t, hold); c.Mode != "hold" || c.Status != "travellers" || len(c.Travellers) != 1 || c.Travellers[0].ProfileID != nil {
		t.Errorf("holdable: %+v", c)
	}
	c := e.checkout(t, instant)
	if c.Mode != "handoff" {
		t.Fatalf("instant payment: %+v", c)
	}
	if _, err := e.svc.Hold(context.Background(), e.user, uuid.MustParse(c.ID)); err == nil {
		t.Error("hold on a handoff checkout")
	}
	h, err := e.svc.Handoff(context.Background(), e.user, uuid.MustParse(c.ID))
	if err != nil || h.Status != "handoff" || h.HandoffURL == nil || !strings.HasPrefix(*h.HandoffURL, "https://www.google.com/travel/flights?q=") {
		t.Fatalf("handoff: %+v %v", h, err)
	}
	if len(e.cards) != 3 || e.cards[2].Kind != "checkout" || e.cards[2].ID != c.ID {
		t.Errorf("cards: %+v", e.cards)
	}
}

func TestProfilesAreMaskedAndMaskedPatchKeepsStored(t *testing.T) {
	e := newFlightEnv(t)
	ctx := context.Background()
	created, err := e.svc.CreateTraveller(ctx, e.user, []byte(fullTraveller))
	if err != nil || created.Passport.Number != "••56" || created.Loyalty[0].Number != "••91" {
		t.Fatalf("create: %+v %v", created, err)
	}
	var data string
	e.svc.pool.QueryRow(ctx, `SELECT data FROM traveller_profiles WHERE id = $1`, created.ID).Scan(&data)
	if strings.Contains(data, "AB123456") || strings.Contains(data, "Tabi") {
		t.Error("profile is stored unsealed")
	}
	id := uuid.MustParse(created.ID)
	patched, err := e.svc.PatchTraveller(ctx, e.user, id, []byte(`{"givenName":"Ebo","passport":{"number":"••56","country":"FR","expiresOn":"2035-01-01"},"loyalty":[{"airline":"TP","number":"••91"}]}`))
	if err != nil || patched.GivenName != "Ebo" || patched.FamilyName != "Tabi" || patched.Passport.Country != "FR" {
		t.Fatalf("patch: %+v %v", patched, err)
	}
	p, _ := e.svc.PatchTraveller(ctx, e.user, id, []byte(`{"seat":"window"}`))
	if p.Loyalty[0].Number != "••91" {
		t.Errorf("loyalty dropped by an unrelated patch: %+v", p.Loyalty)
	}
	// What leaves for Duffel carries the real numbers.
	row, _ := e.svc.open(mustTraveller(t, e, id))
	if row.Passport.Number != "AB123456" || row.Loyalty[0].Number != "TP998891" {
		t.Errorf("stored numbers changed: %+v", row)
	}
	list, _ := e.svc.Travellers(ctx, e.user)
	if len(list) != 1 || list[0].Passport.Number != "••56" {
		t.Errorf("list: %+v", list)
	}
	for _, bad := range []string{`{"phone":"07700"}`, `{"gender":"x"}`, `{"bornOn":"2999-01-01"}`, `{"passport":{"country":"GBR"}}`} {
		if _, err := e.svc.PatchTraveller(ctx, e.user, id, []byte(bad)); err == nil {
			t.Errorf("accepted %s", bad)
		}
	}
	// A second self profile replaces the first.
	second, err := e.svc.CreateTraveller(ctx, e.user, []byte(`{"isSelf":true,"givenName":"Two"}`))
	list, _ = e.svc.Travellers(ctx, e.user)
	if err != nil || !second.IsSelf || list[0].IsSelf {
		t.Errorf("one self: %+v %v", list, err)
	}
	if _, err := e.svc.PatchTraveller(ctx, uuid.New(), id, []byte(`{}`)); !IsNotFound(err) {
		t.Errorf("another user's profile: %v", err)
	}
}

func mustTraveller(t *testing.T, e *flightEnv, id uuid.UUID) *models.TravellerProfile {
	t.Helper()
	p, err := queries.GetTraveller(context.Background(), e.svc.pool, e.user, id)
	if err != nil {
		t.Fatal(err)
	}
	return p
}

func TestReadinessMissing(t *testing.T) {
	e := newFlightEnv(t)
	ctx := context.Background()
	hold, _ := e.refs(t)
	c := e.checkout(t, hold)
	partial, err := e.svc.CreateTraveller(ctx, e.user, []byte(`{"givenName":"Ebot","familyName":"Tabi"}`))
	if err != nil {
		t.Fatal(err)
	}
	v, err := e.svc.PatchTravellers(ctx, e.user, uuid.MustParse(c.ID), []string{partial.ID})
	want := "title,bornOn,gender,email,phone,passport"
	if err != nil || v.Status != "travellers" || v.Travellers[0].Ready || strings.Join(v.Travellers[0].Missing, ",") != want || v.Travellers[0].Name != "Ebot Tabi" {
		t.Fatalf("partial: %+v %v", v.Travellers, err)
	}
	full, _ := e.svc.CreateTraveller(ctx, e.user, []byte(fullTraveller))
	v, err = e.svc.PatchTravellers(ctx, e.user, uuid.MustParse(c.ID), []string{full.ID})
	if err != nil || v.Status != "review" || !v.Travellers[0].Ready || len(v.Travellers[0].Missing) != 0 {
		t.Fatalf("full: %+v %v", v, err)
	}
	if _, err := e.svc.PatchTravellers(ctx, e.user, uuid.MustParse(c.ID), []string{uuid.NewString()}); err == nil {
		t.Error("unknown profile accepted")
	}
	if _, err := e.svc.PatchTravellers(ctx, e.user, uuid.MustParse(c.ID), []string{}); err == nil {
		t.Error("wrong slot count accepted")
	}
}

// reviewed returns a checkout in review with a complete self traveller.
func (e *flightEnv) reviewed(t *testing.T) uuid.UUID {
	t.Helper()
	ctx := context.Background()
	if _, err := e.svc.CreateTraveller(ctx, e.user, []byte(fullTraveller)); err != nil {
		t.Fatal(err)
	}
	hold, _ := e.refs(t)
	c := e.checkout(t, hold) // slot 0 is filled with the self profile
	if c.Status != "review" {
		t.Fatalf("self profile not auto-filled: %+v", c)
	}
	return uuid.MustParse(c.ID)
}

func TestHoldSucceedsOnceAndNotesTheAgent(t *testing.T) {
	e := newFlightEnv(t)
	id := e.reviewed(t)
	ctx := context.Background()
	c, err := e.svc.Hold(ctx, e.user, id)
	if err != nil || c.Status != "held" || c.Booking == nil || c.Booking.Reference != "K7Q2XM" || c.Booking.PayBy == nil {
		t.Fatalf("hold: %+v %v", c, err)
	}
	// real numbers go to Duffel, with the Duffel passenger id
	pax := e.stub.lastOrder["data"].(map[string]any)["passengers"].([]any)[0].(map[string]any)
	doc := pax["identity_documents"].([]any)[0].(map[string]any)
	if pax["id"] != "pas_1" || doc["unique_identifier"] != "AB123456" || pax["phone_number"] != "+447700900123" {
		t.Errorf("order passenger: %v", pax)
	}
	again, err := e.svc.Hold(ctx, e.user, id)
	if err != nil || again.Status != "held" || e.stub.orders != 1 || len(e.notes) != 1 {
		t.Fatalf("second hold: %+v %v orders %d notes %v", again, err, e.stub.orders, e.notes)
	}
	if !strings.Contains(e.notes[0], "K7Q2XM") || !strings.Contains(e.notes[0], "London → Lisbon") || !strings.Contains(e.notes[0], "pay by 2026-10-25T12:00:00Z") {
		t.Errorf("note: %q", e.notes[0])
	}
	var statuses []string
	for _, c := range e.cards {
		if v, ok := c.Data.(*Checkout); ok {
			statuses = append(statuses, v.Status)
		}
	}
	if got := strings.Join(statuses, ","); got != "review,holding,held" {
		t.Errorf("card statuses %s", got)
	}
	bs, err := e.svc.Bookings(ctx, e.user, nil)
	if err != nil || len(bs) != 1 || bs[0].Status != "held" || bs[0].Reference != "K7Q2XM" {
		t.Errorf("bookings: %+v %v", bs, err)
	}
}

func TestHoldPriceWentUpStaysInReview(t *testing.T) {
	e := newFlightEnv(t)
	id := e.reviewed(t)
	e.stub.price = "226.40"
	c, err := e.svc.Hold(context.Background(), e.user, id)
	if err != nil || c.Status != "review" || c.Error == nil || *c.Error != "The price went up to €226.40." || c.Offer.Price.Amount != "226.40" || e.stub.orders != 0 {
		t.Fatalf("price up: %+v %v", c, err)
	}
	c, err = e.svc.Hold(context.Background(), e.user, id) // the user accepts the new price
	if err != nil || c.Status != "held" || c.Error != nil {
		t.Fatalf("hold at the new price: %+v %v", c, err)
	}
}

func TestHoldSoldOutAndExpired(t *testing.T) {
	for _, c := range []struct{ offerErr, orderErr, status, msg string }{
		{"offer_no_longer_available", "", "failed", "That flight just sold out."},
		{"offer_expired", "", "expired", "These prices expired. Search again."},
		{"", "offer_no_longer_available", "failed", "That flight just sold out."},
		{"", "offer_expired", "expired", "These prices expired. Search again."},
	} {
		e := newFlightEnv(t)
		id := e.reviewed(t)
		e.stub.offerErr, e.stub.orderErr = c.offerErr, c.orderErr
		v, err := e.svc.Hold(context.Background(), e.user, id)
		if err != nil || v.Status != c.status || v.Error == nil || *v.Error != c.msg || len(e.notes) != 0 {
			t.Errorf("%+v: %+v %v", c, v, err)
		}
	}
}

func TestOldCheckoutExpiresLazily(t *testing.T) {
	e := newFlightEnv(t)
	id := e.reviewed(t)
	e.svc.now = func() time.Time { return time.Now().Add(2 * time.Hour) }
	c, err := e.svc.GetCheckout(context.Background(), e.user, id)
	if err != nil || c.Status != "expired" || c.Error == nil {
		t.Fatalf("expired: %+v %v", c, err)
	}
	if _, err := e.svc.Hold(context.Background(), e.user, id); err == nil {
		t.Error("held an expired checkout")
	}
}

func TestOtherUsersCheckoutIsNotFound(t *testing.T) {
	e := newFlightEnv(t)
	id := e.reviewed(t)
	other := testUser(t, e.svc.pool)
	if _, err := e.svc.GetCheckout(context.Background(), other, id); !IsNotFound(err) {
		t.Errorf("get: %v", err)
	}
	if _, err := e.svc.CreateCheckout(context.Background(), other, e.search.ID, "1"); !IsNotFound(err) {
		t.Errorf("create from another user's search: %v", err)
	}
}

func TestTripsListsBookingsNotDrafts(t *testing.T) {
	e := newFlightEnv(t)
	ctx := context.Background()
	if _, err := e.svc.Hold(ctx, e.user, e.reviewed(t)); err != nil {
		t.Fatal(err)
	}
	trips, err := e.svc.Trips(ctx, e.user)
	if err != nil || len(trips) != 1 || trips[0].Status != "held" || trips[0].Booking == nil || trips[0].Booking.Reference != "K7Q2XM" {
		t.Fatalf("trips: %+v %v", trips, err)
	}
	if trips[0].Total == nil || trips[0].Total.Amount != "100.00" {
		t.Errorf("total: %+v", trips[0].Total)
	}
	// A checkout still in review is a draft, not a trip.
	e2 := newFlightEnv(t)
	e2.reviewed(t)
	if trips2, err := e2.svc.Trips(ctx, e2.user); err != nil || len(trips2) != 0 {
		t.Errorf("draft listed as a trip: %+v %v", trips2, err)
	}
}

func TestSetExtrasValidatesAndTotals(t *testing.T) {
	e, _ := newPayEnv(t, 0)
	ctx := context.Background()
	hold, _ := e.refs(t)
	id := e.payReview(t, hold)

	cat, err := e.svc.Extras(ctx, e.user, id)
	if err != nil || len(cat.Bags) != 1 || cat.Bags[0].Ref != "ase_bag" || cat.Bags[0].MaxQuantity != 2 || cat.Bags[0].Price.Amount != "30.00" {
		t.Fatalf("catalogue: %+v %v", cat, err)
	}
	for _, bad := range [][]Choice{{{Ref: "nope", Quantity: 1}}, {{Ref: "ase_bag", Quantity: 0}}, {{Ref: "ase_bag", Quantity: 3}}} {
		if _, err := e.svc.SetExtras(ctx, e.user, id, bad); err == nil {
			t.Errorf("accepted %+v", bad)
		}
	}
	v, err := e.svc.SetExtras(ctx, e.user, id, []Choice{{Ref: "ase_bag", Quantity: 2}})
	if err != nil || v.Total == nil || v.Total.Amount != "160.00" || len(v.Extras) != 1 || v.Extras[0].Quantity != 2 || v.Extras[0].Price.Amount != "30.00" {
		t.Fatalf("set: %+v %v", v, err)
	}
	// Once paying, extras are locked.
	if _, err := e.svc.Pay(ctx, e.user, id); err != nil {
		t.Fatal(err)
	}
	if _, err := e.svc.SetExtras(ctx, e.user, id, []Choice{{Ref: "ase_bag", Quantity: 1}}); err == nil {
		t.Error("extras changed after paying started")
	}
}

func TestPayGatesAndClaims(t *testing.T) {
	ctx := context.Background()
	// Payments off: there is no pay endpoint.
	off := newFlightEnv(t)
	if _, err := off.svc.Pay(ctx, off.user, off.reviewed(t)); err == nil {
		t.Error("pay with payments off")
	}
	// Over the ceiling: the €500 fare exceeds a €400 limit.
	over, _ := newPayEnv(t, 40000)
	_, instant := over.refs(t)
	if _, err := over.svc.Pay(ctx, over.user, over.payReview(t, instant)); err == nil {
		t.Error("over-limit pay accepted")
	}
	// Success: a session opens and the checkout is claimed as paying.
	e, _ := newPayEnv(t, 0)
	hold, _ := e.refs(t)
	id := e.payReview(t, hold)
	ps, err := e.svc.Pay(ctx, e.user, id)
	if err != nil || ps.PaymentURL != "https://pay.stripe.test/cs_1" || ps.Checkout.Status != "paying" {
		t.Fatalf("pay: %+v %v", ps, err)
	}
	row, _ := queries.GetCheckout(ctx, e.svc.pool, e.user, id)
	if row.Status != "paying" || row.PaymentID != "cs_1" {
		t.Errorf("not claimed: status %q payment %q", row.Status, row.PaymentID)
	}
	if _, err := e.svc.Pay(ctx, e.user, id); err == nil {
		t.Error("second pay on a paying checkout")
	}
}

// paying moves a pay-mode checkout for ref into the paying state.
func (e *flightEnv) paying(t *testing.T, ref string) uuid.UUID {
	t.Helper()
	id := e.payReview(t, ref)
	if _, err := e.svc.Pay(context.Background(), e.user, id); err != nil {
		t.Fatal(err)
	}
	return id
}

func TestConfirmBooksCapturesAndIsIdempotent(t *testing.T) {
	e, ss := newPayEnv(t, 0)
	ctx := context.Background()
	_, instant := e.refs(t)
	id := e.paying(t, instant)
	ss.piStatus = "requires_capture"
	v, err := e.svc.ConfirmPayment(ctx, e.user, id)
	if err != nil || v.Status != "booked" || v.Booking == nil || v.Booking.Reference != "K7Q2XM" || v.Booking.PaidAt == nil {
		t.Fatalf("confirm: %+v %v", v, err)
	}
	if ss.captures != 1 || e.stub.orders != 1 {
		t.Fatalf("captures %d orders %d", ss.captures, e.stub.orders)
	}
	again, err := e.svc.ConfirmPayment(ctx, e.user, id)
	if err != nil || again.Status != "booked" || ss.captures != 1 || e.stub.orders != 1 {
		t.Fatalf("second confirm booked twice: %+v captures %d orders %d", again, ss.captures, e.stub.orders)
	}
}

func TestConfirmHeldPaysExistingOrder(t *testing.T) {
	e, ss := newPayEnv(t, 0)
	ctx := context.Background()
	hold, _ := e.refs(t)
	id := e.payReview(t, hold)
	if _, err := e.svc.Hold(ctx, e.user, id); err != nil { // reserves the fare: order ord_1
		t.Fatal(err)
	}
	if _, err := e.svc.Pay(ctx, e.user, id); err != nil {
		t.Fatal(err)
	}
	ss.piStatus = "requires_capture"
	v, err := e.svc.ConfirmPayment(ctx, e.user, id)
	// The held order is paid, not re-created: /air/orders stays at the single hold call.
	if err != nil || v.Status != "booked" || ss.captures != 1 || e.stub.orders != 1 {
		t.Fatalf("held confirm: %+v captures %d orders %d", v, ss.captures, e.stub.orders)
	}
}

// newBalanceEnv books and pays from the Duffel balance, with no Stripe.
func newBalanceEnv(t *testing.T, maxMinor int64) *flightEnv {
	return buildEnv(t, nil, "balance", maxMinor)
}

func TestBookRefusesWhenNotBalanceMode(t *testing.T) {
	e, _ := newPayEnv(t, 0)
	ctx := context.Background()
	_, instant := e.refs(t)
	id := e.payReview(t, instant)
	if _, err := e.svc.Book(ctx, e.user, id); err == nil {
		t.Error("book accepted in pay mode")
	}
	if e.stub.orders != 0 {
		t.Errorf("order created: %d", e.stub.orders)
	}
}

func TestBookRefusesOverLimit(t *testing.T) {
	e := newBalanceEnv(t, 40000) // €400 ceiling, the instant fare is €500
	ctx := context.Background()
	_, instant := e.refs(t)
	id := e.payReview(t, instant)
	if _, err := e.svc.Book(ctx, e.user, id); err == nil {
		t.Error("over-limit book accepted")
	}
	if e.stub.orders != 0 {
		t.Errorf("order created despite the limit: %d", e.stub.orders)
	}
}

func TestBookFromReviewBooksFromBalance(t *testing.T) {
	e := newBalanceEnv(t, 0)
	ctx := context.Background()
	_, instant := e.refs(t)
	id := e.payReview(t, instant)
	bs, err := e.svc.Book(ctx, e.user, id)
	if err != nil || bs.Status != "booked" || bs.Reference != "K7Q2XM" {
		t.Fatalf("book: %+v %v", bs, err)
	}
	row, _ := queries.GetCheckout(ctx, e.svc.pool, e.user, id)
	if row.Status != "booked" || row.PaidAt == nil || row.BookingRef != "K7Q2XM" || e.stub.orders != 1 {
		t.Fatalf("row: %+v orders %d", row, e.stub.orders)
	}
	// A second book on a booked checkout refuses and creates no second order.
	if _, err := e.svc.Book(ctx, e.user, id); err == nil {
		t.Error("second book accepted")
	}
	if e.stub.orders != 1 {
		t.Errorf("orders after refused second book: %d", e.stub.orders)
	}
}

func TestBookFromHeldPaysExistingOrder(t *testing.T) {
	e := newBalanceEnv(t, 0)
	ctx := context.Background()
	hold, _ := e.refs(t)
	id := e.payReview(t, hold)
	if _, err := e.svc.Hold(ctx, e.user, id); err != nil { // reserves order ord_1
		t.Fatal(err)
	}
	bs, err := e.svc.Book(ctx, e.user, id)
	// The held order is paid, not re-created: /air/orders stays at the single hold call.
	if err != nil || bs.Status != "booked" || e.stub.orders != 1 {
		t.Fatalf("held book: %+v %v orders %d", bs, err, e.stub.orders)
	}
	row, _ := queries.GetCheckout(ctx, e.svc.pool, e.user, id)
	if row.Status != "booked" || row.PaidAt == nil {
		t.Fatalf("row: %+v", row)
	}
}

func TestConfirmRestoresWhenPaymentNotAuthorised(t *testing.T) {
	e, ss := newPayEnv(t, 0)
	ctx := context.Background()
	_, instant := e.refs(t)
	id := e.paying(t, instant)
	ss.piStatus = "canceled"
	v, err := e.svc.ConfirmPayment(ctx, e.user, id)
	if err != nil || v.Status != "review" || v.Error == nil || e.stub.orders != 0 || ss.captures != 0 {
		t.Fatalf("declined confirm: %+v %v orders %d", v, err, e.stub.orders)
	}
}

func TestConfirmReleasesWhenAirlineFails(t *testing.T) {
	e, ss := newPayEnv(t, 0)
	ctx := context.Background()
	_, instant := e.refs(t)
	id := e.paying(t, instant)
	ss.piStatus = "requires_capture"
	e.stub.orderErr = "offer_no_longer_available"
	v, err := e.svc.ConfirmPayment(ctx, e.user, id)
	if err != nil || v.Status != "review" || v.Error == nil || *v.Error != "That flight just sold out." || ss.releases != 1 || ss.captures != 0 {
		t.Fatalf("airline failure: %+v %v releases %d captures %d", v, err, ss.releases, ss.captures)
	}
}
