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
	"errors"
	"fmt"
	"log"
	"net/url"
	"regexp"
	"strings"
	"time"

	"papermark/internal/harness"
	"papermark/internal/harness/tools"
	"papermark/internal/integrations/duffel"
	"papermark/internal/integrations/stripe"
	"papermark/internal/models"
	"papermark/internal/models/queries"
	"papermark/internal/secretbox"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
)

const (
	searchTTL      = 30 * time.Minute
	recentBookings = 10
	maskPrefix     = "••"
)

// Flight user-facing errors.
const (
	msgSoldOut = "That flight just sold out."
	msgExpired = "These prices expired. Search again."
	msgAirline = "Couldn't reach the airline. Try again."
)

var (
	phoneRe         = regexp.MustCompile(`^\+[1-9][0-9]{6,14}$`)
	countryRe       = regexp.MustCompile(`^[A-Z]{2}$`)
	airlineRe       = regexp.MustCompile(`^[A-Z0-9]{2}$`)
	travellerTitles = []string{"mr", "ms", "mrs", "miss", "dr"}
	seatChoices     = []string{"aisle", "window", "none", ""}
	errNoOffer      = &StatusError{404, "offer not found"}
)

// FlightService searches flights and runs holds. It implements tools.Flights.
type FlightService struct {
	pool   *pgxpool.Pool
	api    *duffel.Client
	stripe *stripe.Client // nil unless payments are "pay"
	box    *secretbox.Box // seals traveller profiles; nil keeps them in plaintext
	// payments is "off", "pay" or "balance"; "pay" only when stripe is set.
	payments  string
	publicURL string
	maxMinor  int64 // payment ceiling in Stripe minor units; 0 means no ceiling
	// emit shows a card in a task; note tells the task's agent something. Set
	// by NewFlightService from the task service; tests replace them.
	emit func(ctx context.Context, user, task uuid.UUID, card tools.CardEvent) error
	note func(ctx context.Context, user, task uuid.UUID, text string) error
	now  func() time.Time
}

// NewFlightService constructs a FlightService. tasks may be nil (no cards, no
// notes); a nil sc forces pay mode off, but balance mode needs no Stripe.
func NewFlightService(pool *pgxpool.Pool, api *duffel.Client, box *secretbox.Box, tasks *TaskService, sc *stripe.Client, publicURL, payments string, maxMinor int64) *FlightService {
	if payments == "pay" && sc == nil {
		payments = "off"
	}
	s := &FlightService{pool: pool, api: api, stripe: sc, box: box, payments: payments, publicURL: publicURL, maxMinor: maxMinor, now: time.Now}
	if tasks != nil {
		s.emit = func(ctx context.Context, user, task uuid.UUID, card tools.CardEvent) error {
			return tasks.ownedTx(ctx, user, task, func(_ pgx.Tx, _ *models.Task, emit harness.Emit) error { return emit(card) })
		}
		s.note = func(ctx context.Context, user, task uuid.UUID, text string) error {
			return tasks.Note(ctx, user, task, "", text)
		}
	}
	return s
}

// CanBook reports whether balance booking is enabled.
func (s *FlightService) CanBook() bool { return s.payments == "balance" }

// ---- search ----

var errNoPlace = func(what string) error { return badRequest("I couldn't find a place called %q.", what) }

func (s *FlightService) place(ctx context.Context, text string) (string, error) {
	code, err := s.api.Resolve(ctx, text)
	if errors.Is(err, duffel.ErrNoPlace) {
		return "", errNoPlace(text)
	}
	return code, err
}

func day(d string) string {
	t, err := time.Parse("2006-01-02", d)
	if err != nil {
		return d
	}
	return t.Format("Mon 2 Jan")
}

