/*
 * Copyright (c) 2026. Papermark - All Rights Reserved
 * Unauthorized copying of this file, via any medium is strictly prohibited
 * Proprietary and confidential
 * Written by Ebot Tabi <ebot.tabi@gmail.com>, 16/10/2026
 */
// Package fireworks streams OpenAI-compatible /chat/completions calls to
// Fireworks. Messages and tools stay raw JSON, so the agent replays exactly
// what it stored.
package fireworks

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log"
	"math/rand/v2"
	"net/http"
	"strconv"
	"strings"
	"time"
)

const (
	firstBackoff = time.Second
	// maxWait caps a backoff. A longer Retry-After gets no retry: the caller
	// is better off moving to the fallback model.
	maxWait = time.Minute
	// maxToolCalls bounds the tool-call index a stream may use.
	maxToolCalls = 128
)

// Client calls one OpenAI-compatible endpoint. It is safe for concurrent use.
type Client struct {
	baseURL, apiKey string
	http            *http.Client
	timeout         time.Duration
	retries         int
	backoff         time.Duration
}

// New returns a client. Each attempt gets timeout; a failed call is retried
// at most retries times.
func New(apiKey, baseURL string, timeout time.Duration, retries int) *Client {
	return &Client{baseURL: strings.TrimRight(baseURL, "/"), apiKey: apiKey, http: &http.Client{},
		timeout: timeout, retries: max(retries, 0), backoff: firstBackoff}
}

// Request is one model call. System is sent as the first message; Messages
// and Tools ({"type":"function","function":{...}}) go out as they are.
// Effort is reasoning_effort, empty for the provider default.
type Request struct {
	Model     string
	System    string
	Messages  []json.RawMessage
	Tools     []json.RawMessage
	MaxTokens int
	Effort    string
}

// ToolCall is a tool the model asked for, in the wire shape. Arguments are
// the model's raw text; they may not be valid JSON.
type ToolCall struct {
	ID       string   `json:"id"`
	Type     string   `json:"type"`
	Function Function `json:"function"`
}

// Function is a tool call's name and arguments.
type Function struct {
	Name      string `json:"name"`
	Arguments string `json:"arguments"`
}

// Message is the assistant turn, ready to store and replay.
type Message struct {
	Role             string     `json:"role"`
	Content          *string    `json:"content"`
	ReasoningContent string     `json:"reasoning_content,omitempty"`
	ToolCalls        []ToolCall `json:"tool_calls,omitempty"`
}

// Usage is one call's token counts. PromptTokens includes CachedTokens.
type Usage struct {
	PromptTokens, CompletionTokens, CachedTokens int64
}

// Response is a finished call.
type Response struct {
	Message      Message
	Usage        Usage
	FinishReason string
}

// Delta is a piece of streamed output: answer text, a fragment of the
// arguments of tool call ToolIndex (whose name so far is ToolName), or
// Thinking when the model is reasoning (its text is not passed on).
type Delta struct {
	Thinking     bool
	Content      string
	ToolIndex    int
	ToolName     string
	ArgsFragment string
}

// ErrorKind classifies a failed call. Provider text is never kept: it can
// echo the prompt.
type ErrorKind string

const (
	KindRateLimited ErrorKind = "rate_limited"
	KindUnavailable ErrorKind = "unavailable"
	KindTimeout     ErrorKind = "timeout"
	KindAuth        ErrorKind = "auth"
	KindBadRequest  ErrorKind = "bad_request"
	KindBadResponse ErrorKind = "bad_response"
)

// Error is a failed model call.
type Error struct {
	Kind       ErrorKind
	Status     int
	RetryAfter time.Duration
	// Streamed reports that output reached onDelta before the failure.
	Streamed bool
}

func (e *Error) Error() string {
	if e.Status != 0 {
		return fmt.Sprintf("model provider: %s (status %d)", e.Kind, e.Status)
	}
	return fmt.Sprintf("model provider: %s", e.Kind)
}

// Retryable reports whether the same call may succeed later.
func (e *Error) Retryable() bool {
	return e.Kind == KindRateLimited || e.Kind == KindUnavailable || e.Kind == KindTimeout
}

// Fallback reports whether err lets the caller try another model: a
// retryable failure before anything was streamed.
func Fallback(err error) bool {
	var e *Error
	return errors.As(err, &e) && e.Retryable() && !e.Streamed
}

