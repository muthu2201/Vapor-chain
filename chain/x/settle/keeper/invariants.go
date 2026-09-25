// Copyright (c) 2026 VaporChain / muthu2201. All rights reserved.
// Proprietary and confidential. See LICENSE at the repository root.
// Provenance: VAPOR-6eabb1be532bdef4

package keeper

import (
	"fmt"

	"cosmossdk.io/collections"
	"cosmossdk.io/math"

	sdk "github.com/cosmos/cosmos-sdk/types"

	"github.com/muthu2201/vapor-chain/chain/x/settle/types"
)

// RecountLedger recomputes Σclaimables + Σpools + Σtabs per denom from scratch.
// Used by tests/simulations (O(state)); the chain itself keeps LedgerTotal
// incrementally and only compares it against the bank (O(denoms)).
func (k Keeper) RecountLedger(ctx sdk.Context) (map[string]math.Int, error) {
	out := map[string]math.Int{}
	add := func(d string, v math.Int) {
		if cur, ok := out[d]; ok {
			out[d] = cur.Add(v)
		} else {
			out[d] = v
		}
	}
	if err := k.Claimables.Walk(ctx, nil, func(key collections.Pair[uint64, string], v math.Int) (bool, error) {
		add(key.K2(), v)
		return false, nil
	}); err != nil {
		return nil, err
	}
	if err := k.Pools.Walk(ctx, nil, func(key collections.Pair[string, string], v math.Int) (bool, error) {
		add(key.K2(), v)
		return false, nil
	}); err != nil {
		return nil, err
	}
	err := k.Tabs.Walk(ctx, nil, func(_ string, t types.Tab) (bool, error) {
		add(t.Denom, t.Amount)
		return false, nil
	})
	return out, err
}

// CheckInvariants returns a description of every violated invariant:
//  1. solvency:  bank(settle, d) >= LedgerTotal[d]  for every denom
//  2. credits:   supply(acredit) <= genesis_supply + credits_minted
func (k Keeper) CheckInvariants(ctx sdk.Context) []string {
	var broken []string
	mod := k.moduleAddr()
	_ = k.LedgerTotal.Walk(ctx, nil, func(denom string, owed math.Int) (bool, error) {
		bal := k.bankKeeper.GetBalance(ctx, mod, denom).Amount
		if bal.LT(owed) {
			broken = append(broken, fmt.Sprintf("solvency: %s balance %s < ledger %s", denom, bal, owed))
		}
		return false, nil
	})
	gen, err := k.GenesisCreditSupply.Get(ctx)
	if err == nil {
		minted, err := k.CreditsMinted.Get(ctx)
		if err != nil {
			minted = math.ZeroInt()
		}
		supply := k.bankKeeper.GetSupply(ctx, k.creditDenom).Amount
		if supply.GT(gen.Add(minted)) {
			broken = append(broken, fmt.Sprintf("credits: supply %s > genesis %s + minted %s", supply, gen, minted))
		}
	}
	return broken
}

// runInvariants enters SAFE MODE on breach: Settle is paused (so no more money
// moves through a possibly-buggy path) and an alertable event is emitted. The
// chain itself keeps producing blocks.
func (k Keeper) runInvariants(ctx sdk.Context) {
	if ctx.BlockHeight()%InvariantCheckInterval != 0 {
		return
	}
	broken := k.CheckInvariants(ctx)
	if len(broken) == 0 {
		return
	}
	for _, b := range broken {
		k.Logger(ctx).Error("SETTLE INVARIANT BROKEN — entering safe mode", "detail", b)
		ctx.EventManager().EmitEvent(sdk.NewEvent("settle_invariant_broken", sdk.NewAttribute("detail", b)))
	}
	if err := k.councilKeeper.SetPause(ctx, "settle", types.ModuleName, "invariant breach: "+broken[0], 0); err != nil {
		k.Logger(ctx).Error("failed to enter safe mode", "err", err)
	}
}

// BeginBlock burns the gas fees collected in the previous block before
// x/distribution can allocate them to validators (settle is ordered ahead of
// distribution in the begin-blockers). See BurnGasFees.
func (k Keeper) BeginBlock(ctx sdk.Context) error {
	k.BurnGasFees(ctx)
	return nil
}

// EndBlock runs validator payouts and the periodic invariant checks.
func (k Keeper) EndBlock(ctx sdk.Context) error {
	if err := k.PayoutValidators(ctx); err != nil {
		// a payout failure must never halt the chain; funds stay in the pool
		k.Logger(ctx).Error("validator payout failed", "err", err)
	}
	k.runInvariants(ctx)
	return nil
}
