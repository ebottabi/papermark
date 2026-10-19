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
	"path"
	"strconv"
	"strings"
	"time"
)

const (
	maxPart = 256 << 20 // decompressed bytes of one workbook part
	maxRow  = 1 << 20
	maxCol  = 1 << 14
)

var errXLS = errors.New("old .xls files are not supported; save it as .xlsx")

type sheet struct {
	name, part string
	hidden     bool
}

type grid struct {
	rows     [][]string
	cols     int
	formulas bool
}

type workbook struct {
	z       *zip.Reader
	sheets  []sheet
	shared  []string
	dateXfs map[int]bool
}

func part(z *zip.Reader, name string) (*xml.Decoder, io.Closer, error) {
	for _, f := range z.File {
		if f.Name == name {
			rc, err := f.Open()
			if err != nil {
				return nil, nil, err
			}
			return xml.NewDecoder(io.LimitReader(rc, maxPart)), rc, nil
		}
	}
	return nil, nil, fs404(name)
}

type fs404 string

func (e fs404) Error() string { return "missing " + string(e) }

// walk calls fn for every start element of a part; fn may consume children
// through d. A missing part is not an error when optional.
func walk(z *zip.Reader, name string, optional bool, fn func(d *xml.Decoder, s xml.StartElement) error) error {
	d, c, err := part(z, name)
	if err != nil {
		if _, miss := err.(fs404); miss && optional {
			return nil
		}
		return fmt.Errorf("unreadable workbook: %w", err)
	}
	defer c.Close()
	for {
		tok, err := d.Token()
		if err == io.EOF {
			return nil
		}
		if err != nil {
			return fmt.Errorf("unreadable workbook: %w", err)
		}
		if s, ok := tok.(xml.StartElement); ok {
			if err := fn(d, s); err != nil {
				return err
			}
		}
	}
}

func attr(s xml.StartElement, name string) string {
	for _, a := range s.Attr {
		if a.Name.Local == name {
			return a.Value
		}
	}
	return ""
}

// text collects the w:t-style text under the current element (direct: the
// element's own character data), skipping phonetic runs.
func text(d *xml.Decoder, direct bool) string {
	var b strings.Builder
	depth, in, skip := 1, direct, 0
	for depth > 0 {
		tok, err := d.Token()
		if err != nil {
			break
		}
		switch t := tok.(type) {
		case xml.StartElement:
			depth++
			if t.Name.Local == "rPh" {
				skip++
			}
			in = t.Name.Local == "t" && skip == 0
		case xml.EndElement:
			depth--
			if t.Name.Local == "rPh" {
				skip--
			}
			in = false
		case xml.CharData:
			if in {
				b.Write(t)
			}
		}
	}
	return b.String()
}

