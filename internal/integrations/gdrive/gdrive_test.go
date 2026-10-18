/*
 * Copyright (c) 2026. Papermark - All Rights Reserved
 * Unauthorized copying of this file, via any medium is strictly prohibited
 * Proprietary and confidential
 * Written by Ebot Tabi <ebot.tabi@gmail.com>, 17/10/2026
 */
package gdrive

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// fakeDrive serves files by id: metadata on files/<id>, bytes on alt=media
// and export. It records the queries files.list received.
type fakeDrive struct {
	files   map[string]File
	media   map[string][]byte
	exports map[string][]byte // "<id> <mime>"
	queries []string
	status  int // forced status for every request, 0 for none
}

func (d *fakeDrive) client(t *testing.T) *Client {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if d.status != 0 {
			http.Error(w, `{"error":{"message":"forced"}}`, d.status)
			return
		}
		p := strings.TrimPrefix(r.URL.Path, "/drive/v3/")
		switch {
		case p == "about":
			json.NewEncoder(w).Encode(map[string]any{"user": map[string]string{"emailAddress": "me@x.com"}})
		case p == "files":
			d.queries = append(d.queries, r.URL.Query().Get("q"))
			var list []File
			for _, f := range d.files {
				list = append(list, f)
			}
			json.NewEncoder(w).Encode(map[string]any{"files": list})
		case strings.HasSuffix(p, "/export"):
			id := strings.TrimSuffix(strings.TrimPrefix(p, "files/"), "/export")
			b, ok := d.exports[id+" "+r.URL.Query().Get("mimeType")]
			if !ok {
				http.Error(w, `{"error":{"message":"bad export"}}`, 400)
				return
			}
			w.Write(b)
		case strings.HasPrefix(p, "files/"):
			id := strings.TrimPrefix(p, "files/")
			f, ok := d.files[id]
			if !ok {
				http.Error(w, `{"error":{"message":"File not found"}}`, 404)
				return
			}
			if r.URL.Query().Get("alt") == "media" {
				w.Write(d.media[id])
				return
			}
			json.NewEncoder(w).Encode(f)
		default:
			t.Errorf("unexpected %s", r.URL)
		}
	}))
	t.Cleanup(srv.Close)
	return &Client{HTTP: srv.Client(), BaseURL: srv.URL}
}

func tool(t *testing.T, tools []Tool, name string) Tool {
	t.Helper()
	for _, x := range tools {
		if x.Name == name {
			return x
		}
	}
	t.Fatalf("no tool %s", name)
	return Tool{}
}

func run(t *testing.T, tl Tool, ws, in string) map[string]any {
	t.Helper()
	out, err := tl.Run(context.Background(), ws, json.RawMessage(in))
	if err != nil {
		t.Fatalf("%s %s: %v", tl.Name, in, err)
	}
	var m map[string]any
	if err := json.Unmarshal([]byte(out), &m); err != nil {
		t.Fatal(err)
	}
	return m
}

func fixture(t *testing.T, name string) []byte {
	b, err := os.ReadFile(filepath.Join("testdata", name))
	if err != nil {
		t.Fatal(err)
	}
	return b
}

func TestFileID(t *testing.T) {
	const id = "1AbC_dEf-GhIjKlMnOp"
	for _, in := range []string{
		id,
		"https://docs.google.com/document/d/" + id + "/edit?usp=sharing",
		"https://docs.google.com/spreadsheets/d/" + id + "/edit#gid=0",
		"https://docs.google.com/presentation/d/" + id + "/edit",
		"https://docs.google.com/document/u/1/d/" + id + "/view",
		"https://drive.google.com/file/d/" + id + "/view?usp=drive_link",
		"https://drive.google.com/open?id=" + id,
		"https://drive.google.com/uc?export=download&id=" + id,
		"https://drive.google.com/drive/u/0/folders/" + id,
	} {
		got, err := FileID(in)
		if err != nil || got != id {
			t.Errorf("FileID(%q) = %q, %v", in, got, err)
		}
	}
	for _, in := range []string{"", "short", "https://evil.com/d/" + id, "https://docs.google.com/document/", "notes.pdf"} {
		if got, err := FileID(in); err == nil {
			t.Errorf("FileID(%q) = %q, want error", in, got)
		}
	}
}

func TestBuildQueryEscapesAndFilters(t *testing.T) {
	q, order, err := buildQuery(searchInput{Query: `Bob's \ plan`, Type: "sheet", SharedWithMe: true, ModifiedAfter: "2026-10-01", Owner: "a'b@x.com"})
	if err != nil {
		t.Fatal(err)
	}
	want := `trashed = false and (fullText contains 'Bob\'s \\ plan' or name contains 'Bob\'s \\ plan') and mimeType = '` + sheetMime +
		`' and sharedWithMe = true and modifiedTime > '2026-10-01T00:00:00Z' and 'a\'b@x.com' in owners`
	if q != want || order != "" {
		t.Fatalf("q = %s (order %q)", q, order)
	}
	if _, order, _ := buildQuery(searchInput{}); order != "modifiedTime desc" {
		t.Fatalf("order without text = %q", order)
	}
	if _, _, err := buildQuery(searchInput{Type: "video"}); err == nil {
		t.Fatal("unknown type accepted")
	}
	if _, _, err := buildQuery(searchInput{ModifiedAfter: "yesterday"}); err == nil {
		t.Fatal("bad date accepted")
	}
}

