/*
 * Copyright (c) 2026. Papermark - All Rights Reserved
 * Unauthorized copying of this file, via any medium is strictly prohibited
 * Proprietary and confidential
 * Written by Ebot Tabi <ebot.tabi@gmail.com>, 15/10/2026
 */

package utils

import "github.com/go-playground/validator/v10"

// validate is a shared, reusable validator instance (it caches struct metadata).
var validate = validator.New()

// ValidateStruct validates a struct using its `validate:"..."` tags and returns
// the first error encountered (or nil when valid).
//
// TODO: map validator.ValidationErrors to friendly, field-level messages for API
// responses instead of returning the raw error.
func ValidateStruct(s interface{}) error {
	return validate.Struct(s)
}
