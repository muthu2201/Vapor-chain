// SPDX-License-Identifier: AGPL-3.0-only
// Copyright (c) 2026 VaporChain / muthu2201
// Provenance: VAPOR-6eabb1be532bdef4

package keeper

import (
	sdk "github.com/cosmos/cosmos-sdk/types"

	"github.com/muthu2201/vapor-chain/chain/x/council/types"
)

func (k Keeper) InitGenesis(ctx sdk.Context, gs types.GenesisState) error {
	if err := k.Params.Set(ctx, gs.Params); err != nil {
		return err
	}
	for _, a := range gs.Admissions {
		if err := k.Admissions.Set(ctx, a.Operator, a); err != nil {
			return err
		}
	}
	for _, p := range gs.Pauses {
		if err := k.Pauses.Set(ctx, p.Target, p); err != nil {
			return err
		}
	}
	prov := gs.Provenance
	if prov.Fingerprint == "" {
		prov = DefaultProvenance(ctx.ChainID())
	}
	return k.Provenance.Set(ctx, prov)
}

func (k Keeper) ExportGenesis(ctx sdk.Context) (*types.GenesisState, error) {
	gs := types.GenesisState{Params: k.GetParams(ctx), Provenance: k.ProvenanceRecord(ctx)}
	err := k.Admissions.Walk(ctx, nil, func(_ string, a types.Admission) (bool, error) {
		gs.Admissions = append(gs.Admissions, a)
		return false, nil
	})
	if err != nil {
		return nil, err
	}
	err = k.Pauses.Walk(ctx, nil, func(_ string, p types.Pause) (bool, error) {
		gs.Pauses = append(gs.Pauses, p)
		return false, nil
	})
	return &gs, err
}
