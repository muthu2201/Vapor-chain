// SPDX-License-Identifier: AGPL-3.0-only
// Copyright (c) 2026 VaporChain / muthu2201
// Provenance: VAPOR-6eabb1be532bdef4

package keeper

import (
	"context"

	sdk "github.com/cosmos/cosmos-sdk/types"

	"github.com/muthu2201/vapor-chain/chain/x/apps/types"
)

type msgServer struct{ Keeper }

var _ types.MsgServer = msgServer{}

func NewMsgServerImpl(k Keeper) types.MsgServer { return msgServer{Keeper: k} }

func (m msgServer) RegisterApp(goCtx context.Context, msg *types.MsgRegisterApp) (*types.MsgRegisterAppResponse, error) {
	owner, err := sdk.AccAddressFromBech32(msg.Owner)
	if err != nil {
		return nil, err
	}
	id, err := m.Keeper.RegisterApp(sdk.UnwrapSDKContext(goCtx), owner, msg.RevenueRecipient, msg.MetadataUri, msg.Domain, msg.ReferrerBps)
	if err != nil {
		return nil, err
	}
	return &types.MsgRegisterAppResponse{AppId: id}, nil
}

func (m msgServer) UpdateApp(goCtx context.Context, msg *types.MsgUpdateApp) (*types.MsgUpdateAppResponse, error) {
	if err := m.Keeper.UpdateApp(sdk.UnwrapSDKContext(goCtx), msg.Owner, msg.AppId, msg.RevenueRecipient, msg.MetadataUri, msg.Domain, msg.ReferrerBps); err != nil {
		return nil, err
	}
	return &types.MsgUpdateAppResponse{}, nil
}

func (m msgServer) TransferOwnership(goCtx context.Context, msg *types.MsgTransferOwnership) (*types.MsgTransferOwnershipResponse, error) {
	if err := m.Keeper.TransferOwnership(sdk.UnwrapSDKContext(goCtx), msg.Owner, msg.AppId, msg.NewOwner); err != nil {
		return nil, err
	}
	return &types.MsgTransferOwnershipResponse{}, nil
}

func (m msgServer) AcceptOwnership(goCtx context.Context, msg *types.MsgAcceptOwnership) (*types.MsgAcceptOwnershipResponse, error) {
	if err := m.Keeper.AcceptOwnership(sdk.UnwrapSDKContext(goCtx), msg.NewOwner, msg.AppId); err != nil {
		return nil, err
	}
	return &types.MsgAcceptOwnershipResponse{}, nil
}

func (m msgServer) AddContract(goCtx context.Context, msg *types.MsgAddContract) (*types.MsgAddContractResponse, error) {
	owner, err := sdk.AccAddressFromBech32(msg.Owner)
	if err != nil {
		return nil, err
	}
	pending, unlock, err := m.Keeper.AddContract(sdk.UnwrapSDKContext(goCtx), owner, msg.AppId, msg.Contract, msg.Proof)
	if err != nil {
		return nil, err
	}
	return &types.MsgAddContractResponse{Pending: pending, UnlockHeight: unlock}, nil
}

func (m msgServer) AcceptContractClaim(goCtx context.Context, msg *types.MsgAcceptContractClaim) (*types.MsgAcceptContractClaimResponse, error) {
	pending, unlock, err := m.Keeper.AcceptContractClaim(sdk.UnwrapSDKContext(goCtx), msg.Owner, msg.AppId, msg.Contract)
	if err != nil {
		return nil, err
	}
	return &types.MsgAcceptContractClaimResponse{Pending: pending, UnlockHeight: unlock}, nil
}

func (m msgServer) RemoveContract(goCtx context.Context, msg *types.MsgRemoveContract) (*types.MsgRemoveContractResponse, error) {
	if err := m.Keeper.RemoveContract(sdk.UnwrapSDKContext(goCtx), msg.Owner, msg.AppId, msg.Contract); err != nil {
		return nil, err
	}
	return &types.MsgRemoveContractResponse{}, nil
}

func (m msgServer) CancelContractMove(goCtx context.Context, msg *types.MsgCancelContractMove) (*types.MsgCancelContractMoveResponse, error) {
	if err := m.Keeper.CancelContractMove(sdk.UnwrapSDKContext(goCtx), msg.Owner, msg.Contract); err != nil {
		return nil, err
	}
	return &types.MsgCancelContractMoveResponse{}, nil
}

func (m msgServer) BondApp(goCtx context.Context, msg *types.MsgBondApp) (*types.MsgBondAppResponse, error) {
	if err := m.Keeper.BondApp(sdk.UnwrapSDKContext(goCtx), msg.Owner, msg.AppId, msg.Amount); err != nil {
		return nil, err
	}
	return &types.MsgBondAppResponse{}, nil
}

func (m msgServer) UnbondApp(goCtx context.Context, msg *types.MsgUnbondApp) (*types.MsgUnbondAppResponse, error) {
	release, err := m.Keeper.UnbondApp(sdk.UnwrapSDKContext(goCtx), msg.Owner, msg.AppId, msg.Amount)
	if err != nil {
		return nil, err
	}
	return &types.MsgUnbondAppResponse{ReleaseHeight: release}, nil
}

func (m msgServer) SetAppStatus(goCtx context.Context, msg *types.MsgSetAppStatus) (*types.MsgSetAppStatusResponse, error) {
	if msg.Authority != m.authority {
		return nil, types.ErrUnauthorized.Wrapf("expected %s", m.authority)
	}
	if err := m.Keeper.SetAppStatus(sdk.UnwrapSDKContext(goCtx), msg.AppId, msg.Status); err != nil {
		return nil, err
	}
	return &types.MsgSetAppStatusResponse{}, nil
}

func (m msgServer) UpdateParams(goCtx context.Context, msg *types.MsgUpdateParams) (*types.MsgUpdateParamsResponse, error) {
	if msg.Authority != m.authority {
		return nil, types.ErrUnauthorized.Wrapf("expected %s", m.authority)
	}
	if err := msg.Params.Validate(); err != nil {
		return nil, err
	}
	ctx := sdk.UnwrapSDKContext(goCtx)
	// switching the bond denom would strand existing bonds in the old one
	if msg.Params.BondDenom != m.GetParams(ctx).BondDenom {
		if total, err := m.BondedTotal(ctx); err != nil || total.IsPositive() {
			return nil, types.ErrInvalidParams.Wrap("bond_denom cannot change while bonds or unbondings exist")
		}
	}
	if err := m.Params.Set(ctx, msg.Params); err != nil {
		return nil, err
	}
	return &types.MsgUpdateParamsResponse{}, nil
}
