/*
 * Copyright (c) 2026. Papermark - All Rights Reserved
 * Unauthorized copying of this file, via any medium is strictly prohibited
 * Proprietary and confidential
 * Written by Ebot Tabi <ebot.tabi@gmail.com>, 16/10/2026
 */

package controllers

import (
	"encoding/base64"
	"encoding/json"

	"papermark/internal/handlers"
	"papermark/internal/middleware"
	"papermark/internal/responses"
	"papermark/internal/services"

	"github.com/gofiber/fiber/v2"
	"github.com/google/uuid"
)

// AuthController serves /v1/auth and /v1/me. Google sign-in is the only way
// in for a new account; passkeys and Face ID keys are enrolled afterwards and
// only sign in existing users.
type AuthController struct {
	*handlers.BaseHandler
	auth *services.AuthService
}

// NewAuthController constructs an AuthController.
func NewAuthController(auth *services.AuthService) *AuthController {
	return &AuthController{BaseHandler: handlers.NewBaseHandler(), auth: auth}
}

// GoogleSignInRequest is the body of POST /v1/auth/google.
type GoogleSignInRequest struct {
	IDToken string `json:"idToken"`
}

// CeremonyFinishRequest is the body of the passkey finish endpoints.
type CeremonyFinishRequest struct {
	CeremonyID string          `json:"ceremonyId"`
	Name       string          `json:"name"`
	Credential json.RawMessage `json:"credential"`
}

// FaceIDRegisterRequest is the body of POST /v1/auth/faceid/register.
type FaceIDRegisterRequest struct {
	DeviceName string `json:"deviceName"`
	PublicKey  string `json:"publicKey"`
}

// FaceIDLoginRequest is the body of POST /v1/auth/faceid/challenge and /login.
type FaceIDLoginRequest struct {
	KeyID       string `json:"keyId"`
	ChallengeID string `json:"challengeId"`
	Signature   string `json:"signature"`
}

func (h *AuthController) fail(c *fiber.Ctx, err error, notFound string) error {
	return sendServiceError(h.BaseHandler, c, err, notFound)
}

func (h *AuthController) session(c *fiber.Ctx, s *services.Session, err error) error {
	if err != nil {
		return h.fail(c, err, "not found")
	}
	return h.SendSuccess(c, responses.SessionResponse(s.Token, s.ExpiresAt, s.User, s.Created))
}

// Google handles POST /v1/auth/google: it creates the account for a new
// Google user and signs in an existing one.
func (h *AuthController) Google(c *fiber.Ctx) error {
	var req GoogleSignInRequest
	if err := c.BodyParser(&req); err != nil || req.IDToken == "" {
		return h.SendBadRequest(c, "idToken is required")
	}
	s, err := h.auth.GoogleSignIn(c.Context(), req.IDToken)
	return h.session(c, s, err)
}

// Logout handles POST /v1/auth/logout: it revokes the calling session.
func (h *AuthController) Logout(c *fiber.Ctx) error {
	token, _ := middleware.BearerToken(c)
	if err := h.auth.Logout(c.Context(), token); err != nil {
		return h.fail(c, err, "not found")
	}
	return h.SendSuccess(c, fiber.Map{"ok": true})
}

// Me handles GET /v1/me.
func (h *AuthController) Me(c *fiber.Ctx) error {
	u, err := h.auth.Me(c.Context(), middleware.UserID(c))
	if err != nil {
		return h.fail(c, err, "no such user")
	}
	return h.SendSuccess(c, responses.UserResponse(u))
}

// Methods handles GET /v1/auth/methods.
func (h *AuthController) Methods(c *fiber.Ctx) error {
	passkey, faceID, err := h.auth.Methods(c.Context(), middleware.UserID(c))
	if err != nil {
		return h.fail(c, err, "no such user")
	}
	return h.SendSuccess(c, responses.AuthMethodsDTO{Passkey: passkey, FaceID: faceID})
}

// PasskeyRegisterBegin handles POST /v1/auth/passkeys/register/begin.
func (h *AuthController) PasskeyRegisterBegin(c *fiber.Ctx) error {
	id, options, err := h.auth.PasskeyRegisterBegin(c.Context(), middleware.UserID(c))
	if err != nil {
		return h.fail(c, err, "no such user")
	}
	return h.SendSuccess(c, fiber.Map{"ceremonyId": id, "options": options})
}

func (h *AuthController) finishRequest(c *fiber.Ctx) (*CeremonyFinishRequest, uuid.UUID, bool) {
	var req CeremonyFinishRequest
	if err := c.BodyParser(&req); err != nil {
		return nil, uuid.Nil, false
	}
	id, err := uuid.Parse(req.CeremonyID)
	if err != nil || len(req.Credential) == 0 {
		return nil, uuid.Nil, false
	}
	return &req, id, true
}

// PasskeyRegisterFinish handles POST /v1/auth/passkeys/register/finish.
func (h *AuthController) PasskeyRegisterFinish(c *fiber.Ctx) error {
	req, id, ok := h.finishRequest(c)
	if !ok {
		return h.SendBadRequest(c, "ceremonyId and credential are required")
	}
	p, err := h.auth.PasskeyRegisterFinish(c.Context(), middleware.UserID(c), id, req.Name, req.Credential)
	if err != nil {
		return h.fail(c, err, "no such user")
	}
	return h.SendCreated(c, responses.PasskeyResponse(p), "passkey added")
}

