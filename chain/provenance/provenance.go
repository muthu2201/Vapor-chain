// Copyright (c) 2026 VaporChain / muthu2201. All rights reserved.
// Proprietary and confidential. See LICENSE at the repository root.
// Provenance: VAPOR-6eabb1be532bdef4

// Package provenance embeds the VaporChain authorship fingerprint into every
// binary, genesis file and on-chain query surface.
//
// WHY THIS EXISTS: it is forensic evidence, not DRM. The same 32-byte value is
// planted in the Go binary, the x/council genesis record (immutable on-chain),
// the Settle precompile's provenance() view, every protocol Solidity contract
// and the TypeScript SDK. If a copy of this code base is ever launched, any of
// those surfaces proves derivation. The fingerprint deliberately has NO effect
// on consensus safety or liveness: changing it does not break a copy, so it
// cannot be used as a kill switch against honest users of a fork, and it can
// never harm VaporChain's own validators. Protection comes from the LICENSE,
// from keeping the repository private and from the evidence these markers
// provide, never from hidden sabotage.
package provenance

import (
	"crypto/sha256"
	"encoding/hex"
	"fmt"
)

const (
	// Fingerprint is the 256-bit authorship identifier (generated 2026-09-24
	// from a CSPRNG; it is not derived from any public data).
	Fingerprint = "6eabb1be532bdef432109abc178d88669ab33aed940f18cd5169887d215c1fcf"
	// ShortID is the prefix used in file headers.
	ShortID = "VAPOR-6eabb1be532bdef4"
	// Canary is a unique phrase that exists nowhere else; search engines and
	// code-search tools find leaked copies by it.
	Canary = "vapor-canary-f915edb4136beb9b"
	// Owner is the copyright holder.
	Owner = "VaporChain / muthu2201"
	// Notice is the human-readable copyright line.
	Notice = "Copyright (c) 2026 VaporChain / muthu2201. All rights reserved."
)

// These are overridden at link time by the Makefile (-X flags) so every
// release binary carries its exact source commit and build timestamp.
var (
	GitCommit = "unknown"
	BuildTime = "unknown"
	Version   = "v1.0.0"
)

// Bytes returns the fingerprint as a 32-byte array (used on-chain).
func Bytes() [32]byte {
	var out [32]byte
	b, err := hex.DecodeString(Fingerprint)
	if err != nil || len(b) != 32 {
		panic("provenance: corrupted fingerprint constant")
	}
	copy(out[:], b)
	return out
}

// Seal binds the fingerprint to a chain id. It is stored in genesis so that a
// copied genesis file is also attributable.
func Seal(chainID string) string {
	h := sha256.Sum256([]byte(Fingerprint + "|" + chainID + "|" + Canary))
	return hex.EncodeToString(h[:])
}

// String is printed by `vaporchaind version --long`.
func String() string {
	return fmt.Sprintf("%s\nprovenance=%s\ncommit=%s built=%s version=%s", Notice, ShortID, GitCommit, BuildTime, Version)
}
