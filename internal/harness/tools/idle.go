/*
 * Copyright (c) 2026. Papermark - All Rights Reserved
 * Unauthorized copying of this file, via any medium is strictly prohibited
 * Proprietary and confidential
 * Written by Ebot Tabi <ebot.tabi@gmail.com>, 15/10/2026
 */
package tools

import "encoding/json"

// Idle ends the task; the agent loop handles the call and takes its input
// as the task's result.
var Idle = Tool{
	Name:        "idle",
	Description: "Call when all work is complete. Ends the task.",
	Schema:      json.RawMessage(`{"type":"object","properties":{"result":{"type":"string","description":"Summary of the result"},"files":{"type":"array","items":{"type":"string"},"description":"Workspace paths of the deliverable files"}},"required":["result"],"additionalProperties":false}`),
}
