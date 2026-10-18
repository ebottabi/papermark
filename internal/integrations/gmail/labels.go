/*
 * Copyright (c) 2026. Papermark - All Rights Reserved
 * Unauthorized copying of this file, via any medium is strictly prohibited
 * Proprietary and confidential
 * Written by Ebot Tabi <ebot.tabi@gmail.com>, 17/10/2026
 */
package gmail

import (
	"context"
	"net/http"
)

// Label is one mailbox label: a system one such as INBOX, or one the user made.
type Label struct {
	ID   string `json:"id"`
	Name string `json:"name"`
	Type string `json:"type"` // system or user
}

// Labels lists the mailbox's labels (users.labels.list).
func (c *Client) Labels(ctx context.Context) ([]Label, error) {
	var out struct{ Labels []Label }
	if err := c.call(ctx, http.MethodGet, "labels", nil, nil, &out); err != nil {
		return nil, err
	}
	if out.Labels == nil {
		out.Labels = []Label{}
	}
	return out.Labels, nil
}
