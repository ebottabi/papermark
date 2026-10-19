/*
 * Copyright (c) 2026. Papermark - All Rights Reserved
 * Unauthorized copying of this file, via any medium is strictly prohibited
 * Proprietary and confidential
 * Written by Ebot Tabi <ebot.tabi@gmail.com>, 19/10/2026
 */
package services

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log"
	"math/big"
	"net/url"
	"time"

	"papermark/internal/harness/tools"
	"papermark/internal/integrations/duffel"
	"papermark/internal/integrations/stripe"
	"papermark/internal/models"
	"papermark/internal/models/queries"

	"github.com/google/uuid"
)

// addMoney adds b to a, keeping a's currency and two decimals.
func addMoney(a, b duffel.Money) duffel.Money {
	x, _ := new(big.Rat).SetString(a.Amount)
	y, _ := new(big.Rat).SetString(b.Amount)
	if x == nil {
		x = new(big.Rat)
	}
	if y == nil {
		y = new(big.Rat)
	}
	return duffel.Money{Amount: x.Add(x, y).FloatString(2), Currency: a.Currency}
}

// checkoutTotal is the fare plus every chosen extra at its quantity.
func checkoutTotal(offer duffel.Detail, extras []CheckoutExtra) duffel.Money {
	total := offer.Breakdown.Total
	for _, e := range extras {
		for i := 0; i < e.Quantity; i++ {
			total = addMoney(total, e.Price)
		}
	}
	return total
}

// Trips returns the user's live and finished trips for the Trips tab.
func (s *FlightService) Trips(ctx context.Context, user uuid.UUID) ([]*Checkout, error) {
	cs, err := queries.ListTripCheckouts(ctx, s.pool, user)
	if err != nil {
		return nil, err
	}
	out := make([]*Checkout, 0, len(cs))
	for _, c := range cs {
		v, err := s.view(ctx, c)
		if err != nil {
			return nil, err
		}
		out = append(out, v)
	}
	return out, nil
}

// storedOffer returns the checkout's offer from its search, with the Duffel id.
func (s *FlightService) storedOffer(ctx context.Context, c *models.Checkout) (duffel.Offer, error) {
	_, offers, err := s.search(ctx, c.UserID, c.SearchID)
	if err != nil {
		return duffel.Offer{}, err
	}
	o, ok := pick(offers, c.Ref)
	if !ok {
		return duffel.Offer{}, errNoOffer
	}
	return o, nil
}

func bagLabel(sv duffel.Service) string {
	if sv.MaxWeightKg != nil {
		return fmt.Sprintf("Checked bag up to %gkg", *sv.MaxWeightKg)
	}
	return "Checked bag"
}

// travellerIndex maps a service to the passenger slot it applies to.
func travellerIndex(o duffel.Offer, sv duffel.Service) int {
	if len(sv.PassengerIDs) == 0 {
		return 0
	}
	for i, pid := range o.PassengerIDs {
		if pid == sv.PassengerIDs[0] {
			return i
		}
	}
	return 0
}

// Extras returns the bags a pay-mode checkout can add. Seat selection is a follow-up.
func (s *FlightService) Extras(ctx context.Context, user, id uuid.UUID) (*ExtrasCatalogue, error) {
	c, err := s.load(ctx, user, id)
	if err != nil {
		return nil, err
	}
	if c.Mode != "pay" {
		return nil, conflict("extras aren't available for this checkout")
	}
	o, err := s.storedOffer(ctx, c)
	if err != nil {
		return nil, err
	}
	svcs, err := s.api.Services(ctx, o.DuffelID)
	if err != nil {
		return nil, offerError(err)
	}
	return bagCatalogue(o, svcs), nil
}

func bagCatalogue(o duffel.Offer, svcs []duffel.Service) *ExtrasCatalogue {
	cat := &ExtrasCatalogue{Bags: []ExtraBag{}, SeatMaps: []any{}}
	for _, sv := range svcs {
		cat.Bags = append(cat.Bags, ExtraBag{Ref: sv.ID, Traveller: travellerIndex(o, sv), Label: bagLabel(sv), Price: sv.Total, MaxQuantity: sv.MaxQuantity})
	}
	return cat
}

