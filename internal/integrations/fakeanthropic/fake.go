/*
 * Copyright (c) 2026. Papermark - All Rights Reserved
 * Unauthorized copying of this file, via any medium is strictly prohibited
 * Proprietary and confidential
 * Written by Ebot Tabi <ebot.tabi@gmail.com>, 15/10/2026
 */
// Package fakeanthropic is a scripted stand-in for the Messages API (POST
// /v1/messages), so the agent server runs end to end without a key. Tests
// start it in process; cmd/fakeanthropic serves it on a port.
//
// Script format: a JSON array with one entry per assistant turn. Each turn is an array of blocks:
//
//	{"text": "..."}                                           text the model writes
//	{"tool": "<name>", "input": {...}}                        a client tool call (file_write, shell_exec, idle, ...)
//	{"web_search": "<query>", "results": [{"title", "url"}]}  a web_search server tool use and its result
//
// A turn with a tool call ends with stop_reason "tool_use", otherwise "end_turn".
// The fake is stateless: a request whose messages hold N assistant turns gets turn N, so restarts, continued
// and forked tasks, and concurrent tasks all follow the script. Past the end it answers "Script finished."
// Requests with "stream": true get Server-Sent Events in the real event format; others get one JSON message.
package fakeanthropic

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"net"
	"net/http"
	"regexp"
	"strings"
	"sync"
)

// Block is one scripted content block.
type Block struct {
	Text      *string         `json:"text,omitempty"`
	Tool      string          `json:"tool,omitempty"`
	Input     json.RawMessage `json:"input,omitempty"`
	WebSearch *string         `json:"web_search,omitempty"`
	Results   []Result        `json:"results,omitempty"`
}

// Result is one scripted web search result.
type Result struct {
	Title string `json:"title"`
	URL   string `json:"url"`
}

// Script is one turn per assistant response.
type Script [][]Block

// Text returns a text block.
func Text(s string) Block { return Block{Text: &s} }

// Tool returns a client tool call block; input is marshaled to JSON.
func Tool(name string, input any) Block {
	raw, err := json.Marshal(input)
	if err != nil {
		panic(err)
	}
	return Block{Tool: name, Input: raw}
}

// WebSearch returns a web_search server tool use with its results.
func WebSearch(query string, results ...Result) Block {
	return Block{WebSearch: &query, Results: results}
}

// Server is a running fake.
type Server struct {
	URL      string
	script   Script
	srv      *http.Server
	mu       sync.Mutex
	requests [][]byte
}

// Start serves script on addr ("127.0.0.1:0" picks a free port).
func Start(script Script, addr string) (*Server, error) {
	ln, err := net.Listen("tcp", addr)
	if err != nil {
		return nil, err
	}
	s := &Server{URL: "http://" + ln.Addr().String(), script: script}
	s.srv = &http.Server{Handler: s}
	go s.srv.Serve(ln)
	return s, nil
}

// Close stops the server.
func (s *Server) Close() error { return s.srv.Close() }

// Requests returns every request body received so far, decoded.
func (s *Server) Requests() []map[string]any {
	s.mu.Lock()
	defer s.mu.Unlock()
	out := make([]map[string]any, len(s.requests))
	for i, raw := range s.requests {
		json.Unmarshal(raw, &out[i])
	}
	return out
}

type usage struct {
	InputTokens              int `json:"input_tokens"`
	OutputTokens             int `json:"output_tokens"`
	CacheReadInputTokens     int `json:"cache_read_input_tokens"`
	CacheCreationInputTokens int `json:"cache_creation_input_tokens"`
}

const outputTokens = 50

var startUsage = usage{1000, 1, 800, 100}

type message struct {
	ID           string            `json:"id"`
	Type         string            `json:"type"`
	Role         string            `json:"role"`
	Model        string            `json:"model"`
	Content      []json.RawMessage `json:"content"`
	StopReason   *string           `json:"stop_reason"`
	StopSequence *string           `json:"stop_sequence"`
	Usage        usage             `json:"usage"`
}

func mustJSON(v any) json.RawMessage {
	raw, err := json.Marshal(v)
	if err != nil {
		panic(err)
	}
	return raw
}

func compact(raw json.RawMessage) json.RawMessage {
	if len(raw) == 0 {
		return json.RawMessage("{}")
	}
	var b bytes.Buffer
	if json.Compact(&b, raw) != nil {
		return raw
	}
	return b.Bytes()
}

type request struct {
	Model    string `json:"model"`
	Stream   bool   `json:"stream"`
	Messages []struct {
		Role string `json:"role"`
	} `json:"messages"`
}

