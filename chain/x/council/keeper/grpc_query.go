// Copyright (c) 2026 VaporChain / muthu2201. All rights reserved.
// Provenance: VAPOR-6eabb1be532bdef4

package keeper

import (
	"context"

	sdk "github.com/cosmos/cosmos-sdk/types"

	"github.com/muthu2201/vapor-chain/chain/provenance"
	"github.com/muthu2201/vapor-chain/chain/x/council/types"
)

type queryServer struct{ Keeper }

var _ types.QueryServer = queryServer{}

func NewQueryServerImpl(k Keeper) types.QueryServer { return queryServer{Keeper: k} }

func (q queryServer) Params(goCtx context.Context, _ *types.QueryParamsRequest) (*types.QueryParamsResponse, error) {
	return &types.QueryParamsResponse{Params: q.GetParams(sdk.UnwrapSDKContext(goCtx))}, nil
}

func (q queryServer) Admissions(goCtx context.Context, _ *types.QueryAdmissionsRequest) (*types.QueryAdmissionsResponse, error) {
	var out []types.Admission
	err := q.Keeper.Admissions.Walk(sdk.UnwrapSDKContext(goCtx), nil, func(_ string, a types.Admission) (bool, error) {
		out = append(out, a)
		return false, nil
	})
	return &types.QueryAdmissionsResponse{Admissions: out}, err
}

func (q queryServer) Pauses(goCtx context.Context, _ *types.QueryPausesRequest) (*types.QueryPausesResponse, error) {
	ctx := sdk.UnwrapSDKContext(goCtx)
	var out []types.Pause
	err := q.Keeper.Pauses.Walk(ctx, nil, func(_ string, p types.Pause) (bool, error) {
		if isPauseActive(ctx, p) {
			out = append(out, p)
		}
		return false, nil
	})
	return &types.QueryPausesResponse{Pauses: out}, err
}

func (q queryServer) Provenance(goCtx context.Context, _ *types.QueryProvenanceRequest) (*types.QueryProvenanceResponse, error) {
	return &types.QueryProvenanceResponse{
		Provenance:        q.ProvenanceRecord(sdk.UnwrapSDKContext(goCtx)),
		BinaryFingerprint: provenance.Fingerprint,
		BinaryCommit:      provenance.GitCommit,
	}, nil
}