func openWorkbook(data []byte) (*workbook, error) {
	z, err := zip.NewReader(bytes.NewReader(data), int64(len(data)))
	if err != nil {
		if bytes.HasPrefix(data, []byte{0xD0, 0xCF, 0x11, 0xE0}) {
			return nil, errXLS
		}
		return nil, fmt.Errorf("unreadable workbook: %w", err)
	}
	w := &workbook{z: z, dateXfs: map[int]bool{}}
	rels := map[string]string{}
	err = walk(z, "xl/_rels/workbook.xml.rels", false, func(_ *xml.Decoder, s xml.StartElement) error {
		if s.Name.Local == "Relationship" {
			t := attr(s, "Target")
			if strings.HasPrefix(t, "/") {
				rels[attr(s, "Id")] = strings.TrimPrefix(t, "/")
			} else {
				rels[attr(s, "Id")] = path.Join("xl", t)
			}
		}
		return nil
	})
	if err != nil {
		return nil, err
	}
	err = walk(z, "xl/workbook.xml", false, func(_ *xml.Decoder, s xml.StartElement) error {
		if s.Name.Local == "sheet" {
			var rid string
			for _, a := range s.Attr {
				if a.Name.Local == "id" {
					rid = a.Value
				}
			}
			w.sheets = append(w.sheets, sheet{attr(s, "name"), rels[rid], attr(s, "state") != "" && attr(s, "state") != "visible"})
		}
		return nil
	})
	if err != nil {
		return nil, err
	}
	err = walk(z, "xl/sharedStrings.xml", true, func(d *xml.Decoder, s xml.StartElement) error {
		if s.Name.Local == "si" {
			w.shared = append(w.shared, text(d, false))
		}
		return nil
	})
	if err != nil {
		return nil, err
	}
	custom := map[int]bool{}
	var xfs []int
	inXfs := false
	err = walk(z, "xl/styles.xml", true, func(_ *xml.Decoder, s xml.StartElement) error {
		switch s.Name.Local {
		case "numFmt":
			id, _ := strconv.Atoi(attr(s, "numFmtId"))
			custom[id] = dateFormat(attr(s, "formatCode"))
		case "cellXfs":
			inXfs = true
		case "cellStyleXfs", "dxfs":
			inXfs = false
		case "xf":
			if inXfs {
				id, _ := strconv.Atoi(attr(s, "numFmtId"))
				xfs = append(xfs, id)
			}
		}
		return nil
	})
	if err != nil {
		return nil, err
	}
	for i, id := range xfs {
		w.dateXfs[i] = custom[id] || (id >= 14 && id <= 22) || (id >= 45 && id <= 47)
	}
	return w, nil
}

// dateFormat reports whether a number format code shows a date or time.
func dateFormat(code string) bool {
	var b strings.Builder
	quoted, bracket := false, false
	for _, r := range code {
		switch {
		case r == '"':
			quoted = !quoted
		case quoted:
		case r == '[':
			bracket = true
		case r == ']':
			bracket = false
		case !bracket:
			b.WriteRune(r)
		}
	}
	return strings.ContainsAny(strings.ToLower(b.String()), "ymdhs")
}

func serial(v string) string {
	f, err := strconv.ParseFloat(v, 64)
	if err != nil || f < 1 || f > 2958465 {
		return v
	}
	t := time.Date(1899, 12, 30, 0, 0, 0, 0, time.UTC).Add(time.Duration(f*86400+0.5) * time.Second)
	if f == float64(int(f)) {
		return t.Format("2006-01-02")
	}
	return t.Format("2006-01-02 15:04")
}

// cellRef parses "B12" into zero-based col and row; -1 when malformed.
func cellRef(ref string) (col, row int) {
	i := 0
	for ; i < len(ref) && ref[i] >= 'A' && ref[i] <= 'Z'; i++ {
		col = col*26 + int(ref[i]-'A') + 1
	}
	n, err := strconv.Atoi(ref[i:])
	if err != nil || i == 0 {
		return -1, -1
	}
	return col - 1, n - 1
}

