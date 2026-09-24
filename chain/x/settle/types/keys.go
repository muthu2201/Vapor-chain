// Copyright (c) 2026 VaporChain / muthu2201. All rights reserved.
// Proprietary and confidential. See LICENSE at the repository root.
// Provenance: VAPOR-6eabb1be532bdef4

package types

import "cosmossdk.io/collections"

const (
	ModuleName = "settle"
	StoreKey   = ModuleName

	PoolValidator = "validator"
	PoolRelayer   = "relayer"
	PoolTreasury  = "treasury"

	MaxBps = 10_000
)

var (
	ParamsKey              = collections.NewPrefix(0)
	ClaimablesKey          = collections.NewPrefix(1)
	PoolsKey               = collections.NewPrefix(2)
	TabsKey                = collections.NewPrefix(3)
	AllowancesKey          = collections.NewPrefix(4)
	TotalsKey              = collections.NewPrefix(5)
	CreditsMintedKey       = collections.NewPrefix(6)
	GenesisCreditSupplyKey = collections.NewPrefix(7)
	LedgerTotalKey         = collections.NewPrefix(8)
	RelayerPaidKey         = collections.NewPrefix(9)
	LastPayoutKey          = collections.NewPrefix(10)
)

// ValidPool reports whether name is a protocol pool.
func ValidPool(name string) bool {
	return name == PoolValidator || name == PoolRelayer || name == PoolTreasury
}
