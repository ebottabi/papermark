/*
 * Copyright (c) 2026. Papermark - All Rights Reserved
 * Unauthorized copying of this file, via any medium is strictly prohibited
 * Proprietary and confidential
 * Written by Ebot Tabi <ebot.tabi@gmail.com>, 19/10/2026
 */

// Package hmacsig verifies "t=<unix>,v1=<hex>" webhook signatures, the
// scheme Stripe and Duffel share: HMAC-SHA256 of "<t>.<body>".
package hmacsig

import (
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"strconv"
	"strings"
	"time"
)

// Tolerance is how old a signed timestamp may be.
const Tolerance = 5 * time.Minute

// ErrInvalid is a missing, stale or wrong signature.
var ErrInvalid = errors.New("hmacsig: invalid signature")

// Sign returns the header value for body at t, for tests.
func Sign(secret string, body []byte, t time.Time) string {
	ts := strconv.FormatInt(t.Unix(), 10)
	return "t=" + ts + ",v1=" + mac(secret, ts, body)
}

func mac(secret, ts string, body []byte) string {
	m := hmac.New(sha256.New, []byte(secret))
	m.Write([]byte(ts + "."))
	m.Write(body)
	return hex.EncodeToString(m.Sum(nil))
}

// Verify checks header against body. Any one v1 value may match, since a
// sender rolling its secret signs with both.
func Verify(header string, body []byte, secret string, now time.Time) error {
	if secret == "" {
		return ErrInvalid
	}
	var ts string
	var sigs []string
	for _, part := range strings.Split(header, ",") {
		k, v, _ := strings.Cut(strings.TrimSpace(part), "=")
		switch k {
		case "t":
			ts = v
		case "v1":
			sigs = append(sigs, v)
		}
	}
	sec, err := strconv.ParseInt(ts, 10, 64)
	if err != nil || len(sigs) == 0 {
		return ErrInvalid
	}
	if d := now.Sub(time.Unix(sec, 0)); d > Tolerance || d < -Tolerance {
		return ErrInvalid
	}
	want := mac(secret, ts, body)
	for _, s := range sigs {
		if hmac.Equal([]byte(s), []byte(want)) {
			return nil
		}
	}
	return ErrInvalid
}