// SetExtras records the chosen bags, recomputes the total and snapshots the
// catalogue at that price. Valid only on a pay-mode checkout in review.
func (s *FlightService) SetExtras(ctx context.Context, user, id uuid.UUID, choices []Choice) (*Checkout, error) {
	c, err := s.load(ctx, user, id)
	if err != nil {
		return nil, err
	}
	if c.Mode != "pay" {
		return nil, conflict("extras aren't available for this checkout")
	}
	if c.Status != models.CheckoutReview {
		return nil, conflict("extras can only be changed before paying")
	}
	o, err := s.storedOffer(ctx, c)
	if err != nil {
		return nil, err
	}
	svcs, err := s.api.Services(ctx, o.DuffelID)
	if err != nil {
		return nil, offerError(err)
	}
	byID := make(map[string]duffel.Service, len(svcs))
	for _, sv := range svcs {
		byID[sv.ID] = sv
	}
	extras := make([]CheckoutExtra, 0, len(choices))
	for _, ch := range choices {
		sv, ok := byID[ch.Ref]
		if !ok {
			return nil, badRequest("that extra is no longer available")
		}
		if ch.Quantity < 1 || (sv.MaxQuantity > 0 && ch.Quantity > sv.MaxQuantity) {
			return nil, badRequest("that extra quantity isn't available")
		}
		extras = append(extras, CheckoutExtra{Ref: sv.ID, Kind: "bag", Label: bagLabel(sv), Quantity: ch.Quantity, Price: sv.Total})
	}
	c.Extras, _ = json.Marshal(extras)
	c.Catalogue, _ = json.Marshal(bagCatalogue(o, svcs)) // price-authoritative snapshot of what was offered
	c.Error = ""
	if err := queries.UpdateCheckout(ctx, s.pool, c); err != nil {
		return nil, err
	}
	return s.view(ctx, c)
}

// Pay opens a Stripe hosted Checkout for the fare plus extras and claims the
// checkout as paying. Over the payment limit, it refuses.
func (s *FlightService) Pay(ctx context.Context, user, id uuid.UUID) (*PaymentStart, error) {
	if s.payments != "pay" {
		return nil, conflict("paying here isn't available yet")
	}
	c, err := s.load(ctx, user, id)
	if err != nil {
		return nil, err
	}
	if c.Mode != "pay" {
		return nil, conflict("this checkout can't be paid here")
	}
	if c.Status != models.CheckoutReview && c.Status != models.CheckoutHeld {
		return nil, conflict("this checkout isn't ready to pay")
	}
	var offer duffel.Detail
	if err := json.Unmarshal(c.Offer, &offer); err != nil {
		return nil, err
	}
	var extras []CheckoutExtra
	_ = json.Unmarshal(c.Extras, &extras)
	amount := checkoutTotal(offer, extras)
	minor, err := stripe.MinorUnits(amount.Amount, amount.Currency)
	if err != nil {
		return nil, badRequest("we can't take this payment")
	}
	if s.maxMinor > 0 && minor > s.maxMinor {
		return nil, conflict("this booking is over the payment limit")
	}
	sess, err := s.stripe.CreateSession(ctx, stripe.SessionRequest{
		Amount: minor, Currency: amount.Currency, Name: routeOf(offer),
		Metadata:   map[string]string{"checkout_id": id.String()},
		SuccessURL: s.payReturnURL(id, "success"), CancelURL: s.payReturnURL(id, "cancel"),
		ExpiresAt: s.now().Add(31 * time.Minute),
	})
	if err != nil {
		return nil, err
	}
	// An unused session expires on its own and its PaymentIntent is never
	// captured, so a lost CAS needs no cleanup.
	from := c.Status
	c.Status, c.PaymentID, c.PaymentIntent, c.Error = models.CheckoutPaying, sess.ID, sess.PaymentIntent, ""
	won, err := queries.SettleCheckout(ctx, s.pool, c, from)
	if err != nil {
		return nil, err
	}
	if !won {
		return nil, conflict("this checkout is already being paid")
	}
	v := s.show(ctx, c)
	return &PaymentStart{Checkout: v, PaymentURL: sess.URL}, nil
}

func (s *FlightService) payReturnURL(id uuid.UUID, status string) string {
	q := url.Values{"checkout": {id.String()}, "status": {status}}
	return s.publicURL + "/v1/checkouts/pay/return?" + q.Encode()
}

