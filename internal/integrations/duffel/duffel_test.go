/*
 * Copyright (c) 2026. Papermark - All Rights Reserved
 * Unauthorized copying of this file, via any medium is strictly prohibited
 * Proprietary and confidential
 * Written by Ebot Tabi <ebot.tabi@gmail.com>, 18/10/2026
 */

package duffel

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"testing"
)

func seg(carrier, num, from, fromTZ, to, toTZ, dep, arr, dur string) string {
	return fmt.Sprintf(`{"marketing_carrier_flight_number":%q,"marketing_carrier":{"name":"Air %s","iata_code":%q},
		"origin":{"iata_code":%q,"city_name":"C-%s","time_zone":%q},"destination":{"iata_code":%q,"city_name":"C-%s","time_zone":%q},
		"origin_terminal":"2","destination_terminal":null,"departing_at":%q,"arriving_at":%q,"duration":%q,"aircraft":{"name":"Airbus A320"},
		"passengers":[{"cabin_class":"economy","baggages":[{"type":"carry_on","quantity":1},{"type":"checked","quantity":1}]}]}`,
		num, carrier, carrier, from, from, fromTZ, to, to, toTZ, dep, arr, dur)
}

func offer(id, total string, instant bool, dur string, segs ...string) string {
	body := ""
	for i, s := range segs {
		if i > 0 {
			body += ","
		}
		body += s
	}
	return fmt.Sprintf(`{"id":%q,"total_amount":%q,"total_currency":"EUR","base_amount":"80.00","tax_amount":"20.00",
		"owner":{"name":"Air TP","iata_code":"TP","logo_symbol_url":null},
		"slices":[{"duration":%q,"segments":[%s]}],"passengers":[{"id":"pas_1"}],
		"conditions":{"refund_before_departure":null,"change_before_departure":{"allowed":true,"penalty_amount":"25.00","penalty_currency":"EUR"}},
		"payment_requirements":{"requires_instant_payment":%t,"payment_required_by":"2026-10-20T12:00:00Z","price_guarantee_expires_at":null},
		"passenger_identity_documents_required":true,"expires_at":"2026-10-19T10:00:00Z"}`, id, total, dur, body, instant)
}

func stub(t *testing.T, orderBody *map[string]any) *Client {
	t.Helper()
	// off_a: cheap but slow, with a layover and an airport change.
	a := offer("off_a", "100.00", false, "PT10H",
		seg("IB", "1", "LHR", "Europe/London", "MAD", "Europe/Madrid", "2026-10-23T08:00:00", "2026-10-23T11:00:00", "PT2H"),
		seg("IB", "2", "TOJ", "Europe/Madrid", "LIS", "Europe/Lisbon", "2026-10-23T13:30:00", "2026-10-23T14:30:00", "PT2H"))
	b := offer("off_b", "300.00", true, "PT2H05M",
		seg("TP", "1363", "LHR", "Europe/London", "LIS", "Europe/Lisbon", "2026-10-23T18:05:00", "2026-10-23T21:10:00", "PT2H05M"))
	c := offer("off_c", "150.00", false, "PT5H",
		seg("TP", "9", "LHR", "Europe/London", "LIS", "Europe/Lisbon", "2026-10-23T06:00:00", "2026-10-23T09:00:00", "PT5H"))
	mux := http.NewServeMux()
	check := func(r *http.Request) {
		if r.Header.Get("Authorization") != "Bearer tok" || r.Header.Get("Duffel-Version") != "v2" {
			t.Errorf("headers: %v", r.Header)
		}
	}
	mux.HandleFunc("/places/suggestions", func(w http.ResponseWriter, r *http.Request) {
		check(r)
		fmt.Fprint(w, `{"data":[{"type":"airport","iata_code":"LIS"},{"type":"city","iata_code":"LON"}]}`)
	})
	mux.HandleFunc("/air/offer_requests", func(w http.ResponseWriter, r *http.Request) {
		check(r)
		if r.URL.Query().Get("return_offers") != "true" {
			t.Error("return_offers missing")
		}
		fmt.Fprintf(w, `{"data":{"offers":[%s,%s,%s]}}`, a, b, c)
	})
	mux.HandleFunc("/air/offers/off_b", func(w http.ResponseWriter, r *http.Request) { fmt.Fprintf(w, `{"data":%s}`, b) })
	mux.HandleFunc("/air/offers/gone", func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(422)
		fmt.Fprint(w, `{"errors":[{"type":"airline_error","code":"offer_no_longer_available","message":"gone"}],"meta":{"status":422}}`)
	})
	mux.HandleFunc("/air/orders", func(w http.ResponseWriter, r *http.Request) {
		check(r)
		raw, _ := io.ReadAll(r.Body)
		json.Unmarshal(raw, orderBody)
		fmt.Fprint(w, `{"data":{"id":"ord_1","booking_reference":"K7Q2XM","payment_status":{"payment_required_by":"2026-10-20T12:00:00Z"}}}`)
	})
	srv := httptest.NewServer(mux)
	t.Cleanup(srv.Close)
	return &Client{HTTP: srv.Client(), BaseURL: srv.URL, Token: "tok"}
}

