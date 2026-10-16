/*
 * Copyright (c) 2026. Papermark - All Rights Reserved
 * Unauthorized copying of this file, via any medium is strictly prohibited
 * Proprietary and confidential
 * Written by Ebot Tabi <ebot.tabi@gmail.com>, 15/10/2026
 */

package database

import (
	"context"
	"fmt"
	"time"

	"github.com/redis/go-redis/v9"
)

// Redis is the package-level client, set by InitRedis.
var Redis *redis.Client

// RedisConfig holds Redis connection configuration.
type RedisConfig struct {
	Host     string
	Port     string
	Password string
	DB       int

	// redisURL, when set (e.g. by Upstash/fly via REDIS_URL), takes precedence
	// over the individual fields. Both redis:// (fly private network) and
	// rediss:// (public TLS) are supported.
	redisURL string
}

// LoadRedisConfig loads Redis configuration from environment variables. It
// prefers REDIS_URL (the convention used by fly.io/Upstash, Heroku, Render,
// etc.) and falls back to individual REDIS_* variables for local development.
func LoadRedisConfig() *RedisConfig {
	if url := getEnv("REDIS_URL", ""); url != "" {
		return &RedisConfig{redisURL: url}
	}
	return &RedisConfig{
		Host:     getEnv("REDIS_HOST", "localhost"),
		Port:     getEnv("REDIS_PORT", "6379"),
		Password: getEnv("REDIS_PASSWORD", ""),
		DB:       0,
	}
}

// InitRedis creates the client and verifies connectivity with a ping.
func InitRedis(cfg *RedisConfig) (*redis.Client, error) {
	opts := &redis.Options{
		PoolSize:     10,
		MinIdleConns: 5,
		DialTimeout:  5 * time.Second,
		ReadTimeout:  3 * time.Second,
		WriteTimeout: 3 * time.Second,
	}

	if cfg.redisURL != "" {
		parsed, err := redis.ParseURL(cfg.redisURL)
		if err != nil {
			return nil, fmt.Errorf("invalid REDIS_URL: %w", err)
		}
		// Keep addr/username/password/db/TLS from the URL, apply our pool tuning.
		parsed.PoolSize = opts.PoolSize
		parsed.MinIdleConns = opts.MinIdleConns
		parsed.DialTimeout = opts.DialTimeout
		parsed.ReadTimeout = opts.ReadTimeout
		parsed.WriteTimeout = opts.WriteTimeout
		opts = parsed
	} else {
		opts.Addr = fmt.Sprintf("%s:%s", cfg.Host, cfg.Port)
		opts.Password = cfg.Password
		opts.DB = cfg.DB
	}

	client := redis.NewClient(opts)

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	if err := client.Ping(ctx).Err(); err != nil {
		return nil, fmt.Errorf("failed to connect to redis: %w", err)
	}

	Redis = client
	return client, nil
}

// CloseRedis closes the connection.
func CloseRedis(client *redis.Client) error {
	if client != nil {
		return client.Close()
	}
	return nil
}

// RedisHealthCheck pings Redis.
func RedisHealthCheck(client *redis.Client) error {
	if client == nil {
		return fmt.Errorf("redis client is nil")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	return client.Ping(ctx).Err()
}
