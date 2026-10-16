/*
 * Copyright (c) 2026. Papermark - All Rights Reserved
 * Unauthorized copying of this file, via any medium is strictly prohibited
 * Proprietary and confidential
 * Written by Ebot Tabi <ebot.tabi@gmail.com>, 15/10/2026
 */

package models

// Device is an APNs device token that receives push alerts. A token is
// unique among live rows (partial index in PostMigrations), so registering
// the same token again updates it in place.
type Device struct {
	Base
	Token       string `gorm:"type:varchar(200);not null" json:"token"`
	Platform    string `gorm:"type:varchar(16);not null;default:'ios'" json:"platform"`
	Environment string `gorm:"type:varchar(16);not null" json:"environment"`
}

// TableName for GORM.
func (Device) TableName() string { return "devices" }
