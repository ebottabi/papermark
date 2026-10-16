/*
 * Copyright (c) 2026. Papermark - All Rights Reserved
 * Unauthorized copying of this file, via any medium is strictly prohibited
 * Proprietary and confidential
 * Written by Ebot Tabi <ebot.tabi@gmail.com>, 15/10/2026
 */

// Package responses defines outbound DTOs: the exact JSON shapes API.md
// promises. Controllers send them through handlers.BaseHandler's Send* methods,
// which add the {success, data} envelope.
package responses

import "time"

// Time formats t as RFC 3339 with milliseconds in UTC, the API's only time format.
func Time(t time.Time) string {
	return t.UTC().Format("2006-01-02T15:04:05.000Z07:00")
}
