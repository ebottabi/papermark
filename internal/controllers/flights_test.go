/*
 * Copyright (c) 2026. Papermark - All Rights Reserved
 * Unauthorized copying of this file, via any medium is strictly prohibited
 * Proprietary and confidential
 * Written by Ebot Tabi <ebot.tabi@gmail.com>, 18/10/2026
 */
package controllers_test

import (
	"context"
	"fmt"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"papermark/internal/controllers"
	"papermark/internal/database"
	"papermark/internal/integrations/duffel"
	"papermark/internal/router"
	"papermark/internal/services"

	"github.com/google/uuid"
)

func duffelOffer(id string, instant bool) string {
	return fmt.Sprintf(`{"id":%q,"total_amount":"100.00","total_currency":"EUR","base_amount":"80.00","tax_amount":"20.00",
		"owner":{"name":"TAP","iata_code":"TP","logo_symbol_url":null},
		"slices":[{"duration":"PT2H5M","segments":[{"marketing_carrier_flight_number":"1363","marketing_carrier":{"name":"TAP","iata_code":"TP"},
		"origin":{"iata_code":"LHR","city_name":"London","time_zone":"Europe/London"},"destination":{"iata_code":"LIS","city_name":"Lisbon","time_zone":"Europe/Lisbon"},
		"departing_at":"2026-10-23T18:05:00","arriving_at":"2026-10-23T21:10:00","duration":"PT2H5M","aircraft":null,"passengers":[{"cabin_class":"economy","baggages":[]}]}]}],
		"passengers":[{"id":"pas_1"}],"conditions":{"refund_before_departure":null,"change_before_departure":null},
		"payment_requirements":{"requires_instant_payment":%t,"payment_required_by":null,"price_guarantee_expires_at":null},
		"passenger_identity_documents_required":false,"expires_at":%q}`, id, instant, time.Now().Add(time.Hour).UTC().Format(time.RFC3339))
}

