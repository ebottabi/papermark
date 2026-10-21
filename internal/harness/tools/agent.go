/*
 * Copyright (c) 2026. Papermark - All Rights Reserved
 * Unauthorized copying of this file, via any medium is strictly prohibited
 * Proprietary and confidential
 * Written by Ebot Tabi <ebot.tabi@gmail.com>, 21/10/2026
 */
package tools

import "encoding/json"

// MaxAgents caps how many sub-agents one agent call may spawn.
const MaxAgents = 4

// Agent spawns parallel sub-agents, one per spec, and waits for them all. The
// agent loop handles it (Run is nil): it spawns the children, parks the turn,
// and resumes with their combined results once every child has finished. A
// sub-agent cannot spawn its own, so the tool is offered only on a top-level task.
var Agent = Tool{
	Name:        "agent",
	Description: "Run independent sub-tasks in parallel as sub-agents, then continue once they all finish. Use it to fan work out, e.g. research several things at once or draft several items, when the pieces do not depend on each other. Each sub-agent has its own fresh workspace and returns text only; it cannot ask you a question, run the shell, or use connectors, so give it a self-contained prompt. Returns every sub-agent's result. Up to " + agentsMax + " at once.",
	Schema:      json.RawMessage(`{"type":"object","properties":{"agents":{"type":"array","minItems":1,"maxItems":4,"items":{"type":"object","properties":{"title":{"type":"string","description":"A short label for this sub-agent, shown to the user"},"prompt":{"type":"string","description":"The self-contained task for this sub-agent"},"mode":{"type":"string","enum":["speed","quality"],"description":"speed is faster and cheaper; quality reasons harder"}},"required":["title","prompt"],"additionalProperties":false}}},"required":["agents"],"additionalProperties":false}`),
}

const agentsMax = "4"

// AgentSpec is one sub-agent an agent call asks to spawn.
type AgentSpec struct {
	Title  string `json:"title"`
	Prompt string `json:"prompt"`
	Mode   string `json:"mode"`
}

// ParseAgents reads an agent call's input into its sub-agent specs.
func ParseAgents(input json.RawMessage) ([]AgentSpec, error) {
	var in struct {
		Agents []AgentSpec `json:"agents"`
	}
	if err := json.Unmarshal(input, &in); err != nil {
		return nil, err
	}
	return in.Agents, nil
}