func TestReadEachType(t *testing.T) {
	d := &fakeDrive{
		files: map[string]File{
			"doc0000000": {ID: "doc0000000", Name: "Plan", MimeType: docMime},
			"sheet00000": {ID: "sheet00000", Name: "Budget", MimeType: sheetMime},
			"slides0000": {ID: "slides0000", Name: "Deck", MimeType: slidesMime},
			"pdf0000000": {ID: "pdf0000000", Name: "Report.pdf", MimeType: pdfMime},
			"docx000000": {ID: "docx000000", Name: "Memo.docx", MimeType: docxMime},
			"text000000": {ID: "text000000", Name: "notes.md", MimeType: "text/markdown"},
			"image00000": {ID: "image00000", Name: "cat.png", MimeType: "image/png"},
			"short00000": {ID: "short00000", Name: "Plan link", MimeType: shortcut, ShortcutDetails: &struct {
				TargetID       string `json:"targetId"`
				TargetMimeType string `json:"targetMimeType"`
			}{TargetID: "doc0000000"}},
		},
		media: map[string][]byte{"pdf0000000": fixture(t, "sample.pdf"), "docx000000": fixture(t, "sample.docx"), "text000000": []byte("# Notes\nhello")},
		exports: map[string][]byte{
			"doc0000000 text/plain": []byte("The plan text"),
			"sheet00000 text/csv":   []byte("a,b\n1,2"),
			"slides0000 text/plain": []byte("Slide one"),
		},
	}
	tools := Tools([]Account{{Email: "me@x.com", Primary: true, Client: d.client(t)}}, nil)
	read := tool(t, tools, "read")
	for id, want := range map[string]string{
		"doc0000000": "The plan text", "sheet00000": "a,b\n1,2", "slides0000": "Slide one", "short00000": "The plan text",
		"pdf0000000": "Revenue grew in Lisbon.", "docx000000": "Quarterly plan\nRevenue grew in Lisbon.", "text000000": "# Notes\nhello",
	} {
		m := run(t, read, "", `{"file":"`+id+`"}`)
		if !strings.Contains(m["text"].(string), want) || m["account"] != "me@x.com" {
			t.Errorf("read %s = %v", id, m)
		}
	}
	if m := run(t, read, "", `{"file":"https://docs.google.com/spreadsheets/d/sheet00000/edit"}`); !strings.Contains(m["note"].(string), "first sheet") {
		t.Errorf("sheet note missing: %v", m)
	}
	if _, err := read.Run(context.Background(), "", json.RawMessage(`{"file":"image00000"}`)); err == nil || !strings.Contains(err.Error(), "cannot read this type") {
		t.Errorf("image read: %v", err)
	}
}

func TestReadTruncates(t *testing.T) {
	d := &fakeDrive{files: map[string]File{"doc0000000": {ID: "doc0000000", MimeType: docMime}},
		exports: map[string][]byte{"doc0000000 text/plain": []byte(strings.Repeat("é", 1500))}}
	read := tool(t, Tools([]Account{{Email: "me@x.com", Client: d.client(t)}}, nil), "read")
	m := run(t, read, "", `{"file":"doc0000000","maxChars":1000}`)
	if len([]rune(m["text"].(string))) != 1000 || m["truncated"] != true || !strings.Contains(m["note"].(string), "1000 of 1500") {
		t.Fatalf("truncation: %v", m["note"])
	}
}

func TestDownloadPathSafety(t *testing.T) {
	d := &fakeDrive{
		files: map[string]File{
			"doc0000000": {ID: "doc0000000", Name: "../../Plan", MimeType: docMime},
			"pdf0000000": {ID: "pdf0000000", Name: ".hidden/../x.pdf", MimeType: pdfMime},
		},
		media:   map[string][]byte{"pdf0000000": []byte("%PDF")},
		exports: map[string][]byte{"doc0000000 application/pdf": []byte("pdf bytes"), "doc0000000 " + docxMime: []byte("docx bytes")},
	}
	dl := tool(t, Tools([]Account{{Email: "me@x.com", Client: d.client(t)}}, nil), "download")
	ws := t.TempDir()
	m := run(t, dl, ws, `{"file":"doc0000000"}`)
	if m["path"] != "drive/doc00000-_.._Plan.pdf" || m["mime"] != pdfMime || m["size"].(float64) != 9 {
		t.Fatalf("download: %v", m)
	}
	if b, err := os.ReadFile(filepath.Join(ws, m["path"].(string))); err != nil || string(b) != "pdf bytes" {
		t.Fatalf("saved file: %q %v", b, err)
	}
	if m := run(t, dl, ws, `{"file":"doc0000000","format":"docx"}`); !strings.HasSuffix(m["path"].(string), ".docx") {
		t.Fatalf("docx: %v", m)
	}
	if m := run(t, dl, ws, `{"file":"pdf0000000"}`); m["path"] != "drive/pdf00000-hidden_.._x.pdf" {
		t.Fatalf("pdf: %v", m)
	}
	if _, err := dl.Run(context.Background(), ws, json.RawMessage(`{"file":"doc0000000","format":"xlsx"}`)); err == nil {
		t.Fatal("xlsx of a doc accepted")
	}
	if _, err := dl.Run(context.Background(), ws, json.RawMessage(`{"file":"pdf0000000","format":"docx"}`)); err == nil {
		t.Fatal("conversion of a stored file accepted")
	}
	// A drive directory the agent pointed outside the workspace is refused.
	ws2, outside := t.TempDir(), t.TempDir()
	if err := os.Symlink(outside, filepath.Join(ws2, "drive")); err != nil {
		t.Fatal(err)
	}
	if _, err := dl.Run(context.Background(), ws2, json.RawMessage(`{"file":"doc0000000"}`)); err == nil {
		t.Fatal("download through a symlink accepted")
	}
	if entries, _ := os.ReadDir(outside); len(entries) != 0 {
		t.Fatalf("wrote outside the workspace: %v", entries)
	}
}

