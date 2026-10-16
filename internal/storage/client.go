/*
 * Copyright (c) 2026. Papermark - All Rights Reserved
 * Unauthorized copying of this file, via any medium is strictly prohibited
 * Proprietary and confidential
 * Written by Ebot Tabi <ebot.tabi@gmail.com>, 15/10/2026
 */

// Package storage abstracts file storage behind a small interface so the
// backing implementation (local disk, S3, GCS, ...) can change without touching
// callers.
package storage

import (
	"context"
	"errors"
	"io"
	"os"
	"path/filepath"
	"strings"
)

// ErrEscape is returned for a key that resolves outside the storage root.
var ErrEscape = errors.New("storage: path escapes the root")

// Storage is the minimal contract services depend on.
type Storage interface {
	Put(ctx context.Context, key string, r io.Reader) error
	Get(ctx context.Context, key string) (io.ReadCloser, error)
	Delete(ctx context.Context, key string) error
}

// Client stores keys as files under Root on local disk.
type Client struct {
	Root string
}

// NewClient constructs a storage Client rooted at root.
func NewClient(root string) *Client { return &Client{Root: root} }

// Path resolves key under Root, rejecting keys that escape it lexically.
func (c *Client) Path(key string) (string, error) {
	return Resolve(c.Root, key)
}

func (c *Client) Put(ctx context.Context, key string, r io.Reader) error {
	p, err := c.Path(key)
	if err != nil {
		return err
	}
	if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
		return err
	}
	f, err := os.Create(p)
	if err != nil {
		return err
	}
	if _, err := io.Copy(f, r); err != nil {
		f.Close()
		return err
	}
	return f.Close()
}

func (c *Client) Get(ctx context.Context, key string) (io.ReadCloser, error) {
	p, err := c.Path(key)
	if err != nil {
		return nil, err
	}
	return os.Open(p)
}

func (c *Client) Delete(ctx context.Context, key string) error {
	p, err := c.Path(key)
	if err != nil {
		return err
	}
	return os.RemoveAll(p)
}

// Resolve joins rel onto root and fails with ErrEscape when the result is
// not root itself or below it.
func Resolve(root, rel string) (string, error) {
	absRoot, err := filepath.Abs(root)
	if err != nil {
		return "", err
	}
	p := filepath.Join(absRoot, rel)
	if p != absRoot && !strings.HasPrefix(p, absRoot+string(filepath.Separator)) {
		return "", ErrEscape
	}
	return p, nil
}
