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

// NotifyUser sends a progress message without waiting for a reply.
var NotifyUser = Tool{
	Name:        "message_notify_user",
	Description: "Send the user a progress message without waiting for a reply.",
	Schema:      json.RawMessage(`{"type":"object","properties":{"text":{"type":"string","description":"Message"}},"required":["text"],"additionalProperties":false}`),
	Run:         notifyUser,
}

func notifyUser(ctx context.Context, env *Env, input json.RawMessage) (Output, error) {
	var in struct {
		Text *string `json:"text"`
	}
	if err := json.Unmarshal(input, &in); err != nil {
		return Output{}, err
	}
	if in.Text == nil {
		return Output{}, errors.New("text must be a string")
	}
	if err := env.Notify(ctx, *in.Text); err != nil {
		return Output{}, err
	}
	return Output{Text: "delivered"}, nil
}
