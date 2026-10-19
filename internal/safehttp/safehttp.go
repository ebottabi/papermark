/*
 * Copyright (c) 2026. Papermark - All Rights Reserved
 * Unauthorized copying of this file, via any medium is strictly prohibited
 * Proprietary and confidential
 * Written by Ebot Tabi <ebot.tabi@gmail.com>, 17/10/2026
 */

// Package safehttp fetches URLs a user chose, from a server that must not be
// turned against its own network. Every address a request dials is checked
// after DNS, on every redirect hop, so a name that resolves (or later
// rebinds) to an internal address is refused.
package safehttp

import (
	"context"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/netip"
	"net/url"
	"strconv"
	"strings"
	"sync"
	"time"
)

// Defaults for a Client built with a zero Config field.
const (
	DefaultTimeout      = 10 * time.Second
	DefaultCap          = 2 << 20
	DefaultMaxRedirects = 5
	// HostInterval is the spacing the Limiter keeps between requests to one host.
	HostInterval = 10 * time.Second
)

// ErrBlocked is wrapped by every refusal of an address or host.
var ErrBlocked = errors.New("address not allowed")

// ErrTooLarge means the body is over the client's cap.
var ErrTooLarge = errors.New("page is larger than the size limit")

// RateLimited means the host was asked too recently, or sent Retry-After.
// Nothing was fetched; retry after RetryAfter.
type RateLimited struct {
	Host       string
	RetryAfter time.Duration
}

func (e *RateLimited) Error() string {
	return fmt.Sprintf("%s is rate limited, retry in %s", e.Host, e.RetryAfter.Round(time.Second))
}

// Config sets up a Client. A zero field takes its default.
type Config struct {
	Timeout      time.Duration
	Cap          int64
	MaxRedirects int
	// DenyHosts are refused whatever they resolve to (the server's own host).
	DenyHosts []string
	// Limiter, when set, spaces requests per host and records Retry-After.
	Limiter *Limiter
	// Blocked decides which resolved addresses are refused; nil means BlockedAddr.
	Blocked func(netip.Addr) bool
	// Lookup resolves a host; nil means the system resolver.
	Lookup func(ctx context.Context, host string) ([]netip.Addr, error)
}

// Client fetches public http(s) pages.
type Client struct {
	http         *http.Client
	cap          int64
	maxRedirects int
	deny         map[string]bool
	limiter      *Limiter
	blocked      func(netip.Addr) bool
	lookup       func(ctx context.Context, host string) ([]netip.Addr, error)
}

// New returns a Client for cfg.
func New(cfg Config) *Client {
	c := &Client{cap: cfg.Cap, maxRedirects: cfg.MaxRedirects, deny: map[string]bool{}, limiter: cfg.Limiter,
		blocked: cfg.Blocked, lookup: cfg.Lookup}
	if cfg.Timeout == 0 {
		cfg.Timeout = DefaultTimeout
	}
	if c.cap == 0 {
		c.cap = DefaultCap
	}
	if c.maxRedirects == 0 {
		c.maxRedirects = DefaultMaxRedirects
	}
	if c.blocked == nil {
		c.blocked = BlockedAddr
	}
	if c.lookup == nil {
		c.lookup = func(ctx context.Context, host string) ([]netip.Addr, error) {
			return net.DefaultResolver.LookupNetIP(ctx, "ip", host)
		}
	}
	for _, h := range cfg.DenyHosts {
		if h = strings.ToLower(strings.TrimSuffix(h, ".")); h != "" {
			c.deny[h] = true
		}
	}
	dialer := &net.Dialer{Timeout: cfg.Timeout}
	c.http = &http.Client{
		Timeout: cfg.Timeout,
		Transport: &http.Transport{
			DialContext:           c.dial(dialer),
			TLSHandshakeTimeout:   cfg.Timeout,
			ResponseHeaderTimeout: cfg.Timeout,
			MaxIdleConns:          10,
			IdleConnTimeout:       30 * time.Second,
		},
		CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse },
	}
	return c
}

// dial resolves the host itself and connects to the vetted address, so the
// address checked is the address used.
func (c *Client) dial(d *net.Dialer) func(ctx context.Context, network, address string) (net.Conn, error) {
	return func(ctx context.Context, network, address string) (net.Conn, error) {
		host, port, err := net.SplitHostPort(address)
		if err != nil {
			return nil, err
		}
		addrs, err := c.resolve(ctx, host)
		if err != nil {
			return nil, err
		}
		var last error
		for _, a := range addrs {
			conn, err := d.DialContext(ctx, network, net.JoinHostPort(a.String(), port))
			if err == nil {
				return conn, nil
			}
			last = err
		}
		return nil, last
	}
}