func (s *Server) message(req request) message {
	n := 0
	for _, m := range req.Messages {
		if m.Role == "assistant" {
			n++
		}
	}
	turn := []Block{Text("Script finished.")}
	if n < len(s.script) {
		turn = s.script[n]
	}
	var content []json.RawMessage
	stop := "end_turn"
	for i, b := range turn {
		switch {
		case b.Text != nil:
			content = append(content, mustJSON(struct {
				Type string `json:"type"`
				Text string `json:"text"`
			}{"text", *b.Text}))
		case b.Tool != "":
			stop = "tool_use"
			content = append(content, mustJSON(struct {
				Type  string          `json:"type"`
				ID    string          `json:"id"`
				Name  string          `json:"name"`
				Input json.RawMessage `json:"input"`
			}{"tool_use", fmt.Sprintf("toolu_%d_%d", n, i), b.Tool, compact(b.Input)}))
		case b.WebSearch != nil:
			id := fmt.Sprintf("srvtoolu_%d_%d", n, i)
			content = append(content, mustJSON(struct {
				Type  string            `json:"type"`
				ID    string            `json:"id"`
				Name  string            `json:"name"`
				Input map[string]string `json:"input"`
			}{"server_tool_use", id, "web_search", map[string]string{"query": *b.WebSearch}}))
			type result struct {
				Type             string  `json:"type"`
				Title            string  `json:"title"`
				URL              string  `json:"url"`
				EncryptedContent string  `json:"encrypted_content"`
				PageAge          *string `json:"page_age"`
			}
			results := []result{}
			for _, r := range b.Results {
				results = append(results, result{"web_search_result", r.Title, r.URL, "fake", nil})
			}
			content = append(content, mustJSON(struct {
				Type      string   `json:"type"`
				ToolUseID string   `json:"tool_use_id"`
				Content   []result `json:"content"`
			}{"web_search_tool_result", id, results}))
		}
	}
	u := startUsage
	u.OutputTokens = outputTokens
	return message{ID: fmt.Sprintf("msg_fake_%d", n), Type: "message", Role: "assistant", Model: req.Model,
		Content: content, StopReason: &stop, Usage: u}
}

var words = regexp.MustCompile(`\S+\s*`)

type event struct {
	name string
	data any
}

// events renders msg as the event sequence the real API streams: text and tool input arrive in small deltas.
func events(msg message) []event {
	start := msg
	start.Content, start.StopReason, start.Usage = []json.RawMessage{}, nil, startUsage
	out := []event{{"message_start", map[string]any{"type": "message_start", "message": start}}}
	delta := func(index int, d any) event {
		return event{"content_block_delta", map[string]any{"type": "content_block_delta", "index": index, "delta": d}}
	}
	for index, raw := range msg.Content {
		var block map[string]json.RawMessage
		json.Unmarshal(raw, &block)
		var typ string
		json.Unmarshal(block["type"], &typ)
		begin := func(b json.RawMessage) event {
			return event{"content_block_start", map[string]any{"type": "content_block_start", "index": index, "content_block": b}}
		}
		switch typ {
		case "text":
			out = append(out, begin(json.RawMessage(`{"type":"text","text":""}`)))
			var text string
			json.Unmarshal(block["text"], &text)
			for _, w := range words.FindAllString(text, -1) {
				out = append(out, delta(index, map[string]string{"type": "text_delta", "text": w}))
			}
		case "tool_use", "server_tool_use":
			out = append(out, begin(withEmptyInput(raw)))
			input := []rune(string(block["input"]))
			for i := 0; i < len(input); i += 16 {
				out = append(out, delta(index, map[string]string{"type": "input_json_delta", "partial_json": string(input[i:min(i+16, len(input))])}))
			}
		default:
			out = append(out, begin(raw))
		}
		out = append(out, event{"content_block_stop", map[string]any{"type": "content_block_stop", "index": index}})
	}
	out = append(out,
		event{"message_delta", map[string]any{"type": "message_delta",
			"delta": map[string]any{"stop_reason": msg.StopReason, "stop_sequence": nil},
			"usage": map[string]int{"output_tokens": outputTokens}}},
		event{"message_stop", map[string]string{"type": "message_stop"}})
	return out
}

// withEmptyInput returns a tool_use block with input {}, keeping key order.
func withEmptyInput(raw json.RawMessage) json.RawMessage {
	var b struct {
		Type  string          `json:"type"`
		ID    string          `json:"id"`
		Name  string          `json:"name"`
		Input json.RawMessage `json:"input"`
	}
	json.Unmarshal(raw, &b)
	b.Input = json.RawMessage("{}")
	return mustJSON(b)
}

func (s *Server) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost || !strings.HasPrefix(r.URL.Path, "/v1/messages") {
		w.WriteHeader(http.StatusNotFound)
		return
	}
	raw, err := io.ReadAll(r.Body)
	var req request
	if err == nil {
		err = json.Unmarshal(raw, &req)
	}
	if err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}
	s.mu.Lock()
	s.requests = append(s.requests, raw)
	s.mu.Unlock()
	msg := s.message(req)
	if !req.Stream {
		w.Header().Set("content-type", "application/json")
		w.Write(mustJSON(msg))
		return
	}
	w.Header().Set("content-type", "text/event-stream")
	w.WriteHeader(http.StatusOK)
	for _, e := range events(msg) {
		fmt.Fprintf(w, "event: %s\ndata: %s\n\n", e.name, mustJSON(e.data))
	}
}
