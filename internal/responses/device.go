/*
 * Copyright (c) 2026. Papermark - All Rights Reserved
 * Unauthorized copying of this file, via any medium is strictly prohibited
 * Proprietary and confidential
 * Written by Ebot Tabi <ebot.tabi@gmail.com>, 15/10/2026
 */

package responses

import "papermark/internal/models"

// DeviceDTO is a registered push device.
type DeviceDTO struct {
	Token       string `json:"token"`
	Platform    string `json:"platform"`
	Environment string `json:"environment"`
	CreatedAt   string `json:"createdAt"`
}

// DeviceResponse maps a device onto its DTO.
func DeviceResponse(d *models.Device) DeviceDTO {
	return DeviceDTO{Token: d.Token, Platform: d.Platform, Environment: d.Environment, CreatedAt: Time(d.CreatedAt)}
}
