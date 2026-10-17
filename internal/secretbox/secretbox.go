/*
 * Copyright (c) 2026. Papermark - All Rights Reserved
 * Unauthorized copying of this file, via any medium is strictly prohibited
 * Proprietary and confidential
 * Written by Ebot Tabi <ebot.tabi@gmail.com>, 15/10/2026
 */
// Package secretbox seals secrets stored in the database with AES-256-GCM.
package secretbox

import (
	"crypto/aes"
	"crypto/cipher"
	"crypto/rand"
	"encoding/base64"
	"errors"
	"fmt"
)

// prefix marks a sealed value. Anything without it is a legacy plaintext row and reads as is.
const prefix = "enc1:"

// Box seals and opens strings. A nil Box stores plaintext, for servers
// running without TOKEN_ENCRYPTION_KEY.
type Box struct{ gcm cipher.AEAD }

// ParseKey decodes a base64 key of exactly 32 bytes. An empty value means no key.
func ParseKey(b64 string) ([]byte, error) {
	if b64 == "" {
		return nil, nil
	}
	key, err := base64.StdEncoding.DecodeString(b64)
	if err != nil {
		return nil, fmt.Errorf("TOKEN_ENCRYPTION_KEY is not base64: %w", err)
	}
	if len(key) != 32 {
		return nil, fmt.Errorf("TOKEN_ENCRYPTION_KEY must decode to 32 bytes, got %d", len(key))
	}
	return key, nil
}

// New returns a Box for key, or nil for an empty key.
func New(key []byte) (*Box, error) {
	if len(key) == 0 {
		return nil, nil
	}
	block, err := aes.NewCipher(key)
	if err != nil {
		return nil, err
	}
	gcm, err := cipher.NewGCM(block)
	if err != nil {
		return nil, err
	}
	return &Box{gcm}, nil
}

// Seal encrypts s under a fresh nonce. The empty string stays empty.
func (b *Box) Seal(s string) (string, error) {
	if b == nil || s == "" {
		return s, nil
	}
	nonce := make([]byte, b.gcm.NonceSize())
	if _, err := rand.Read(nonce); err != nil {
		return "", err
	}
	return prefix + base64.StdEncoding.EncodeToString(b.gcm.Seal(nonce, nonce, []byte(s), nil)), nil
}

// Open reverses Seal. A value without the prefix is returned unchanged.
func (b *Box) Open(s string) (string, error) {
	if len(s) < len(prefix) || s[:len(prefix)] != prefix {
		return s, nil
	}
	if b == nil {
		return "", errors.New("secretbox: value is encrypted but TOKEN_ENCRYPTION_KEY is not set")
	}
	raw, err := base64.StdEncoding.DecodeString(s[len(prefix):])
	if err != nil || len(raw) < b.gcm.NonceSize() {
		return "", errors.New("secretbox: malformed encrypted value")
	}
	n := b.gcm.NonceSize()
	plain, err := b.gcm.Open(nil, raw[:n], raw[n:], nil)
	if err != nil {
		return "", errors.New("secretbox: cannot decrypt value (wrong TOKEN_ENCRYPTION_KEY?)")
	}
	return string(plain), nil
}