// resolve returns host's addresses, or ErrBlocked when any of them is not public.
func (c *Client) resolve(ctx context.Context, host string) ([]netip.Addr, error) {
	var addrs []netip.Addr
	if a, err := netip.ParseAddr(host); err == nil {
		addrs = []netip.Addr{a}
	} else if addrs, err = c.lookup(ctx, host); err != nil {
		return nil, err
	}
	if len(addrs) == 0 {
		return nil, fmt.Errorf("%s did not resolve", host)
	}
	for _, a := range addrs {
		if c.blocked(a.Unmap()) {
			return nil, fmt.Errorf("%w: %s resolves to a private or reserved address", ErrBlocked, host)
		}
	}
	return addrs, nil
}

// Check rejects a URL the client would never fetch, before any request:
// not http(s), an internal or denied host, or a name that resolves to a
// non-public address. A name that does not resolve passes; the fetch reports it.
func (c *Client) Check(ctx context.Context, raw string) (*url.URL, error) {
	u, err := url.Parse(strings.TrimSpace(raw))
	if err != nil || u.Host == "" {
		return nil, errors.New("not a valid URL")
	}
	if u.Scheme != "http" && u.Scheme != "https" {
		return nil, fmt.Errorf("url must be http or https, got %q", u.Scheme)
	}
	host := strings.ToLower(strings.TrimSuffix(u.Hostname(), "."))
	if BlockedHost(host) || c.deny[host] {
		return nil, fmt.Errorf("%w: %s is an internal host", ErrBlocked, host)
	}
	if _, err := c.resolve(ctx, host); errors.Is(err, ErrBlocked) {
		return nil, err
	}
	return u, nil
}

// Conditions are the validators of the copy the caller already has.
type Conditions struct{ ETag, LastModified string }

// Response is a fetched page. NotModified is true for a 304, with no body.
type Response struct {
	URL          *url.URL
	Status       int
	Body         []byte
	ContentType  string
	ETag         string
	LastModified string
	NotModified  bool
}

// Get fetches raw, following redirects by hand and checking every hop. A
// status of 400 or more is returned as a Response, not an error. Accept
// is sent as given.
func (c *Client) Get(ctx context.Context, raw, accept string, cond Conditions) (*Response, error) {
	u, err := c.Check(ctx, raw)
	if err != nil {
		return nil, err
	}
	booked := ""
	for hop := 0; ; hop++ {
		host := strings.ToLower(u.Hostname())
		if c.limiter != nil && host != booked {
			booked = host
			if wait := c.limiter.Take(host, time.Now()); wait > 0 {
				return nil, &RateLimited{Host: host, RetryAfter: wait}
			}
		}
		req, err := http.NewRequestWithContext(ctx, http.MethodGet, u.String(), nil)
		if err != nil {
			return nil, errors.New("not a valid URL")
		}
		req.Header.Set("Accept", accept)
		req.Header.Set("User-Agent", "Keen/1.0")
		if hop == 0 && cond.ETag != "" {
			req.Header.Set("If-None-Match", cond.ETag)
		}
		if hop == 0 && cond.LastModified != "" {
			req.Header.Set("If-Modified-Since", cond.LastModified)
		}
		res, err := c.http.Do(req)
		if err != nil {
			if errors.Is(err, ErrBlocked) {
				return nil, fmt.Errorf("%w: %s resolves to a private or reserved address", ErrBlocked, host)
			}
			return nil, fmt.Errorf("could not fetch %s", u.String())
		}
		if res.StatusCode == http.StatusTooManyRequests || res.StatusCode == http.StatusServiceUnavailable {
			res.Body.Close()
			if after := retryAfter(res.Header.Get("Retry-After")); after > 0 {
				if c.limiter != nil {
					c.limiter.Defer(host, time.Now().Add(after))
				}
				return nil, &RateLimited{Host: host, RetryAfter: after}
			}
		}
		loc := res.Header.Get("Location")
		if res.StatusCode >= 300 && res.StatusCode < 400 && res.StatusCode != http.StatusNotModified && loc != "" {
			res.Body.Close()
			if hop == c.maxRedirects {
				return nil, fmt.Errorf("more than %d redirects", c.maxRedirects)
			}
			next, err := u.Parse(loc)
			if err != nil {
				return nil, errors.New("bad redirect location")
			}
			if u, err = c.Check(ctx, next.String()); err != nil {
				return nil, err
			}
			continue
		}
		defer res.Body.Close()
		out := &Response{URL: u, Status: res.StatusCode, ContentType: res.Header.Get("Content-Type"),
			ETag: res.Header.Get("ETag"), LastModified: res.Header.Get("Last-Modified"), NotModified: res.StatusCode == http.StatusNotModified}
		if out.NotModified {
			return out, nil
		}
		if out.Body, err = io.ReadAll(io.LimitReader(res.Body, c.cap+1)); err != nil {
			return nil, fmt.Errorf("could not read %s", u.String())
		}
		if int64(len(out.Body)) > c.cap {
			return nil, fmt.Errorf("%s: %w (%d bytes)", u.String(), ErrTooLarge, c.cap)
		}
		return out, nil
	}
}

