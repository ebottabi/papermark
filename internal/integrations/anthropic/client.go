/*
 * Copyright (c) 2026. Papermark - All Rights Reserved
 * Unauthorized copying of this file, via any medium is strictly prohibited
 * Proprietary and confidential
 * Written by Ebot Tabi <ebot.tabi@gmail.com>, 15/10/2026
 */
// Package anthropic streams Messages API calls through the official SDK while
// keeping messages, tools and response content as raw JSON, so the agent
// resends exactly the bytes the API returned.
package anthropic

import (
	"bytes"
	"context"
	"encoding/json"

	sdk "github.com/anthropics/anthropic-sdk-go"
	"github.com/anthropics/anthropic-sdk-go/option"
)

// Client is a Messages API client.
type Client struct{ c sdk.Client }

// New builds a client. An empty baseURL uses the SDK default (or ANTHROPIC_BASE_URL).
func New(apiKey, baseURL string) *Client {
	opts := []option.RequestOption{option.WithAPIKey(apiKey)}
	if baseURL != "" {
		opts = append(opts, option.WithBaseURL(baseURL))
	}
	return &Client{c: sdk.NewClient(opts...)}
}

// Request is one model call. Tools and each message are raw JSON.
type Request struct {
	Model     string
	MaxTokens int64
	System    string
	Tools     json.RawMessage
	Messages  []json.RawMessage
	Effort    string // low | high
}

// Usage is one response's token counts.
type Usage struct {
	InputTokens, OutputTokens, CacheReadTokens, CacheWriteTokens int64
}

// Response is the finished assistant message.
type Response struct {
	Content    json.RawMessage
	StopReason string
	Usage      Usage
}

// OnEvent sees every stream event with the message accumulated so far.
type OnEvent func(ev sdk.BetaRawMessageStreamEventUnion, snapshot *sdk.BetaMessage)

// Stream sends req with streaming on and returns the accumulated message.
func (c *Client) Stream(ctx context.Context, req Request, onEvent OnEvent) (*Response, error) {
	messages := append([]byte("["), bytes.Join(asBytes(req.Messages), []byte(","))...)
	messages = append(messages, ']')
	params := sdk.BetaMessageNewParams{
		Model:     sdk.Model(req.Model),
		MaxTokens: req.MaxTokens,
		Betas:     []sdk.AnthropicBeta{"server-side-fallback-2026-07-01"},
	}
	stream := c.c.Beta.Messages.NewStreaming(ctx, params,
		option.WithJSONSet("system", req.System),
		option.WithJSONSet("tools", req.Tools),
		option.WithJSONSet("messages", json.RawMessage(messages)),
		option.WithJSONSet("thinking", map[string]string{"type": "adaptive"}),
		option.WithJSONSet("output_config", map[string]string{"effort": req.Effort}),
		option.WithJSONSet("cache_control", map[string]string{"type": "ephemeral"}),
		option.WithJSONSet("fallbacks", "default"),
	)
	defer stream.Close()
	var msg sdk.BetaMessage
	for stream.Next() {
		ev := stream.Current()
		if err := msg.Accumulate(ev); err != nil {
			return nil, err
		}
		if onEvent != nil {
			onEvent(ev, &msg)
		}
	}
	if err := stream.Err(); err != nil {
		return nil, err
	}
	var raw struct {
		Content json.RawMessage `json:"content"`
	}
	if err := json.Unmarshal([]byte(msg.RawJSON()), &raw); err != nil {
		return nil, err
	}
	return &Response{
		Content:    raw.Content,
		StopReason: string(msg.StopReason),
		Usage: Usage{
			InputTokens:      msg.Usage.InputTokens,
			OutputTokens:     msg.Usage.OutputTokens,
			CacheReadTokens:  msg.Usage.CacheReadInputTokens,
			CacheWriteTokens: msg.Usage.CacheCreationInputTokens,
		},
	}, nil
}

func asBytes(ms []json.RawMessage) [][]byte {
	out := make([][]byte, len(ms))
	for i, m := range ms {
		out[i] = m
	}
	return out
}
