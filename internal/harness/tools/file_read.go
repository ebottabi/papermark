/*
 * Copyright (c) 2026. Papermark - All Rights Reserved
 * Unauthorized copying of this file, via any medium is strictly prohibited
 * Proprietary and confidential
 * Written by Ebot Tabi <ebot.tabi@gmail.com>, 15/10/2026
 */
package tools

import (
	"archive/zip"
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"os"
	"path/filepath"
	"strings"

	"papermark/internal/docs"
)

// FileRead reads a workspace file as text, whatever its type.
var FileRead = Tool{
	Name:        "file_read",
	Description: "Read a workspace file as text. Spreadsheets: the first call gives the sheet list; pass sheet and rows to page. CSV: rows pages. PDF and Word give text (pages for PDF). Images, audio and video are described or transcribed (question for images, at for video).",
	Schema:      json.RawMessage(`{"type":"object","properties":{"path":{"type":"string","description":"Path relative to the workspace"},"sheet":{"type":"string","description":"Spreadsheet sheet name or 1-based number"},"rows":{"type":"string","description":"Row range for spreadsheets and CSV, like 1-200 (default first 200)"},"pages":{"type":"string","description":"PDF page range, like 1-5"},"question":{"type":"string","description":"For images: what to look for"},"at":{"type":"string","description":"For video: a time as MM:SS to describe that frame"}},"required":["path"],"additionalProperties":false}`),
	Run:         fileRead,
}

func fileRead(ctx context.Context, env *Env, input json.RawMessage) (Output, error) {
	var in struct {
		Path     *string `json:"path"`
		Sheet    string  `json:"sheet"`
		Rows     string  `json:"rows"`
		Pages    string  `json:"pages"`
		Question string  `json:"question"`
		At       string  `json:"at"`
	}
	if err := json.Unmarshal(input, &in); err != nil {
		return Output{}, err
	}
	if in.Path == nil {
		return Output{}, errors.New("path must be a string")
	}
	full, err := env.inside(*in.Path)
	if err != nil {
		return Output{}, err
	}
	raw, err := os.ReadFile(full)
	if err != nil {
		return Output{}, err
	}
	kind := sniff(raw, full)
	var text string
	switch kind {
	case "image", "audio", "video":
		if env.Media == nil {
			return Output{}, errors.New("reading images, audio and video is not set up on this server")
		}
		switch kind {
		case "image":
			text, err = env.Media.Image(ctx, full, in.Question)
		case "audio":
			text, err = env.Media.Audio(ctx, full)
		default:
			text, err = env.Media.Video(ctx, full, in.At)
		}
	case "csv", "tsv":
		delim := ','
		if kind == "tsv" {
			delim = '\t'
		}
		text, err = docs.CSV(raw, delim, in.Rows)
	case "xlsx":
		if in.Sheet == "" {
			text, err = docs.XLSXOverview(raw)
		} else {
			text, err = docs.XLSXSheet(raw, in.Sheet, in.Rows)
		}
	case "xls":
		err = errors.New("old .xls files are not supported; save it as .xlsx")
	case "pdf":
		from, to := 1, 0
		if in.Pages != "" {
			if from, to, err = docs.ParseRange(in.Pages); err != nil {
				return Output{}, errors.New("pages must look like 1-5")
			}
		}
		text, err = docs.PDFPages(raw, maxRead*2, from, to)
	case "docx":
		text, err = docs.DocxText(raw)
	case "binary":
		return Output{Text: fmt.Sprintf("%s: %s, %d bytes. This file type can't be read as text.", filepath.Base(full), http.DetectContentType(raw), len(raw))}, nil
	default:
		text = string(raw)
	}
	if err != nil {
		return Output{}, err
	}
	if n := jsLen(text); n > maxRead {
		text = fmt.Sprintf("%s\n[truncated: %d chars total, file kept at %s]", truncateRunes(text, maxRead), n, *in.Path)
	}
	return Output{Text: text}, nil
}

// sniff names how to read a file: by content first, then by extension.
func sniff(raw []byte, name string) string {
	ext := strings.ToLower(filepath.Ext(name))
	ct := http.DetectContentType(raw)
	switch {
	case bytes.HasPrefix(raw, []byte("%PDF-")):
		return "pdf"
	case bytes.HasPrefix(raw, []byte{0xD0, 0xCF, 0x11, 0xE0}):
		return "xls"
	case bytes.HasPrefix(raw, []byte("PK\x03\x04")):
		if z, err := zip.NewReader(bytes.NewReader(raw), int64(len(raw))); err == nil {
			for _, f := range z.File {
				switch f.Name {
				case "xl/workbook.xml":
					return "xlsx"
				case "word/document.xml":
					return "docx"
				}
			}
		}
		return "binary"
	case strings.HasPrefix(ct, "image/") && ct != "image/svg+xml":
		return "image"
	case strings.HasPrefix(ct, "audio/"):
		return "audio"
	case strings.HasPrefix(ct, "video/") && ext != ".m4a":
		return "video"
	}
	switch ext {
	case ".heic", ".webp", ".png", ".jpg", ".jpeg", ".gif":
		return "image"
	case ".mp3", ".m4a", ".wav", ".aac", ".ogg", ".flac":
		return "audio"
	case ".mp4", ".mov", ".m4v", ".webm":
		return "video"
	}
	if bytes.IndexByte(raw[:min(len(raw), 8000)], 0) >= 0 {
		return "binary"
	}
	switch ext {
	case ".csv":
		return "csv"
	case ".tsv":
		return "tsv"
	}
	return "text"
}
