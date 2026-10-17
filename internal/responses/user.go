/*
 * Copyright (c) 2026. Papermark - All Rights Reserved
 * Unauthorized copying of this file, via any medium is strictly prohibited
 * Proprietary and confidential
 * Written by Ebot Tabi <ebot.tabi@gmail.com>, 16/10/2026
 */

package responses

import (
	"time"

	"papermark/internal/models"
)

// UserDTO is the signed-in account.
type UserDTO struct {
	ID        string `json:"id"`
	Email     string `json:"email"`
	Name      string `json:"name"`
	Picture   string `json:"picture"`
	CreatedAt string `json:"createdAt"`
}

// UserResponse maps a user onto its DTO.
func UserResponse(u *models.User) UserDTO {
	return UserDTO{ID: u.ID.String(), Email: u.Email, Name: u.Name, Picture: u.Picture, CreatedAt: Time(u.CreatedAt)}
}

// SessionDTO is what every sign-in returns. created is true only when a
// Google sign-in made the account.
type SessionDTO struct {
	Token     string  `json:"token"`
	ExpiresAt string  `json:"expiresAt"`
	User      UserDTO `json:"user"`
	Created   bool    `json:"created"`
}

// SessionResponse maps a new session onto its DTO.
func SessionResponse(token string, expiresAt time.Time, u *models.User, created bool) SessionDTO {
	return SessionDTO{Token: token, ExpiresAt: Time(expiresAt), User: UserResponse(u), Created: created}
}

// CredentialDTO is an enrolled passkey or Face ID key.
type CredentialDTO struct {
	ID         string  `json:"id"`
	Name       string  `json:"name"`
	CreatedAt  string  `json:"createdAt"`
	LastUsedAt *string `json:"lastUsedAt"`
}

// PasskeyResponse maps a passkey onto its DTO.
func PasskeyResponse(p *models.Passkey) CredentialDTO {
	return CredentialDTO{ID: p.ID.String(), Name: p.Name, CreatedAt: Time(p.CreatedAt), LastUsedAt: optTime(p.LastUsedAt)}
}

// DeviceKeyResponse maps a Face ID key onto its DTO.
func DeviceKeyResponse(k *models.DeviceKey) CredentialDTO {
	return CredentialDTO{ID: k.ID.String(), Name: k.Name, CreatedAt: Time(k.CreatedAt), LastUsedAt: optTime(k.LastUsedAt)}
}

// AuthMethodsDTO says which sign-in methods besides Google the user has enrolled.
type AuthMethodsDTO struct {
	Passkey bool `json:"passkey"`
	FaceID  bool `json:"faceId"`
}