// Search runs q (origin and destination may be names) and stores the result.
func (s *FlightService) Search(ctx context.Context, user, task uuid.UUID, q duffel.Query) (*tools.FlightSearchResult, error) {
	var err error
	if q.Origin, err = s.place(ctx, q.Origin); err != nil {
		return nil, err
	}
	if q.Destination, err = s.place(ctx, q.Destination); err != nil {
		return nil, err
	}
	offers, err := s.api.Search(ctx, q)
	if err != nil {
		return nil, fmt.Errorf("flight search: %w", err)
	}
	route, dates := q.Origin+" → "+q.Destination, day(q.DepartDate)
	if q.ReturnDate != "" {
		dates += " – " + day(q.ReturnDate)
	}
	expires := s.now().Add(searchTTL)
	if len(offers) > 0 {
		sl := offers[0].Slices[0]
		route = sl.OriginCity + " → " + sl.DestinationCity
	}
	for _, o := range offers {
		if t, err := time.Parse(time.RFC3339, o.ExpiresAt); err == nil && t.Before(expires) {
			expires = t
		}
	}
	raw, err := json.Marshal(offers)
	if err != nil {
		return nil, err
	}
	pax := q.Adults + len(q.ChildAges)
	fs := &models.FlightSearch{UserID: user, TaskID: task, Route: route, Dates: dates, Passengers: pax, Cabin: q.Cabin, Offers: raw, ExpiresAt: expires}
	if err := queries.InsertFlightSearch(ctx, s.pool, fs); err != nil {
		return nil, err
	}
	return &tools.FlightSearchResult{ID: fs.ID, Route: route, Dates: dates, Passengers: pax, Cabin: q.Cabin, Offers: offers, ExpiresAt: expires}, nil
}

func (s *FlightService) search(ctx context.Context, user, id uuid.UUID) (*models.FlightSearch, []duffel.Offer, error) {
	fs, err := queries.GetFlightSearch(ctx, s.pool, user, id)
	if err != nil {
		return nil, nil, err
	}
	var offers []duffel.Offer
	return fs, offers, json.Unmarshal(fs.Offers, &offers)
}

func pick(offers []duffel.Offer, ref string) (duffel.Offer, bool) {
	for _, o := range offers {
		if o.Ref == ref {
			return o, true
		}
	}
	return duffel.Offer{}, false
}

// Offers returns every offer of a search, ranked.
func (s *FlightService) Offers(ctx context.Context, user, id uuid.UUID) ([]duffel.Summary, error) {
	_, offers, err := s.search(ctx, user, id)
	out := make([]duffel.Summary, len(offers))
	for i, o := range offers {
		out[i] = o.Summary()
	}
	return out, err
}

// fresh re-reads an offer from Duffel, keeping its place in the search.
func (s *FlightService) fresh(ctx context.Context, stored duffel.Offer) (*duffel.Offer, error) {
	o, err := s.api.GetOffer(ctx, stored.DuffelID)
	if err != nil {
		return nil, offerError(err)
	}
	o.Ref, o.Label = stored.Ref, stored.Label
	return o, nil
}

// offerError turns Duffel's offer errors into user-facing ones; other errors pass through.
func offerError(err error) error {
	var ae *duffel.APIError
	if errors.As(err, &ae) {
		switch ae.Code {
		case "offer_no_longer_available":
			return &StatusError{409, msgSoldOut}
		case "offer_expired", "not_found":
			return &StatusError{409, msgExpired}
		}
	}
	return err
}

// Offer returns one stored offer with its current price and rules.
func (s *FlightService) Offer(ctx context.Context, user, search uuid.UUID, ref string) (*duffel.Offer, error) {
	_, offers, err := s.search(ctx, user, search)
	if err != nil {
		return nil, err
	}
	stored, ok := pick(offers, ref)
	if !ok {
		return nil, errNoOffer
	}
	return s.fresh(ctx, stored)
}

// ---- travellers ----

// ErrNoSuchTraveller is a profile id that is not the user's.
var ErrNoSuchTraveller = badRequest("unknown traveller")

func (s *FlightService) open(p *models.TravellerProfile) (Traveller, error) {
	var t Traveller
	plain, err := s.box.Open(p.Data)
	if err == nil {
		err = json.Unmarshal([]byte(plain), &t)
	}
	t.ID, t.IsSelf = p.ID.String(), p.IsSelf
	if t.Loyalty == nil {
		t.Loyalty = []Loyalty{}
	}
	return t, err
}

// Travellers lists the user's profiles with passport and loyalty numbers masked.
func (s *FlightService) Travellers(ctx context.Context, user uuid.UUID) ([]Traveller, error) {
	ps, err := queries.ListTravellers(ctx, s.pool, user)
	if err != nil {
		return nil, err
	}
	out := make([]Traveller, 0, len(ps))
	for _, p := range ps {
		t, err := s.open(p)
		if err != nil {
			return nil, err
		}
		out = append(out, t.Masked())
	}
	return out, nil
}

