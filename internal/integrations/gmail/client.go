/*
 * Copyright (c) 2026. Papermark - All Rights Reserved
 * Unauthorized copying of this file, via any medium is strictly prohibited
 * Proprietary and confidential
 * Written by Ebot Tabi <ebot.tabi@gmail.com>, 15/10/2026
 */
// Package gmail talks to the Gmail REST API and exposes it as agent tools.
package gmail

import (
	"bytes"
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
	"sync"

	"golang.org/x/oauth2"
)

// ErrNeedsAuth means Google rejected the credentials: the access token was
// refused or the refresh token no longer works. The user must reconnect.
var ErrNeedsAuth = errors.New("gmail: authorization required")

// APIError is an error response from the Gmail API.
type APIError struct {
	Status  int
	Message string
}

func (e *APIError) Error() string { return fmt.Sprintf("gmail: %d %s", e.Status, e.Message) }

// Client calls the Gmail API as one user. HTTP must attach that user's bearer
// token (an oauth2 transport) and BaseURL is the API origin.
type Client struct {
	HTTP    *http.Client
	BaseURL string
}

const maxResponse = 64 << 20

// call sends one request to users/me/<path> and decodes the JSON reply into out (nil to discard).
func (c *Client) call(ctx context.Context, method, path string, q url.Values, body, out any) error {
	var rd io.Reader
	if body != nil {
		b, err := json.Marshal(body)
		if err != nil {
			return err
		}
		rd = bytes.NewReader(b)
	}
	u := strings.TrimRight(c.BaseURL, "/") + "/gmail/v1/users/me/" + path
	if len(q) > 0 {
		u += "?" + q.Encode()
	}
	req, err := http.NewRequestWithContext(ctx, method, u, rd)
	if err != nil {
		return err
	}
	if body != nil {
		req.Header.Set("Content-Type", "application/json")
	}
	resp, err := c.HTTP.Do(req)
	if err != nil {
		var re *oauth2.RetrieveError
		if errors.As(err, &re) && (re.Response.StatusCode == http.StatusBadRequest || re.Response.StatusCode == http.StatusUnauthorized) {
			return ErrNeedsAuth
		}
		return err
	}
	defer resp.Body.Close()
	raw, err := io.ReadAll(io.LimitReader(resp.Body, maxResponse))
	if err != nil {
		return err
	}
	switch {
	case resp.StatusCode == http.StatusUnauthorized:
		return ErrNeedsAuth
	case resp.StatusCode >= 300:
		var e struct {
			Error struct{ Message string }
		}
		json.Unmarshal(raw, &e)
		if e.Error.Message == "" {
			e.Error.Message = http.StatusText(resp.StatusCode)
		}
		return &APIError{resp.StatusCode, e.Error.Message}
	}
	if out == nil {
		return nil
	}
	return json.Unmarshal(raw, out)
}

// Header is one RFC 5322 header of a message part.
type Header struct {
	Name  string `json:"name"`
	Value string `json:"value"`
}

// Part is one node of a message's MIME tree.
type Part struct {
	MimeType string   `json:"mimeType"`
	Filename string   `json:"filename"`
	Headers  []Header `json:"headers"`
	Body     struct {
		AttachmentID string `json:"attachmentId"`
		Size         int    `json:"size"`
		Data         string `json:"data"`
	} `json:"body"`
	Parts []Part `json:"parts"`
}

// Message is a Gmail message; Payload is empty unless fetched with format full or metadata.
type Message struct {
	ID           string   `json:"id"`
	ThreadID     string   `json:"threadId"`
	LabelIDs     []string `json:"labelIds"`
	Snippet      string   `json:"snippet"`
	InternalDate string   `json:"internalDate"`
	Payload      Part     `json:"payload"`
}

func (p *Part) header(name string) string {
	for _, h := range p.Headers {
		if strings.EqualFold(h.Name, name) {
			return h.Value
		}
	}
	return ""
}

func (m *Message) has(label string) bool {
	for _, l := range m.LabelIDs {
		if l == label {
			return true
		}
	}
	return false
}

// Profile is the signed-in mailbox.
type Profile struct {
	Email     string `json:"emailAddress"`
	HistoryID string `json:"historyId"`
}

// Profile returns the mailbox address and its current history id.
func (c *Client) Profile(ctx context.Context) (*Profile, error) {
	p := &Profile{}
	return p, c.call(ctx, http.MethodGet, "profile", nil, nil, p)
}

func (c *Client) getMessage(ctx context.Context, id, format string, headers ...string) (*Message, error) {
	q := url.Values{"format": {format}}
	for _, h := range headers {
		q.Add("metadataHeaders", h)
	}
	m := &Message{}
	return m, c.call(ctx, http.MethodGet, "messages/"+url.PathEscape(id), q, nil, m)
}

func (c *Client) attachment(ctx context.Context, messageID, attachmentID string) ([]byte, error) {
	var out struct{ Data string }
	if err := c.call(ctx, http.MethodGet, "messages/"+url.PathEscape(messageID)+"/attachments/"+url.PathEscape(attachmentID), nil, nil, &out); err != nil {
		return nil, err
	}
	return decodeB64(out.Data)
}

func decodeB64(s string) ([]byte, error) {
	return base64.RawURLEncoding.DecodeString(strings.TrimRight(s, "="))
}

// each runs fn for 0..n-1, at most limit at a time, and returns the first error.
func each(n, limit int, fn func(i int) error) error {
	var (
		wg    sync.WaitGroup
		mu    sync.Mutex
		first error
		sem   = make(chan struct{}, limit)
	)
	for i := 0; i < n; i++ {
		sem <- struct{}{}
		wg.Add(1)
		go func() {
			defer func() { <-sem; wg.Done() }()
			if err := fn(i); err != nil {
				mu.Lock()
				if first == nil {
					first = err
				}
				mu.Unlock()
			}
		}()
	}
	wg.Wait()
	return first
}

func base64URL(b []byte) string { return base64.URLEncoding.EncodeToString(b) }
