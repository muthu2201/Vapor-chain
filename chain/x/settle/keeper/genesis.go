// Copyright (c) 2026 VaporChain / muthu2201. All rights reserved.
// Provenance: VAPOR-6eabb1be532bdef4

package keeper

import (
	"cosmossdk.io/collections"
	"cosmossdk.io/math"

	sdk "github.com/cosmos/cosmos-sdk/types"

	"github.com/muthu2201/vapor-chain/chain/x/settle/types"
)

func (k Keeper) InitGenesis(ctx sdk.Context, gs types.GenesisState) error {
	if err := k.Params.Set(ctx, gs.Params); err != nil {
		return err
	}
	for _, c := range gs.Claimables {
		if err := k.addClaimable(ctx, c.AppId, c.Denom, c.Amount); err != nil {
			return err
		}
	}
	for _, p := range gs.Pools {
		if err := k.addPool(ctx, p.Pool, p.Denom, p.Amount); err != nil {
			return err
		}
	}
	for _, t := range gs.Tabs {
		payer, err := sdk.AccAddressFromBech32(t.Payer)
		if err != nil {
			return err
		}
		payee, err := sdk.AccAddressFromBech32(t.Payee)
		if err != nil {
			return err
		}
		if err := k.Tabs.Set(ctx, TabKey(t.AppId, payer, payee, t.Denom), t); err != nil {
			return err
		}
		if err := k.ledger(ctx, t.Denom, t.Amount); err != nil {
			return err
		}
	}
	for _, a := range gs.Allowances {
		if err := k.Allowances.Set(ctx, collections.Join3(a.Owner, a.AppId, a.Denom), a.Amount); err != nil {
			return err
		}
	}
	for _, t := range gs.Totals {
		if err := k.Totals.Set(ctx, t.Denom, t); err != nil {
			return err
		}
	}
	for _, c := range gs.RelayerPaidThisInterval {
		if err := k.RelayerPaid.Set(ctx, c.Denom, c.Amount); err != nil {
			return err
		}
	}
	if err := k.CreditsMinted.Set(ctx, gs.CreditsMinted); err != nil {
		return err
	}
	// Record the credit supply that exists before any x/settle mint. The
	// invariant  supply <= genesis_supply + minted  catches any unauthorised
	// minting path of the gas credit.
	supply := k.bankKeeper.GetSupply(ctx, k.creditDenom).Amount
	base := supply.Sub(gs.CreditsMinted)
	if base.IsNegative() {
		base = math.ZeroInt()
	}
	if err := k.GenesisCreditSupply.Set(ctx, base); err != nil {
		return err
	}
	return k.LastPayout.Set(ctx, ctx.BlockHeight())
}

func (k Keeper) ExportGenesis(ctx sdk.Context) (*types.GenesisState, error) {
	gs := &types.GenesisState{Params: k.GetParams(ctx)}
	if err := k.Claimables.Walk(ctx, nil, func(key collections.Pair[uint64, string], v math.Int) (bool, error) {
		gs.Claimables = append(gs.Claimables, types.Claimable{AppId: key.K1(), Denom: key.K2(), Amount: v})
		return false, nil
	}); err != nil {
		return nil, err
	}
	if err := k.Pools.Walk(ctx, nil, func(key collections.Pair[string, string], v math.Int) (bool, error) {
		gs.Pools = append(gs.Pools, types.PoolBalance{Pool: key.K1(), Denom: key.K2(), Amount: v})
		return false, nil
	}); err != nil {
		return nil, err
	}
	if err := k.Tabs.Walk(ctx, nil, func(_ string, t types.Tab) (bool, error) {
		gs.Tabs = append(gs.Tabs, t)
		return false, nil
	}); err != nil {
		return nil, err
	}
	if err := k.Allowances.Walk(ctx, nil, func(key collections.Triple[string, uint64, string], v math.Int) (bool, error) {
		gs.Allowances = append(gs.Allowances, types.AppAllowance{Owner: key.K1(), AppId: key.K2(), Denom: key.K3(), Amount: v})
		return false, nil
	}); err != nil {
		return nil, err
	}
	if err := k.Totals.Walk(ctx, nil, func(_ string, t types.Totals) (bool, error) {
		gs.Totals = append(gs.Totals, t)
		return false, nil
	}); err != nil {
		return nil, err
	}
	if err := k.RelayerPaid.Walk(ctx, nil, func(d string, v math.Int) (bool, error) {
		gs.RelayerPaidThisInterval = gs.RelayerPaidThisInterval.Add(sdk.NewCoin(d, v))
		return false, nil
	}); err != nil {
		return nil, err
	}
	minted, err := k.CreditsMinted.Get(ctx)
	if err != nil {
		minted = math.ZeroInt()
	}
	gs.CreditsMinted = minted
	return gs, nil
}
