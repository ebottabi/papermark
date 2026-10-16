/*
 * Copyright (c) 2026. Papermark - All Rights Reserved
 * Unauthorized copying of this file, via any medium is strictly prohibited
 * Proprietary and confidential
 * Written by Ebot Tabi <ebot.tabi@gmail.com>, 15/10/2026
 */

package config

import (
	"log"
	"os"
	"strconv"
	"strings"
	"time"

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

	// APIToken is the bearer token every /v1 request must carry.
	APIToken string

	// WorkspaceDir holds one directory per task: WorkspaceDir/<task id>.
	WorkspaceDir string

	// BodyLimit caps request bodies (attachments are base64 in JSON).
	BodyLimit int

	Agent  AgentConfig
	Worker WorkerConfig
	APNS   APNSConfig

	// Connectors is the fixed catalog the app offers, by connector name.
	Connectors map[string]ConnectorConfig
}

// ConnectorConfig is one catalog connector: its MCP server and, for
// providers without dynamic client registration, a pre-registered OAuth client.
// Scopes replaces the broad list the server advertises; AuthParams are extra
// authorization URL parameters.
type ConnectorConfig struct {
	URL          string
	ClientID     string
	ClientSecret string
	Scopes       []string
	AuthParams   map[string]string
}

// APNSConfig is the APNs token-auth key. Push is off unless every field is set.
type APNSConfig struct {
	KeyPath string
	KeyID   string
	TeamID  string
	Topic   string
}

// AgentConfig configures the model the agent loop talks to.
type AgentConfig struct {
	AnthropicAPIKey  string
	AnthropicBaseURL string
	Model            string
	MaxTokens        int
	MaxSteps         int
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
	return &Config{
		Host:         getEnv("HOST", "127.0.0.1"),
		Port:         port,
		Environment:  getEnv("ENVIRONMENT", "development"),
		PublicURL:    strings.TrimRight(getEnv("PUBLIC_URL", "http://127.0.0.1:"+port), "/"),
		APIToken:     getEnv("API_TOKEN", ""),
		WorkspaceDir: getEnv("WORKSPACE_DIR", "data/workspaces"),
		BodyLimit:    getEnvInt("BODY_LIMIT_BYTES", 20*1024*1024),

		Agent: AgentConfig{
			AnthropicAPIKey:  getEnv("ANTHROPIC_API_KEY", ""),
			AnthropicBaseURL: getEnv("ANTHROPIC_BASE_URL", ""),
			Model:            getEnv("AGENT_MODEL", "claude-opus-5-5"),
			MaxTokens:        getEnvInt("AGENT_MAX_TOKENS", 16000),
			MaxSteps:         getEnvInt("AGENT_MAX_STEPS", 100),
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

		Connectors: map[string]ConnectorConfig{
			"gmail": {
				URL:          getEnv("GMAIL_MCP_URL", "https://gmailmcp.googleapis.com/mcp/v1"),
				ClientID:     getEnv("GMAIL_CLIENT_ID", ""),
				ClientSecret: getEnv("GMAIL_CLIENT_SECRET", ""),
				Scopes: strings.Fields(getEnv("GMAIL_SCOPES",
					"https://www.googleapis.com/auth/gmail.readonly https://www.googleapis.com/auth/gmail.compose")),
				// Google issues a refresh token only for offline access, and again only with consent.
				AuthParams: map[string]string{"access_type": "offline", "prompt": "consent"},
			},
			"github": {
				URL:          getEnv("GITHUB_MCP_URL", "https://api.githubcopilot.com/mcp/"),
				ClientID:     getEnv("GITHUB_CLIENT_ID", ""),
				ClientSecret: getEnv("GITHUB_CLIENT_SECRET", ""),
				Scopes:       strings.Fields(getEnv("GITHUB_SCOPES", "repo read:org read:user user:email")),
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

func getEnvInt(key string, def int) int {
	if v := os.Getenv(key); v != "" {
		if n, err := strconv.Atoi(v); err == nil {
			return n
		}
	}
	return def
}
