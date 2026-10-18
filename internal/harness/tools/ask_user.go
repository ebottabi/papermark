/*
 * Copyright (c) 2026. Papermark - All Rights Reserved
 * Unauthorized copying of this file, via any medium is strictly prohibited
 * Proprietary and confidential
 * Written by Ebot Tabi <ebot.tabi@gmail.com>, 15/10/2026
 */
package tools

import "encoding/json"

// AskUser asks a question and parks the task; the agent loop handles the
// call, and the user's reply is its result.
var AskUser = Tool{
	Name:        "message_ask_user",
	Description: "Ask the user a question and wait for their reply. Pauses the task.",
	Schema:      json.RawMessage(`{"type":"object","properties":{"text":{"type":"string","description":"Question"}},"required":["text"],"additionalProperties":false}`),
}
