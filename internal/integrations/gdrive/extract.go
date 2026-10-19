/*
 * Copyright (c) 2026. Papermark - All Rights Reserved
 * Unauthorized copying of this file, via any medium is strictly prohibited
 * Proprietary and confidential
 * Written by Ebot Tabi <ebot.tabi@gmail.com>, 17/10/2026
 */
package gdrive

import (
	"fmt"
	"net/url"
	"regexp"
	"strings"
)

var idPattern = regexp.MustCompile(`^[A-Za-z0-9_-]{10,200}$`)

// FileID returns the Drive file id in s: a bare id or a Drive, Docs, Sheets
// or Slides link (/d/<id>, /folders/<id>, ?id=<id>).
func FileID(s string) (string, error) {
	s = strings.TrimSpace(s)
	if idPattern.MatchString(s) {
		return s, nil
	}
	u, err := url.Parse(s)
	if err != nil || u.Host == "" {
		return "", fmt.Errorf("%q is not a Drive file id or link", s)
	}
	host := strings.ToLower(u.Hostname())
	if host != "drive.google.com" && host != "docs.google.com" && host != "sheets.google.com" && host != "slides.google.com" {
		return "", fmt.Errorf("%q is not a Google Drive, Docs, Sheets or Slides link", s)
	}
	if id := u.Query().Get("id"); idPattern.MatchString(id) {
		return id, nil
	}
	parts := strings.Split(strings.Trim(u.Path, "/"), "/")
	for i := 0; i+1 < len(parts); i++ {
		if (parts[i] == "d" || parts[i] == "folders") && idPattern.MatchString(parts[i+1]) {
			return parts[i+1], nil
		}
	}
	return "", fmt.Errorf("no file id in %q", s)
}
