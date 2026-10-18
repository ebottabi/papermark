/*
 * Copyright (c) 2026. Papermark - All Rights Reserved
 * Unauthorized copying of this file, via any medium is strictly prohibited
 * Proprietary and confidential
 * Written by Ebot Tabi <ebot.tabi@gmail.com>, 17/10/2026
 */
package gmail

import (
	"context"
	"errors"
	"net/http"
	"net/url"
	"strings"
)

// Arrival is a message that reached the inbox, with what rule matching needs.
type Arrival struct {
	ID            string
	From          string
	Subject       string
	Snippet       string
	Labels        []string
	HasAttachment bool
	FromKeen      bool
}

// NewMail lists the inbox messages added after history id start and the
// history id the listing read through to. expired is true when Gmail no longer
// keeps start (about a week), and the caller must resync from the profile.
func (c *Client) NewMail(ctx context.Context, start string) (ids []string, next string, expired bool, err error) {
	seen := map[string]bool{}
	token := ""
	for {
		v := url.Values{"startHistoryId": {start}, "historyTypes": {"messageAdded"}, "labelId": {"INBOX"}, "maxResults": {"500"}}
		if token != "" {
			v.Set("pageToken", token)
		}
		var out struct {
			History []struct {
				MessagesAdded []struct{ Message struct{ ID string } }
			}
			HistoryID     string
			NextPageToken string
		}
		if err := c.call(ctx, http.MethodGet, "history", v, nil, &out); err != nil {
			var ae *APIError
			if errors.As(err, &ae) && ae.Status == http.StatusNotFound {
				return nil, "", true, nil
			}
			return nil, "", false, err
		}
		for _, h := range out.History {
			for _, m := range h.MessagesAdded {
				if !seen[m.Message.ID] {
					seen[m.Message.ID] = true
					ids = append(ids, m.Message.ID)
				}
			}
		}
		next = out.HistoryID
		if token = out.NextPageToken; token == "" {
			return ids, next, false, nil
		}
	}
}

// Arrival reads message id. withParts also fetches the MIME tree, which is
// the only way to see attachments. A message deleted since it arrived is nil.
func (c *Client) Arrival(ctx context.Context, id string, withParts bool) (*Arrival, error) {
	format := "metadata"
	if withParts {
		format = "full"
	}
	m, err := c.getMessage(ctx, id, format, "From", "Subject", KeenHeader)
	if err != nil {
		var ae *APIError
		if errors.As(err, &ae) && ae.Status == http.StatusNotFound {
			return nil, nil
		}
		return nil, err
	}
	a := &Arrival{ID: m.ID, From: m.Payload.header("From"), Subject: m.Payload.header("Subject"), Snippet: m.Snippet, Labels: m.LabelIDs,
		FromKeen: strings.TrimSpace(m.Payload.header(KeenHeader)) != ""}
	if withParts {
		_, atts := extract(&m.Payload)
		a.HasAttachment = len(atts) > 0
	}
	return a, nil
}
