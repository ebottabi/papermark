/*
 * Copyright (c) 2026. Papermark - All Rights Reserved
 * Unauthorized copying of this file, via any medium is strictly prohibited
 * Proprietary and confidential
 * Written by Ebot Tabi <ebot.tabi@gmail.com>, 16/10/2026
 */

package services

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"time"

	"papermark/internal/models"
	"papermark/internal/models/queries"

	"github.com/go-webauthn/webauthn/protocol"
	"github.com/go-webauthn/webauthn/webauthn"
	"github.com/google/uuid"
)

const (
	ceremonyTTL          = 5 * time.Minute
	kindPasskeyRegister  = "passkey_register"
	kindPasskeyLogin     = "passkey_login"
	passkeyNameMaxLength = 64
)

// passkeyUser is a user as go-webauthn sees it. The user handle is the
// account's uuid, so a discoverable login names its account.
type passkeyUser struct {
	user  *models.User
	creds []webauthn.Credential
}

func (u *passkeyUser) WebAuthnID() []byte                         { return u.user.ID[:] }
func (u *passkeyUser) WebAuthnName() string                       { return u.user.Email }
func (u *passkeyUser) WebAuthnDisplayName() string                { return u.user.Name }
func (u *passkeyUser) WebAuthnCredentials() []webauthn.Credential { return u.creds }

func toCredential(p *models.Passkey) webauthn.Credential {
	transports := make([]protocol.AuthenticatorTransport, len(p.Transports))
	for i, t := range p.Transports {
		transports[i] = protocol.AuthenticatorTransport(t)
	}
	return webauthn.Credential{
		ID: p.CredentialID, PublicKey: p.PublicKey, Transport: transports,
		Flags:         webauthn.CredentialFlags{UserPresent: true, UserVerified: true, BackupEligible: p.BackupEligible, BackupState: p.BackupState},
		Authenticator: webauthn.Authenticator{SignCount: uint32(p.SignCount)},
	}
}

func (s *AuthService) passkeyUser(ctx context.Context, userID uuid.UUID) (*passkeyUser, error) {
	user, err := queries.GetUser(ctx, s.pool, userID)
	if err != nil {
		return nil, err
	}
	keys, err := queries.ListPasskeys(ctx, s.pool, userID)
	if err != nil {
		return nil, err
	}
	u := &passkeyUser{user: user}
	for _, k := range keys {
		u.creds = append(u.creds, toCredential(k))
	}
	return u, nil
}

func (s *AuthService) requirePasskeys() error {
	if s.webauthn == nil {
		return &StatusError{503, "passkeys are not configured"}
	}
	return nil
}

// saveCeremony stores the server half of a ceremony and returns its id.
func (s *AuthService) saveCeremony(ctx context.Context, kind string, user *uuid.UUID, session *webauthn.SessionData) (uuid.UUID, error) {
	data, err := json.Marshal(session)
	if err != nil {
		return uuid.Nil, err
	}
	now := s.now()
	return queries.InsertAuthChallenge(ctx, s.pool, kind, user, data, now, now.Add(ceremonyTTL))
}

// takeCeremony consumes a ceremony; an unknown, used or expired one is 400.
func (s *AuthService) takeCeremony(ctx context.Context, id uuid.UUID, kind string) (*models.AuthChallenge, *webauthn.SessionData, error) {
	c, err := queries.TakeAuthChallenge(ctx, s.pool, id, kind, s.now())
	if errors.Is(err, queries.ErrNotFound) {
		return nil, nil, badRequest("ceremony is unknown or expired")
	}
	if err != nil {
		return nil, nil, err
	}
	var session webauthn.SessionData
	if err := json.Unmarshal(c.Data, &session); err != nil {
		return nil, nil, err
	}
	return c, &session, nil
}

// PasskeyRegisterBegin starts enrolling a passkey for a signed-in user: a
// discoverable credential that needs user verification (Face ID).
func (s *AuthService) PasskeyRegisterBegin(ctx context.Context, userID uuid.UUID) (uuid.UUID, *protocol.CredentialCreation, error) {
	if err := s.requirePasskeys(); err != nil {
		return uuid.Nil, nil, err
	}
	u, err := s.passkeyUser(ctx, userID)
	if err != nil {
		return uuid.Nil, nil, err
	}
	creation, session, err := s.webauthn.BeginRegistration(u,
		webauthn.WithAuthenticatorSelection(protocol.AuthenticatorSelection{
			ResidentKey:        protocol.ResidentKeyRequirementRequired,
			RequireResidentKey: protocol.ResidentKeyRequired(),
			UserVerification:   protocol.VerificationRequired,
		}),
		webauthn.WithExclusions(webauthn.Credentials(u.creds).CredentialDescriptors()))
	if err != nil {
		return uuid.Nil, nil, err
	}
	id, err := s.saveCeremony(ctx, kindPasskeyRegister, &userID, session)
	return id, creation, err
}