func inList(list []string, v string) bool {
	for _, s := range list {
		if s == v {
			return true
		}
	}
	return false
}

func isDate(v string) (time.Time, bool) {
	t, err := time.Parse("2006-01-02", v)
	return t, err == nil
}

// validate checks the fields that are present; missing ones are reported by readiness.
func (s *FlightService) validate(t *Traveller) error {
	switch {
	case t.Title != "" && !inList(travellerTitles, t.Title):
		return badRequest("title must be mr, ms, mrs, miss or dr")
	case t.Gender != "" && t.Gender != "m" && t.Gender != "f":
		return badRequest("gender must be m or f")
	case len(t.GivenName) > 100 || len(t.FamilyName) > 100:
		return badRequest("name is too long")
	case t.Email != "" && (!strings.Contains(t.Email, "@") || len(t.Email) > 254 || strings.ContainsAny(t.Email, " \r\n")):
		return badRequest("email is not valid")
	case t.Phone != "" && !phoneRe.MatchString(t.Phone):
		return badRequest("phone must be international, like +447700900123")
	case !inList(seatChoices, t.Seat):
		return badRequest("seat must be aisle, window or none")
	}
	if t.BornOn != "" {
		if d, ok := isDate(t.BornOn); !ok || d.After(s.now()) {
			return badRequest("bornOn must be a past date like 1990-04-21")
		}
	}
	if p := t.Passport; p != nil {
		if p.Number != "" && len(p.Number) > 20 {
			return badRequest("passport number is not valid")
		}
		if p.Country != "" && !countryRe.MatchString(p.Country) {
			return badRequest("passport country must be a 2-letter code like GB")
		}
		if p.ExpiresOn != "" {
			if d, ok := isDate(p.ExpiresOn); !ok || d.Before(s.now()) {
				return badRequest("passport expiresOn must be a future date")
			}
		}
	}
	for _, l := range t.Loyalty {
		if !airlineRe.MatchString(l.Airline) || l.Number == "" || len(l.Number) > 30 {
			return badRequest("loyalty needs a 2-letter airline code and a number")
		}
	}
	return nil
}

func (s *FlightService) saveTraveller(ctx context.Context, user uuid.UUID, existing *models.TravellerProfile, raw []byte) (Traveller, error) {
	var t Traveller
	var old Traveller
	if existing != nil {
		var err error
		if old, err = s.open(existing); err != nil {
			return t, err
		}
		t = old
		if old.Passport != nil {
			// A copy, so unmarshalling the patch can't overwrite the stored number.
			pp := *old.Passport
			t.Passport = &pp
		}
	}
	// Unmarshalling over the stored profile changes only the fields sent.
	oldLoyalty := append([]Loyalty(nil), old.Loyalty...)
	t.Loyalty = nil
	if err := json.Unmarshal(raw, &t); err != nil {
		return t, badRequest("body must be a JSON object")
	}
	if !strings.Contains(string(raw), `"loyalty"`) {
		t.Loyalty = oldLoyalty
	}
	if t.Passport != nil && strings.HasPrefix(t.Passport.Number, maskPrefix) {
		if old.Passport == nil {
			return t, badRequest("passport number is not valid")
		}
		t.Passport.Number = old.Passport.Number
	}
	for i, l := range t.Loyalty {
		if !strings.HasPrefix(l.Number, maskPrefix) {
			continue
		}
		t.Loyalty[i].Number = ""
		for _, o := range oldLoyalty {
			if o.Airline == l.Airline {
				t.Loyalty[i].Number = o.Number
			}
		}
	}
	if err := s.validate(&t); err != nil {
		return t, err
	}
	data, err := json.Marshal(Traveller{Title: t.Title, GivenName: t.GivenName, FamilyName: t.FamilyName, BornOn: t.BornOn, Gender: t.Gender,
		Email: t.Email, Phone: t.Phone, Passport: t.Passport, Loyalty: t.Loyalty, Seat: t.Seat})
	if err != nil {
		return t, err
	}
	sealed, err := s.box.Seal(string(data))
	if err != nil {
		return t, err
	}
	err = pgx.BeginFunc(ctx, s.pool, func(tx pgx.Tx) error {
		var p *models.TravellerProfile
		if existing == nil {
			p = &models.TravellerProfile{UserID: user, IsSelf: t.IsSelf, Data: sealed}
			if t.IsSelf { // clear first so the one-self index holds
				if err := queries.ClearSelfTraveller(ctx, tx, user, uuid.Nil); err != nil {
					return err
				}
			}
			if err := queries.InsertTraveller(ctx, tx, p); err != nil {
				return err
			}
		} else {
			if t.IsSelf {
				if err := queries.ClearSelfTraveller(ctx, tx, user, existing.ID); err != nil {
					return err
				}
			}
			var err error
			if p, err = queries.UpdateTraveller(ctx, tx, user, existing.ID, t.IsSelf, sealed); err != nil {
				return err
			}
		}
		t.ID = p.ID.String()
		return nil
	})
	return t.Masked(), err
}

