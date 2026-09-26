// SPDX-License-Identifier: AGPL-3.0-only
// Copyright (c) 2026 VaporChain / muthu2201
// Provenance: VAPOR-6eabb1be532bdef4

package types

import (
	"fmt"

	"cosmossdk.io/math"
)

func DefaultGenesisState() *GenesisState {
	return &GenesisState{Params: DefaultParams(), CreditsMinted: math.ZeroInt()}
}

func (gs GenesisState) Validate() error {
	if err := gs.Params.Validate(); err != nil {
		return err
	}
	if gs.CreditsMinted.IsNil() || gs.CreditsMinted.IsNegative() {
		return fmt.Errorf("credits_minted must be >= 0")
	}
	for _, p := range gs.Pools {
		if !ValidPool(p.Pool) {
			return fmt.Errorf("unknown pool %q", p.Pool)
		}
		if p.Amount.IsNil() || p.Amount.IsNegative() {
			return fmt.Errorf("pool %s/%s negative", p.Pool, p.Denom)
		}
	}
	for _, c := range gs.Claimables {
		if c.Amount.IsNil() || c.Amount.IsNegative() {
			return fmt.Errorf("claimable %d/%s negative", c.AppId, c.Denom)
		}
	}
	for _, t := range gs.Tabs {
		if t.Amount.IsNil() || t.Amount.IsNegative() {
			return fmt.Errorf("tab negative")
		}
	}
	return nil
}
