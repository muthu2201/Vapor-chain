// SPDX-License-Identifier: AGPL-3.0-only
// Copyright (c) 2026 VaporChain / muthu2201
// Provenance: VAPOR-6eabb1be532bdef4

//go:build test

package testutil

import evmtypes "github.com/cosmos/evm/x/vm/types"

// resetEVMGlobals clears cosmos/evm's process-wide EVM configuration so that
// every test can boot a fresh app (only possible under the `test` build tag).
func resetEVMGlobals() { evmtypes.NewEVMConfigurator().ResetTestConfig() }