// CreateTraveller saves a new profile from a JSON body.
func (s *FlightService) CreateTraveller(ctx context.Context, user uuid.UUID, raw []byte) (Traveller, error) {
	return s.saveTraveller(ctx, user, nil, raw)
}

// PatchTraveller changes the fields present in raw. A masked number keeps the stored one.
func (s *FlightService) PatchTraveller(ctx context.Context, user, id uuid.UUID, raw []byte) (Traveller, error) {
	p, err := queries.GetTraveller(ctx, s.pool, user, id)
	if err != nil {
		return Traveller{}, err
	}
	return s.saveTraveller(ctx, user, p, raw)
}

// DeleteTraveller removes a profile.
func (s *FlightService) DeleteTraveller(ctx context.Context, user, id uuid.UUID) error {
	return queries.DeleteTraveller(ctx, s.pool, user, id)
}

// missing lists the camelCase fields a profile still needs for the offer.
func missing(t Traveller, docs bool) []string {
	out := []string{}
	for _, f := range []struct {
		name string
		ok   bool
	}{{"title", t.Title != ""}, {"givenName", t.GivenName != ""}, {"familyName", t.FamilyName != ""}, {"bornOn", t.BornOn != ""},
		{"gender", t.Gender != ""}, {"email", t.Email != ""}, {"phone", t.Phone != ""}} {
		if !f.ok {
			out = append(out, f.name)
		}
	}
	if docs && (t.Passport == nil || t.Passport.Number == "" || t.Passport.Country == "" || t.Passport.ExpiresOn == "") {
		out = append(out, "passport")
	}
	return out
}

// ---- checkout ----

func parseIDs(raw json.RawMessage) []string {
	ids := []string{}
	_ = json.Unmarshal(raw, &ids)
	return ids
}

func (s *FlightService) slot(ctx context.Context, user uuid.UUID, id string, docs bool, n int) (TravellerSlot, error) {
	slot := TravellerSlot{Name: fmt.Sprintf("Traveller %d", n+1), Missing: []string{"profile"}}
	pid, err := uuid.Parse(id)
	if err != nil {
		return slot, nil
	}
	p, err := queries.GetTraveller(ctx, s.pool, user, pid)
	if IsNotFound(err) {
		return slot, nil
	} else if err != nil {
		return slot, err
	}
	t, err := s.open(p)
	if err != nil {
		return slot, err
	}
	slot.ProfileID, slot.Missing = &id, missing(t, docs)
	slot.Ready = len(slot.Missing) == 0
	if name := strings.TrimSpace(t.GivenName + " " + t.FamilyName); name != "" {
		slot.Name = name
	}
	return slot, nil
}

