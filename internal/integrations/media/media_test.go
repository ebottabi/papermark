/*
 * Copyright (c) 2026. Papermark - All Rights Reserved
 * Unauthorized copying of this file, via any medium is strictly prohibited
 * Proprietary and confidential
 * Written by Ebot Tabi <ebot.tabi@gmail.com>, 18/10/2026
 */
package media

import (
	"bytes"
	"context"
	"image"
	"image/png"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"papermark/internal/config"
)

func newTest(t *testing.T, h http.HandlerFunc) *Media {
	srv := httptest.NewServer(h)
	t.Cleanup(srv.Close)
	return New(config.AgentConfig{FireworksAPIKey: "k", FireworksBaseURL: srv.URL,
		Media: config.MediaConfig{VisionModel: "v", TranscribeURL: srv.URL + "/t", TranscribeModel: "w", FFmpeg: "no-such-ffmpeg", FFprobe: "no-such-ffprobe"}})
}

func TestImageRequestAndCache(t *testing.T) {
	dir := t.TempDir()
	p := filepath.Join(dir, "a.png")
	var buf bytes.Buffer
	_ = png.Encode(&buf, image.NewRGBA(image.Rect(0, 0, 3, 2)))
	os.WriteFile(p, buf.Bytes(), 0o644)
	os.Chtimes(p, time.Now().Add(-time.Hour), time.Now().Add(-time.Hour))
	calls := 0
	m := newTest(t, func(w http.ResponseWriter, r *http.Request) {
		calls++
		b := new(bytes.Buffer)
		b.ReadFrom(r.Body)
		s := b.String()
		if r.Header.Get("Authorization") != "Bearer k" || !strings.Contains(s, "data:image/png;base64,") || !strings.Contains(s, "what colour?") {
			t.Errorf("bad request: %s", s)
		}
		w.Write([]byte(`{"choices":[{"message":{"content":"blue"}}]}`))
	})
	for range 2 {
		got, err := m.Image(context.Background(), p, "what colour?")
		if err != nil || got != "Image 3x2.\nblue" {
			t.Fatalf("got %q, %v", got, err)
		}
	}
	if calls != 1 {
		t.Fatalf("cache miss: %d calls", calls)
	}
}

func TestAudioFormat(t *testing.T) {
	p := filepath.Join(t.TempDir(), "a.mp3")
	os.WriteFile(p, []byte("x"), 0o644)
	m := newTest(t, func(w http.ResponseWriter, r *http.Request) {
		if err := r.ParseMultipartForm(1 << 20); err != nil {
			t.Fatal(err)
		}
		if r.FormValue("response_format") != "verbose_json" || r.FormValue("model") != "w" || r.MultipartForm.File["file"] == nil {
			t.Errorf("bad form: %v", r.MultipartForm.Value)
		}
		w.Write([]byte(`{"duration":75,"segments":[{"start":0,"text":" hi"},{"start":12.5,"text":" there"}]}`))
	})
	got, err := m.Audio(context.Background(), p)
	if want := "Audio 1:15.\n[00:00] hi\n[00:12] there"; err != nil || got != want {
		t.Fatalf("got %q, %v", got, err)
	}
}

func TestParseAt(t *testing.T) {
	for in, want := range map[string]float64{"02:30": 150, "1:02:03": 3723, "45": 45} {
		if got, err := parseAt(in); err != nil || got != want {
			t.Errorf("%s: %v %v", in, got, err)
		}
	}
	if _, err := parseAt("a:b"); err == nil {
		t.Error("want error")
	}
}

func TestVideoNeedsFFmpeg(t *testing.T) {
	m := newTest(t, nil)
	if _, err := m.Video(context.Background(), "x.mp4", ""); err == nil || err.Error() != "reading video needs ffmpeg on the server" {
		t.Fatalf("got %v", err)
	}
}
