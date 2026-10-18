/*
 * Copyright (c) 2026. Papermark - All Rights Reserved
 * Unauthorized copying of this file, via any medium is strictly prohibited
 * Proprietary and confidential
 * Written by Ebot Tabi <ebot.tabi@gmail.com>, 17/10/2026
 */
package services

import (
	"context"

	"papermark/internal/integrations/gmail"
	"papermark/internal/models"
	"papermark/internal/models/queries"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"
)

// GmailLabelService lists a signed-in Gmail account's labels, so an email
// routine can be filtered by a label the user picks by name.
type GmailLabelService struct {
	pool   *pgxpool.Pool
	client func(context.Context, *models.GmailAccount) (*gmail.Client, error)
}

// NewGmailLabelService reaches Gmail with the account's tokens from cs.
func NewGmailLabelService(pool *pgxpool.Pool, cs *ConnectorService) *GmailLabelService {
	return &GmailLabelService{pool: pool, client: cs.MailClient}
}

// List returns the labels of user's gmail account id. Another user's account,
// or a calendar account, is queries.ErrNotFound; a revoked one is gmail.ErrNeedsAuth.
func (s *GmailLabelService) List(ctx context.Context, user, id uuid.UUID) ([]gmail.Label, error) {
	a, err := queries.GetGmailAccount(ctx, s.pool, user, id)
	if err != nil {
		return nil, err
	}
	c, err := queries.GetConnector(ctx, s.pool, a.ConnectorID)
	if err != nil {
		return nil, err
	}
	if c.Kind != models.ConnectorGmail {
		return nil, queries.ErrNotFound
	}
	client, err := s.client(ctx, a)
	if err != nil {
		return nil, err
	}
	return client.Labels(ctx)
}