// ConfirmPayment reconciles a paying checkout with Stripe and the airline. It
// is idempotent: a non-paying checkout returns as is, and the SettleCheckout
// compare-and-set from "paying" makes the booking and capture run once.
func (s *FlightService) ConfirmPayment(ctx context.Context, user, id uuid.UUID) (*Checkout, error) {
	if s.payments != "pay" {
		return nil, conflict("paying here isn't available yet")
	}
	c, err := s.load(ctx, user, id)
	if err != nil {
		return nil, err
	}
	if c.Status != models.CheckoutPaying {
		return s.view(ctx, c)
	}
	// The reconciliation runs even if the client has gone.
	wctx := context.WithoutCancel(ctx)
	sess, err := s.stripe.GetSession(wctx, c.PaymentID)
	if err != nil {
		return nil, err
	}
	switch {
	case sess.PaymentIntent == "":
		s.restore(c, "We couldn't confirm your payment. Try again.")
	default:
		c.PaymentIntent = sess.PaymentIntent
		intent, err := s.stripe.GetPaymentIntent(wctx, sess.PaymentIntent)
		if err != nil {
			return nil, err
		}
		if intent.Status != "requires_capture" {
			s.restore(c, payMessage(intent.Status))
		} else if err := s.bookPaid(wctx, c, sess.PaymentIntent); err != nil {
			log.Printf("flights: confirm %s: %v", c.ID, err)
		}
	}
	won, err := queries.SettleCheckout(wctx, s.pool, c, models.CheckoutPaying)
	if err != nil {
		return nil, err
	}
	if !won {
		return s.GetCheckout(wctx, user, id)
	}
	s.show(wctx, c)
	return s.view(wctx, c)
}

// restore puts a checkout that didn't book back where the user can act: held
// when the fare is still reserved, else review.
func (s *FlightService) restore(c *models.Checkout, msg string) {
	if c.DuffelOrderID != "" {
		c.Status = models.CheckoutHeld
	} else {
		c.Status = models.CheckoutReview
	}
	c.Error = msg
}

func payMessage(status string) string {
	if status == "canceled" {
		return "The payment was cancelled. Try again when you're ready."
	}
	return "Your payment wasn't completed. Try again."
}

// bookOrder pays a held order or creates an instant one from the Duffel
// balance, recording the booking on c. It returns the airline error unwrapped.
func (s *FlightService) bookOrder(ctx context.Context, c *models.Checkout, amount duffel.Money, extras []CheckoutExtra) error {
	if c.DuffelOrderID != "" { // a prior hold: pay the existing order
		return s.api.PayOrder(ctx, c.DuffelOrderID, amount)
	}
	// instant order paid from the Duffel balance
	cur, err := s.storedOffer(ctx, c)
	if err != nil {
		return err
	}
	fresh, err := s.fresh(ctx, cur)
	if err != nil {
		return err
	}
	pax, err := s.passengers(ctx, c, fresh)
	if err != nil {
		return err
	}
	order, err := s.api.CreateOrder(ctx, duffel.OrderRequest{Type: "instant", OfferID: fresh.DuffelID, Passengers: pax,
		Services: selected(extras), Payment: &amount, Metadata: map[string]string{"checkout_id": c.ID.String()}})
	if err != nil {
		// A timed-out create may still have booked. Adopt that order rather
		// than release, so we never cancel the charge for a real booking.
		found, ferr := s.api.FindOrder(ctx, pax[0].FamilyName, "checkout_id", c.ID.String())
		if ferr != nil || found == nil {
			return err
		}
		order = found
	}
	c.BookingRef, c.DuffelOrderID, c.DepartsAt = order.BookingReference, order.ID, fresh.DepartsUTC
	return nil
}

// bookPaid books the airline against an authorised payment, then captures it.
// On a booking failure it releases the authorisation and restores the checkout.
func (s *FlightService) bookPaid(ctx context.Context, c *models.Checkout, pi string) error {
	var offer duffel.Detail
	if err := json.Unmarshal(c.Offer, &offer); err != nil {
		return err
	}
	var extras []CheckoutExtra
	_ = json.Unmarshal(c.Extras, &extras)
	amount := checkoutTotal(offer, extras)
	if err := s.bookOrder(ctx, c, amount, extras); err != nil {
		return s.releaseRestore(ctx, c, pi, err)
	}

	// The airline is booked, so the customer must be charged.
	c.Status, c.Error = models.CheckoutBooked, ""
	if err := s.stripe.Capture(ctx, pi); err != nil {
		log.Printf("flights: checkout %s capture: %v", c.ID, err) // booked, paid_at nil: the capture sweeper (follow-up) retries
		return nil
	}
	now := s.now().UTC()
	c.PaidAt = &now
	return nil
}