func TestFlightsAPI(t *testing.T) {
	pool, _ := database.NewTestDatabase(t)
	mux := http.NewServeMux()
	mux.HandleFunc("/air/offer_requests", func(w http.ResponseWriter, r *http.Request) {
		fmt.Fprintf(w, `{"data":{"offers":[%s,%s]}}`, duffelOffer("off_hold", false), duffelOffer("off_instant", true))
	})
	mux.HandleFunc("/air/offers/", func(w http.ResponseWriter, r *http.Request) {
		fmt.Fprintf(w, `{"data":%s}`, duffelOffer("off_hold", false))
	})
	mux.HandleFunc("/air/orders", func(w http.ResponseWriter, r *http.Request) {
		fmt.Fprint(w, `{"data":{"id":"ord_1","booking_reference":"K7Q2XM","payment_status":{"payment_required_by":null}}}`)
	})
	srv := httptest.NewServer(mux)
	t.Cleanup(srv.Close)
	svc := services.NewFlightService(pool, &duffel.Client{HTTP: srv.Client(), BaseURL: srv.URL, Token: "tok"}, nil, nil, nil, "https://api.keen.test", "off", 0)
	api := newAPI(t, &router.Config{Pool: pool, Flights: controllers.NewFlightController(svc)})
	res, err := svc.Search(context.Background(), api.user, uuid.New(), duffel.Query{Origin: "LHR", Destination: "LIS", DepartDate: "2026-10-23", Adults: 1, Cabin: "economy"})
	if err != nil {
		t.Fatal(err)
	}
	sid := res.ID.String()
	holdRef, instantRef := "", ""
	for _, o := range res.Offers {
		if o.Holdable {
			holdRef = o.Ref
		} else {
			instantRef = o.Ref
		}
	}

	code, body := api.do("GET", "/v1/flights/searches/"+sid+"/offers", "")
	if offers, _ := body.data()["offers"].([]any); code != 200 || len(offers) != 2 || offers[0].(map[string]any)["duffelId"] != nil {
		t.Fatalf("offers: %d %v", code, body)
	}
	code, body = api.do("GET", "/v1/flights/searches/"+sid+"/offers/"+holdRef, "")
	if d := body.data(); code != 200 || d["holdable"] != true || d["segments"] == nil || d["ref"] != holdRef {
		t.Fatalf("detail: %d %v", code, body)
	}
	if code, _ := api.do("GET", "/v1/flights/searches/"+sid+"/offers/99", ""); code != 404 {
		t.Errorf("unknown ref: %d", code)
	}

	// travellers: masked on the way out, validated on the way in
	code, body = api.do("POST", "/v1/travellers", `{"isSelf":true,"title":"mr","givenName":"Ebot","familyName":"Tabi","bornOn":"1990-01-02","gender":"m","email":"e@example.com","phone":"+447700900123","passport":{"number":"AB123456","country":"GB","expiresOn":"2035-01-01"}}`)
	tid, _ := body.data()["id"].(string)
	if p, _ := body.data()["passport"].(map[string]any); code != 201 || p["number"] != "••56" {
		t.Fatalf("create traveller: %d %v", code, body)
	}
	if code, _ := api.do("POST", "/v1/travellers", `{"phone":"nope"}`); code != 400 {
		t.Errorf("bad phone: %d", code)
	}
	if code, body = api.do("GET", "/v1/travellers", ""); code != 200 || len(body.data()["travellers"].([]any)) != 1 {
		t.Fatalf("list: %d %v", code, body)
	}

	code, body = api.do("POST", "/v1/checkouts", `{"searchId":"`+sid+`","ref":"`+holdRef+`"}`)
	cid := body.data()["id"].(string)
	if d := body.data(); code != 201 || d["mode"] != "hold" || d["status"] != "review" || d["booking"] != nil || d["handoffUrl"] != nil {
		t.Fatalf("checkout: %d %v", code, body)
	}
	for _, bad := range []string{`{}`, `{"searchId":"nope","ref":"1"}`, `{"searchId":"` + sid + `"}`} {
		if code, _ := api.do("POST", "/v1/checkouts", bad); code != 400 {
			t.Errorf("%s: %d", bad, code)
		}
	}
	for _, bad := range []string{`{}`, `{"travellers":"x"}`, `{"travellers":[1]}`, `{"travellers":["` + tid + `","` + tid + `"]}`} {
		if code, _ := api.do("PATCH", "/v1/checkouts/"+cid, bad); code != 400 {
			t.Errorf("patch %s: %d", bad, code)
		}
	}
	if code, body = api.do("PATCH", "/v1/checkouts/"+cid, `{"travellers":["`+tid+`"]}`); code != 200 || body.data()["status"] != "review" {
		t.Fatalf("patch: %d %v", code, body)
	}
	if code, _ := api.do("POST", "/v1/checkouts/"+cid+"/handoff", ""); code != 409 {
		t.Errorf("handoff on a holdable fare: %d", code)
	}

	other := api.as(newUser(t, pool))
	for _, r := range [][3]string{{"GET", "/v1/checkouts/" + cid, ""}, {"POST", "/v1/checkouts/" + cid + "/hold", ""},
		{"GET", "/v1/flights/searches/" + sid + "/offers", ""}, {"PATCH", "/v1/travellers/" + tid, `{}`}, {"DELETE", "/v1/travellers/" + tid, ""}} {
		if code, _ := other.do(r[0], r[1], r[2]); code != 404 {
			t.Errorf("%s %s as another user: %d", r[0], r[1], code)
		}
	}

	code, body = api.do("POST", "/v1/checkouts/"+cid+"/hold", "")
	if b, _ := body.data()["booking"].(map[string]any); code != 200 || body.data()["status"] != "held" || b["reference"] != "K7Q2XM" || b["payBy"] != nil {
		t.Fatalf("hold: %d %v", code, body)
	}
	if code, body = api.do("POST", "/v1/checkouts/"+cid+"/hold", ""); code != 200 || body.data()["status"] != "held" {
		t.Errorf("second hold: %d %v", code, body)
	}

	code, body = api.do("POST", "/v1/checkouts", `{"searchId":"`+sid+`","ref":"`+instantRef+`"}`)
	hid := body.data()["id"].(string)
	if code != 201 || body.data()["mode"] != "handoff" {
		t.Fatalf("handoff checkout: %d %v", code, body)
	}
	if code, body = api.do("POST", "/v1/checkouts/"+hid+"/handoff", ""); code != 200 || body.data()["status"] != "handoff" || body.data()["handoffUrl"] == nil {
		t.Errorf("handoff: %d %v", code, body)
	}
	if code, _ := api.do("DELETE", "/v1/travellers/"+tid, ""); code != 200 {
		t.Errorf("delete traveller: %d", code)
	}
}
