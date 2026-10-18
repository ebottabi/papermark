/*
 * Copyright (c) 2026. Papermark - All Rights Reserved
 * Unauthorized copying of this file, via any medium is strictly prohibited
 * Proprietary and confidential
 * Written by Ebot Tabi <ebot.tabi@gmail.com>, 17/10/2026
 */
// Package gcontacts reads Google Contacts through the People API v1. It only
// reads: nothing is ever written back to Google.
package gcontacts

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"

	"golang.org/x/oauth2"
)

// ErrNeedsAuth means Google rejected the credentials: the access token was
// refused or the refresh token no longer works. The user must reconnect.
var ErrNeedsAuth = errors.New("contacts: authorization required")

// ErrSyncTokenExpired means a sync token is too old; the caller runs a full sync.
var ErrSyncTokenExpired = errors.New("contacts: sync token expired")

// APIError is an error response from the People API.
type APIError struct {
	Status  int
	Message string
}

func (e *APIError) Error() string { return fmt.Sprintf("contacts: %d %s", e.Status, e.Message) }

// Client calls the People API as one user. HTTP must attach that user's
// bearer token (an oauth2 transport) and BaseURL is the API origin.
type Client struct {
	HTTP    *http.Client
	BaseURL string
}

const (
	maxResponse = 32 << 20
	maxPages    = 200 // 200k contacts at 1000 a page
	pageSize    = "1000"
	// PersonFields are the fields read for saved contacts.
	PersonFields = "names,emailAddresses,phoneNumbers,organizations,biographies,relations,birthdays,memberships,metadata"
	// OtherFields are the fields otherContacts allows.
	OtherFields = "names,emailAddresses,phoneNumbers,metadata"
)

func (c *Client) call(ctx context.Context, path string, q url.Values, out any) error {
	u := strings.TrimRight(c.BaseURL, "/") + "/v1/" + path
	if len(q) > 0 {
		u += "?" + q.Encode()
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, u, nil)
	if err != nil {
		return err
	}
	resp, err := c.HTTP.Do(req)
	if err != nil {
		var re *oauth2.RetrieveError
		if errors.As(err, &re) && re.Response != nil && (re.Response.StatusCode == http.StatusBadRequest || re.Response.StatusCode == http.StatusUnauthorized) {
			return ErrNeedsAuth
		}
		return err
	}
	defer resp.Body.Close()
	raw, err := io.ReadAll(io.LimitReader(resp.Body, maxResponse))
	if err != nil {
		return err
	}
	if resp.StatusCode == http.StatusUnauthorized {
		return ErrNeedsAuth
	}
	if resp.StatusCode >= 300 {
		var e struct {
			Error struct {
				Message string
				Status  string
				Details []struct{ Reason string }
			}
		}
		json.Unmarshal(raw, &e)
		if resp.StatusCode == http.StatusGone || strings.Contains(string(raw), "EXPIRED_SYNC_TOKEN") ||
			strings.Contains(strings.ToLower(e.Error.Message), "sync token is expired") {
			return ErrSyncTokenExpired
		}
		if e.Error.Message == "" {
			e.Error.Message = http.StatusText(resp.StatusCode)
		}
		return &APIError{resp.StatusCode, e.Error.Message}
	}
	if out == nil || len(raw) == 0 {
		return nil
	}
	return json.Unmarshal(raw, out)
}

// Person is a People API person resource, the fields the sync stores.
type Person struct {
	ResourceName string `json:"resourceName"`
	ETag         string `json:"etag"`
	Metadata     struct {
		Deleted bool `json:"deleted"`
		Sources []struct {
			Type       string `json:"type"`
			UpdateTime string `json:"updateTime"`
		} `json:"sources"`
	} `json:"metadata"`
	Names []struct {
		DisplayName string `json:"displayName"`
	} `json:"names"`
	EmailAddresses []struct {
		Value    string `json:"value"`
		Type     string `json:"type"`
		Metadata struct {
			Primary bool `json:"primary"`
		} `json:"metadata"`
	} `json:"emailAddresses"`
	PhoneNumbers []struct {
		Value         string `json:"value"`
		CanonicalForm string `json:"canonicalForm"`
		Type          string `json:"type"`
	} `json:"phoneNumbers"`
	Organizations []struct {
		Name  string `json:"name"`
		Title string `json:"title"`
	} `json:"organizations"`
	Biographies []struct {
		Value string `json:"value"`
	} `json:"biographies"`
	Relations []struct {
		Person string `json:"person"`
		Type   string `json:"type"`
	} `json:"relations"`
	Birthdays []struct {
		Text string `json:"text"`
		Date *struct {
			Year, Month, Day int
		} `json:"date"`
	} `json:"birthdays"`
	Memberships []struct {
		ContactGroupMembership *struct {
			ContactGroupResourceName string `json:"contactGroupResourceName"`
		} `json:"contactGroupMembership"`
	} `json:"memberships"`
}

