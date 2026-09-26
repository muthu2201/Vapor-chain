// SPDX-License-Identifier: AGPL-3.0-only
// Copyright (c) 2026 VaporChain / muthu2201
// Provenance: VAPOR-6eabb1be532bdef4

package rpc

import (
	"slices"
	"testing"
)

func TestEnsureNamespaceOrder(t *testing.T) {
	cases := []struct{ in, want []string }{
		{[]string{"eth", "net", "web3"}, []string{"eth", "net", "web3", "vapor"}},
		{[]string{"vapor", "eth", "net"}, []string{"eth", "net", "vapor"}},
		{[]string{"eth", "vapor", "vapor", "debug"}, []string{"eth", "debug", "vapor"}},
		{[]string{"net", "web3"}, []string{"net", "web3"}},
		{nil, []string{}},
	}
	for _, c := range cases {
		got := EnsureNamespaceOrder(c.in)
		if !slices.Equal(got, c.want) {
			t.Errorf("EnsureNamespaceOrder(%v) = %v, want %v", c.in, got, c.want)
		}
	}
}

func TestProvenanceAPI(t *testing.T) {
	p := VaporAPI{}.Provenance()
	if p.Fingerprint == "" || p.ShortID == "" || p.Owner == "" {
		t.Fatalf("provenance not embedded: %+v", p)
	}
}
