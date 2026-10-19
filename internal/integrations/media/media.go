/*
 * Copyright (c) 2026. Papermark - All Rights Reserved
 * Unauthorized copying of this file, via any medium is strictly prohibited
 * Proprietary and confidential
 * Written by Ebot Tabi <ebot.tabi@gmail.com>, 18/10/2026
 */
// Package media turns images, audio and video into text: a Fireworks vision
// model describes pictures, Fireworks Whisper transcribes speech and ffmpeg
// extracts frames and audio from video.
package media

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"image"
	_ "image/gif"
	_ "image/jpeg"
	_ "image/png"
	"io"
	"mime/multipart"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"sync"
	"time"

	"papermark/internal/config"
)

const (
	maxImageBytes  = 4 << 20
	maxAudioBytes  = 25 << 20
	maxVideo       = 20 * time.Minute
	maxFrames      = 24
	callTimeout    = 3 * time.Minute
	ffTimeout      = 5 * time.Minute
	describePrompt = "Describe this image for someone who cannot see it. First copy every piece of visible text exactly, then describe the content, layout, and any charts or tables with their values."
	framePrompt    = "Describe this video frame in one concise line, then copy any visible text exactly."
)

// Media implements tools.MediaReader. It is safe for concurrent use.
type Media struct {
	apiKey, baseURL string
	cfg             config.MediaConfig
	http            *http.Client
}

// New returns a reader using the agent's Fireworks key and base URL.
func New(cfg config.AgentConfig) *Media {
	return &Media{apiKey: cfg.FireworksAPIKey, baseURL: strings.TrimRight(cfg.FireworksBaseURL, "/"), cfg: cfg.Media, http: &http.Client{Timeout: callTimeout}}
}

func hasBin(p string) bool { _, err := exec.LookPath(p); return err == nil }

func (m *Media) ffmpeg(ctx context.Context, args ...string) error {
	ctx, cancel := context.WithTimeout(ctx, ffTimeout)
	defer cancel()
	out, err := exec.CommandContext(ctx, m.cfg.FFmpeg, append([]string{"-hide_banner", "-loglevel", "error", "-y"}, args...)...).CombinedOutput()
	if err != nil {
		return fmt.Errorf("ffmpeg: %w: %s", err, strings.TrimSpace(string(out)))
	}
	return nil
}

// cached returns the cached text for path/kind/key when newer than the source.
func cached(path, kind, key string, read func() (string, error)) (string, error) {
	name := filepath.Base(path) + "." + kind
	if key != "" {
		h := sha256.Sum256([]byte(key))
		name += "." + hex.EncodeToString(h[:4])
	}
	cp := filepath.Join(filepath.Dir(path), ".cache", name+".txt")
	if si, err := os.Stat(path); err == nil {
		if ci, err := os.Stat(cp); err == nil && ci.ModTime().After(si.ModTime()) {
			if b, err := os.ReadFile(cp); err == nil {
				return string(b), nil
			}
		}
	}
	out, err := read()
	if err != nil {
		return "", err
	}
	if os.MkdirAll(filepath.Dir(cp), 0o755) == nil {
		_ = os.WriteFile(cp, []byte(out), 0o644) // best effort: a miss only costs a re-read
	}
	return out, nil
}

// Image describes the image at path, or answers question about it.
func (m *Media) Image(ctx context.Context, path, question string) (string, error) {
	return cached(path, "image", question, func() (string, error) { return m.image(ctx, path, question) })
}

func (m *Media) image(ctx context.Context, path, question string) (string, error) {
	b, err := os.ReadFile(path)
	if err != nil {
		return "", err
	}
	ext := strings.ToLower(filepath.Ext(path))
	heic := ext == ".heic" || ext == ".heif"
	if heic || len(b) > maxImageBytes {
		if !hasBin(m.cfg.FFmpeg) {
			if heic {
				return "", errors.New("HEIC images need ffmpeg on the server")
			}
		} else if b, err = m.toJPEG(ctx, path, "scale='min(2048,iw)':'min(2048,ih)':force_original_aspect_ratio=decrease"); err != nil {
			return "", err
		} else {
			ext = ".jpg"
		}
	}
	return m.describe(ctx, b, ext, questionPrompt(question))
}

func questionPrompt(q string) string {
	if q == "" {
		return describePrompt
	}
	return "Answer this question about the image: " + q
}

func (m *Media) toJPEG(ctx context.Context, src, vf string) ([]byte, error) {
	dir, err := os.MkdirTemp("", "media")
	if err != nil {
		return nil, err
	}
	defer os.RemoveAll(dir)
	out := filepath.Join(dir, "o.jpg")
	if err := m.ffmpeg(ctx, "-i", src, "-vf", vf, "-frames:v", "1", out); err != nil {
		return nil, err
	}
	return os.ReadFile(out)
}

