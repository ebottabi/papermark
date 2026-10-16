/*
 * Copyright (c) 2026. Papermark - All Rights Reserved
 * Unauthorized copying of this file, via any medium is strictly prohibited
 * Proprietary and confidential
 * Written by Ebot Tabi <ebot.tabi@gmail.com>, 15/10/2026
 */
// Package apns sends alerts through Apple Push Notification service over
// HTTP/2 with token-based (ES256 JWT) authentication.
package apns

import (
	"bytes"
	"context"
	"crypto/ecdsa"
	"crypto/rand"
	"crypto/sha256"
	"crypto/x509"
	"encoding/base64"
	"encoding/json"
	"encoding/pem"
	"errors"
	"fmt"
	"io"
	"net/http"
	"os"
	"sync"
	"time"
)

// Environments a device token belongs to.
const (
	EnvSandbox    = "sandbox"
	EnvProduction = "production"
)

// tokenTTL stays under Apple's 60 minute limit and over its 20 minute refresh floor.
const tokenTTL = 50 * time.Minute

// Config is the APNs auth key and the app it pushes to.
type Config struct {
	KeyPath string // .p8 file
	KeyID   string
	TeamID  string
	Topic   string // app bundle id
}

// Configured reports whether every field push needs is set.
func (c Config) Configured() bool {
	return c.KeyPath != "" && c.KeyID != "" && c.TeamID != "" && c.Topic != ""
}

// Error is a non-2xx APNs reply.
type Error struct {
	Status int
	Reason string
}

func (e *Error) Error() string { return fmt.Sprintf("apns: status %d: %s", e.Status, e.Reason) }

// Unregistered reports whether the token is dead and should be pruned.
func (e *Error) Unregistered() bool {
	return e.Status == http.StatusGone || e.Reason == "BadDeviceToken" || e.Reason == "Unregistered"
}

// IsUnregistered reports whether err says the device token is no longer valid.
func IsUnregistered(err error) bool {
	var ae *Error
	return errors.As(err, &ae) && ae.Unregistered()
}

// Client sends alerts. It is safe for concurrent use.
type Client struct {
	cfg   Config
	key   *ecdsa.PrivateKey
	http  *http.Client
	hosts map[string]string

	mu       sync.Mutex
	jwt      string
	jwtIssue time.Time
}

// NewClient loads the .p8 key in cfg.KeyPath.
func NewClient(cfg Config) (*Client, error) {
	if !cfg.Configured() {
		return nil, errors.New("apns: APNS_KEY_PATH, APNS_KEY_ID, APNS_TEAM_ID and APNS_TOPIC are required")
	}
	raw, err := os.ReadFile(cfg.KeyPath)
	if err != nil {
		return nil, fmt.Errorf("apns: read key: %w", err)
	}
	key, err := ParseKey(raw)
	if err != nil {
		return nil, err
	}
	return &Client{
		cfg: cfg,
		key: key,
		http: &http.Client{
			Timeout:   15 * time.Second,
			Transport: &http.Transport{ForceAttemptHTTP2: true},
		},
		hosts: map[string]string{
			EnvSandbox:    "https://api.sandbox.push.apple.com",
			EnvProduction: "https://api.push.apple.com",
		},
	}, nil
}

// ParseKey parses a PEM PKCS#8 P-256 key, the .p8 format Apple issues.
func ParseKey(pemBytes []byte) (*ecdsa.PrivateKey, error) {
	block, _ := pem.Decode(pemBytes)
	if block == nil {
		return nil, errors.New("apns: key is not PEM")
	}
	k, err := x509.ParsePKCS8PrivateKey(block.Bytes)
	if err != nil {
		return nil, fmt.Errorf("apns: parse key: %w", err)
	}
	ek, ok := k.(*ecdsa.PrivateKey)
	if !ok {
		return nil, errors.New("apns: key is not ECDSA")
	}
	return ek, nil
}

// Alert is what one push carries: the alert text plus the task it is about.
type Alert struct {
	Text   string
	TaskID string
	Type   string
}

type payload struct {
	APS struct {
		Alert string `json:"alert"`
		Sound string `json:"sound"`
	} `json:"aps"`
	TaskID string `json:"taskId"`
	Type   string `json:"type"`
}

// Send pushes a to deviceToken on the host for environment.
func (c *Client) Send(ctx context.Context, deviceToken, environment string, a Alert) error {
	host, ok := c.hosts[environment]
	if !ok {
		return fmt.Errorf("apns: unknown environment %q", environment)
	}
	var p payload
	p.APS.Alert, p.APS.Sound, p.TaskID, p.Type = a.Text, "default", a.TaskID, a.Type
	body, err := json.Marshal(p)
	if err != nil {
		return err
	}
	bearer, err := c.token(time.Now())
	if err != nil {
		return err
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, host+"/3/device/"+deviceToken, bytes.NewReader(body))
	if err != nil {
		return fmt.Errorf("apns: %w", err)
	}
	req.Header.Set("authorization", "bearer "+bearer)
	req.Header.Set("apns-topic", c.cfg.Topic)
	req.Header.Set("apns-push-type", "alert")
	req.Header.Set("content-type", "application/json")
	res, err := c.http.Do(req)
	if err != nil {
		return fmt.Errorf("apns: send: %w", err)
	}
	defer res.Body.Close()
	if res.StatusCode == http.StatusOK {
		_, _ = io.Copy(io.Discard, io.LimitReader(res.Body, 4096))
		return nil
	}
	var reply struct {
		Reason string `json:"reason"`
	}
	_ = json.NewDecoder(io.LimitReader(res.Body, 4096)).Decode(&reply)
	return &Error{Status: res.StatusCode, Reason: reply.Reason}
}

// token returns the provider JWT, reusing it for tokenTTL.
func (c *Client) token(now time.Time) (string, error) {
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.jwt != "" && now.Sub(c.jwtIssue) < tokenTTL {
		return c.jwt, nil
	}
	enc := base64.RawURLEncoding
	header, _ := json.Marshal(map[string]string{"alg": "ES256", "kid": c.cfg.KeyID})
	claims, _ := json.Marshal(map[string]any{"iss": c.cfg.TeamID, "iat": now.Unix()})
	signing := enc.EncodeToString(header) + "." + enc.EncodeToString(claims)
	sum := sha256.Sum256([]byte(signing))
	r, s, err := ecdsa.Sign(rand.Reader, c.key, sum[:])
	if err != nil {
		return "", fmt.Errorf("apns: sign: %w", err)
	}
	sig := make([]byte, 64)
	r.FillBytes(sig[:32])
	s.FillBytes(sig[32:])
	c.jwt, c.jwtIssue = signing+"."+enc.EncodeToString(sig), now
	return c.jwt, nil
}
