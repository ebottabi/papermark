/*
 * Copyright (c) 2026. Papermark - All Rights Reserved
 * Unauthorized copying of this file, via any medium is strictly prohibited
 * Proprietary and confidential
 * Written by Ebot Tabi <ebot.tabi@gmail.com>, 18/10/2026
 */
package controllers

import (
	"encoding/json"
	"net/url"

	"papermark/internal/handlers"
	"papermark/internal/middleware"
	"papermark/internal/services"

	"github.com/gofiber/fiber/v2"
	"github.com/google/uuid"
)

// FlightController serves /v1/flights, /v1/checkouts and /v1/travellers.
type FlightController struct {
	*handlers.BaseHandler
	flights *services.FlightService
}

// NewFlightController constructs a FlightController.
func NewFlightController(fs *services.FlightService) *FlightController {
	return &FlightController{BaseHandler: handlers.NewBaseHandler(), flights: fs}
}

// id parses the :id param; a bad id is a 404 like any unknown row.
func id(ctx *fiber.Ctx) (uuid.UUID, bool) {
	v, err := uuid.Parse(ctx.Params("id"))
	return v, err == nil
}

// Offers handles GET /v1/flights/searches/:id/offers.
func (c *FlightController) Offers(ctx *fiber.Ctx) error {
	sid, ok := id(ctx)
	if !ok {
		return c.SendNotFound(ctx, "search not found")
	}
	offers, err := c.flights.Offers(ctx.Context(), middleware.UserID(ctx), sid)
	if err != nil {
		return sendServiceError(c.BaseHandler, ctx, err, "search not found")
	}
	return c.SendSuccess(ctx, fiber.Map{"offers": offers})
}

// Offer handles GET /v1/flights/searches/:id/offers/:ref.
func (c *FlightController) Offer(ctx *fiber.Ctx) error {
	sid, ok := id(ctx)
	if !ok {
		return c.SendNotFound(ctx, "search not found")
	}
	o, err := c.flights.Offer(ctx.Context(), middleware.UserID(ctx), sid, ctx.Params("ref"))
	if err != nil {
		return sendServiceError(c.BaseHandler, ctx, err, "search not found")
	}
	return c.SendSuccess(ctx, o.Detail())
}

// CreateCheckout handles POST /v1/checkouts.
func (c *FlightController) CreateCheckout(ctx *fiber.Ctx) error {
	b, err := parseBody(ctx)
	if err != nil {
		return c.SendBadRequest(ctx, err.Error())
	}
	search, _ := b["searchId"].(string)
	ref, _ := b["ref"].(string)
	sid, err := uuid.Parse(search)
	if err != nil || ref == "" {
		return c.SendBadRequest(ctx, "searchId and ref are required")
	}
	co, err := c.flights.CreateCheckout(ctx.Context(), middleware.UserID(ctx), sid, ref)
	if err != nil {
		return sendServiceError(c.BaseHandler, ctx, err, "search not found")
	}
	return c.SendCreated(ctx, co, "")
}

// checkout runs fn on the :id checkout and sends its result.
func (c *FlightController) checkout(ctx *fiber.Ctx, fn func(user, id uuid.UUID) (any, error)) error {
	cid, ok := id(ctx)
	if !ok {
		return c.SendNotFound(ctx, "checkout not found")
	}
	v, err := fn(middleware.UserID(ctx), cid)
	if err != nil {
		return sendServiceError(c.BaseHandler, ctx, err, "checkout not found")
	}
	return c.SendSuccess(ctx, v)
}

// GetCheckout handles GET /v1/checkouts/:id.
func (c *FlightController) GetCheckout(ctx *fiber.Ctx) error {
	return c.checkout(ctx, func(u, i uuid.UUID) (any, error) { return c.flights.GetCheckout(ctx.Context(), u, i) })
}

// PatchCheckout handles PATCH /v1/checkouts/:id: "travellers" sets the
// passengers, "extras" sets the chosen bags.
func (c *FlightController) PatchCheckout(ctx *fiber.Ctx) error {
	b, err := parseBody(ctx)
	if err != nil {
		return c.SendBadRequest(ctx, err.Error())
	}
	if raw, present := b["travellers"].([]any); present {
		ids := make([]string, len(raw))
		for i, v := range raw {
			s, ok := v.(string)
			if !ok {
				return c.SendBadRequest(ctx, "travellers must be an array of profile ids")
			}
			ids[i] = s
		}
		return c.checkout(ctx, func(u, i uuid.UUID) (any, error) { return c.flights.PatchTravellers(ctx.Context(), u, i, ids) })
	}
	if raw := b.raw("extras"); raw != nil {
		var choices []services.Choice
		if err := json.Unmarshal(raw, &choices); err != nil {
			return c.SendBadRequest(ctx, "extras must be an array of {ref, quantity}")
		}
		return c.checkout(ctx, func(u, i uuid.UUID) (any, error) { return c.flights.SetExtras(ctx.Context(), u, i, choices) })
	}
	return c.SendBadRequest(ctx, "send travellers or extras")
}

