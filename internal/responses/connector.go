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
}

// ConnectorResponse maps a connector onto its DTO.
func ConnectorResponse(c *models.Connector) ConnectorDTO {
	return ConnectorDTO{Name: c.Name, Kind: c.Kind, URL: c.URL, Status: c.Status, Error: c.Error, Tools: c.Tools, Account: c.Account}
}

// ConnectorsResponse maps connectors onto DTOs (never null).
func ConnectorsResponse(cs []*models.Connector) []ConnectorDTO {
	out := make([]ConnectorDTO, 0, len(cs))
	for _, c := range cs {
		out = append(out, ConnectorResponse(c))
	}
	return out
}
