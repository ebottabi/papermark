/*
 * Copyright (c) 2026. Papermark - All Rights Reserved
 * Unauthorized copying of this file, via any medium is strictly prohibited
 * Proprietary and confidential
 * Written by Ebot Tabi <ebot.tabi@gmail.com>, 15/10/2026
 */
package responses

import "papermark/internal/models"

// ConnectorDTO is API.md's Connector.
type ConnectorDTO struct {
	Name    string  `json:"name"`
	Kind    string  `json:"kind"`
	URL     string  `json:"url"`
	Status  string  `json:"status"`
	Error   *string `json:"error"`
	Tools   int     `json:"tools"`
	Account *string `json:"account"`
	// Accounts is the signed-in Google accounts of a gmail or calendar connector; empty for mcp.
	Accounts []GmailAccountDTO `json:"accounts"`
}

// GmailAccountDTO is API.md's GmailAccount, used by calendar connectors too.
type GmailAccountDTO struct {
	ID      string  `json:"id"`
	Email   string  `json:"email"`
	Status  string  `json:"status"`
	Error   *string `json:"error"`
	Primary bool    `json:"primary"`
}

// ConnectorResponse maps a connector onto its DTO.
func ConnectorResponse(c *models.Connector) ConnectorDTO {
	accounts := make([]GmailAccountDTO, 0, len(c.Accounts))
	for _, a := range c.Accounts {
		accounts = append(accounts, GmailAccountDTO{ID: a.ID.String(), Email: a.Email, Status: a.Status, Error: a.Error, Primary: a.Primary})
	}
	return ConnectorDTO{Name: c.Name, Kind: c.Kind, URL: c.URL, Status: c.Status, Error: c.Error, Tools: c.Tools, Account: c.Account, Accounts: accounts}
}

// ConnectorsResponse maps connectors onto DTOs (never null).
func ConnectorsResponse(cs []*models.Connector) []ConnectorDTO {
	out := make([]ConnectorDTO, 0, len(cs))
	for _, c := range cs {
		out = append(out, ConnectorResponse(c))
	}
	return out
}
