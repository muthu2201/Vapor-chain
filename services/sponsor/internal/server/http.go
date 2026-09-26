// SPDX-License-Identifier: AGPL-3.0-only
// Copyright (c) 2026 VaporChain / muthu2201
// Provenance: VAPOR-6eabb1be532bdef4

package server

import (
	"net"
	"net/http"
	"strings"
)

// ClientIP returns the address rate limits are keyed on.
//
// X-Forwarded-For is attacker-controlled unless it was written by our own
// load balancer. It is honoured only when the direct peer is a trusted proxy,
// and then read right to left: the first hop that is not a trusted proxy is
// the real client. Anything else a client prepends is ignored, so rotating
// fake XFF values cannot mint fresh rate-limit buckets.
func ClientIP(r *http.Request, trusted []*net.IPNet) string {
	peer, _, err := net.SplitHostPort(r.RemoteAddr)
	if err != nil {
		peer = r.RemoteAddr
	}
	if !inNets(peer, trusted) {
		return peer
	}
	hops := strings.Split(r.Header.Get("X-Forwarded-For"), ",")
	for i := len(hops) - 1; i >= 0; i-- {
		h := strings.TrimSpace(hops[i])
		if h == "" {
			continue
		}
		if net.ParseIP(h) == nil {
			return peer // malformed chain: fall back to the proxy itself
		}
		if !inNets(h, trusted) {
			return h
		}
	}
	return peer
}

func inNets(ip string, nets []*net.IPNet) bool {
	p := net.ParseIP(ip)
	if p == nil {
		return false
	}
	for _, n := range nets {
		if n.Contains(p) {
			return true
		}
	}
	return false
}

// ParseCIDRs parses a comma-separated list of CIDRs or bare IPs.
func ParseCIDRs(s string) ([]*net.IPNet, error) {
	var out []*net.IPNet
	for _, part := range strings.Split(s, ",") {
		part = strings.TrimSpace(part)
		if part == "" {
			continue
		}
		if !strings.Contains(part, "/") {
			if ip := net.ParseIP(part); ip != nil && ip.To4() != nil {
				part += "/32"
			} else {
				part += "/128"
			}
		}
		_, n, err := net.ParseCIDR(part)
		if err != nil {
			return nil, err
		}
		out = append(out, n)
	}
	return out, nil
}

// CORS lets browser dapps call the API. Credentials are never allowed, so
// the wildcard default grants a web page nothing a server-side caller does
// not already have; operators can still pin origins.
func CORS(origins []string, methods string, next http.Handler) http.Handler {
	allowAll := len(origins) == 0
	allowed := map[string]bool{}
	for _, o := range origins {
		o = strings.TrimSpace(o)
		if o == "*" {
			allowAll = true
		}
		allowed[o] = true
	}
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		origin := r.Header.Get("Origin")
		h := w.Header()
		if origin != "" && (allowAll || allowed[origin]) {
			if allowAll {
				h.Set("Access-Control-Allow-Origin", "*")
			} else {
				h.Set("Access-Control-Allow-Origin", origin)
				h.Add("Vary", "Origin")
			}
			h.Set("Access-Control-Allow-Methods", methods)
			h.Set("Access-Control-Allow-Headers", "content-type")
			h.Set("Access-Control-Max-Age", "600")
		}
		if r.Method == http.MethodOptions {
			w.WriteHeader(http.StatusNoContent)
			return
		}
		next.ServeHTTP(w, r)
	})
}
