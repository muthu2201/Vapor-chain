// Copyright (c) 2026 VaporChain / muthu2201. All rights reserved.
// Provenance: VAPOR-6eabb1be532bdef4

package keeper

import (
	"context"
	"errors"

	"github.com/ethereum/go-ethereum/common"

	"cosmossdk.io/collections"
	"cosmossdk.io/math"

	sdk "github.com/cosmos/cosmos-sdk/types"
	"github.com/cosmos/cosmos-sdk/types/query"

	"github.com/muthu2201/vapor-chain/chain/x/apps/types"
)

type queryServer struct{ Keeper }

var _ types.QueryServer = queryServer{}

func NewQueryServerImpl(k Keeper) types.QueryServer { return queryServer{Keeper: k} }

func (q queryServer) Params(goCtx context.Context, _ *types.QueryParamsRequest) (*types.QueryParamsResponse, error) {
	return &types.QueryParamsResponse{Params: q.GetParams(sdk.UnwrapSDKContext(goCtx))}, nil
}

func (q queryServer) App(goCtx context.Context, req *types.QueryAppRequest) (*types.QueryAppResponse, error) {
	app, err := q.GetApp(sdk.UnwrapSDKContext(goCtx), req.AppId)
	if err != nil {
		return nil, err
	}
	return &types.QueryAppResponse{App: app}, nil
}

func (q queryServer) Apps(goCtx context.Context, req *types.QueryAppsRequest) (*types.QueryAppsResponse, error) {
	apps, page, err := query.CollectionPaginate(goCtx, q.Keeper.Apps, req.Pagination, func(_ uint64, a types.App) (types.App, error) {
		return a, nil
	})
	if err != nil {
		return nil, err
	}
	return &types.QueryAppsResponse{Apps: apps, Pagination: page}, nil
}

func (q queryServer) AppByContract(goCtx context.Context, req *types.QueryAppByContractRequest) (*types.QueryAppByContractResponse, error) {
	if !common.IsHexAddress(req.Contract) {
		return nil, types.ErrInvalidField.Wrap("contract must be 0x address")
	}
	ctx := sdk.UnwrapSDKContext(goCtx)
	b, err := q.Bindings.Get(ctx, types.NormalizeContract(req.Contract))
	if errors.Is(err, collections.ErrNotFound) {
		return nil, types.ErrNotBound
	}
	if err != nil {
		return nil, err
	}
	app, err := q.GetApp(ctx, b.AppId)
	if err != nil {
		return nil, err
	}
	return &types.QueryAppByContractResponse{Binding: b, App: app}, nil
}

func (q queryServer) Contracts(goCtx context.Context, req *types.QueryContractsRequest) (*types.QueryContractsResponse, error) {
	ctx := sdk.UnwrapSDKContext(goCtx)
	var out []types.ContractBinding
	err := q.AppContracts.Walk(ctx, collections.NewPrefixedPairRange[uint64, string](req.AppId), func(key collections.Pair[uint64, string]) (bool, error) {
		b, err := q.Bindings.Get(ctx, key.K2())
		if err == nil {
			out = append(out, b)
		}
		return false, nil
	})
	return &types.QueryContractsResponse{Bindings: out}, err
}

func (q queryServer) Quota(goCtx context.Context, req *types.QueryQuotaRequest) (*types.QueryQuotaResponse, error) {
	ctx := sdk.UnwrapSDKContext(goCtx)
	epoch, start := q.Keeper.CurrentEpoch(ctx)
	st, err := q.EpochStats.Get(ctx, collections.Join(epoch, req.AppId))
	if err != nil {
		st = types.EpochStats{AppId: req.AppId, Epoch: epoch, FeeWeight: math.ZeroInt()}
	}
	return &types.QueryQuotaResponse{
		Quota:             q.QuotaFor(ctx, req.AppId),
		CurrentStats:      st,
		EpochEndsAtHeight: start + q.GetParams(ctx).EpochLengthBlocks - 1,
	}, nil
}

func (q queryServer) PendingMoves(goCtx context.Context, _ *types.QueryPendingMovesRequest) (*types.QueryPendingMovesResponse, error) {
	var out []types.PendingMove
	err := q.Keeper.PendingMoves.Walk(sdk.UnwrapSDKContext(goCtx), nil, func(_ string, m types.PendingMove) (bool, error) {
		out = append(out, m)
		return len(out) >= 1000, nil
	})
	return &types.QueryPendingMovesResponse{Moves: out}, err
}

func (q queryServer) PendingClaims(goCtx context.Context, req *types.QueryPendingClaimsRequest) (*types.QueryPendingClaimsResponse, error) {
	var out []types.PendingClaim
	err := q.Keeper.PendingClaims.Walk(sdk.UnwrapSDKContext(goCtx), collections.NewPrefixedPairRange[uint64, string](req.AppId), func(_ collections.Pair[uint64, string], c types.PendingClaim) (bool, error) {
		out = append(out, c)
		return len(out) >= 1000, nil
	})
	return &types.QueryPendingClaimsResponse{Claims: out}, err
}

func (q queryServer) CurrentEpoch(goCtx context.Context, _ *types.QueryCurrentEpochRequest) (*types.QueryCurrentEpochResponse, error) {
	ctx := sdk.UnwrapSDKContext(goCtx)
	epoch, start := q.Keeper.CurrentEpoch(ctx)
	return &types.QueryCurrentEpochResponse{Epoch: epoch, StartedAtHeight: start, EndsAtHeight: start + q.GetParams(ctx).EpochLengthBlocks - 1}, nil
}
