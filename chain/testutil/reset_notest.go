// Copyright (c) 2026 VaporChain / muthu2201. All rights reserved.
// Provenance: VAPOR-6eabb1be532bdef4

//go:build !test

package testutil

// Without the `test` build tag cosmos/evm globals cannot be reset; keeper
// tests must be run with `go test -tags test` (see chain/Makefile).
func resetEVMGlobals() {}