// PasskeyRegisterFinish verifies the authenticator's attestation and stores
// the passkey. credential is the PublicKeyCredential JSON from the device.
func (s *AuthService) PasskeyRegisterFinish(ctx context.Context, userID, ceremonyID uuid.UUID, name string, credential []byte) (*models.Passkey, error) {
	if err := s.requirePasskeys(); err != nil {
		return nil, err
	}
	if len(name) > passkeyNameMaxLength {
		return nil, badRequest("name must be at most %d characters", passkeyNameMaxLength)
	}
	c, session, err := s.takeCeremony(ctx, ceremonyID, kindPasskeyRegister)
	if err != nil {
		return nil, err
	}
	if c.UserID == nil || *c.UserID != userID {
		return nil, badRequest("ceremony is unknown or expired")
	}
	parsed, err := protocol.ParseCredentialCreationResponseBytes(credential)
	if err != nil {
		return nil, badRequest("credential: %v", err)
	}
	u, err := s.passkeyUser(ctx, userID)
	if err != nil {
		return nil, err
	}
	cred, err := s.webauthn.CreateCredential(u, *session, parsed)
	if err != nil {
		return nil, badRequest("passkey rejected: %v", err)
	}
	p := &models.Passkey{
		UserID: userID, CredentialID: cred.ID, PublicKey: cred.PublicKey, SignCount: int64(cred.Authenticator.SignCount),
		BackupEligible: cred.Flags.BackupEligible, BackupState: cred.Flags.BackupState, Name: name,
	}
	for _, t := range cred.Transport {
		p.Transports = append(p.Transports, string(t))
	}
	if err := queries.InsertPasskey(ctx, s.pool, p); errors.Is(err, queries.ErrDuplicate) {
		return nil, conflict("passkey is already registered")
	} else if err != nil {
		return nil, err
	}
	return p, nil
}

// PasskeyLoginBegin starts a discoverable login: the device offers its
// passkeys for this relying party and must verify the user.
func (s *AuthService) PasskeyLoginBegin(ctx context.Context) (uuid.UUID, *protocol.CredentialAssertion, error) {
	if err := s.requirePasskeys(); err != nil {
		return uuid.Nil, nil, err
	}
	assertion, session, err := s.webauthn.BeginDiscoverableLogin(webauthn.WithUserVerification(protocol.VerificationRequired))
	if err != nil {
		return uuid.Nil, nil, err
	}
	id, err := s.saveCeremony(ctx, kindPasskeyLogin, nil, session)
	return id, assertion, err
}

// PasskeyLoginFinish verifies the assertion against the stored passkey and
// signs its owner in. It never creates an account.
func (s *AuthService) PasskeyLoginFinish(ctx context.Context, ceremonyID uuid.UUID, credential []byte) (*Session, error) {
	if err := s.requirePasskeys(); err != nil {
		return nil, err
	}
	_, session, err := s.takeCeremony(ctx, ceremonyID, kindPasskeyLogin)
	if err != nil {
		return nil, err
	}
	parsed, err := protocol.ParseCredentialRequestResponseBytes(credential)
	if err != nil {
		return nil, badRequest("credential: %v", err)
	}
	var key *models.Passkey
	var owner *passkeyUser
	_, cred, err := s.webauthn.ValidatePasskeyLogin(func(rawID, userHandle []byte) (webauthn.User, error) {
		if key, err = queries.GetPasskeyByCredentialID(ctx, s.pool, rawID); err != nil {
			return nil, err
		}
		if !bytes.Equal(userHandle, key.UserID[:]) {
			return nil, errors.New("user handle does not own the credential")
		}
		owner, err = s.passkeyUser(ctx, key.UserID)
		return owner, err
	}, *session, parsed)
	if err != nil {
		return nil, unauthorized("passkey sign-in failed")
	}
	if cred.Authenticator.CloneWarning {
		return nil, unauthorized("passkey sign-in failed")
	}
	if err := queries.MarkPasskeyUsed(ctx, s.pool, key.ID, int64(cred.Authenticator.SignCount), cred.Flags.BackupState, s.now()); err != nil {
		return nil, err
	}
	return s.newSession(ctx, owner.user)
}

// Passkeys lists the user's passkeys.
func (s *AuthService) Passkeys(ctx context.Context, userID uuid.UUID) ([]*models.Passkey, error) {
	return queries.ListPasskeys(ctx, s.pool, userID)
}

// DeletePasskey removes one of the user's passkeys.
func (s *AuthService) DeletePasskey(ctx context.Context, userID, id uuid.UUID) error {
	return queries.DeletePasskey(ctx, s.pool, userID, id)
}
