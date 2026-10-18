/*
 * Copyright (c) 2026. Papermark - All Rights Reserved
 * Unauthorized copying of this file, via any medium is strictly prohibited
 * Proprietary and confidential
 * Written by Ebot Tabi <ebot.tabi@gmail.com>, 17/10/2026
 */
package safehttp

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"net/netip"
	"net/url"
	"strings"
	"testing"
	"time"
)

func TestCheckRefusesInternalTargets(t *testing.T) {
	c := New(Config{DenyHosts: []string{"keen.example.com"}, Lookup: func(_ context.Context, host string) ([]netip.Addr, error) {
		if host == "rebind.test" {
			return []netip.Addr{netip.MustParseAddr("10.1.2.3")}, nil
		}
		return []netip.Addr{netip.MustParseAddr("93.184.216.34")}, nil
	}})
	for _, raw := range []string{
		"ftp://example.com/x", "file:///etc/passwd", "javascript:alert(1)", "not a url", "http://",
		"http://localhost/", "http://127.0.0.1:8080/", "http://[::1]/", "http://0.0.0.0/",
		"http://10.0.0.5/", "http://192.168.1.1/", "http://172.16.0.1/", "http://100.64.0.1/",
		"http://169.254.169.254/latest/meta-data/", "http://metadata.google.internal/", "http://[fe80::1]/", "http://[fd00::1]/",
		"http://[::ffff:127.0.0.1]/", "http://printer.local/", "http://KEEN.example.com./", "http://rebind.test/",
	} {
		if _, err := c.Check(context.Background(), raw); err == nil {
			t.Errorf("%s was accepted", raw)
		}
	}
	for _, raw := range []string{"https://example.com/a?b=1", "http://example.com:8080/"} {
		if _, err := c.Check(context.Background(), raw); err != nil {
			t.Errorf("%s: %v", raw, err)
		}
	}
}

// local serves handler on loopback and returns a client that may reach it
// under the names pub.test and priv.test, where only priv.test is private.
func local(t *testing.T, handler http.HandlerFunc, cfg Config) (*Client, string) {
	t.Helper()
	srv := httptest.NewServer(handler)
	t.Cleanup(srv.Close)
	u, _ := url.Parse(srv.URL)
	cfg.Blocked = func(a netip.Addr) bool { return a == netip.MustParseAddr("10.9.9.9") }
	cfg.Lookup = func(_ context.Context, host string) ([]netip.Addr, error) {
		if host == "priv.test" {
			return []netip.Addr{netip.MustParseAddr("10.9.9.9")}, nil
		}
		return []netip.Addr{netip.MustParseAddr("127.0.0.1")}, nil
	}
	return New(cfg), u.Port()
}

func TestGetSendsValidatorsAndTreats304AsNoChange(t *testing.T) {
	c, port := local(t, func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("If-None-Match") == `"v1"` && r.Header.Get("If-Modified-Since") == "Mon, 01 Jan 2026 00:00:00 GMT" {
			w.WriteHeader(http.StatusNotModified)
			return
		}
		w.Header().Set("ETag", `"v1"`)
		w.Header().Set("Last-Modified", "Mon, 01 Jan 2026 00:00:00 GMT")
		fmt.Fprint(w, "<html>hi</html>")
	}, Config{})
	res, err := c.Get(context.Background(), "http://pub.test:"+port+"/", "text/html", Conditions{})
	if err != nil || res.NotModified || string(res.Body) != "<html>hi</html>" || res.ETag != `"v1"` {
		t.Fatalf("first: %+v %v", res, err)
	}
	res, err = c.Get(context.Background(), "http://pub.test:"+port+"/", "text/html", Conditions{ETag: res.ETag, LastModified: res.LastModified})
	if err != nil || !res.NotModified || len(res.Body) != 0 {
		t.Fatalf("second: %+v %v", res, err)
	}
}

