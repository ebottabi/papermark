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
	"strings"
	"testing"
)

func zipOf(t *testing.T, parts map[string]string) []byte {
	t.Helper()
	var buf bytes.Buffer
	w := zip.NewWriter(&buf)
	for name, body := range parts {
		f, err := w.Create(name)
		if err != nil {
			t.Fatal(err)
		}
		f.Write([]byte(body))
	}
	if err := w.Close(); err != nil {
		t.Fatal(err)
	}
	return buf.Bytes()
}

func testXLSX(t *testing.T) []byte {
	return zipOf(t, map[string]string{
		"xl/workbook.xml": `<workbook xmlns:r="http://schemas.openxmlformats.org/officeDocument/2006/relationships"><sheets>
<sheet name="Sales" sheetId="1" r:id="rId1"/><sheet name="Old" sheetId="2" state="hidden" r:id="rId2"/></sheets></workbook>`,
		"xl/_rels/workbook.xml.rels": `<Relationships><Relationship Id="rId1" Target="worksheets/sheet1.xml"/><Relationship Id="rId2" Target="/xl/worksheets/sheet2.xml"/></Relationships>`,
		"xl/sharedStrings.xml":       `<sst><si><t>Name</t></si><si><r><t>Ac</t></r><r><t>me</t></r></si></sst>`,
		"xl/styles.xml":              `<styleSheet><cellXfs><xf numFmtId="0"/><xf numFmtId="14"/></cellXfs></styleSheet>`,
		"xl/worksheets/sheet1.xml": `<worksheet><sheetData>
<row r="1"><c r="A1" t="s"><v>0</v></c><c r="B1" t="inlineStr"><is><t>Due</t></is></c><c r="C1" t="s"><v>0</v></c></row>
<row r="2"><c r="A2" t="s"><v>1</v></c><c r="B2" s="1"><v>45292</v></c><c r="C2" t="b"><v>1</v></c></row>
<row r="3"><c r="A3"><f>1+2</f><v>3</v></c></row>
<row r="4"><c r="A4" t="s"><v>1</v></c></row></sheetData><mergeCells><mergeCell ref="A4:B4"/></mergeCells></worksheet>`,
		"xl/worksheets/sheet2.xml": `<worksheet><sheetData><row r="1"><c r="A1"><v>7</v></c></row></sheetData></worksheet>`,
	})
}

func TestXLSXOverview(t *testing.T) {
	got, err := XLSXOverview(testXLSX(t))
	if err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{`"Sales": 4 rows x 3 cols (has formulas)`, `header: Name,Due,Name`, `"Old": 1 rows x 1 cols (hidden)`} {
		if !strings.Contains(got, want) {
			t.Errorf("missing %q in\n%s", want, got)
		}
	}
}

func TestXLSXSheet(t *testing.T) {
	got, err := XLSXSheet(testXLSX(t), "1", "2-4")
	if err != nil {
		t.Fatal(err)
	}
	want := "2: Acme,2024-01-01,TRUE\n3: 3\n4: Acme,Acme"
	if !strings.HasSuffix(got, want) {
		t.Errorf("got\n%s\nwant suffix\n%s", got, want)
	}
	if _, err := XLSXSheet(testXLSX(t), "nope", ""); err == nil {
		t.Error("unknown sheet should fail")
	}
	if _, err := XLSXOverview([]byte("\xD0\xCF\x11\xE0 old")); err == nil || !strings.Contains(err.Error(), ".xls") {
		t.Errorf("xls: %v", err)
	}
}

func TestCSVRagged(t *testing.T) {
	got, err := CSV([]byte("a,b\n1\n2,3,4\n"), ',', "2-3")
	if err != nil {
		t.Fatal(err)
	}
	if want := "3 rows (row 1 is the header)\n2: 1\n3: 2,3,4"; got != want {
		t.Errorf("got %q want %q", got, want)
	}
}

func TestDocxText(t *testing.T) {
	data := zipOf(t, map[string]string{"word/document.xml": `<w:document xmlns:w="x"><w:body><w:p><w:r><w:t>Hello</w:t></w:r></w:p><w:p><w:r><w:t>World</w:t></w:r></w:p></w:body></w:document>`})
	got, err := DocxText(data)
	if err != nil || got != "Hello\nWorld" {
		t.Errorf("got %q, %v", got, err)
	}
}
