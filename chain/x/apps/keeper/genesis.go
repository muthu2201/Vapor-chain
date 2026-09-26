// SPDX-License-Identifier: AGPL-3.0-only
// Copyright (c) 2026 VaporChain / muthu2201
// Provenance: VAPOR-6eabb1be532bdef4

package keeper

import (
	"cosmossdk.io/collections"
	"cosmossdk.io/math"

	sdk "github.com/cosmos/cosmos-sdk/types"

	"github.com/muthu2201/vapor-chain/chain/x/apps/types"
)

func (k Keeper) InitGenesis(ctx sdk.Context, gs types.GenesisState) error {
	if err := k.Params.Set(ctx, gs.Params); err != nil {
		return err
	}
	if err := k.NextAppID.Set(ctx, gs.NextAppId); err != nil {
		return err
	}
	for _, a := range gs.Apps {
		if err := k.Apps.Set(ctx, a.AppId, a); err != nil {
			return err
		}
	}
	for _, b := range gs.Bindings {
		c := types.NormalizeContract(b.Contract)
		b.Contract = c
		if err := k.Bindings.Set(ctx, c, b); err != nil {
			return err
		}
		if err := k.AppContracts.Set(ctx, collections.Join(b.AppId, c)); err != nil {
			return err
		}
	}
	for _, m := range gs.PendingMoves {
		if err := k.PendingMoves.Set(ctx, m.Contract, m); err != nil {
			return err
		}
		if err := k.MovesByUnlock.Set(ctx, collections.Join(m.UnlockHeight, m.Contract)); err != nil {
			return err
		}
	}
	for _, c := range gs.PendingClaims {
		if err := k.PendingClaims.Set(ctx, collections.Join(c.AppId, c.Contract), c); err != nil {
			return err
		}
	}
	for _, s := range gs.EpochStats {
		if err := k.EpochStats.Set(ctx, collections.Join(s.Epoch, s.AppId), s); err != nil {
			return err
		}
	}
	for _, q := range gs.Quotas {
		if err := k.Quotas.Set(ctx, q.AppId, q); err != nil {
			return err
		}
	}
	for _, b := range gs.Bonds {
		if err := k.Bonds.Set(ctx, b.AppId, b.Amount); err != nil {
			return err
		}
	}
	for _, u := range gs.Unbondings {
		key := collections.Join3(u.ReleaseHeight, u.AppId, u.Owner)
		prev, err := k.Unbondings.Get(ctx, key)
		if err != nil {
			prev = math.ZeroInt()
		}
		if err := k.Unbondings.Set(ctx, key, prev.Add(u.Amount)); err != nil {
			return err
		}
	}
	if err := k.EpochNumber.Set(ctx, 0); err != nil {
		return err
	}
	return k.EpochStart.Set(ctx, ctx.BlockHeight())
}

func (k Keeper) ExportGenesis(ctx sdk.Context) (*types.GenesisState, error) {
	next, err := k.NextAppID.Peek(ctx)
	if err != nil {
		return nil, err
	}
	gs := &types.GenesisState{Params: k.GetParams(ctx), NextAppId: next}
	if err := k.Apps.Walk(ctx, nil, func(_ uint64, a types.App) (bool, error) { gs.Apps = append(gs.Apps, a); return false, nil }); err != nil {
		return nil, err
	}
	if err := k.Bindings.Walk(ctx, nil, func(_ string, b types.ContractBinding) (bool, error) {
		gs.Bindings = append(gs.Bindings, b)
		return false, nil
	}); err != nil {
		return nil, err
	}
	if err := k.PendingMoves.Walk(ctx, nil, func(_ string, m types.PendingMove) (bool, error) {
		gs.PendingMoves = append(gs.PendingMoves, m)
		return false, nil
	}); err != nil {
		return nil, err
	}
	if err := k.PendingClaims.Walk(ctx, nil, func(_ collections.Pair[uint64, string], c types.PendingClaim) (bool, error) {
		gs.PendingClaims = append(gs.PendingClaims, c)
		return false, nil
	}); err != nil {
		return nil, err
	}
	if err := k.EpochStats.Walk(ctx, nil, func(_ collections.Pair[uint64, uint64], s types.EpochStats) (bool, error) {
		gs.EpochStats = append(gs.EpochStats, s)
		return false, nil
	}); err != nil {
		return nil, err
	}
	if err := k.Quotas.Walk(ctx, nil, func(_ uint64, q types.Quota) (bool, error) { gs.Quotas = append(gs.Quotas, q); return false, nil }); err != nil {
		return nil, err
	}
	if err := k.Bonds.Walk(ctx, nil, func(id uint64, v math.Int) (bool, error) {
		gs.Bonds = append(gs.Bonds, types.AppBond{AppId: id, Amount: v})
		return false, nil
	}); err != nil {
		return nil, err
	}
	err = k.Unbondings.Walk(ctx, nil, func(key collections.Triple[int64, uint64, string], v math.Int) (bool, error) {
		gs.Unbondings = append(gs.Unbondings, types.Unbonding{AppId: key.K2(), Owner: key.K3(), Amount: v, ReleaseHeight: key.K1()})
		return false, nil
	})
	return gs, err
}