func retryAfter(v string) time.Duration {
	if n, err := strconv.Atoi(strings.TrimSpace(v)); err == nil && n >= 0 {
		return time.Duration(n) * time.Second
	}
	if t, err := http.ParseTime(v); err == nil {
		return max(0, time.Until(t))
	}
	return 0
}

// Limiter spaces requests to one host. It is safe for concurrent use and is
// meant to be shared by every fetcher in the process.
type Limiter struct {
	mu       sync.Mutex
	interval time.Duration
	next     map[string]time.Time
}

// NewLimiter allows one request per interval per host.
func NewLimiter(interval time.Duration) *Limiter {
	return &Limiter{interval: interval, next: map[string]time.Time{}}
}

// Take books a request to host at now and returns 0, or returns how long to
// wait first and books nothing.
func (l *Limiter) Take(host string, now time.Time) time.Duration {
	l.mu.Lock()
	defer l.mu.Unlock()
	if at := l.next[host]; at.After(now) {
		return at.Sub(now)
	}
	l.next[host] = now.Add(l.interval)
	if len(l.next) > 4096 {
		for h, at := range l.next {
			if !at.After(now) {
				delete(l.next, h)
			}
		}
	}
	return 0
}

// Defer holds host back until at (a Retry-After).
func (l *Limiter) Defer(host string, at time.Time) {
	l.mu.Lock()
	defer l.mu.Unlock()
	if at.After(l.next[host]) {
		l.next[host] = at
	}
}

// BlockedAddr reports whether a is not a public unicast address.
func BlockedAddr(a netip.Addr) bool {
	if a.Is4In6() || a.IsLoopback() || a.IsPrivate() || a.IsLinkLocalUnicast() || a.IsLinkLocalMulticast() ||
		a.IsInterfaceLocalMulticast() || a.IsMulticast() || a.IsUnspecified() {
		return true
	}
	v4 := func(a, b, c, d byte, bits int) netip.Prefix {
		return netip.PrefixFrom(netip.AddrFrom4([4]byte{a, b, c, d}), bits)
	}
	v6 := func(hi uint16, bits int) netip.Prefix {
		return netip.PrefixFrom(netip.AddrFrom16([16]byte{byte(hi >> 8), byte(hi)}), bits)
	}
	reserved := []netip.Prefix{
		v4(0, 0, 0, 0, 8), v4(100, 64, 0, 0, 10), v4(192, 0, 0, 0, 24), v4(192, 0, 2, 0, 24),
		v4(198, 18, 0, 0, 15), v4(198, 51, 100, 0, 24), v4(203, 0, 113, 0, 24), v4(240, 0, 0, 0, 4),
		// ::/8, 64:ff9b::/96 and 2002::/16 embed an IPv4 address, which may be internal.
		v6(0x0000, 8), v6(0x0100, 16), v6(0x2002, 16),
		netip.PrefixFrom(netip.AddrFrom16([16]byte{0x20, 0x01, 0x0d, 0xb8}), 32),
	}
	for _, p := range reserved {
		if p.Contains(a) {
			return true
		}
	}
	return false
}

// BlockedHost reports whether h names an internal host by its spelling.
func BlockedHost(h string) bool {
	h = strings.ToLower(strings.TrimSuffix(h, "."))
	if h == "" || h == "localhost" || h == "metadata.google.internal" || h == "metadata.goog" {
		return true
	}
	for _, s := range []string{".local", ".localhost", ".internal", ".lan"} {
		if strings.HasSuffix(h, s) {
			return true
		}
	}
	return false
}
