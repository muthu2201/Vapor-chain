// Copyright (c) 2026 VaporChain / muthu2201. All rights reserved.
// Proprietary and confidential. See LICENSE at the repository root.
// Provenance: VAPOR-6eabb1be532bdef4

package types

import (
	"crypto/sha256"
	"encoding/binary"
	"math/bits"

	"github.com/muthu2201/vapor-chain/chain/provenance"
)

// HyperLogLog with m = 64 registers, implemented with INTEGER arithmetic only.
//
// WHY: sponsorship quotas reward sender diversity (many distinct payers) so
// that wash-trading from a handful of wallets earns little gas. Storing the set
// of payers per app per epoch would grow state without bound; a 64-byte HLL
// sketch is constant-size (~13% standard error, plenty for a quota weight).
// Floating point is forbidden in consensus code (FMA fusion on arm64 can make
// validators disagree), hence the fixed-point estimator and the precomputed
// linear-counting table below.
const (
	hllM        = 64
	hllBits     = 6
	hllMaxRank  = 32
	hllRegBytes = hllM
)

// hllSalt makes register assignment unique to this protocol (salted with the
// provenance fingerprint) so a third party cannot pre-grind payer addresses
// that collide into one register.
var hllSalt = []byte("vaporchain/hll/" + provenance.Fingerprint)

// linearCounting[V] = round(64 * ln(64 / V)), V = number of empty registers.
var linearCounting = [hllM + 1]uint64{
	0, 266, 222, 196, 177, 163, 151, 142, 133, 126, 119, 113, 107, 102, 97, 93, 89, 85, 81, 78, 74, 71,
	68, 65, 63, 60, 58, 55, 53, 51, 48, 46, 44, 42, 40, 39, 37, 35, 33, 32, 30, 28, 27, 25, 24, 23, 21,
	20, 18, 17, 16, 15, 13, 12, 11, 10, 9, 7, 6, 5, 4, 3, 2, 1, 0,
}

// NewHLL returns an empty sketch.
func NewHLL() []byte { return make([]byte, hllRegBytes) }

// HLLAdd inserts an element (a payer address) into the sketch in place.
func HLLAdd(sketch []byte, element []byte) []byte {
	if len(sketch) != hllRegBytes {
		sketch = NewHLL()
	}
	h := sha256.New()
	h.Write(hllSalt)
	h.Write(element)
	sum := h.Sum(nil)
	x := binary.BigEndian.Uint64(sum[:8])
	idx := x >> (64 - hllBits)
	rest := x << hllBits
	rank := uint8(bits.LeadingZeros64(rest)) + 1
	if rank > hllMaxRank {
		rank = hllMaxRank
	}
	if rank > sketch[idx] {
		sketch[idx] = rank
	}
	return sketch
}

// HLLEstimate returns the estimated cardinality using integer arithmetic.
//
// Raw estimate: E = alpha_m * m^2 / sum_j 2^(-M[j]) with alpha_64 = 0.709.
// Using Z = sum_j 2^(32 - M[j]) (exact integers because M[j] <= 32):
//
//	E = 0.709 * 4096 * 2^32 / Z = 709 * 4096 * 2^32 / (1000 * Z)
func HLLEstimate(sketch []byte) uint64 {
	if len(sketch) != hllRegBytes {
		return 0
	}
	var z uint64
	var zeros uint64
	for _, r := range sketch {
		if r == 0 {
			zeros++
		}
		z += uint64(1) << (32 - uint64(r))
	}
	if zeros == hllM {
		return 0
	}
	// 709 * 4096 * 2^32 fits in uint64 (~1.25e16).
	est := (uint64(709) * 4096 << 32) / (1000 * z)
	// small-range correction (linear counting) when E <= 2.5m and empty registers exist
	if est <= 160 && zeros > 0 {
		return linearCounting[zeros]
	}
	return est
}