// Book pays the checkout from the operator's Duffel balance and books the
// airline. It is the agent's pay path and takes no card from the user. It
// runs only in balance mode. The claim CAS to paying makes a double call
// safe; the booking runs once and settles to booked. A crash mid-book
// leaves the checkout paying for the settlement sweeper (TODO) to finish.
func (s *FlightService) Book(ctx context.Context, user, id uuid.UUID) (tools.BookingStatus, error) {
	if s.payments != "balance" {
		return tools.BookingStatus{}, conflict("booking from balance isn't enabled")
	}
	c, err := s.load(ctx, user, id)
	if err != nil {
		return tools.BookingStatus{}, err
	}
	if c.Mode != "pay" {
		return tools.BookingStatus{}, conflict("this checkout can't be booked here")
	}
	if c.Status != models.CheckoutReview && c.Status != models.CheckoutHeld {
		return tools.BookingStatus{}, conflict("this checkout isn't ready to book")
	}
	var offer duffel.Detail
	if err := json.Unmarshal(c.Offer, &offer); err != nil {
		return tools.BookingStatus{}, err
	}
	var extras []CheckoutExtra
	_ = json.Unmarshal(c.Extras, &extras)
	amount := checkoutTotal(offer, extras)
	minor, err := stripe.MinorUnits(amount.Amount, amount.Currency)
	if err != nil {
		return tools.BookingStatus{}, badRequest("we can't take this payment")
	}
	if s.maxMinor > 0 && minor > s.maxMinor {
		return tools.BookingStatus{}, conflict("this booking is over the payment limit")
	}
	from := c.Status
	c.Status, c.Error = models.CheckoutPaying, ""
	won, err := queries.SettleCheckout(ctx, s.pool, c, from)
	if err != nil {
		return tools.BookingStatus{}, err
	}
	if !won {
		return bookingStatus(c), nil
	}
	// The booking runs to completion even if the client has gone.
	wctx := context.WithoutCancel(ctx)
	if err := s.bookOrder(wctx, c, amount, extras); err != nil {
		msg := "The airline couldn't complete the booking."
		if se, ok := offerError(err).(*StatusError); ok {
			msg = se.Message
		}
		s.restore(c, msg)
		if _, serr := queries.SettleCheckout(wctx, s.pool, c, models.CheckoutPaying); serr != nil {
			return tools.BookingStatus{}, serr
		}
		s.show(wctx, c)
		return bookingStatus(c), nil
	}
	now := s.now().UTC()
	c.Status, c.PaidAt, c.Error = models.CheckoutBooked, &now, ""
	if _, serr := queries.SettleCheckout(wctx, s.pool, c, models.CheckoutPaying); serr != nil {
		return tools.BookingStatus{}, serr
	}
	s.show(wctx, c)
	return bookingStatus(c), nil
}

func selected(extras []CheckoutExtra) []duffel.Selected {
	if len(extras) == 0 {
		return nil
	}
	out := make([]duffel.Selected, 0, len(extras))
	for _, e := range extras {
		out = append(out, duffel.Selected{ID: e.Ref, Quantity: e.Quantity})
	}
	return out
}

// releaseRestore cancels the authorisation and restores the checkout; it returns cause.
func (s *FlightService) releaseRestore(ctx context.Context, c *models.Checkout, pi string, cause error) error {
	if err := s.stripe.Release(ctx, pi); err != nil && !errors.Is(err, stripe.ErrCaptured) {
		log.Printf("flights: checkout %s release: %v", c.ID, err)
	}
	if se, ok := offerError(cause).(*StatusError); ok {
		s.restore(c, se.Message)
	} else {
		s.restore(c, "The airline couldn't complete the booking. You weren't charged.")
	}
	return cause
}

// TODO(webhooks): a stripe webhook (checkout.session.completed /
// payment_intent.*) and a settlement sweeper would reconcile confirms that
// never reached the server; STRIPE_WEBHOOK_SECRET is read for it. Out of scope here.
