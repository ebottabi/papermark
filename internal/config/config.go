/*
 * Copyright (c) 2026. Papermark - All Rights Reserved
 * Unauthorized copying of this file, via any medium is strictly prohibited
 * Proprietary and confidential
 * Written by Ebot Tabi <ebot.tabi@gmail.com>, 15/10/2026
 */

package config

import (
	"log"
	"net/url"
	"os"
	"strconv"
	"strings"
	"time"

	"papermark/internal/secretbox"

	"github.com/joho/godotenv"
)

// Config holds process configuration read from the environment.
// Database and Redis connection settings live in internal/database
// (LoadPostgresConfig / LoadRedisConfig) so both binaries share them.
type Config struct {
	Host        string
	Port        string
	Environment string
	PublicURL   string

	Auth AuthConfig

	// WorkspaceDir holds one directory per task: WorkspaceDir/<task id>.
	WorkspaceDir string

	// BodyLimit caps request bodies (attachments are base64 in JSON).
	BodyLimit int

	// TokenKey encrypts OAuth tokens at rest (AES-256-GCM); nil stores them in plaintext.
	TokenKey []byte

	Agent  AgentConfig
	Worker WorkerConfig
	APNS   APNSConfig
	Ntfy   NtfyConfig

	// Connectors is the fixed catalog the app offers, by connector name.
	Connectors map[string]ConnectorConfig
}

// ConnectorConfig is one catalog connector: its MCP server (or, for Kind
// "gmail", the Gmail API origin) and, for providers without dynamic client
// registration, a pre-registered OAuth client. Scopes replaces the broad list
// the server advertises; AuthParams are extra authorization URL parameters.
// A gmail connector has no OAuth discovery, so it names Google's AuthURL,
// TokenURL and RevokeURL itself.
type ConnectorConfig struct {
	Kind         string // "mcp" (default) or "gmail"
	URL          string
	AuthURL      string
	TokenURL     string
	RevokeURL    string
	ClientID     string
	ClientSecret string
	Scopes       []string
	AuthParams   map[string]string
}

// AuthConfig configures sign-in. GoogleClientIDs are the OAuth client ids
// whose ID tokens are accepted (the iOS client id at least). Passkeys are
// bound to WebAuthnRPID, a domain serving apple-app-site-association.
type AuthConfig struct {
	GoogleClientIDs []string
	WebAuthnRPID    string
	WebAuthnRPName  string
	WebAuthnOrigins []string
	SessionTTL      time.Duration
}

// APNSConfig is the APNs token-auth key. Push is off unless every field is set.
type APNSConfig struct {
	KeyPath string
	KeyID   string
	TeamID  string
	Topic   string
}

// NtfyConfig is the ntfy server. Delivery is off unless URL, TopicPrefix and
// TopicSecret (or TOKEN_ENCRYPTION_KEY) are set.
type NtfyConfig struct {
	URL         string
	TopicPrefix string
	TopicSecret string
	Token       string
}

// AgentConfig configures the models the agent loop talks to (Fireworks).
type AgentConfig struct {
	// PublicHost is the host of PUBLIC_URL: the server must not fetch itself.
	PublicHost       string
	FireworksAPIKey  string
	FireworksBaseURL string
	QualityModel     string
	SpeedModel       string
	MaxTokens        int
	MaxSteps         int
	RoutineMaxSteps  int
	ContextBudget    int
	ModelTimeout     time.Duration
	ModelMaxRetries  int
	TavilyAPIKey     string
}

// WorkerConfig configures the background job worker.
type WorkerConfig struct {
	Concurrency  int
	Lease        time.Duration
	PollInterval time.Duration
}