func (m *Media) describe(ctx context.Context, b []byte, ext, prompt string) (string, error) {
	mime := map[string]string{".png": "image/png", ".gif": "image/gif", ".webp": "image/webp", ".jpg": "image/jpeg", ".jpeg": "image/jpeg"}[ext]
	if mime == "" {
		mime = "image/jpeg"
	}
	body, _ := json.Marshal(map[string]any{"model": m.cfg.VisionModel, "max_tokens": 1500, "stream": false,
		"messages": []any{map[string]any{"role": "user", "content": []any{
			map[string]any{"type": "text", "text": prompt},
			map[string]any{"type": "image_url", "image_url": map[string]string{"url": "data:" + mime + ";base64," + base64.StdEncoding.EncodeToString(b)}},
		}}}})
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, m.baseURL+"/chat/completions", bytes.NewReader(body))
	if err != nil {
		return "", err
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Authorization", "Bearer "+m.apiKey)
	raw, err := m.do(req)
	if err != nil {
		return "", err
	}
	var r struct {
		Choices []struct {
			Message struct {
				Content string `json:"content"`
			} `json:"message"`
		} `json:"choices"`
	}
	if err := json.Unmarshal(raw, &r); err != nil || len(r.Choices) == 0 {
		return "", errors.New("vision model returned no answer")
	}
	text := strings.TrimSpace(r.Choices[0].Message.Content)
	if cfg, _, err := image.DecodeConfig(bytes.NewReader(b)); err == nil {
		text = fmt.Sprintf("Image %dx%d.\n%s", cfg.Width, cfg.Height, text)
	}
	return text, nil
}

func (m *Media) do(req *http.Request) ([]byte, error) {
	resp, err := m.http.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	raw, err := io.ReadAll(io.LimitReader(resp.Body, 8<<20))
	if err != nil {
		return nil, err
	}
	if resp.StatusCode/100 != 2 {
		return nil, fmt.Errorf("media request failed: %s: %.300s", resp.Status, raw)
	}
	return raw, nil
}

// Audio transcribes the file at path with segment timestamps.
func (m *Media) Audio(ctx context.Context, path string) (string, error) {
	return cached(path, "audio", "", func() (string, error) { return m.audio(ctx, path) })
}

func (m *Media) audio(ctx context.Context, path string) (string, error) {
	src := path
	switch ext := strings.ToLower(filepath.Ext(path)); {
	case ext == ".mp3" || ext == ".wav" || ext == ".flac" || ext == ".m4a" || ext == ".ogg":
		if fi, err := os.Stat(path); err != nil {
			return "", err
		} else if fi.Size() <= maxAudioBytes {
			break
		}
		fallthrough
	default:
		if !hasBin(m.cfg.FFmpeg) {
			return "", errors.New("this audio needs ffmpeg on the server to convert it")
		}
		dir, err := os.MkdirTemp("", "media")
		if err != nil {
			return "", err
		}
		defer os.RemoveAll(dir)
		src = filepath.Join(dir, "a.mp3")
		if err := m.ffmpeg(ctx, "-i", path, "-vn", "-ar", "16000", "-ac", "1", src); err != nil {
			return "", err
		}
	}
	return m.transcribe(ctx, src)
}

func (m *Media) transcribe(ctx context.Context, file string) (string, error) {
	f, err := os.Open(file)
	if err != nil {
		return "", err
	}
	defer f.Close()
	var buf bytes.Buffer
	w := multipart.NewWriter(&buf)
	_ = w.WriteField("model", m.cfg.TranscribeModel)
	_ = w.WriteField("response_format", "verbose_json")
	_ = w.WriteField("timestamp_granularities[]", "segment")
	fw, _ := w.CreateFormFile("file", filepath.Base(file))
	if _, err := io.Copy(fw, f); err != nil {
		return "", err
	}
	w.Close()
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, m.cfg.TranscribeURL, &buf)
	if err != nil {
		return "", err
	}
	req.Header.Set("Content-Type", w.FormDataContentType())
	req.Header.Set("Authorization", "Bearer "+m.apiKey)
	raw, err := m.do(req)
	if err != nil {
		return "", err
	}
	var r struct {
		Duration float64 `json:"duration"`
		Text     string  `json:"text"`
		Segments []struct {
			Start float64 `json:"start"`
			Text  string  `json:"text"`
		} `json:"segments"`
	}
	if err := json.Unmarshal(raw, &r); err != nil {
		return "", errors.New("transcription returned invalid JSON")
	}
	var sb strings.Builder
	fmt.Fprintf(&sb, "Audio %d:%02d.", int(r.Duration)/60, int(r.Duration)%60)
	for _, s := range r.Segments {
		fmt.Fprintf(&sb, "\n[%s] %s", clock(s.Start), strings.TrimSpace(s.Text))
	}
	if len(r.Segments) == 0 && r.Text != "" {
		sb.WriteString("\n" + strings.TrimSpace(r.Text))
	}
	return sb.String(), nil
}

func clock(sec float64) string { return fmt.Sprintf("%02d:%02d", int(sec)/60, int(sec)%60) }

