// Copyright (c) 2026 VaporChain / muthu2201. All rights reserved.
// Provenance: VAPOR-6eabb1be532bdef4

package keeper

import (
	"context"

	sdk "github.com/cosmos/cosmos-sdk/types"

	"github.com/muthu2201/vapor-chain/chain/x/settle/types"
)

type msgServer struct{ Keeper }

var _ types.MsgServer = msgServer{}

func NewMsgServerImpl(k Keeper) types.MsgServer { return msgServer{Keeper: k} }

// Pay: Cosmos-account payments always use the no-app bucket; a plain account
// cannot prove it is an app, and letting callers choose attribution would let
// anyone farm another app's quota.
func (m msgServer) Pay(goCtx context.Context, msg *types.MsgPay) (*types.MsgPayResponse, error) {
	ctx := sdk.UnwrapSDKContext(goCtx)
	payer, err := sdk.AccAddressFromBech32(msg.Payer)
	if err != nil {
		return nil, err
	}
	payee, err := sdk.AccAddressFromBech32(msg.Payee)
	if err != nil {
		return nil, err
	}
	res, err := m.Keeper.Pay(ctx, PayRequest{Payer: payer, Payee: payee, Amount: msg.Amount})
	if err != nil {
		return nil, err
	}
	return &types.MsgPayResponse{Fee: sdk.NewCoin(msg.Amount.Denom, res.Fee), Net: sdk.NewCoin(msg.Amount.Denom, res.Net)}, nil
}

func (m msgServer) ClaimRevenue(goCtx context.Context, msg *types.MsgClaimRevenue) (*types.MsgClaimRevenueResponse, error) {
	claimed, err := m.Keeper.ClaimRevenue(sdk.UnwrapSDKContext(goCtx), msg.AppId, msg.Denoms)
	if err != nil {
		return nil, err
	}
	return &types.MsgClaimRevenueResponse{Claimed: claimed}, nil
}

func (m msgServer) BuyCredits(goCtx context.Context, msg *types.MsgBuyCredits) (*types.MsgBuyCreditsResponse, error) {
	buyer, err := sdk.AccAddressFromBech32(msg.Buyer)
	if err != nil {
		return nil, err
	}
	var recipient sdk.AccAddress
	if msg.Recipient != "" {
		if recipient, err = sdk.AccAddressFromBech32(msg.Recipient); err != nil {
			return nil, err
		}
	}
	out, err := m.Keeper.BuyCredits(sdk.UnwrapSDKContext(goCtx), buyer, recipient, msg.Payment)
	if err != nil {
		return nil, err
	}
	return &types.MsgBuyCreditsResponse{Credits: out}, nil
}

func (m msgServer) CloseTab(goCtx context.Context, msg *types.MsgCloseTab) (*types.MsgCloseTabResponse, error) {
	sender, err := sdk.AccAddressFromBech32(msg.Sender)
	if err != nil {
		return nil, err
	}
	payer, err := sdk.AccAddressFromBech32(msg.Payer)
	if err != nil {
		return nil, err
	}
	payee, err := sdk.AccAddressFromBech32(msg.Payee)
	if err != nil {
		return nil, err
	}
	res, err := m.Keeper.CloseTab(sdk.UnwrapSDKContext(goCtx), sender, msg.AppId, payer, payee, msg.Denom)
	if err != nil {
		return nil, err
	}
	return &types.MsgCloseTabResponse{Fee: sdk.NewCoin(msg.Denom, res.Fee), Net: sdk.NewCoin(msg.Denom, res.Net)}, nil
}

func (m msgServer) WithdrawTreasury(goCtx context.Context, msg *types.MsgWithdrawTreasury) (*types.MsgWithdrawTreasuryResponse, error) {
	to, err := sdk.AccAddressFromBech32(msg.Recipient)
	if err != nil {
		return nil, err
	}
	if err := m.Keeper.WithdrawTreasury(sdk.UnwrapSDKContext(goCtx), msg.Signer, to, msg.Amount); err != nil {
		return nil, err
	}
	return &types.MsgWithdrawTreasuryResponse{}, nil
}

func (m msgServer) DisburseRelayerPool(goCtx context.Context, msg *types.MsgDisburseRelayerPool) (*types.MsgDisburseRelayerPoolResponse, error) {
	to, err := sdk.AccAddressFromBech32(msg.Recipient)
	if err != nil {
		return nil, err
	}
	out, err := m.Keeper.DisburseRelayerPool(sdk.UnwrapSDKContext(goCtx), msg.Signer, to, msg.Amount, msg.AsCredits)
	if err != nil {
		return nil, err
	}
	return &types.MsgDisburseRelayerPoolResponse{Paid: out}, nil
}

func (m msgServer) UpdateParams(goCtx context.Context, msg *types.MsgUpdateParams) (*types.MsgUpdateParamsResponse, error) {
	if msg.Authority != m.authority {
		return nil, types.ErrUnauthorized.Wrapf("expected %s", m.authority)
	}
	if err := msg.Params.Validate(); err != nil {
		return nil, err
	}
	if err := m.Params.Set(sdk.UnwrapSDKContext(goCtx), msg.Params); err != nil {
		return nil, err
	}
	return &types.MsgUpdateParamsResponse{}, nil
}
