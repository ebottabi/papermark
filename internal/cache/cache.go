/*
 * Copyright (c) 2026. Papermark - All Rights Reserved
 * Unauthorized copying of this file, via any medium is strictly prohibited
 * Proprietary and confidential
 * Written by Ebot Tabi <ebot.tabi@gmail.com>, 15/10/2026
 */

// Package cache is the Redis cache-aside layer. Services MUST go through this
// package rather than touching *redis.Client directly. Every write sets a TTL;
// every underlying-record mutation invalidates the affected key(s).
package cache

import (
	"context"
	"fmt"
	"time"

	"github.com/redis/go-redis/v9"
)

// Key prefix and common TTLs shared across the app.
const keyPrefix = "papermark"

const (
	TTL1Minute   = 1 * time.Minute
	TTL5Minutes  = 5 * time.Minute
	TTL15Minutes = 15 * time.Minute
	TTL1Hour     = 1 * time.Hour
	TTL24Hours   = 24 * time.Hour
)

// Cache wraps a Redis client with cache-aside helpers.
type Cache struct {
	rdb *redis.Client
}

// New returns a Cache backed by the given Redis client.
func New(rdb *redis.Client) *Cache {
	return &Cache{rdb: rdb}
}

// Key builds a namespaced cache key, e.g. Key("project", id) -> "papermark:project:<id>".
func Key(parts ...string) string {
	key := keyPrefix
	for _, p := range parts {
		key += ":" + p
	}
	return key
}

// Get returns the string value for key, or redis.Nil if absent.
func (c *Cache) Get(ctx context.Context, key string) (string, error) {
	if c == nil || c.rdb == nil {
		return "", fmt.Errorf("cache not configured")
	}
	return c.rdb.Get(ctx, key).Result()
}

// Set stores value at key with a mandatory TTL.
func (c *Cache) Set(ctx context.Context, key, value string, ttl time.Duration) error {
	if c == nil || c.rdb == nil {
		return nil // graceful no-op when cache is disabled
	}
	return c.rdb.Set(ctx, key, value, ttl).Err()
}

// Delete removes one or more keys (invalidation).
func (c *Cache) Delete(ctx context.Context, keys ...string) error {
	if c == nil || c.rdb == nil {
		return nil
	}
	return c.rdb.Del(ctx, keys...).Err()
}

// Exists reports whether key is present.
func (c *Cache) Exists(ctx context.Context, key string) (bool, error) {
	if c == nil || c.rdb == nil {
		return false, nil
	}
	n, err := c.rdb.Exists(ctx, key).Result()
	return n > 0, err
}

// TODO: add typed helpers as patterns emerge, e.g.
//   GetJSON(ctx, key, dest any) / SetJSON(ctx, key, v any, ttl)
//   Remember(ctx, key, ttl, loader func() (any, error)) — full cache-aside wrapper.
