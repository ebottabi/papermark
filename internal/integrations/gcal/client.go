/*
 * Copyright (c) 2026. Papermark - All Rights Reserved
 * Unauthorized copying of this file, via any medium is strictly prohibited
 * Proprietary and confidential
 * Written by Ebot Tabi <ebot.tabi@gmail.com>, 17/10/2026
 */
// Package gcal talks to the Google Calendar API v3 and exposes it as agent tools.
package gcal

import (
	"bytes"
	"context"
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
var ErrNeedsAuth = errors.New("calendar: authorization required")

// APIError is an error response from the Calendar API.
type APIError struct {
	Status  int
	Message string
}

func (e *APIError) Error() string { return fmt.Sprintf("calendar: %d %s", e.Status, e.Message) }

// Client calls the Calendar API as one user. HTTP must attach that user's
// bearer token (an oauth2 transport) and BaseURL is the API origin.
type Client struct {
	HTTP    *http.Client
	BaseURL string
}

const maxResponse = 16 << 20

// call sends one request to calendar/v3/<path> and decodes the JSON reply into out (nil to discard).
func (c *Client) call(ctx context.Context, method, path string, q url.Values, body, out any) error {
	var rd io.Reader
	if body != nil {
		b, err := json.Marshal(body)
		if err != nil {
			return err
		}
		rd = bytes.NewReader(b)
	}
	u := strings.TrimRight(c.BaseURL, "/") + "/calendar/v3/" + path
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
	if out == nil || len(raw) == 0 {
		return nil
	}
	return json.Unmarshal(raw, out)
}

// CalendarEntry is one calendar of the user's calendar list.
type CalendarEntry struct {
	ID              string `json:"id"`
	Summary         string `json:"summary"`
	SummaryOverride string `json:"summaryOverride,omitempty"`
	Primary         bool   `json:"primary"`
	AccessRole      string `json:"accessRole"`
	TimeZone        string `json:"timeZone"`
	Selected        bool   `json:"selected"`
}

// PrimaryCalendar returns the signed-in user's primary calendar; its id is their address.
func (c *Client) PrimaryCalendar(ctx context.Context) (*CalendarEntry, error) {
	e := &CalendarEntry{}
	return e, c.call(ctx, http.MethodGet, "users/me/calendarList/primary", nil, nil, e)
}

const maxCalendarPages = 10

// Calendars returns the user's calendar list, every page.
func (c *Client) Calendars(ctx context.Context) ([]CalendarEntry, error) {
	var all []CalendarEntry
	token := ""
	for range maxCalendarPages {
		q := url.Values{"maxResults": {"250"}}
		if token != "" {
			q.Set("pageToken", token)
		}
		var page struct {
			Items         []CalendarEntry
			NextPageToken string
		}
		if err := c.call(ctx, http.MethodGet, "users/me/calendarList", q, nil, &page); err != nil {
			return nil, err
		}
		all = append(all, page.Items...)
		if token = page.NextPageToken; token == "" {
			break
		}
	}
	return all, nil
}

// EventTime is an event's start or end: Date for all-day events, DateTime otherwise.
type EventTime struct {
	Date     string `json:"date,omitempty"`
	DateTime string `json:"dateTime,omitempty"`
	TimeZone string `json:"timeZone,omitempty"`
}

// Attendee is one guest of an event.
type Attendee struct {
	Email          string `json:"email,omitempty"`
	DisplayName    string `json:"displayName,omitempty"`
	ResponseStatus string `json:"responseStatus,omitempty"`
	Optional       bool   `json:"optional,omitempty"`
	Organizer      bool   `json:"organizer,omitempty"`
	Self           bool   `json:"self,omitempty"`
	Resource       bool   `json:"resource,omitempty"`
	Comment        string `json:"comment,omitempty"`
}

// Event is a Calendar API event resource, the fields the tools read.
type Event struct {
	ID               string     `json:"id"`
	Status           string     `json:"status"`
	HTMLLink         string     `json:"htmlLink"`
	Summary          string     `json:"summary"`
	Description      string     `json:"description"`
	Location         string     `json:"location"`
	Start            EventTime  `json:"start"`
	End              EventTime  `json:"end"`
	RecurringEventID string     `json:"recurringEventId"`
	HangoutLink      string     `json:"hangoutLink"`
	Attendees        []Attendee `json:"attendees"`
	Organizer        struct {
		Email       string `json:"email"`
		DisplayName string `json:"displayName"`
		Self        bool   `json:"self"`
	} `json:"organizer"`
	ConferenceData struct {
		EntryPoints []struct {
			EntryPointType string `json:"entryPointType"`
			URI            string `json:"uri"`
		} `json:"entryPoints"`
	} `json:"conferenceData"`
}

func eventPath(calendarID, eventID string) string {
	p := "calendars/" + url.PathEscape(calendarID) + "/events"
	if eventID != "" {
		p += "/" + url.PathEscape(eventID)
	}
	return p
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
