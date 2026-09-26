// SPDX-License-Identifier: AGPL-3.0-only
// Copyright (c) 2026 VaporChain / muthu2201
// Provenance: VAPOR-6eabb1be532bdef4

package server

import (
	"fmt"
	"net/http"
	"net/http/httptest"
	"testing"

	"golang.org/x/time/rate"
)

func TestClientIPIgnoresSpoofedXFF(t *testing.T) {
	lb, _ := ParseCIDRs("10.0.0.0/8")
	cases := []struct {
		name, remote, xff string
		trusted           bool
		want              string
	}{
		{"direct client, spoofed XFF ignored", "203.0.113.9:5555", "1.2.3.4", false, "203.0.113.9"},
		{"behind trusted LB", "10.0.0.5:443", "198.51.100.7", true, "198.51.100.7"},
		{"client prepends junk, LB appends real ip", "10.0.0.5:443", "6.6.6.6, 198.51.100.7", true, "198.51.100.7"},
		{"two trusted hops", "10.0.0.5:443", "198.51.100.7, 10.1.2.3", true, "198.51.100.7"},
		{"malformed hop", "10.0.0.5:443", "not-an-ip", true, "10.0.0.5"},
		{"trusted LB, no XFF", "10.0.0.5:443", "", true, "10.0.0.5"},
	}
	for _, c := range cases {
		r := httptest.NewRequest(http.MethodPost, "/", nil)
		r.RemoteAddr = c.remote
		if c.xff != "" {
			r.Header.Set("X-Forwarded-For", c.xff)
		}
		nets := lb
		if !c.trusted {
			nets = nil
		}
		if got := ClientIP(r, nets); got != c.want {
			t.Errorf("%s: got %s want %s", c.name, got, c.want)
		}
	}
}

func TestParseCIDRs(t *testing.T) {
	n, err := ParseCIDRs("10.0.0.0/8, 192.168.1.1 ,::1")
	if err != nil || len(n) != 3 {
		t.Fatalf("%v %v", n, err)
	}
	if _, err := ParseCIDRs("nope/99"); err == nil {
		t.Fatal("bad cidr accepted")
	}
}

func TestCORS(t *testing.T) {
	ok := http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) { w.WriteHeader(200) })

	pre := httptest.NewRequest(http.MethodOptions, "/", nil)
	pre.Header.Set("Origin", "https://shop.example")
	w := httptest.NewRecorder()
	CORS(nil, "POST, OPTIONS", ok).ServeHTTP(w, pre)
	if w.Code != 204 || w.Header().Get("Access-Control-Allow-Origin") != "*" || w.Header().Get("Access-Control-Allow-Headers") != "content-type" {
		t.Fatalf("preflight: %d %v", w.Code, w.Header())
	}
	if w.Header().Get("Access-Control-Allow-Credentials") != "" {
		t.Fatal("credentials must never be allowed")
	}

	pinned := CORS([]string{"https://shop.example"}, "POST", ok)
	for origin, want := range map[string]string{"https://shop.example": "https://shop.example", "https://evil.example": ""} {
		r := httptest.NewRequest(http.MethodPost, "/", nil)
		r.Header.Set("Origin", origin)
		w := httptest.NewRecorder()
		pinned.ServeHTTP(w, r)
		if got := w.Header().Get("Access-Control-Allow-Origin"); got != want {
			t.Errorf("origin %s: ACAO %q want %q", origin, got, want)
		}
	}
}

func TestEvictionKeepsThrottledClients(t *testing.T) {
	s := &Server{cfg: Config{RatePerSecond: 0.001, Burst: 2}, limiters: map[string]*rate.Limiter{}}
	// an abusive client drains its bucket
	abuser := s.limiter("198.51.100.7")
	abuser.Allow()
	abuser.Allow()
	// fill the table with idle clients up to the cap
	for i := 0; i < maxLimiters-1; i++ {
		s.limiters[fmt.Sprintf("idle-%d", i)] = rate.NewLimiter(rate.Limit(0.001), 2)
	}
	s.limiter("203.0.113.1") // triggers eviction
	if s.limiters["198.51.100.7"] != abuser {
		t.Fatal("eviction reset a throttled client's bucket")
	}
	if abuser.Allow() {
		t.Fatal("throttled client regained budget")
	}
	if len(s.limiters) > 3 {
		t.Fatalf("idle limiters not evicted: %d left", len(s.limiters))
	}
}