// Load reads configuration from environment variables (and a local .env if present).
func Load() *Config {
	if err := godotenv.Load(); err != nil {
		log.Println("No .env file found, using system environment variables")
	}

	port := getEnv("PORT", "8787")
	publicURL := strings.TrimRight(getEnv("PUBLIC_URL", "http://127.0.0.1:"+port), "/")
	rpID := "localhost"
	if u, err := url.Parse(publicURL); err == nil && u.Hostname() != "" {
		rpID = u.Hostname()
	}
	tokenKey, err := secretbox.ParseKey(getEnv("TOKEN_ENCRYPTION_KEY", ""))
	if err != nil {
		log.Fatalf("config: %v", err)
	}
	return &Config{
		TokenKey:     tokenKey,
		Host:         getEnv("HOST", "127.0.0.1"),
		Port:         port,
		Environment:  getEnv("ENVIRONMENT", "development"),
		PublicURL:    publicURL,
		WorkspaceDir: getEnv("WORKSPACE_DIR", "data/workspaces"),
		BodyLimit:    getEnvInt("BODY_LIMIT_BYTES", 20*1024*1024),

		Agent: AgentConfig{
			PublicHost:       rpID,
			FireworksAPIKey:  getEnv("FIREWORKS_API_KEY", ""),
			FireworksBaseURL: getEnv("FIREWORKS_BASE_URL", "https://api.fireworks.ai/inference/v1"),
			QualityModel:     getEnv("AGENT_QUALITY_MODEL", "accounts/fireworks/models/glm-5p3-flash"),
			SpeedModel:       getEnv("AGENT_SPEED_MODEL", "accounts/fireworks/models/deepseek-v4p1-flash"),
			MaxTokens:        getEnvInt("AGENT_MAX_TOKENS", 16000),
			MaxSteps:         getEnvInt("AGENT_MAX_STEPS", 100),
			RoutineMaxSteps:  getEnvInt("ROUTINE_MAX_STEPS", 40),
			ContextBudget:    getEnvInt("AGENT_CONTEXT_BUDGET", 300000),
			ModelTimeout:     time.Duration(getEnvInt("AGENT_MODEL_TIMEOUT_SECONDS", 120)) * time.Second,
			ModelMaxRetries:  getEnvInt("AGENT_MODEL_MAX_RETRIES", 2),
			TavilyAPIKey:     getEnv("TAVILY_API_KEY", ""),
		},

		Auth: AuthConfig{
			GoogleClientIDs: splitList(getEnv("GOOGLE_CLIENT_IDS", "")),
			WebAuthnRPID:    getEnv("WEBAUTHN_RP_ID", rpID),
			WebAuthnRPName:  getEnv("WEBAUTHN_RP_NAME", "Keen"),
			WebAuthnOrigins: splitList(getEnv("WEBAUTHN_ORIGINS", publicURL)),
			SessionTTL:      time.Duration(getEnvInt("SESSION_TTL_HOURS", 24*30)) * time.Hour,
		},

		Worker: WorkerConfig{
			Concurrency:  getEnvInt("WORKER_CONCURRENCY", 16),
			Lease:        time.Duration(getEnvInt("WORKER_LEASE_SECONDS", 30)) * time.Second,
			PollInterval: time.Duration(getEnvInt("WORKER_POLL_MS", 1000)) * time.Millisecond,
		},

		APNS: APNSConfig{
			KeyPath: getEnv("APNS_KEY_PATH", ""),
			KeyID:   getEnv("APNS_KEY_ID", ""),
			TeamID:  getEnv("APNS_TEAM_ID", ""),
			Topic:   getEnv("APNS_TOPIC", ""),
		},

		Ntfy: NtfyConfig{
			URL:         strings.TrimRight(getEnv("NTFY_URL", ""), "/"),
			TopicPrefix: getEnv("NTFY_TOPIC", ""),
			TopicSecret: getEnv("NTFY_TOPIC_SECRET", getEnv("TOKEN_ENCRYPTION_KEY", "")),
			Token:       getEnv("NTFY_TOKEN", ""),
		},

		Connectors: map[string]ConnectorConfig{
			"gmail": {
				Kind:         "gmail",
				URL:          getEnv("GMAIL_API_URL", "https://gmail.googleapis.com"),
				AuthURL:      "https://accounts.google.com/o/oauth2/v2/auth",
				TokenURL:     "https://oauth2.googleapis.com/token",
				RevokeURL:    "https://oauth2.googleapis.com/revoke",
				ClientID:     getEnv("GMAIL_CLIENT_ID", ""),
				ClientSecret: getEnv("GMAIL_CLIENT_SECRET", ""),
				Scopes: strings.Fields(getEnv("GMAIL_SCOPES",
					"https://www.googleapis.com/auth/gmail.modify https://www.googleapis.com/auth/gmail.compose")),
				// Google issues a refresh token only for offline access, and again only with consent.
				AuthParams: map[string]string{"access_type": "offline", "prompt": "consent"},
			},
		},
	}
}

// IsDevelopment reports whether the app is running in development mode.
func (c *Config) IsDevelopment() bool { return c.Environment == "development" }

// IsProduction reports whether the app is running in production mode.
func (c *Config) IsProduction() bool { return c.Environment == "production" }

// --- helpers ---

func getEnv(key, def string) string {
	if v := os.Getenv(key); v != "" {
		return v
	}
	return def
}

// splitList splits a comma separated value, dropping blanks.
func splitList(v string) []string {
	var out []string
	for _, s := range strings.Split(v, ",") {
		if s = strings.TrimSpace(s); s != "" {
			out = append(out, s)
		}
	}
	return out
}

func getEnvInt(key string, def int) int {
	if v := os.Getenv(key); v != "" {
		if n, err := strconv.Atoi(v); err == nil {
			return n
		}
	}
	return def
}
