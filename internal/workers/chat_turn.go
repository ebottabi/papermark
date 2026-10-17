/*
 * Copyright (c) 2026. Papermark - All Rights Reserved
 * Unauthorized copying of this file, via any medium is strictly prohibited
 * Proprietary and confidential
 * Written by Ebot Tabi <ebot.tabi@gmail.com>, 15/10/2026
 */
package workers

import (
	"context"
	"encoding/json"

	"papermark/internal/harness"
	"papermark/internal/models"
)

// chatTurn runs a task's chat turn (job type harness.TurnJob).
type chatTurn struct{ h *harness.Harness }

func (chatTurn) Type() string { return harness.TurnJob }

func (c chatTurn) Handle(ctx context.Context, job *models.Job) error {
	var p harness.TurnPayload
	if err := json.Unmarshal(job.Payload, &p); err != nil {
		return err
	}
	return c.h.ChatTurn(ctx, p.TaskID)
}