// PasskeyLoginBegin handles POST /v1/auth/passkeys/login/begin.
func (h *AuthController) PasskeyLoginBegin(c *fiber.Ctx) error {
	id, options, err := h.auth.PasskeyLoginBegin(c.Context())
	if err != nil {
		return h.fail(c, err, "not found")
	}
	return h.SendSuccess(c, fiber.Map{"ceremonyId": id, "options": options})
}

// PasskeyLoginFinish handles POST /v1/auth/passkeys/login/finish.
func (h *AuthController) PasskeyLoginFinish(c *fiber.Ctx) error {
	req, id, ok := h.finishRequest(c)
	if !ok {
		return h.SendBadRequest(c, "ceremonyId and credential are required")
	}
	s, err := h.auth.PasskeyLoginFinish(c.Context(), id, req.Credential)
	return h.session(c, s, err)
}

// Passkeys handles GET /v1/auth/passkeys.
func (h *AuthController) Passkeys(c *fiber.Ctx) error {
	list, err := h.auth.Passkeys(c.Context(), middleware.UserID(c))
	if err != nil {
		return h.fail(c, err, "not found")
	}
	out := make([]responses.CredentialDTO, len(list))
	for i, p := range list {
		out[i] = responses.PasskeyResponse(p)
	}
	return h.SendSuccess(c, out)
}

// DeletePasskey handles DELETE /v1/auth/passkeys/:id.
func (h *AuthController) DeletePasskey(c *fiber.Ctx) error {
	id, ok := idParam(c)
	if !ok {
		return h.SendNotFound(c, "no such passkey")
	}
	if err := h.auth.DeletePasskey(c.Context(), middleware.UserID(c), id); err != nil {
		return h.fail(c, err, "no such passkey")
	}
	return h.SendSuccess(c, fiber.Map{"ok": true})
}

// FaceIDRegister handles POST /v1/auth/faceid/register.
func (h *AuthController) FaceIDRegister(c *fiber.Ctx) error {
	var req FaceIDRegisterRequest
	if err := c.BodyParser(&req); err != nil || req.PublicKey == "" {
		return h.SendBadRequest(c, "publicKey is required")
	}
	k, err := h.auth.FaceIDRegister(c.Context(), middleware.UserID(c), req.DeviceName, req.PublicKey)
	if err != nil {
		return h.fail(c, err, "not found")
	}
	return h.SendCreated(c, responses.DeviceKeyResponse(k), "Face ID key added")
}

// FaceIDChallenge handles POST /v1/auth/faceid/challenge.
func (h *AuthController) FaceIDChallenge(c *fiber.Ctx) error {
	var req FaceIDLoginRequest
	if err := c.BodyParser(&req); err != nil {
		return h.SendBadRequest(c, "keyId is required")
	}
	keyID, err := uuid.Parse(req.KeyID)
	if err != nil {
		return h.SendBadRequest(c, "keyId is required")
	}
	id, challenge, expires, err := h.auth.FaceIDChallenge(c.Context(), keyID)
	if err != nil {
		return h.fail(c, err, "no such key")
	}
	return h.SendSuccess(c, fiber.Map{
		"challengeId": id,
		"challenge":   base64.StdEncoding.EncodeToString(challenge),
		"expiresAt":   responses.Time(expires),
	})
}

// FaceIDLogin handles POST /v1/auth/faceid/login.
func (h *AuthController) FaceIDLogin(c *fiber.Ctx) error {
	var req FaceIDLoginRequest
	if err := c.BodyParser(&req); err != nil {
		return h.SendBadRequest(c, "keyId, challengeId and signature are required")
	}
	keyID, kerr := uuid.Parse(req.KeyID)
	challengeID, cerr := uuid.Parse(req.ChallengeID)
	if kerr != nil || cerr != nil || req.Signature == "" {
		return h.SendBadRequest(c, "keyId, challengeId and signature are required")
	}
	s, err := h.auth.FaceIDLogin(c.Context(), keyID, challengeID, req.Signature)
	return h.session(c, s, err)
}

// FaceIDKeys handles GET /v1/auth/faceid.
func (h *AuthController) FaceIDKeys(c *fiber.Ctx) error {
	list, err := h.auth.DeviceKeys(c.Context(), middleware.UserID(c))
	if err != nil {
		return h.fail(c, err, "not found")
	}
	out := make([]responses.CredentialDTO, len(list))
	for i, k := range list {
		out[i] = responses.DeviceKeyResponse(k)
	}
	return h.SendSuccess(c, out)
}

// RevokeFaceIDKey handles DELETE /v1/auth/faceid/:id.
func (h *AuthController) RevokeFaceIDKey(c *fiber.Ctx) error {
	id, ok := idParam(c)
	if !ok {
		return h.SendNotFound(c, "no such key")
	}
	if err := h.auth.RevokeDeviceKey(c.Context(), middleware.UserID(c), id); err != nil {
		return h.fail(c, err, "no such key")
	}
	return h.SendSuccess(c, fiber.Map{"ok": true})
}