func (s *FlightService) view(ctx context.Context, c *models.Checkout) (*Checkout, error) {
	var offer duffel.Detail
	if err := json.Unmarshal(c.Offer, &offer); err != nil {
		return nil, err
	}
	out := &Checkout{ID: c.ID.String(), TaskID: c.TaskID.String(), SearchID: c.SearchID.String(), Ref: c.Ref, Status: c.Status, Mode: c.Mode,
		Offer: offer, Travellers: []TravellerSlot{}, ExpiresAt: c.ExpiresAt.Format(time.RFC3339)}
	for i, id := range parseIDs(c.Travellers) {
		slot, err := s.slot(ctx, c.UserID, id, offer.DocsRequired, i)
		if err != nil {
			return nil, err
		}
		out.Travellers = append(out.Travellers, slot)
	}
	if c.BookingRef != "" {
		b := &Booking{Reference: c.BookingRef}
		if c.PayBy != nil {
			p := c.PayBy.Format(time.RFC3339)
			b.PayBy = &p
		}
		if c.PaidAt != nil {
			p := c.PaidAt.Format(time.RFC3339)
			b.PaidAt = &p
		}
		out.Booking = b
	}
	if c.HandoffURL != "" {
		out.HandoffURL = &c.HandoffURL
	}
	if c.Error != "" {
		out.Error = &c.Error
	}
	out.Extras = []CheckoutExtra{}
	_ = json.Unmarshal(c.Extras, &out.Extras)
	total := checkoutTotal(offer, out.Extras)
	out.Total = &total
	if c.DepartsAt != nil {
		d := c.DepartsAt.UTC().Format(time.RFC3339)
		out.DepartsAt = &d
	}
	if c.Change != "" {
		out.Change = &c.Change
	}
	if c.Mode == "pay" {
		out.CanHold = offer.Holdable && c.Status == models.CheckoutReview
		out.CanPay = c.Status == models.CheckoutReview || c.Status == models.CheckoutHeld
	}
	return out, nil
}

// show emits the checkout card. A failure to show must not undo what happened.
func (s *FlightService) show(ctx context.Context, c *models.Checkout) *Checkout {
	v, err := s.view(ctx, c)
	if err != nil {
		log.Printf("flights: checkout %s view: %v", c.ID, err)
		return nil
	}
	if s.emit != nil {
		if err := s.emit(ctx, c.UserID, c.TaskID, tools.CardEvent{Type: "card", ID: v.ID, Kind: "checkout", Data: v}); err != nil {
			log.Printf("flights: checkout %s card: %v", c.ID, err)
		}
	}
	return v
}

// Checkout starts a checkout for an offer and shows its card (tools.Flights).
func (s *FlightService) Checkout(ctx context.Context, user, search uuid.UUID, ref string) error {
	_, err := s.CreateCheckout(ctx, user, search, ref)
	return err
}

// CreateCheckout starts a checkout. Mode is hold when the airline lets us hold the fare, else handoff.
func (s *FlightService) CreateCheckout(ctx context.Context, user, search uuid.UUID, ref string) (*Checkout, error) {
	fs, offers, err := s.search(ctx, user, search)
	if err != nil {
		return nil, err
	}
	o, ok := pick(offers, ref)
	if !ok {
		return nil, errNoOffer
	}
	exp, err := time.Parse(time.RFC3339, o.ExpiresAt)
	if err != nil {
		exp = fs.ExpiresAt
	}
	if !exp.After(s.now()) {
		return nil, &StatusError{409, msgExpired}
	}
	mode := "handoff"
	if o.Holdable {
		mode = "hold"
	}
	if s.payments == "pay" || s.payments == "balance" {
		mode = "pay" // Keen collects the payment; canHold/canPay drive the buttons
	}
	ids := make([]string, o.Passengers)
	if selfs, err := queries.ListTravellers(ctx, s.pool, user); err == nil {
		for _, p := range selfs {
			if p.IsSelf {
				ids[0] = p.ID.String()
			}
		}
	}
	detail, _ := json.Marshal(o.Detail())
	idsJSON, _ := json.Marshal(ids)
	c := &models.Checkout{UserID: user, TaskID: fs.TaskID, SearchID: fs.ID, Ref: o.Ref, Status: models.CheckoutTravellers, Mode: mode,
		Travellers: idsJSON, Offer: detail, ExpiresAt: exp}
	c.Status = s.readiness(ctx, c)
	if err := queries.InsertCheckout(ctx, s.pool, c); err != nil {
		return nil, err
	}
	return s.show(ctx, c), nil
}

// readiness is review when every slot is ready, else travellers.
func (s *FlightService) readiness(ctx context.Context, c *models.Checkout) string {
	v, err := s.view(ctx, c)
	if err != nil || len(v.Travellers) == 0 {
		return models.CheckoutTravellers
	}
	for _, t := range v.Travellers {
		if !t.Ready {
			return models.CheckoutTravellers
		}
	}
	return models.CheckoutReview
}

