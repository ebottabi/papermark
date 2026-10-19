/*
 * Copyright (c) 2026. Papermark - All Rights Reserved
 * Unauthorized copying of this file, via any medium is strictly prohibited
 * Proprietary and confidential
 * Written by Ebot Tabi <ebot.tabi@gmail.com>, 18/10/2026
 */
package docs

import (
	"bytes"
	"encoding/csv"
	"fmt"
	"strconv"
	"strings"
)

const defaultRows = 200

// ParseRange reads "5" or "1-200" (1-based, inclusive); empty is the first
// defaultRows rows.
func ParseRange(s string) (from, to int, err error) {
	s = strings.TrimSpace(s)
	if s == "" {
		return 1, defaultRows, nil
	}
	a, b, found := strings.Cut(s, "-")
	if from, err = strconv.Atoi(strings.TrimSpace(a)); err != nil || from < 1 {
		return 0, 0, fmt.Errorf("rows must look like 1-200, got %q", s)
	}
	if !found {
		return from, from, nil
	}
	if to, err = strconv.Atoi(strings.TrimSpace(b)); err != nil || to < from {
		return 0, 0, fmt.Errorf("rows must look like 1-200, got %q", s)
	}
	return from, to, nil
}

// numbered renders rows[from-1:to] as CSV lines prefixed with row numbers.
func numbered(rows [][]string, rng string) (string, error) {
	from, to, err := ParseRange(rng)
	if err != nil {
		return "", err
	}
	var b strings.Builder
	for i := from; i <= to && i <= len(rows); i++ {
		row := rows[i-1]
		for len(row) > 0 && row[len(row)-1] == "" {
			row = row[:len(row)-1]
		}
		var line bytes.Buffer
		w := csv.NewWriter(&line)
		_ = w.Write(row)
		w.Flush()
		fmt.Fprintf(&b, "%d: %s", i, line.String())
	}
	if from > len(rows) {
		fmt.Fprintf(&b, "no rows in %d-%d\n", from, to)
	} else if to < len(rows) {
		fmt.Fprintf(&b, "[%d more rows; pass rows=\"%d-%d\" for the next page]\n", len(rows)-to, to+1, to+defaultRows)
	}
	return strings.TrimRight(b.String(), "\n"), nil
}

// CSV renders delimited text: the row count, then numbered rows. Ragged rows
// and stray quotes are tolerated.
func CSV(data []byte, delim rune, rng string) (string, error) {
	data = bytes.TrimPrefix(data, []byte("\xef\xbb\xbf"))
	r := csv.NewReader(bytes.NewReader(data))
	r.Comma = delim
	r.FieldsPerRecord = -1
	r.LazyQuotes = true
	rows, err := r.ReadAll()
	if err != nil && len(rows) == 0 {
		return "", fmt.Errorf("unreadable CSV: %w", err)
	}
	out, err := numbered(rows, rng)
	if err != nil {
		return "", err
	}
	return fmt.Sprintf("%d rows (row 1 is the header)\n%s", len(rows), out), nil
}
