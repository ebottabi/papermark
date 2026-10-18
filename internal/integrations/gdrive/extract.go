/*
 * Copyright (c) 2026. Papermark - All Rights Reserved
 * Unauthorized copying of this file, via any medium is strictly prohibited
 * Proprietary and confidential
 * Written by Ebot Tabi <ebot.tabi@gmail.com>, 17/10/2026
 */
package gdrive

import (
	"archive/zip"
	"bytes"
	"encoding/xml"
	"errors"
	"fmt"
	"io"
	"net/url"
	"regexp"
	"strings"

	"github.com/ledongthuc/pdf"
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

// pdfText extracts the text of a PDF, page by page, stopping once it has
// more than limit bytes. The parser panics on some malformed files, so a
// panic becomes an error.
func pdfText(data []byte, limit int) (text string, err error) {
	defer func() {
		if r := recover(); r != nil {
			err = fmt.Errorf("unreadable PDF: %v", r)
		}
	}()
	r, err := pdf.NewReader(bytes.NewReader(data), int64(len(data)))
	if err != nil {
		return "", fmt.Errorf("unreadable PDF: %w", err)
	}
	var b strings.Builder
	fonts := map[string]*pdf.Font{}
	for i := 1; i <= r.NumPage() && b.Len() <= limit; i++ {
		p := r.Page(i)
		if p.V.IsNull() {
			continue
		}
		for _, name := range p.Fonts() {
			if _, ok := fonts[name]; !ok {
				f := p.Font(name)
				fonts[name] = &f
			}
		}
		t, err := p.GetPlainText(fonts)
		if err != nil {
			return "", fmt.Errorf("unreadable PDF page %d: %w", i, err)
		}
		if b.Len() > 0 {
			b.WriteString("\n\n")
		}
		b.WriteString(strings.TrimSpace(t))
	}
	return b.String(), nil
}

const maxDocXML = 64 << 20 // decompressed word/document.xml

// docxText extracts the paragraphs of a Word document's body.
func docxText(data []byte) (string, error) {
	z, err := zip.NewReader(bytes.NewReader(data), int64(len(data)))
	if err != nil {
		return "", fmt.Errorf("unreadable Word document: %w", err)
	}
	for _, f := range z.File {
		if f.Name != "word/document.xml" {
			continue
		}
		rc, err := f.Open()
		if err != nil {
			return "", err
		}
		defer rc.Close()
		return wordXML(io.LimitReader(rc, maxDocXML))
	}
	return "", errors.New("unreadable Word document: no word/document.xml")
}

// wordXML collects the w:t runs of WordprocessingML: a paragraph ends a
// line, w:tab is a tab and w:br a line break.
func wordXML(r io.Reader) (string, error) {
	d := xml.NewDecoder(r)
	var b strings.Builder
	inText := false
	for {
		tok, err := d.Token()
		if err == io.EOF {
			break
		}
		if err != nil {
			return "", fmt.Errorf("unreadable Word document: %w", err)
		}
		switch t := tok.(type) {
		case xml.StartElement:
			switch t.Name.Local {
			case "t":
				inText = true
			case "tab":
				b.WriteByte('\t')
			case "br", "cr":
				b.WriteByte('\n')
			}
		case xml.EndElement:
			switch t.Name.Local {
			case "t":
				inText = false
			case "p":
				b.WriteByte('\n')
			}
		case xml.CharData:
			if inText {
				b.Write(t)
			}
		}
	}
	return strings.TrimSpace(b.String()), nil
}