func TestSearchMergesAccountsAndReportsErrors(t *testing.T) {
	a := &fakeDrive{files: map[string]File{"a000000000": {ID: "a000000000", Name: "A", MimeType: docMime, ModifiedTime: "2026-10-01T00:00:00Z",
		Owners: []Person{{DisplayName: "Ann", EmailAddress: "ann@x.com"}}}}}
	b := &fakeDrive{files: map[string]File{"b000000000": {ID: "b000000000", Name: "B", MimeType: pdfMime, ModifiedTime: "2026-10-05T00:00:00Z", Size: "42"}}}
	broken := &fakeDrive{status: 500}
	expired := &fakeDrive{status: 401}
	var refused []string
	tools := Tools([]Account{
		{Email: "a@x.com", Primary: true, Client: a.client(t)}, {Email: "b@x.com", Client: b.client(t)},
		{Email: "c@x.com", Client: broken.client(t)}, {Email: "d@x.com", Client: expired.client(t)},
	}, func(e string) { refused = append(refused, e) })
	m := run(t, tool(t, tools, "search"), "", `{"query":"plan"}`)
	files := m["files"].([]any)
	if len(files) != 2 || files[0].(map[string]any)["id"] != "b000000000" || files[0].(map[string]any)["account"] != "b@x.com" ||
		files[1].(map[string]any)["owner"] != "Ann <ann@x.com>" || files[0].(map[string]any)["size"].(float64) != 42 {
		t.Fatalf("files: %v", files)
	}
	errs := m["errors"].([]any)
	if len(errs) != 2 || !strings.Contains(errs[1].(map[string]any)["error"].(string), "d@x.com needs reconnecting") {
		t.Fatalf("errors: %v", errs)
	}
	if len(refused) != 1 || refused[0] != "d@x.com" {
		t.Fatalf("onAuth: %v", refused)
	}
	if len(a.queries) != 1 || !strings.Contains(a.queries[0], "fullText contains 'plan'") {
		t.Fatalf("query: %v", a.queries)
	}
	run(t, tool(t, tools, "search"), "", `{"account":"a@x.com"}`)
	if len(b.queries) != 1 {
		t.Fatal("account filter searched other accounts")
	}
}

func TestSingleFileToolsFallBackOn404(t *testing.T) {
	primary := &fakeDrive{files: map[string]File{}}
	other := &fakeDrive{files: map[string]File{"x000000000": {ID: "x000000000", Name: "Shared", MimeType: docMime, Shared: true,
		Permissions: []Permission{{Type: "anyone", Role: "reader"}, {Type: "user", Role: "writer", EmailAddress: "bo@x.com"}, {Type: "domain", Role: "reader", Domain: "x.com"}}}}}
	tools := Tools([]Account{{Email: "b@x.com", Client: other.client(t)}, {Email: "a@x.com", Primary: true, Client: primary.client(t)}}, nil)
	m := run(t, tool(t, tools, "get"), "", `{"file":"x000000000"}`)
	sharing := m["sharing"].(map[string]any)
	if m["account"] != "b@x.com" || sharing["anyoneWithLink"] != true || len(sharing["people"].([]any)) != 1 || sharing["total"].(float64) != 3 {
		t.Fatalf("get: %v", m)
	}
	if _, err := tool(t, tools, "get").Run(context.Background(), "", json.RawMessage(`{"file":"missing000"}`)); err == nil || !strings.Contains(err.Error(), "not found") {
		t.Fatalf("missing file: %v", err)
	}
	if _, err := tool(t, tools, "get").Run(context.Background(), "", json.RawMessage(`{"file":"x000000000","account":"a@x.com"}`)); err == nil {
		t.Fatal("named account fell back to another")
	}
}

func TestToolsNeedAnAccount(t *testing.T) {
	if Tools(nil, nil) != nil {
		t.Fatal("tools without accounts")
	}
}
