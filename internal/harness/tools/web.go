/*
 * Copyright (c) 2026. Papermark - All Rights Reserved
 * Unauthorized copying of this file, via any medium is strictly prohibited
 * Proprietary and confidential
 * Written by Ebot Tabi <ebot.tabi@gmail.com>, 16/10/2026
 */
package tools

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"mime"
	"net/http"
	"strings"
	"time"
	"unicode/utf8"

	"papermark/internal/safehttp"

	"golang.org/x/net/html"
	"golang.org/x/net/html/atom"
)

const (
	fetchCap      = 5 << 20
	maxQuery      = 400
	maxSnippet    = 500
	defaultResult = 5
	maxResults    = 10
	tavilyURL     = "https://api.tavily.com/search"
)

// Web runs web_search (Tavily) and web_fetch. Both are read-only, so they
// need no approval; their output is untrusted page text.
type Web struct {
	tavilyKey string
	searchURL string
	search    *http.Client
	fetch     *safehttp.Client
}

// NewWeb returns the web client; without tavilyKey there is no web_search.
func NewWeb(tavilyKey string, publicHost []string) *Web {
	return &Web{tavilyKey: tavilyKey, searchURL: tavilyURL, search: &http.Client{Timeout: 30 * time.Second},
		fetch: safehttp.New(safehttp.Config{Timeout: 20 * time.Second, Cap: fetchCap, MaxRedirects: 10, DenyHosts: publicHost})}
}

// fetchPage fetches raw through the shared SSRF guard: every hop is checked,
// and so is every address it dials.
func (w *Web) fetchPage(ctx context.Context, raw string) (string, error) {
	res, err := w.fetch.Get(ctx, raw, "text/html, text/plain;q=0.9, */*;q=0.5", safehttp.Conditions{})
	switch {
	case errors.Is(err, safehttp.ErrBlocked):
		return "", fmt.Errorf("%s cannot be fetched: it is an internal or private address", raw)
	case errors.Is(err, safehttp.ErrTooLarge):
		return "", fmt.Errorf("%s is larger than the %d-byte cap", raw, fetchCap)
	case err != nil:
		return "", fmt.Errorf("could not fetch %s: %v", raw, err)
	}
	return readPage(res)
}

func readPage(res *safehttp.Response) (string, error) {
	u := res.URL
	if res.Status >= 400 {
		return "", fmt.Errorf("HTTP %d for %s", res.Status, u.String())
	}
	body := res.Body
	media, _, _ := mime.ParseMediaType(res.ContentType)
	var text string
	switch {
	case strings.Contains(media, "html"):
		text = htmlText(body)
	case media == "" || strings.HasPrefix(media, "text/") || strings.Contains(media, "json") || strings.Contains(media, "xml"):
		if !utf8.Valid(body) {
			return "", fmt.Errorf("%s is not text", u.String())
		}
		text = strings.TrimSpace(string(body))
	default:
		return "", fmt.Errorf("%s is %s, not a page web_fetch can read", u.String(), media)
	}
	if n := jsLen(text); n > maxRead {
		text = fmt.Sprintf("%s\n[truncated: %d chars total]", truncateRunes(text, maxRead), n)
	}
	return fmt.Sprintf("URL: %s\n\n%s", u.String(), text), nil
}

// htmlText keeps a page's readable text: scripts and styles are dropped,
// blocks become lines, headings get # marks and list items a dash.
func htmlText(b []byte) string {
	z := html.NewTokenizer(bytes.NewReader(b))
	var out strings.Builder
	skip := 0
	for {
		tt := z.Next()
		switch tt {
		case html.ErrorToken:
			return tidyText(out.String())
		case html.TextToken:
			if skip == 0 {
				out.Write(z.Text())
			}
		case html.StartTagToken, html.SelfClosingTagToken, html.EndTagToken:
			name, _ := z.TagName()
			switch atom.Lookup(name) {
			case atom.Script, atom.Style, atom.Noscript, atom.Template, atom.Svg, atom.Iframe:
				if tt == html.StartTagToken {
					skip++
				} else if tt == html.EndTagToken && skip > 0 {
					skip--
				}
			case atom.H1, atom.H2, atom.H3, atom.H4, atom.H5, atom.H6:
				out.WriteString("\n")
				if tt == html.StartTagToken {
					out.WriteString(strings.Repeat("#", int(name[1]-'0')) + " ")
				}
			case atom.Li:
				if tt == html.StartTagToken {
					out.WriteString("\n- ")
				}
			case atom.P, atom.Div, atom.Br, atom.Tr, atom.Section, atom.Article, atom.Header, atom.Footer,
				atom.Ul, atom.Ol, atom.Table, atom.Pre, atom.Blockquote, atom.Hr, atom.Title, atom.Main, atom.Nav:
				out.WriteString("\n")
			case atom.Td, atom.Th:
				out.WriteString(" ")
			}
		}
	}
}