// Hold handles POST /v1/checkouts/:id/hold.
func (c *FlightController) Hold(ctx *fiber.Ctx) error {
	return c.checkout(ctx, func(u, i uuid.UUID) (any, error) { return c.flights.Hold(ctx.Context(), u, i) })
}

// Extras handles GET /v1/checkouts/:id/extras.
func (c *FlightController) Extras(ctx *fiber.Ctx) error {
	return c.checkout(ctx, func(u, i uuid.UUID) (any, error) { return c.flights.Extras(ctx.Context(), u, i) })
}

// Pay handles POST /v1/checkouts/:id/pay.
func (c *FlightController) Pay(ctx *fiber.Ctx) error {
	return c.checkout(ctx, func(u, i uuid.UUID) (any, error) { return c.flights.Pay(ctx.Context(), u, i) })
}

// ConfirmPay handles POST /v1/checkouts/:id/pay/confirm.
func (c *FlightController) ConfirmPay(ctx *fiber.Ctx) error {
	return c.checkout(ctx, func(u, i uuid.UUID) (any, error) { return c.flights.ConfirmPayment(ctx.Context(), u, i) })
}

// Trips handles GET /v1/trips.
func (c *FlightController) Trips(ctx *fiber.Ctx) error {
	trips, err := c.flights.Trips(ctx.Context(), middleware.UserID(ctx))
	if err != nil {
		return sendServiceError(c.BaseHandler, ctx, err, "")
	}
	return c.SendSuccess(ctx, fiber.Map{"trips": trips})
}

// PayReturn handles GET /v1/checkouts/pay/return: the public page Stripe sends
// the customer to, which deep-links back into the app. Stripe can't redirect to
// a custom scheme, so this is the bridge.
func (c *FlightController) PayReturn(ctx *fiber.Ctx) error {
	q := url.Values{"status": {"cancel"}}
	if id, err := uuid.Parse(ctx.Query("checkout")); err == nil {
		q.Set("checkout", id.String())
	}
	if ctx.Query("status") == "success" {
		q.Set("status", "success")
	}
	return ctx.Redirect("keen://checkout/pay/return?"+q.Encode(), fiber.StatusFound)
}

// Handoff handles POST /v1/checkouts/:id/handoff.
func (c *FlightController) Handoff(ctx *fiber.Ctx) error {
	return c.checkout(ctx, func(u, i uuid.UUID) (any, error) { return c.flights.Handoff(ctx.Context(), u, i) })
}

// Travellers handles GET /v1/travellers.
func (c *FlightController) Travellers(ctx *fiber.Ctx) error {
	ts, err := c.flights.Travellers(ctx.Context(), middleware.UserID(ctx))
	if err != nil {
		return sendServiceError(c.BaseHandler, ctx, err, "traveller not found")
	}
	return c.SendSuccess(ctx, fiber.Map{"travellers": ts})
}

// CreateTraveller handles POST /v1/travellers.
func (c *FlightController) CreateTraveller(ctx *fiber.Ctx) error {
	t, err := c.flights.CreateTraveller(ctx.Context(), middleware.UserID(ctx), ctx.Body())
	if err != nil {
		return sendServiceError(c.BaseHandler, ctx, err, "traveller not found")
	}
	return c.SendCreated(ctx, t, "")
}

// PatchTraveller handles PATCH /v1/travellers/:id.
func (c *FlightController) PatchTraveller(ctx *fiber.Ctx) error {
	tid, ok := id(ctx)
	if !ok {
		return c.SendNotFound(ctx, "traveller not found")
	}
	t, err := c.flights.PatchTraveller(ctx.Context(), middleware.UserID(ctx), tid, ctx.Body())
	if err != nil {
		return sendServiceError(c.BaseHandler, ctx, err, "traveller not found")
	}
	return c.SendSuccess(ctx, t)
}

// DeleteTraveller handles DELETE /v1/travellers/:id.
func (c *FlightController) DeleteTraveller(ctx *fiber.Ctx) error {
	tid, ok := id(ctx)
	if !ok {
		return c.SendNotFound(ctx, "traveller not found")
	}
	if err := c.flights.DeleteTraveller(ctx.Context(), middleware.UserID(ctx), tid); err != nil {
		return sendServiceError(c.BaseHandler, ctx, err, "traveller not found")
	}
	return c.SendSuccessWithMessage(ctx, nil, "traveller deleted")
}