// parseAt parses "MM:SS", "HH:MM:SS" or plain seconds.
func parseAt(at string) (float64, error) {
	t := 0.0
	parts := strings.Split(strings.TrimSpace(at), ":")
	if len(parts) > 3 {
		return 0, fmt.Errorf("bad time %q: use MM:SS or HH:MM:SS", at)
	}
	for _, p := range parts {
		n, err := strconv.ParseFloat(p, 64)
		if err != nil || n < 0 {
			return 0, fmt.Errorf("bad time %q: use MM:SS or HH:MM:SS", at)
		}
		t = t*60 + n
	}
	return t, nil
}

// Video returns a timeline of frames and speech, or the frame at `at`.
func (m *Media) Video(ctx context.Context, path, at string) (string, error) {
	if !hasBin(m.cfg.FFmpeg) || !hasBin(m.cfg.FFprobe) {
		return "", errors.New("reading video needs ffmpeg on the server")
	}
	return cached(path, "video", at, func() (string, error) { return m.video(ctx, path, at) })
}

func (m *Media) video(ctx context.Context, path, at string) (string, error) {
	pctx, cancel := context.WithTimeout(ctx, time.Minute)
	defer cancel()
	out, err := exec.CommandContext(pctx, m.cfg.FFprobe, "-v", "error", "-select_streams", "v:0", "-show_entries", "stream=width,height:format=duration", "-of", "json", path).Output()
	if err != nil {
		return "", fmt.Errorf("ffprobe: %w", err)
	}
	var pr struct {
		Streams []struct{ Width, Height int } `json:"streams"`
		Format  struct {
			Duration string `json:"duration"`
		} `json:"format"`
	}
	if err := json.Unmarshal(out, &pr); err != nil {
		return "", errors.New("ffprobe returned invalid JSON")
	}
	dur, _ := strconv.ParseFloat(pr.Format.Duration, 64)
	if dur > maxVideo.Seconds() {
		return "", fmt.Errorf("video is %dm%02ds; the limit is 20 minutes", int(dur)/60, int(dur)%60)
	}
	w, h := 0, 0
	if len(pr.Streams) > 0 {
		w, h = pr.Streams[0].Width, pr.Streams[0].Height
	}
	dir, err := os.MkdirTemp("", "media")
	if err != nil {
		return "", err
	}
	defer os.RemoveAll(dir)
	if at != "" {
		t, err := parseAt(at)
		if err != nil {
			return "", err
		}
		fr := filepath.Join(dir, "f.jpg")
		if err := m.ffmpeg(ctx, "-ss", strconv.FormatFloat(t, 'f', 2, 64), "-i", path, "-frames:v", "1", fr); err != nil {
			return "", err
		}
		b, err := os.ReadFile(fr)
		if err != nil || len(b) == 0 {
			return "", fmt.Errorf("no frame at %s", at)
		}
		return m.describe(ctx, b, ".jpg", describePrompt)
	}
	type line struct {
		t    float64
		text string
	}
	var (
		mu    sync.Mutex
		lines []line
		wg    sync.WaitGroup
	)
	step := max(5, dur/maxFrames)
	if err := m.ffmpeg(ctx, "-i", path, "-vf", fmt.Sprintf("fps=1/%g,scale=1024:-2", step), "-frames:v", strconv.Itoa(maxFrames), filepath.Join(dir, "f%03d.jpg")); err != nil {
		return "", err
	}
	frames, _ := filepath.Glob(filepath.Join(dir, "f*.jpg"))
	sort.Strings(frames)
	sem := make(chan struct{}, 3)
	var firstErr error
	for i, fp := range frames {
		wg.Add(1)
		sem <- struct{}{}
		go func() {
			defer func() { <-sem; wg.Done() }()
			b, err := os.ReadFile(fp)
			var d string
			if err == nil {
				d, err = m.describe(ctx, b, ".jpg", framePrompt)
			}
			mu.Lock()
			defer mu.Unlock()
			if err != nil {
				firstErr = err
				return
			}
			if _, rest, ok := strings.Cut(d, ".\n"); ok && strings.HasPrefix(d, "Image ") {
				d = rest
			}
			lines = append(lines, line{float64(i) * step, "[frame] " + strings.ReplaceAll(d, "\n", " ")})
		}()
	}
	wg.Wait()
	if firstErr != nil && len(lines) == 0 {
		return "", firstErr
	}
	if tr, err := m.audio(ctx, path); err == nil {
		for _, l := range strings.Split(tr, "\n")[1:] {
			if len(l) > 7 && l[0] == '[' && l[6] == ']' {
				t, _ := parseAt(l[1:6])
				lines = append(lines, line{t, "[speech]" + l[7:]})
			}
		}
	}
	sort.SliceStable(lines, func(i, j int) bool { return lines[i].t < lines[j].t })
	var sb strings.Builder
	fmt.Fprintf(&sb, "Video %dm%02ds, %dx%d.", int(dur)/60, int(dur)%60, w, h)
	for _, l := range lines {
		fmt.Fprintf(&sb, "\n%s %s", clock(l.t), l.text)
	}
	return sb.String(), nil
}
