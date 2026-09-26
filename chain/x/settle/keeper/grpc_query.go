// SPDX-License-Identifier: AGPL-3.0-only
// Copyright (c) 2026 VaporChain / muthu2201
// Provenance: VAPOR-6eabb1be532bdef4

package keeper

import (
	"context"

	"cosmossdk.io/collections"
	"cosmossdk.io/math"

	sdk "github.com/cosmos/cosmos-sdk/types"

	"github.com/muthu2201/vapor-chain/chain/x/settle/types"
)

type queryServer struct{ Keeper }

var _ types.QueryServer = queryServer{}

func NewQueryServerImpl(k Keeper) types.QueryServer { return queryServer{Keeper: k} }

func parseInt(s string) (math.Int, error) {
	v, ok := math.NewIntFromString(s)
	if !ok || v.IsNegative() {
		return math.Int{}, types.ErrInvalidAmount.Wrapf("%q", s)
	}
	return v, nil
}

func (q queryServer) Params(goCtx context.Context, _ *types.QueryParamsRequest) (*types.QueryParamsResponse, error) {
	return &types.QueryParamsResponse{Params: q.GetParams(sdk.UnwrapSDKContext(goCtx))}, nil
}

func (q queryServer) QuoteFee(goCtx context.Context, req *types.QueryQuoteFeeRequest) (*types.QueryQuoteFeeResponse, error) {
	ctx := sdk.UnwrapSDKContext(goCtx)
	amt, err := parseInt(req.Amount)
	if err != nil {
		return nil, err
	}
	params := q.GetParams(ctx)
	asset, ok := params.Asset(req.Denom)
	if !ok || !asset.Enabled {
		return nil, types.ErrAssetNotAllowed
	}
	fee := types.ComputeFee(amt, asset, params.FeeBps)
	return &types.QueryQuoteFeeResponse{Fee: fee.String(), Net: amt.Sub(fee).String()}, nil
}

func (q queryServer) Claimable(goCtx context.Context, req *types.QueryClaimableRequest) (*types.QueryClaimableResponse, error) {
	coins := sdk.NewCoins()
	err := q.Claimables.Walk(sdk.UnwrapSDKContext(goCtx), collections.NewPrefixedPairRange[uint64, string](req.AppId), func(key collections.Pair[uint64, string], v math.Int) (bool, error) {
		coins = coins.Add(sdk.NewCoin(key.K2(), v))
		return false, nil
	})
	return &types.QueryClaimableResponse{Claimable: coins}, err
}

func (q queryServer) Pools(goCtx context.Context, _ *types.QueryPoolsRequest) (*types.QueryPoolsResponse, error) {
	var out []types.PoolBalance
	err := q.Keeper.Pools.Walk(sdk.UnwrapSDKContext(goCtx), nil, func(key collections.Pair[string, string], v math.Int) (bool, error) {
		out = append(out, types.PoolBalance{Pool: key.K1(), Denom: key.K2(), Amount: v})
		return false, nil
	})
	return &types.QueryPoolsResponse{Pools: out}, err
}

func (q queryServer) Tab(goCtx context.Context, req *types.QueryTabRequest) (*types.QueryTabResponse, error) {
	payer, err := sdk.AccAddressFromBech32(req.Payer)
	if err != nil {
		return nil, err
	}
	payee, err := sdk.AccAddressFromBech32(req.Payee)
	if err != nil {
		return nil, err
	}
	t, err := q.Tabs.Get(sdk.UnwrapSDKContext(goCtx), TabKey(req.AppId, payer, payee, req.Denom))
	if err != nil {
		return &types.QueryTabResponse{Found: false, Tab: types.Tab{Amount: math.ZeroInt()}}, nil
	}
	return &types.QueryTabResponse{Found: true, Tab: t}, nil
}

func (q queryServer) Allowance(goCtx context.Context, req *types.QueryAllowanceRequest) (*types.QueryAllowanceResponse, error) {
	return &types.QueryAllowanceResponse{Amount: q.Keeper.Allowance(sdk.UnwrapSDKContext(goCtx), req.Owner, req.AppId, req.Denom).String()}, nil
}

func (q queryServer) Totals(goCtx context.Context, _ *types.QueryTotalsRequest) (*types.QueryTotalsResponse, error) {
	ctx := sdk.UnwrapSDKContext(goCtx)
	var out []types.Totals
	err := q.Keeper.Totals.Walk(ctx, nil, func(_ string, t types.Totals) (bool, error) {
		out = append(out, t)
		return false, nil
	})
	minted, _ := q.CreditsMinted.Get(ctx)
	if minted.IsNil() {
		minted = math.ZeroInt()
	}
	return &types.QueryTotalsResponse{Totals: out, CreditsMinted: minted.String()}, err
}

func (q queryServer) QuoteCredits(goCtx context.Context, req *types.QueryQuoteCreditsRequest) (*types.QueryQuoteCreditsResponse, error) {
	amt, err := parseInt(req.Amount)
	if err != nil {
		return nil, err
	}
	c, err := q.Keeper.QuoteCredits(sdk.UnwrapSDKContext(goCtx), req.Denom, amt)
	if err != nil {
		return nil, err
	}
	return &types.QueryQuoteCreditsResponse{Credits: c.String()}, nil
}
