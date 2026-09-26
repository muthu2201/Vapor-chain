// SPDX-License-Identifier: AGPL-3.0-only
// Copyright (c) 2026 VaporChain / muthu2201
// Provenance: VAPOR-6eabb1be532bdef4

package types

import "cosmossdk.io/collections"

const (
	ModuleName = "apps"
	StoreKey   = ModuleName
	// NoApp is the attribution bucket for payments not made by a registered
	// contract. App ids start at 1.
	NoApp uint64 = 0
)

var (
	ParamsKey        = collections.NewPrefix(0)
	NextAppIDKey     = collections.NewPrefix(1)
	AppsKey          = collections.NewPrefix(2)
	BindingsKey      = collections.NewPrefix(3)
	AppContractsKey  = collections.NewPrefix(4)
	PendingMovesKey  = collections.NewPrefix(5)
	MovesByUnlockKey = collections.NewPrefix(6)
	PendingClaimsKey = collections.NewPrefix(7)
	EpochStatsKey    = collections.NewPrefix(8)
	QuotasKey        = collections.NewPrefix(9)
	// 10 held domain attestations (human attestors, removed); never reuse it.
	EpochNumberKey = collections.NewPrefix(11)
	EpochStartKey  = collections.NewPrefix(12)
	BondsKey       = collections.NewPrefix(13)
	UnbondingsKey  = collections.NewPrefix(14)
)