func TestGetRefusesRedirectToPrivateAddress(t *testing.T) {
	var port string
	c, p := local(t, func(w http.ResponseWriter, r *http.Request) {
		http.Redirect(w, r, "http://priv.test:"+port+"/secret", http.StatusFound)
	}, Config{})
	port = p
	if _, err := c.Get(context.Background(), "http://pub.test:"+port+"/", "*/*", Conditions{}); !errors.Is(err, ErrBlocked) {
		t.Fatalf("redirect to a private name: %v", err)
	}
}

func TestGetRefusesRedirectToLiteralLoopback(t *testing.T) {
	c, port := local(t, func(w http.ResponseWriter, r *http.Request) {
		http.Redirect(w, r, "http://127.0.0.1:1/", http.StatusFound)
	}, Config{Blocked: nil})
	c.blocked = BlockedAddr
	if _, err := c.Get(context.Background(), "http://pub.test:"+port+"/", "*/*", Conditions{}); err == nil {
		t.Fatal("redirect to 127.0.0.1 was followed")
	}
}

func TestGetFollowsSameHostRedirectsWithOneRequestSlot(t *testing.T) {
	var port string
	c, p := local(t, func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/old" {
			http.Redirect(w, r, "/new", http.StatusMovedPermanently)
			return
		}
		fmt.Fprint(w, "new")
	}, Config{Limiter: NewLimiter(HostInterval)})
	port = p
	res, err := c.Get(context.Background(), "http://pub.test:"+port+"/old", "*/*", Conditions{})
	if err != nil || string(res.Body) != "new" {
		t.Fatalf("%+v %v", res, err)
	}
}

func TestGetCapsBodySize(t *testing.T) {
	c, port := local(t, func(w http.ResponseWriter, r *http.Request) { fmt.Fprint(w, strings.Repeat("x", 5000)) }, Config{Cap: 1000})
	if _, err := c.Get(context.Background(), "http://pub.test:"+port+"/", "*/*", Conditions{}); !errors.Is(err, ErrTooLarge) {
		t.Fatalf("err: %v", err)
	}
}

func TestGetLimitsRequestsPerHost(t *testing.T) {
	hits := 0
	c, port := local(t, func(w http.ResponseWriter, r *http.Request) { hits++; fmt.Fprint(w, "ok") }, Config{Limiter: NewLimiter(HostInterval)})
	if _, err := c.Get(context.Background(), "http://pub.test:"+port+"/", "*/*", Conditions{}); err != nil {
		t.Fatal(err)
	}
	var rl *RateLimited
	if _, err := c.Get(context.Background(), "http://pub.test:"+port+"/", "*/*", Conditions{}); !errors.As(err, &rl) || rl.RetryAfter <= 0 || rl.RetryAfter > HostInterval {
		t.Fatalf("second request: %v", err)
	}
	if hits != 1 {
		t.Fatalf("server saw %d requests", hits)
	}
}

func TestGetHonoursRetryAfter(t *testing.T) {
	c, port := local(t, func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Retry-After", "120")
		w.WriteHeader(http.StatusTooManyRequests)
	}, Config{Limiter: NewLimiter(time.Second)})
	var rl *RateLimited
	if _, err := c.Get(context.Background(), "http://pub.test:"+port+"/", "*/*", Conditions{}); !errors.As(err, &rl) || rl.RetryAfter != 120*time.Second {
		t.Fatalf("first: %v", err)
	}
	if _, err := c.Get(context.Background(), "http://pub.test:"+port+"/", "*/*", Conditions{}); !errors.As(err, &rl) || rl.RetryAfter < 100*time.Second {
		t.Fatalf("second request ignored Retry-After: %v", err)
	}
}

func TestLimiter(t *testing.T) {
	l := NewLimiter(10 * time.Second)
	now := time.Now()
	if l.Take("a", now) != 0 || l.Take("a", now.Add(time.Second)) != 9*time.Second || l.Take("b", now) != 0 || l.Take("a", now.Add(10*time.Second)) != 0 {
		t.Fatal("limiter spacing")
	}
}
