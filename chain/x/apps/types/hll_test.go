// Copyright (c) 2026 VaporChain / muthu2201. All rights reserved.
// Provenance: VAPOR-6eabb1be532bdef4

package types

import (
	"encoding/binary"
	"testing"
)

func TestHLLEstimateAccuracy(t *testing.T) {
	for _, n := range []int{1, 5, 20, 100, 1_000, 10_000, 100_000} {
		s := NewHLL()
		for i := 0; i < n; i++ {
			var b [8]byte
			binary.BigEndian.PutUint64(b[:], uint64(i))
			s = HLLAdd(s, b[:])
		}
		est := HLLEstimate(s)
		lo, hi := float64(n)*0.6, float64(n)*1.4+3
		if float64(est) < lo || float64(est) > hi {
			t.Fatalf("n=%d estimate=%d outside [%.0f,%.0f]", n, est, lo, hi)
		}
	}
}

func TestHLLDuplicatesDoNotInflate(t *testing.T) {
	s := NewHLL()
	for i := 0; i < 100_000; i++ {
		s = HLLAdd(s, []byte("same-payer"))
	}
	if est := HLLEstimate(s); est > 2 {
		t.Fatalf("100k payments from one payer estimated as %d unique payers", est)
	}
}

func TestHLLDeterministic(t *testing.T) {
	a, b := NewHLL(), NewHLL()
	for i := 0; i < 5000; i++ {
		var k [8]byte
		binary.BigEndian.PutUint64(k[:], uint64(i*7919))
		a = HLLAdd(a, k[:])
	}
	for i := 4999; i >= 0; i-- { // insertion order must not matter
		var k [8]byte
		binary.BigEndian.PutUint64(k[:], uint64(i*7919))
		b = HLLAdd(b, k[:])
	}
	if string(a) != string(b) || HLLEstimate(a) != HLLEstimate(b) {
		t.Fatal("HLL is not order-independent")
	}
}
