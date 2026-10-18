/*
 * Copyright (c) 2026. Papermark - All Rights Reserved
 * Unauthorized copying of this file, via any medium is strictly prohibited
 * Proprietary and confidential
 * Written by Ebot Tabi <ebot.tabi@gmail.com>, 17/10/2026
 */
// Package gdrive reads Google Drive through the Drive API v3 and exposes it
// as read-only agent tools. Nothing is ever written to Drive.
package gdrive

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
var ErrNeedsAuth = errors.New("drive: authorization required")

// ErrTooLarge means a download is over the size cap.
var ErrTooLarge = errors.New("drive: file is too large")

// APIError is an error response from the Drive API.
type APIError struct {
	Status  int
	Message string
}

func (e *APIError) Error() string { return fmt.Sprintf("drive: %d %s", e.Status, e.Message) }

// notFound reports whether err is Drive's answer for a file the account cannot see.
func notFound(err error) bool {
	var ae *APIError
	return errors.As(err, &ae) && ae.Status == http.StatusNotFound
}

// Client calls the Drive API as one user. HTTP must attach that user's
// bearer token (an oauth2 transport) and BaseURL is the API origin.
type Client struct {
	HTTP    *http.Client
	BaseURL string
}

const maxJSON = 16 << 20

// do sends a GET to drive/v3/<path>. A reply over limit bytes is ErrTooLarge.
func (c *Client) do(ctx context.Context, path string, q url.Values, limit int64) ([]byte, error) {
	u := strings.TrimRight(c.BaseURL, "/") + "/drive/v3/" + path
	if len(q) > 0 {
		u += "?" + q.Encode()
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, u, nil)
	if err != nil {
		return nil, err
	}
	resp, err := c.HTTP.Do(req)
	if err != nil {
		var re *oauth2.RetrieveError
		if errors.As(err, &re) && re.Response != nil && (re.Response.StatusCode == http.StatusBadRequest || re.Response.StatusCode == http.StatusUnauthorized) {
			return nil, ErrNeedsAuth
		}
		return nil, err
	}
	defer resp.Body.Close()
	if resp.StatusCode == http.StatusUnauthorized {
		return nil, ErrNeedsAuth
	}
	if resp.StatusCode >= 300 {
		raw, _ := io.ReadAll(io.LimitReader(resp.Body, 1<<20))
		var e struct {
			Error struct{ Message string }
		}
		json.Unmarshal(raw, &e)
		if e.Error.Message == "" {
			e.Error.Message = http.StatusText(resp.StatusCode)
		}
		return nil, &APIError{resp.StatusCode, e.Error.Message}
	}
	raw, err := io.ReadAll(io.LimitReader(resp.Body, limit+1))
	if err != nil {
		return nil, err
	}
	if int64(len(raw)) > limit {
		return nil, ErrTooLarge
	}
	return raw, nil
}

func (c *Client) call(ctx context.Context, path string, q url.Values, out any) error {
	raw, err := c.do(ctx, path, q, maxJSON)
	if err != nil {
		return err
	}
	return json.Unmarshal(raw, out)
}

// Person is a Drive user: an owner or the user who shared a file.
type Person struct {
	DisplayName  string `json:"displayName"`
	EmailAddress string `json:"emailAddress"`
}

// Permission is one grant on a file.
type Permission struct {
	Type         string `json:"type"` // user, group, domain or anyone
	Role         string `json:"role"`
	EmailAddress string `json:"emailAddress"`
	Domain       string `json:"domain"`
	DisplayName  string `json:"displayName"`
}

// File is a Drive file resource, the fields the tools read.
type File struct {
	ID              string       `json:"id"`
	Name            string       `json:"name"`
	MimeType        string       `json:"mimeType"`
	Owners          []Person     `json:"owners"`
	ModifiedTime    string       `json:"modifiedTime"`
	CreatedTime     string       `json:"createdTime"`
	SharingUser     *Person      `json:"sharingUser"`
	LastModifying   *Person      `json:"lastModifyingUser"`
	Size            string       `json:"size"` // int64 as a string; absent for Google Docs
	WebViewLink     string       `json:"webViewLink"`
	Shared          bool         `json:"shared"`
	Permissions     []Permission `json:"permissions"`
	ShortcutDetails *struct {
		TargetID       string `json:"targetId"`
		TargetMimeType string `json:"targetMimeType"`
	} `json:"shortcutDetails"`
}

const (
	listFields = "nextPageToken,files(id,name,mimeType,owners(displayName,emailAddress),modifiedTime,sharingUser(displayName,emailAddress),size,webViewLink)"
	getFields  = "id,name,mimeType,owners(displayName,emailAddress),modifiedTime,createdTime,sharingUser(displayName,emailAddress),lastModifyingUser(displayName,emailAddress),size,webViewLink,shared,permissions(type,role,emailAddress,domain,displayName),shortcutDetails"
)

// About returns the address of the signed-in user.
func (c *Client) About(ctx context.Context) (string, error) {
	var a struct{ User Person }
	if err := c.call(ctx, "about", url.Values{"fields": {"user"}}, &a); err != nil {
		return "", err
	}
	if a.User.EmailAddress == "" {
		return "", errors.New("drive: Google did not return the account's address")
	}
	return a.User.EmailAddress, nil
}

// Search lists up to limit files matching the Drive query q, across My Drive,
// files shared with the user and shared drives.
func (c *Client) Search(ctx context.Context, q string, limit int, orderBy string) ([]File, error) {
	v := url.Values{"q": {q}, "fields": {listFields}, "pageSize": {fmt.Sprint(min(limit, 100))},
		"corpora": {"allDrives"}, "includeItemsFromAllDrives": {"true"}, "supportsAllDrives": {"true"}}
	if orderBy != "" {
		v.Set("orderBy", orderBy)
	}
	var all []File
	for len(all) < limit {
		var page struct {
			Files         []File
			NextPageToken string
		}
		if err := c.call(ctx, "files", v, &page); err != nil {
			return nil, err
		}
		all = append(all, page.Files...)
		if page.NextPageToken == "" {
			break
		}
		v.Set("pageToken", page.NextPageToken)
	}
	if len(all) > limit {
		all = all[:limit]
	}
	return all, nil
}

// Get returns a file's metadata, with its permissions when the user may see them.
func (c *Client) Get(ctx context.Context, id string) (*File, error) {
	f := &File{}
	return f, c.call(ctx, "files/"+url.PathEscape(id), url.Values{"fields": {getFields}, "supportsAllDrives": {"true"}}, f)
}

// Export converts a Google Docs, Sheets or Slides file to mime, at most limit bytes.
func (c *Client) Export(ctx context.Context, id, mime string, limit int64) ([]byte, error) {
	return c.do(ctx, "files/"+url.PathEscape(id)+"/export", url.Values{"mimeType": {mime}}, limit)
}

// Media downloads a stored file's bytes, at most limit.
func (c *Client) Media(ctx context.Context, id string, limit int64) ([]byte, error) {
	return c.do(ctx, "files/"+url.PathEscape(id), url.Values{"alt": {"media"}, "supportsAllDrives": {"true"}}, limit)
}