// Me returns the primary address of the signed-in user.
func (c *Client) Me(ctx context.Context) (string, error) {
	var p Person
	if err := c.call(ctx, "people/me", url.Values{"personFields": {"emailAddresses"}}, &p); err != nil {
		return "", err
	}
	for _, e := range p.EmailAddresses {
		if e.Metadata.Primary && e.Value != "" {
			return e.Value, nil
		}
	}
	if len(p.EmailAddresses) > 0 && p.EmailAddresses[0].Value != "" {
		return p.EmailAddresses[0].Value, nil
	}
	return "", errors.New("contacts: Google did not return the account's address (is the userinfo.email scope granted?)")
}

// Connections returns the user's saved contacts, every page. With syncToken it
// returns only what changed since (deleted ones have Metadata.Deleted), and
// ErrSyncTokenExpired when the token is too old. next is the token of the
// following incremental sync.
func (c *Client) Connections(ctx context.Context, syncToken string) (people []Person, next string, err error) {
	return c.pages(ctx, "people/me/connections", url.Values{"personFields": {PersonFields}}, syncToken, func(raw json.RawMessage) ([]Person, error) {
		var p struct{ Connections []Person }
		return p.Connections, json.Unmarshal(raw, &p)
	})
}

// OtherContacts is Connections for the "other contacts" Google collected from mail.
func (c *Client) OtherContacts(ctx context.Context, syncToken string) (people []Person, next string, err error) {
	return c.pages(ctx, "otherContacts", url.Values{"readMask": {OtherFields}}, syncToken, func(raw json.RawMessage) ([]Person, error) {
		var p struct{ OtherContacts []Person }
		return p.OtherContacts, json.Unmarshal(raw, &p)
	})
}

func (c *Client) pages(ctx context.Context, path string, q url.Values, syncToken string, items func(json.RawMessage) ([]Person, error)) ([]Person, string, error) {
	q.Set("pageSize", pageSize)
	q.Set("requestSyncToken", "true")
	if syncToken != "" {
		q.Set("syncToken", syncToken)
	}
	var all []Person
	for range maxPages {
		var raw json.RawMessage
		if err := c.call(ctx, path, q, &raw); err != nil {
			return nil, "", err
		}
		people, err := items(raw)
		if err != nil {
			return nil, "", err
		}
		all = append(all, people...)
		var page struct{ NextPageToken, NextSyncToken string }
		if err := json.Unmarshal(raw, &page); err != nil {
			return nil, "", err
		}
		if page.NextPageToken == "" {
			return all, page.NextSyncToken, nil
		}
		q.Set("pageToken", page.NextPageToken)
	}
	return nil, "", fmt.Errorf("contacts: more than %d pages", maxPages)
}

// Groups returns the names of the user's contact groups by resource name.
func (c *Client) Groups(ctx context.Context) (map[string]string, error) {
	names := map[string]string{}
	q := url.Values{"pageSize": {"1000"}}
	for range maxPages {
		var page struct {
			ContactGroups []struct{ ResourceName, Name, FormattedName string }
			NextPageToken string
		}
		if err := c.call(ctx, "contactGroups", q, &page); err != nil {
			return nil, err
		}
		for _, g := range page.ContactGroups {
			n := g.FormattedName
			if n == "" {
				n = g.Name
			}
			names[g.ResourceName] = n
		}
		if page.NextPageToken == "" {
			break
		}
		q.Set("pageToken", page.NextPageToken)
	}
	return names, nil
}