// load returns the user's checkout, moving an open one past its expiry to expired.
func (s *FlightService) load(ctx context.Context, user, id uuid.UUID) (*models.Checkout, error) {
	c, err := queries.GetCheckout(ctx, s.pool, user, id)
	if err != nil {
		return nil, err
	}
	if (c.Status == models.CheckoutTravellers || c.Status == models.CheckoutReview) && !c.ExpiresAt.After(s.now()) {
		c.Status, c.Error = models.CheckoutExpired, msgExpired
		if err := queries.UpdateCheckout(ctx, s.pool, c); err != nil {
			return nil, err
		}
		s.show(ctx, c)
	}
	return c, nil
}

// GetCheckout returns one checkout.
func (s *FlightService) GetCheckout(ctx context.Context, user, id uuid.UUID) (*Checkout, error) {
	c, err := s.load(ctx, user, id)
	if err != nil {
		return nil, err
	}
	return s.view(ctx, c)
}

// PatchTravellers sets the profile of each passenger slot ("" leaves a slot empty).
func (s *FlightService) PatchTravellers(ctx context.Context, user, id uuid.UUID, ids []string) (*Checkout, error) {
	c, err := s.load(ctx, user, id)
	if err != nil {
		return nil, err
	}
	if c.Status != models.CheckoutTravellers && c.Status != models.CheckoutReview {
		return nil, conflict("this checkout can no longer be changed")
	}
	if want := len(parseIDs(c.Travellers)); len(ids) != want {
		return nil, badRequest("travellers needs %d entries", want)
	}
	seen := map[string]bool{}
	for _, v := range ids {
		if v == "" {
			continue
		}
		pid, err := uuid.Parse(v)
		if err != nil || seen[v] {
			return nil, ErrNoSuchTraveller
		}
		seen[v] = true
		if _, err := queries.GetTraveller(ctx, s.pool, user, pid); err != nil {
			if IsNotFound(err) {
				return nil, ErrNoSuchTraveller
			}
			return nil, err
		}
	}
	c.Travellers, _ = json.Marshal(ids)
	c.Error = ""
	c.Status = s.readiness(ctx, c)
	if err := queries.UpdateCheckout(ctx, s.pool, c); err != nil {
		return nil, err
	}
	return s.show(ctx, c), nil
}

// Hold re-prices the offer and reserves it without paying. A second call on a held checkout returns it.
func (s *FlightService) Hold(ctx context.Context, user, id uuid.UUID) (*Checkout, error) {
	c, err := s.load(ctx, user, id)
	if err != nil {
		return nil, err
	}
	switch {
	case c.Status == models.CheckoutHeld:
		return s.view(ctx, c)
	case !s.canHold(c):
		return nil, conflict("this fare can't be held")
	case c.Status != models.CheckoutReview:
		return nil, conflict("add every traveller before holding")
	}
	won, err := queries.MoveCheckout(ctx, s.pool, user, id, models.CheckoutReview, models.CheckoutHolding)
	if err != nil {
		return nil, err
	}
	if !won {
		return nil, conflict("this checkout is already being held")
	}
	c.Status, c.Error = models.CheckoutHolding, ""
	s.show(ctx, c)
	// The outcome is written even when the client has gone.
	wctx := context.WithoutCancel(ctx)
	if err := s.runHold(wctx, c); err != nil {
		log.Printf("flights: hold %s: %v", c.ID, err)
	}
	if err := queries.UpdateCheckout(wctx, s.pool, c); err != nil {
		return nil, err
	}
	v := s.show(wctx, c)
	if c.Status == models.CheckoutHeld && s.note != nil {
		var o duffel.Detail
		_ = json.Unmarshal(c.Offer, &o)
		text := fmt.Sprintf("The user held booking %s for %s", c.BookingRef, routeOf(o))
		if c.PayBy != nil {
			text += ", pay by " + c.PayBy.Format(time.RFC3339)
		}
		if err := s.note(wctx, user, c.TaskID, text+"."); err != nil {
			log.Printf("flights: hold %s note: %v", c.ID, err)
		}
	}
	return v, nil
}

func routeOf(o duffel.Detail) string {
	if len(o.Slices) == 0 {
		return "the trip"
	}
	return o.Slices[0].OriginCity + " → " + o.Slices[0].DestinationCity
}

