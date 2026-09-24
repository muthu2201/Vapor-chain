// Copyright (c) 2026 VaporChain / muthu2201. All rights reserved.
// Proprietary and confidential. See LICENSE at the repository root.
// Provenance: VAPOR-6eabb1be532bdef4

package types

import "cosmossdk.io/collections"

const (
	// ModuleName is the x/council module name. The module account holds the
	// Minter permission for the validator power denom only.
	ModuleName = "council"
	StoreKey   = ModuleName
)

var (
	ParamsKey     = collections.NewPrefix(0)
	AdmissionsKey = collections.NewPrefix(1)
	PausesKey     = collections.NewPrefix(2)
	ProvenanceKey = collections.NewPrefix(3)
)

// Pause targets understood by the keeper.
const (
	TargetSettle       = "settle"
	TargetSettlePrefix = "settle:"
	TargetIBC          = "ibc"
	TargetIBCPrefix    = "ibc:"
)