func TestSearchNormalisesRanksAndLabels(t *testing.T) {
	c := stub(t, new(map[string]any))
	offers, err := c.Search(context.Background(), Query{Origin: "LHR", Destination: "LIS", DepartDate: "2026-10-23", Adults: 1, Cabin: "economy"})
	if err != nil || len(offers) != 3 {
		t.Fatalf("%v %d", err, len(offers))
	}
	// scores: a 100+200=300, b 300+40=340, c 150+100=250
	for i, want := range []struct{ id, label string }{{"off_c", "best"}, {"off_a", "cheapest"}, {"off_b", "fastest"}} {
		if o := offers[i]; o.DuffelID != want.id || o.Label != want.label || o.Ref != fmt.Sprint(i+1) {
			t.Errorf("offer %d = %s %q ref %s", i, o.DuffelID, o.Label, o.Ref)
		}
	}
	b := offers[2]
	if b.Slices[0].DepartAt != "2026-10-23T18:05" || b.Slices[0].Minutes != 125 || b.Slices[0].Stops != 0 || b.Segments[0][0].Flight != "TP1363" ||
		b.Segments[0][0].LayoverMinutes != nil || b.Holdable || b.Price.Amount != "300.00" || b.Owner.Logo != nil || !b.DocsRequired {
		t.Errorf("normalised: %+v", b)
	}
	if b.Segments[0][0].FromTerminal != "2" || b.Segments[0][0].ToTerminal != "" || len(b.Baggage) != 2 || b.Cabin != "economy" || b.PassengerIDs[0] != "pas_1" {
		t.Errorf("details: %+v", b)
	}
	a := offers[1]
	first := a.Segments[0][0]
	// 11:00 Madrid arrival, 13:30 departure from another airport.
	if first.LayoverMinutes == nil || *first.LayoverMinutes != 150 || !first.AirportChange || a.Slices[0].Stops != 1 || a.Slices[0].Minutes != 600 {
		t.Errorf("layover: %+v", a.Slices[0])
	}
	if a.Conditions.Refundable || !a.Conditions.Changeable || a.Conditions.ChangeFee == nil || a.Conditions.ChangeFee.Amount != "25.00" || a.Conditions.RefundFee != nil {
		t.Errorf("conditions: %+v", a.Conditions)
	}
	raw, _ := json.Marshal(a.Summary())
	var m map[string]any
	json.Unmarshal(raw, &m)
	if _, leaked := m["duffelId"]; leaked || m["ref"] != "2" {
		t.Errorf("summary leaks or lacks ref: %s", raw)
	}
}

func TestResolveHoldAndErrors(t *testing.T) {
	var order map[string]any
	c := stub(t, &order)
	ctx := context.Background()
	if code, err := c.Resolve(ctx, "lis"); err != nil || code != "LIS" {
		t.Errorf("iata: %q %v", code, err)
	}
	if code, err := c.Resolve(ctx, "London"); err != nil || code != "LON" {
		t.Errorf("city is preferred: %q %v", code, err)
	}
	if _, err := c.GetOffer(ctx, "gone"); err == nil {
		t.Fatal("want error")
	} else if ae := new(*APIError); !errors.As(err, ae) || (*ae).Code != "offer_no_longer_available" || (*ae).Status != 422 {
		t.Errorf("api error: %v", err)
	}
	o, err := c.CreateOrder(ctx, OrderRequest{Type: "hold", OfferID: "off_b", Passengers: []Passenger{{ID: "pas_1", Title: "mr", Gender: "m", GivenName: "Ebot", FamilyName: "Tabi", BornOn: "1990-01-02",
		Email: "e@example.com", PhoneNumber: "+447000000000", Documents: []Document{{"passport", "X123", "GB", "2030-01-01"}}}}})
	if err != nil || o.BookingReference != "K7Q2XM" || o.PayBy == nil || *o.PayBy != "2026-10-20T12:00:00Z" {
		t.Fatalf("hold: %+v %v", o, err)
	}
	data := order["data"].(map[string]any)
	p := data["passengers"].([]any)[0].(map[string]any)
	doc := p["identity_documents"].([]any)[0].(map[string]any)
	if data["type"] != "hold" || data["selected_offers"].([]any)[0] != "off_b" || data["payments"] != nil || p["id"] != "pas_1" ||
		p["given_name"] != "Ebot" || p["phone_number"] != "+447000000000" || doc["unique_identifier"] != "X123" || doc["issuing_country_code"] != "GB" {
		t.Errorf("hold body: %v", order)
	}
	if !PriceUp(Money{"212.40", "EUR"}, Money{"226.40", "EUR"}) || PriceUp(Money{"226.40", "EUR"}, Money{"226.4", "EUR"}) {
		t.Error("PriceUp")
	}
}
