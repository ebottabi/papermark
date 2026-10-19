/*
 * Copyright (c) 2026. Papermark - All Rights Reserved
 * Unauthorized copying of this file, via any medium is strictly prohibited
 * Proprietary and confidential
 * Written by Ebot Tabi <ebot.tabi@gmail.com>, 19/10/2026
 */

package stripe

import (
	"context"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"net/url"
	"testing"
	"time"
)

func TestMinorUnits(t *testing.T) {
	for _, c := range []struct {
		amount, cur string
		want        int64
	}{{"212.40", "EUR", 21240}, {"212.4", "gbp", 21240}, {"9", "USD", 900}, {"12000", "JPY", 12000}} {
		if got, err := MinorUnits(c.amount, c.cur); err != nil || got != c.want {
			t.Errorf("%s %s = %d %v", c.amount, c.cur, got, err)
		}
	}
	for _, c := range [][2]string{{"1.005", "EUR"}, {"12.5", "JPY"}, {"10.000", "KWD"}, {"0", "EUR"}, {"-3", "EUR"}, {"x", "EUR"}, {"1", "EURO"}} {
		if _, err := MinorUnits(c[0], c[1]); err == nil {
			t.Errorf("%v accepted", c)
		}
	}
}

func TestSessionAndCapture(t *testing.T) {
	var form url.Values
	var idem string
	mux := http.NewServeMux()
	mux.HandleFunc("/v1/checkout/sessions", func(w http.ResponseWriter, r *http.Request) {
		raw, _ := io.ReadAll(r.Body)
		form, _ = url.ParseQuery(string(raw))
		idem = r.Header.Get("Idempotency-Key")
		if r.Header.Get("Authorization") != "Bearer sk_test" {
			t.Error("auth header")
		}
		fmt.Fprint(w, `{"id":"cs_1","url":"https://checkout.stripe.com/c/cs_1","status":"open"}`)
	})
	captured := false
	mux.HandleFunc("/v1/payment_intents/pi_1/capture", func(w http.ResponseWriter, r *http.Request) {
		if captured {
			w.WriteHeader(400)
			fmt.Fprint(w, `{"error":{"type":"invalid_request_error","code":"payment_intent_unexpected_state","message":"done"}}`)
			return
		}
		captured = true
		fmt.Fprint(w, `{"id":"pi_1","status":"succeeded"}`)
	})
	mux.HandleFunc("/v1/payment_intents/pi_1", func(w http.ResponseWriter, r *http.Request) {
		fmt.Fprint(w, `{"id":"pi_1","status":"succeeded","amount":21240,"currency":"eur"}`)
	})
	mux.HandleFunc("/v1/payment_intents/pi_1/cancel", func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(400)
		fmt.Fprint(w, `{"error":{"code":"payment_intent_unexpected_state"}}`)
	})
	srv := httptest.NewServer(mux)
	defer srv.Close()
	c := &Client{HTTP: srv.Client(), BaseURL: srv.URL, Key: "sk_test"}
	ctx := context.Background()
	s, err := c.CreateSession(ctx, SessionRequest{Amount: 21240, Currency: "EUR", Name: "London → Lisbon", Metadata: map[string]string{"checkout_id": "c1"},
		SuccessURL: "https://x/ok", CancelURL: "https://x/no", ExpiresAt: time.Unix(1_800_000_000, 0), Idempotency: "pay-c1-1"})
	if err != nil || s.URL == "" {
		t.Fatal(s, err)
	}
	if form.Get("payment_intent_data[capture_method]") != "manual" || form.Get("line_items[0][price_data][unit_amount]") != "21240" ||
		form.Get("line_items[0][price_data][currency]") != "eur" || form.Get("metadata[checkout_id]") != "c1" ||
		form.Get("payment_intent_data[metadata][checkout_id]") != "c1" || form.Get("expires_at") != "1800000000" || idem != "pay-c1-1" {
		t.Errorf("form: %v %q", form, idem)
	}
	if err := c.Capture(ctx, "pi_1"); err != nil {
		t.Fatal(err)
	}
	if err := c.Capture(ctx, "pi_1"); err != nil {
		t.Errorf("a second capture should be fine: %v", err)
	}
	if err := c.Release(ctx, "pi_1"); err != ErrCaptured {
		t.Errorf("release after capture: %v", err)
	}
}
