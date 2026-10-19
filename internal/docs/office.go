/*
 * Copyright (c) 2026. Papermark - All Rights Reserved
 * Unauthorized copying of this file, via any medium is strictly prohibited
 * Proprietary and confidential
 * Written by Ebot Tabi <ebot.tabi@gmail.com>, 18/10/2026
 */
package docs

import (
	"archive/zip"
	"bytes"
	"encoding/xml"
	"errors"
	"fmt"
	"io"
	"strings"

	"github.com/ledongthuc/pdf"
)

// PDFText extracts the text of a PDF, page by page, stopping once it has
// more than limit bytes. The parser panics on some malformed files, so a
// panic becomes an error.
func PDFText(data []byte, limit int) (string, error) { return PDFPages(data, limit, 1, 0) }

// PDFPages is PDFText for pages from..to (to 0: through the last page).
func PDFPages(data []byte, limit, from, to int) (text string, err error) {
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
	if to <= 0 || to > r.NumPage() {
		to = r.NumPage()
	}
	for i := max(from, 1); i <= to && b.Len() <= limit; i++ {
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

// DocxText extracts the paragraphs of a Word document's body.
func DocxText(data []byte) (string, error) {
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