func (w *workbook) read(s sheet) (*grid, error) {
	g := &grid{}
	var merges []string
	col, row := 0, -1
	err := walk(w.z, s.part, false, func(d *xml.Decoder, e xml.StartElement) error {
		switch e.Name.Local {
		case "row":
			row++
			col = 0
			if n, err := strconv.Atoi(attr(e, "r")); err == nil {
				row = n - 1
			}
		case "mergeCell":
			merges = append(merges, attr(e, "ref"))
		case "c":
			if c, r := cellRef(attr(e, "r")); c >= 0 {
				col, row = c, r
			}
			typ, xf := attr(e, "t"), 0
			xf, _ = strconv.Atoi(attr(e, "s"))
			var v string
			inline := ""
			for depth := 1; depth > 0; {
				tok, err := d.Token()
				if err != nil {
					return fmt.Errorf("unreadable workbook: %w", err)
				}
				switch t := tok.(type) {
				case xml.StartElement:
					switch t.Name.Local {
					case "v":
						v = text(d, true)
					case "is":
						inline = text(d, false)
					case "f":
						g.formulas = true
						depth++
					default:
						depth++
					}
				case xml.EndElement:
					depth--
				}
			}
			switch typ {
			case "s":
				if i, err := strconv.Atoi(v); err == nil && i >= 0 && i < len(w.shared) {
					v = w.shared[i]
				}
			case "inlineStr":
				v = inline
			case "b":
				v = map[string]string{"1": "TRUE", "0": "FALSE"}[v]
			case "", "n":
				if w.dateXfs[xf] {
					v = serial(v)
				}
			}
			if row >= 0 && row < maxRow && col < maxCol {
				for len(g.rows) <= row {
					g.rows = append(g.rows, nil)
				}
				for len(g.rows[row]) <= col {
					g.rows[row] = append(g.rows[row], "")
				}
				g.rows[row][col] = v
				g.cols = max(g.cols, col+1)
			}
			col++
		}
		return nil
	})
	if err != nil {
		return nil, err
	}
	for _, m := range merges {
		a, b, _ := strings.Cut(m, ":")
		c1, r1 := cellRef(a)
		c2, r2 := cellRef(b)
		if c1 < 0 || c2 < 0 || r1 >= len(g.rows) || (r2-r1+1)*(c2-c1+1) > 1<<16 {
			continue
		}
		var v string
		if c1 < len(g.rows[r1]) {
			v = g.rows[r1][c1]
		}
		for r := r1; r <= r2 && r < len(g.rows); r++ {
			for len(g.rows[r]) <= c2 {
				g.rows[r] = append(g.rows[r], "")
			}
			for c := c1; c <= c2; c++ {
				g.rows[r][c] = v
			}
		}
		g.cols = max(g.cols, c2+1)
	}
	return g, nil
}

// XLSXOverview lists a workbook's sheets with size, header row and markers.
func XLSXOverview(data []byte) (string, error) {
	w, err := openWorkbook(data)
	if err != nil {
		return "", err
	}
	var b strings.Builder
	fmt.Fprintf(&b, "Workbook with %d sheets. Pass sheet (name or number) and rows to read one.\n", len(w.sheets))
	for i, s := range w.sheets {
		g, err := w.read(s)
		if err != nil {
			return "", err
		}
		fmt.Fprintf(&b, "\n%d. %q: %d rows x %d cols", i+1, s.name, len(g.rows), g.cols)
		if s.hidden {
			b.WriteString(" (hidden)")
		}
		if g.formulas {
			b.WriteString(" (has formulas)")
		}
		for _, r := range g.rows {
			if line, _ := numbered([][]string{r}, "1"); strings.TrimSpace(strings.TrimPrefix(line, "1: ")) != "" {
				fmt.Fprintf(&b, "\n   header: %s", strings.TrimPrefix(line, "1: "))
				break
			}
		}
	}
	return b.String(), nil
}

// XLSXSheet renders one sheet, chosen by name or 1-based number, as numbered
// CSV rows.
func XLSXSheet(data []byte, name, rng string) (string, error) {
	w, err := openWorkbook(data)
	if err != nil {
		return "", err
	}
	idx := -1
	if n, err := strconv.Atoi(name); err == nil && n >= 1 && n <= len(w.sheets) {
		idx = n - 1
	}
	for i, s := range w.sheets {
		if strings.EqualFold(s.name, name) {
			idx = i
		}
	}
	if idx < 0 {
		return "", fmt.Errorf("no sheet %q; call without sheet to list them", name)
	}
	g, err := w.read(w.sheets[idx])
	if err != nil {
		return "", err
	}
	out, err := numbered(g.rows, rng)
	if err != nil {
		return "", err
	}
	return fmt.Sprintf("%q: %d rows x %d cols\n%s", w.sheets[idx].name, len(g.rows), g.cols, out), nil
}