// runHold fills c with the outcome: held, back to review with a new price, failed or expired.
func (s *FlightService) runHold(ctx context.Context, c *models.Checkout) error {
	_, offers, err := s.search(ctx, c.UserID, c.SearchID)
	if err != nil {
		return s.fail(c, models.CheckoutFailed, msgAirline, err)
	}
	stored, ok := pick(offers, c.Ref)
	if !ok {
		return s.fail(c, models.CheckoutFailed, msgExpired, errNoOffer)
	}
	cur, err := s.fresh(ctx, stored)
	if err != nil {
		return s.outcome(c, err, msgAirline)
	}
	var shown duffel.Detail
	if err := json.Unmarshal(c.Offer, &shown); err != nil {
		return s.fail(c, models.CheckoutFailed, msgAirline, err)
	}
	if !cur.Holdable {
		if c.Mode == "pay" {
			return s.reprice(c, cur, "This fare can't be held any more. You can pay now instead.")
		}
		c.Mode = "handoff"
		return s.reprice(c, cur, "This fare can't be held any more. Book it with the airline.")
	}
	if duffel.PriceUp(shown.Price, cur.Price) {
		return s.reprice(c, cur, "The price went up to "+cur.Price.String()+".")
	}
	pax, err := s.passengers(ctx, c, cur)
	if err != nil {
		return s.fail(c, models.CheckoutFailed, msgAirline, err)
	}
	order, err := s.api.CreateOrder(ctx, duffel.OrderRequest{Type: "hold", OfferID: cur.DuffelID, Passengers: pax})
	if err != nil {
		var ae *duffel.APIError
		if errors.As(err, &ae) && ae.Code == "price_changed" {
			if again, ferr := s.fresh(ctx, stored); ferr == nil {
				return s.reprice(c, again, "The price went up to "+again.Price.String()+".")
			}
		}
		return s.outcome(c, err, "We couldn't confirm the hold with the airline.")
	}
	c.Status, c.Error = models.CheckoutHeld, ""
	c.BookingRef, c.DuffelOrderID = order.BookingReference, order.ID
	c.PayBy = nil
	if order.PayBy != nil {
		if t, err := time.Parse(time.RFC3339, *order.PayBy); err == nil {
			c.PayBy = &t
		}
	}
	c.DepartsAt = cur.DepartsUTC
	c.Offer, _ = json.Marshal(cur.Detail())
	return nil
}

// canHold reports whether this checkout's fare can be reserved without paying.
func (s *FlightService) canHold(c *models.Checkout) bool {
	switch c.Mode {
	case "hold":
		return true
	case "pay":
		var o duffel.Detail
		_ = json.Unmarshal(c.Offer, &o)
		return o.Holdable
	}
	return false
}

// reprice puts the checkout back in review with the new offer and a sentence.
func (s *FlightService) reprice(c *models.Checkout, cur *duffel.Offer, msg string) error {
	c.Status, c.Error = models.CheckoutReview, msg
	c.Offer, _ = json.Marshal(cur.Detail())
	return nil
}

// outcome maps an error before the order exists. A network failure leaves the
// checkout in review so the user can try again; a failure of the order call
// itself is failed, because the airline may or may not have the hold.
func (s *FlightService) outcome(c *models.Checkout, err error, fallback string) error {
	switch e := offerError(err).(type) {
	case *StatusError:
		status := models.CheckoutFailed
		if e.Message == msgExpired {
			status = models.CheckoutExpired
		}
		return s.fail(c, status, e.Message, err)
	}
	if fallback == msgAirline { // nothing was sent yet
		c.Status, c.Error = models.CheckoutReview, fallback
		return err
	}
	return s.fail(c, models.CheckoutFailed, fallback, err)
}

func (s *FlightService) fail(c *models.Checkout, status, msg string, err error) error {
	c.Status, c.Error = status, msg
	return err
}

