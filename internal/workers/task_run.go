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

	"papermark/internal/models"
	"papermark/internal/services"
)

// taskRun runs a task's agent loop (job type services.RunTaskJob).
type taskRun struct{ agent *services.AgentService }

func (taskRun) Type() string { return services.RunTaskJob }

func (h taskRun) Handle(ctx context.Context, job *models.Job) error {
	var p services.RunTaskPayload
	if err := json.Unmarshal(job.Payload, &p); err != nil {
		return err
	}
	return h.agent.Run(ctx, p.TaskID)
}