// Stream makes one call, passing output to onDelta (may be nil) as it
// arrives. Failures are retried with backoff only while nothing has been
// streamed, so onDelta never sees the same text twice.
func (c *Client) Stream(ctx context.Context, req Request, onDelta func(Delta)) (*Response, error) {
	body, err := json.Marshal(wire(req))
	if err != nil {
		return nil, fmt.Errorf("encode model request: %w", err)
	}
	start := time.Now()
	for attempt := 1; ; attempt++ {
		resp, callErr := c.attempt(ctx, body, onDelta)
		if callErr == nil {
			log.Printf("model call: model=%s in=%d out=%d cached=%d finish=%s took=%s attempts=%d", req.Model,
				resp.Usage.PromptTokens, resp.Usage.CompletionTokens, resp.Usage.CachedTokens, resp.FinishReason, time.Since(start), attempt)
			return resp, nil
		}
		if ctx.Err() != nil {
			return nil, ctx.Err()
		}
		wait := c.wait(attempt, callErr.RetryAfter)
		if !callErr.Retryable() || callErr.Streamed || attempt > c.retries || wait > maxWait {
			log.Printf("model call failed: model=%s kind=%s status=%d attempts=%d", req.Model, callErr.Kind, callErr.Status, attempt)
			return nil, callErr
		}
		timer := time.NewTimer(wait)
		select {
		case <-ctx.Done():
			timer.Stop()
			return nil, ctx.Err()
		case <-timer.C:
		}
	}
}

func (c *Client) wait(attempt int, retryAfter time.Duration) time.Duration {
	if retryAfter > 0 {
		return retryAfter
	}
	d := min(c.backoff<<min(attempt-1, 16), maxWait)
	return d/2 + rand.N(d/2+1)
}

func (c *Client) attempt(ctx context.Context, body []byte, onDelta func(Delta)) (*Response, *Error) {
	actx, cancel := context.WithTimeout(ctx, c.timeout)
	defer cancel()
	req, err := http.NewRequestWithContext(actx, http.MethodPost, c.baseURL+"/chat/completions", bytes.NewReader(body))
	if err != nil {
		return nil, &Error{Kind: KindBadRequest}
	}
	req.Header.Set("Authorization", "Bearer "+c.apiKey)
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Accept", "text/event-stream")
	res, err := c.http.Do(req)
	if err != nil {
		return nil, transportError(actx)
	}
	defer res.Body.Close()
	if res.StatusCode != http.StatusOK {
		return nil, &Error{Kind: statusKind(res.StatusCode), Status: res.StatusCode, RetryAfter: parseRetryAfter(res.Header.Get("Retry-After"))}
	}
	resp, streamed, e := read(res, onDelta)
	if e != nil {
		if e.Kind == "" {
			e = transportError(actx)
		}
		e.Streamed = streamed
		return nil, e
	}
	return resp, nil
}

// read parses the SSE stream until [DONE]. A transport failure returns an
// Error with no Kind for the caller to classify.
func read(res *http.Response, onDelta func(Delta)) (*Response, bool, *Error) {
	var (
		content, reasoning strings.Builder
		calls              []ToolCall
		out                Response
		streamed, done     bool
	)
	sc := bufio.NewScanner(res.Body)
	sc.Buffer(make([]byte, 0, 64*1024), 4*1024*1024)
	for sc.Scan() {
		data, ok := strings.CutPrefix(sc.Text(), "data:")
		if !ok {
			continue
		}
		data = strings.TrimSpace(data)
		if data == "[DONE]" {
			done = true
			break
		}
		var chunk wireChunk
		if err := json.Unmarshal([]byte(data), &chunk); err != nil {
			return nil, streamed, &Error{Kind: KindBadResponse}
		}
		if chunk.Error != nil {
			return nil, streamed, &Error{Kind: KindUnavailable}
		}
		if u := chunk.Usage; u != nil {
			out.Usage = Usage{u.PromptTokens, u.CompletionTokens, u.PromptTokensDetails.CachedTokens}
		}
		for _, choice := range chunk.Choices {
			if choice.Index != 0 {
				continue
			}
			d := choice.Delta
			if r := d.ReasoningContent + d.Reasoning; r != "" {
				reasoning.WriteString(r)
				if onDelta != nil {
					onDelta(Delta{Thinking: true})
				}
			}
			if d.Content != "" {
				content.WriteString(d.Content)
				streamed = true
				if onDelta != nil {
					onDelta(Delta{Content: d.Content})
				}
			}
			for _, tc := range d.ToolCalls {
				if tc.Index < 0 || tc.Index >= maxToolCalls {
					return nil, streamed, &Error{Kind: KindBadResponse}
				}
				for len(calls) <= tc.Index {
					calls = append(calls, ToolCall{Type: "function"})
				}
				call := &calls[tc.Index]
				if tc.ID != "" {
					call.ID = tc.ID
				}
				call.Function.Name += tc.Function.Name
				call.Function.Arguments += tc.Function.Arguments
				streamed = true
				if onDelta != nil && tc.Function.Arguments != "" {
					onDelta(Delta{ToolIndex: tc.Index, ToolName: call.Function.Name, ArgsFragment: tc.Function.Arguments})
				}
			}
			if choice.FinishReason != "" {
				out.FinishReason = choice.FinishReason
			}
		}
	}
	if sc.Err() != nil {
		return nil, streamed, &Error{}
	}
	if !done && out.FinishReason == "" {
		return nil, streamed, &Error{Kind: KindUnavailable}
	}
	out.Message = Message{Role: "assistant", ReasoningContent: reasoning.String(), ToolCalls: calls}
	if text := content.String(); text != "" || len(calls) == 0 {
		out.Message.Content = &text
	}
	return &out, streamed, nil
}