func (s *FlightService) passengers(ctx context.Context, c *models.Checkout, cur *duffel.Offer) ([]duffel.Passenger, error) {
	ids := parseIDs(c.Travellers)
	if len(ids) != len(cur.PassengerIDs) {
		return nil, errors.New("passenger count changed")
	}
	pax := make([]duffel.Passenger, len(ids))
	for i, id := range ids {
		pid, err := uuid.Parse(id)
		if err != nil {
			return nil, errors.New("traveller missing")
		}
		p, err := queries.GetTraveller(ctx, s.pool, c.UserID, pid)
		if err != nil {
			return nil, err
		}
		t, err := s.open(p)
		if err != nil {
			return nil, err
		}
		d := duffel.Passenger{ID: cur.PassengerIDs[i], Title: t.Title, Gender: t.Gender, GivenName: t.GivenName, FamilyName: t.FamilyName,
			BornOn: t.BornOn, Email: t.Email, PhoneNumber: t.Phone}
		if t.Passport != nil && t.Passport.Number != "" {
			d.Documents = []duffel.Document{{Type: "passport", UniqueIdentifier: t.Passport.Number, IssuingCountryCode: t.Passport.Country, ExpiresOn: t.Passport.ExpiresOn}}
		}
		for _, l := range t.Loyalty {
			d.Loyalty = append(d.Loyalty, duffel.Loyalty{Airline: l.Airline, Number: l.Number})
		}
		pax[i] = d
	}
	return pax, linkInfants(pax, cur.PassengerTypes)
}

// linkInfants puts each infant without a seat on the lap of a different adult, in order.
func linkInfants(pax []duffel.Passenger, types []string) error {
	adult := 0
	for i, t := range types {
		if t != "infant_without_seat" || i >= len(pax) {
			continue
		}
		for adult < len(types) && types[adult] != "adult" {
			adult++
		}
		if adult >= len(pax) {
			return errors.New("each infant needs an adult")
		}
		pax[adult].InfantID = pax[i].ID
		adult++
	}
	return nil
}

// Handoff marks a checkout whose fare can't be held as handed off and returns the link.
func (s *FlightService) Handoff(ctx context.Context, user, id uuid.UUID) (*Checkout, error) {
	c, err := s.load(ctx, user, id)
	if err != nil {
		return nil, err
	}
	if c.Mode != "handoff" {
		return nil, conflict("this fare can be held; hold it instead")
	}
	if c.Status == models.CheckoutHandoff {
		return s.view(ctx, c)
	}
	if c.Status != models.CheckoutTravellers && c.Status != models.CheckoutReview {
		return nil, conflict("this checkout can no longer be handed off")
	}
	var o duffel.Detail
	if err := json.Unmarshal(c.Offer, &o); err != nil {
		return nil, err
	}
	q := "Flights"
	for i, sl := range o.Slices {
		if i == 0 {
			q += " from " + sl.Origin + " to " + sl.Destination + " on " + sl.DepartAt[:10]
		} else {
			q += " returning " + sl.DepartAt[:10]
		}
	}
	c.Status, c.Error = models.CheckoutHandoff, ""
	c.HandoffURL = "https://www.google.com/travel/flights?q=" + url.QueryEscape(q)
	if err := queries.UpdateCheckout(ctx, s.pool, c); err != nil {
		return nil, err
	}
	return s.show(ctx, c), nil
}

// Bookings is the status of one checkout or the user's most recent (tools.Flights).
func (s *FlightService) Bookings(ctx context.Context, user uuid.UUID, id *uuid.UUID) ([]tools.BookingStatus, error) {
	var cs []*models.Checkout
	if id != nil {
		c, err := s.load(ctx, user, *id)
		if err != nil {
			return nil, err
		}
		cs = append(cs, c)
	} else {
		list, err := queries.ListRecentCheckouts(ctx, s.pool, user, recentBookings)
		if err != nil {
			return nil, err
		}
		for _, c := range list {
			if c, err = s.load(ctx, user, c.ID); err != nil {
				return nil, err
			}
			cs = append(cs, c)
		}
	}
	out := make([]tools.BookingStatus, len(cs))
	for i, c := range cs {
		out[i] = bookingStatus(c)
	}
	return out, nil
}

// bookingStatus is one checkout's status as the model sees it.
func bookingStatus(c *models.Checkout) tools.BookingStatus {
	var o duffel.Detail
	_ = json.Unmarshal(c.Offer, &o)
	return tools.BookingStatus{ID: c.ID, Route: routeOf(o), Status: c.Status, Reference: c.BookingRef, PayBy: c.PayBy, Error: c.Error}
}