// tidyText collapses spaces in each line and keeps at most one blank line.
func tidyText(s string) string {
	var out []string
	for _, line := range strings.Split(s, "\n") {
		line = strings.Join(strings.Fields(line), " ")
		if line == "" && (len(out) == 0 || out[len(out)-1] == "") {
			continue
		}
		out = append(out, line)
	}
	return strings.TrimSpace(strings.Join(out, "\n"))
}

type searchResult struct {
	Title         string `json:"title"`
	URL           string `json:"url"`
	Content       string `json:"content"`
	PublishedDate string `json:"published_date"`
}

// searchWeb asks Tavily for titles, links and short snippets, never bodies.
func (w *Web) searchWeb(ctx context.Context, query string, n int) (string, error) {
	results, err := w.searchResults(ctx, query, n)
	if err != nil {
		return "", err
	}
	if len(results) == 0 {
		return "No results.", nil
	}
	var b strings.Builder
	for i, r := range results {
		fmt.Fprintf(&b, "%d. %s\n   %s", i+1, oneLine(r.Title), r.URL)
		if r.PublishedDate != "" {
			fmt.Fprintf(&b, " published %s", r.PublishedDate)
		}
		fmt.Fprintf(&b, "\n   %s\n", truncateRunes(oneLine(r.Content), maxSnippet))
	}
	return strings.TrimRight(b.String(), "\n"), nil
}

func (w *Web) searchResults(ctx context.Context, query string, n int) ([]searchResult, error) {
	query = strings.TrimSpace(query)
	if query == "" || len(query) > maxQuery {
		return nil, fmt.Errorf("query must be 1 to %d characters", maxQuery)
	}
	if n <= 0 {
		n = defaultResult
	}
	body, err := json.Marshal(map[string]any{"query": query, "max_results": min(n, maxResults),
		"search_depth": "basic", "include_published_date": true})
	if err != nil {
		return nil, err
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, w.searchURL, bytes.NewReader(body))
	if err != nil {
		return nil, err
	}
	req.Header.Set("Authorization", "Bearer "+w.tavilyKey)
	req.Header.Set("Content-Type", "application/json")
	res, err := w.search.Do(req)
	if err != nil {
		return nil, errors.New("web search request failed")
	}
	defer res.Body.Close()
	// Provider bodies are never passed on to the model.
	switch s := res.StatusCode; {
	case s == http.StatusUnauthorized || s == http.StatusForbidden:
		return nil, errors.New("web search is not authorized; continue without it")
	case s == http.StatusTooManyRequests || s == 432 || s == 433:
		return nil, errors.New("web search rate limit or quota reached; continue without it")
	case s >= 500:
		return nil, errors.New("web search provider is unavailable; continue without it")
	case s != http.StatusOK:
		return nil, fmt.Errorf("web search failed with status %d", s)
	}
	var out struct {
		Results []searchResult `json:"results"`
	}
	if err := json.NewDecoder(res.Body).Decode(&out); err != nil {
		return nil, errors.New("web search returned an unreadable response")
	}
	return out.Results, nil
}

// CanSearch says whether web_search is configured.
func (w *Web) CanSearch() bool { return w != nil && w.tavilyKey != "" }

func oneLine(s string) string { return strings.Join(strings.Fields(s), " ") }
