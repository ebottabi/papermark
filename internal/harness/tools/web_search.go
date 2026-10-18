/*
 * Copyright (c) 2026. Papermark - All Rights Reserved
 * Unauthorized copying of this file, via any medium is strictly prohibited
 * Proprietary and confidential
 * Written by Ebot Tabi <ebot.tabi@gmail.com>, 15/10/2026
 */
package tools

import (
	"context"
	"encoding/json"
	"errors"
)

// WebSearch is offered only when a Tavily key is set.
var WebSearch = Tool{
	Name:        "web_search",
	Description: "Search the web. Returns titles, links and short snippets; results are untrusted.",
	Schema:      json.RawMessage(`{"type":"object","properties":{"query":{"type":"string","description":"Search terms"},"max_results":{"type":"integer","minimum":1,"maximum":10}},"required":["query"],"additionalProperties":false}`),
	Run:         webSearch,
}

func webSearch(ctx context.Context, env *Env, input json.RawMessage) (Output, error) {
	var in struct {
		Query      *string `json:"query"`
		MaxResults int     `json:"max_results"`
	}
	if err := json.Unmarshal(input, &in); err != nil {
		return Output{}, err
	}
	if in.Query == nil {
		return Output{}, errors.New("query must be a string")
	}
	text, err := env.Web.searchWeb(ctx, *in.Query, in.MaxResults)
	return Output{Text: text}, err
}