func transportError(ctx context.Context) *Error {
	if errors.Is(ctx.Err(), context.DeadlineExceeded) {
		return &Error{Kind: KindTimeout}
	}
	return &Error{Kind: KindUnavailable}
}

func statusKind(status int) ErrorKind {
	switch {
	case status == http.StatusTooManyRequests:
		return KindRateLimited
	case status == http.StatusUnauthorized || status == http.StatusForbidden:
		return KindAuth
	case status == http.StatusRequestTimeout:
		return KindTimeout
	case status >= 500:
		return KindUnavailable
	default:
		return KindBadRequest
	}
}

func parseRetryAfter(v string) time.Duration {
	if v == "" {
		return 0
	}
	if secs, err := strconv.Atoi(v); err == nil {
		return max(time.Duration(secs)*time.Second, 0)
	}
	if at, err := http.ParseTime(v); err == nil {
		return max(time.Until(at), 0)
	}
	return 0
}

// EstimateTokens is a deliberate overestimate of a request: bytes over three.
func EstimateTokens(req Request) int {
	n := len(req.System)
	for _, m := range req.Messages {
		n += len(m)
	}
	for _, t := range req.Tools {
		n += len(t)
	}
	return (n + 2) / 3
}

type wireRequest struct {
	Model           string            `json:"model"`
	Messages        []json.RawMessage `json:"messages"`
	Tools           []json.RawMessage `json:"tools,omitempty"`
	MaxTokens       int               `json:"max_tokens"`
	ReasoningEffort string            `json:"reasoning_effort,omitempty"`
	Stream          bool              `json:"stream"`
	StreamOptions   struct {
		IncludeUsage bool `json:"include_usage"`
	} `json:"stream_options"`
}

func wire(req Request) wireRequest {
	w := wireRequest{Model: req.Model, Tools: req.Tools, MaxTokens: req.MaxTokens, ReasoningEffort: req.Effort, Stream: true}
	w.StreamOptions.IncludeUsage = true
	system, _ := json.Marshal(map[string]string{"role": "system", "content": req.System})
	w.Messages = append([]json.RawMessage{system}, req.Messages...)
	return w
}

type wireChunk struct {
	Choices []struct {
		Index int `json:"index"`
		Delta struct {
			Content          string `json:"content"`
			ReasoningContent string `json:"reasoning_content"`
			Reasoning        string `json:"reasoning"`
			ToolCalls        []struct {
				Index    int      `json:"index"`
				ID       string   `json:"id"`
				Function Function `json:"function"`
			} `json:"tool_calls"`
		} `json:"delta"`
		FinishReason string `json:"finish_reason"`
	} `json:"choices"`
	Usage *struct {
		PromptTokens        int64 `json:"prompt_tokens"`
		CompletionTokens    int64 `json:"completion_tokens"`
		PromptTokensDetails struct {
			CachedTokens int64 `json:"cached_tokens"`
		} `json:"prompt_tokens_details"`
	} `json:"usage"`
	Error any `json:"error"`
}
