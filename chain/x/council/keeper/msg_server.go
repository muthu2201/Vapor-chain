// SPDX-License-Identifier: AGPL-3.0-only
// Copyright (c) 2026 VaporChain / muthu2201
// Provenance: VAPOR-6eabb1be532bdef4

package keeper

import (
	"context"

	sdk "github.com/cosmos/cosmos-sdk/types"

	"github.com/muthu2201/vapor-chain/chain/x/council/types"
)

type msgServer struct{ Keeper }

var _ types.MsgServer = msgServer{}

func NewMsgServerImpl(k Keeper) types.MsgServer { return msgServer{Keeper: k} }

func (m msgServer) requireAuthority(a string) error {
	if a != m.authority {
		return types.ErrUnauthorized.Wrapf("expected %s, got %s", m.authority, a)
	}
	return nil
}

func (m msgServer) AdmitValidator(goCtx context.Context, msg *types.MsgAdmitValidator) (*types.MsgAdmitValidatorResponse, error) {
	if err := m.requireAuthority(msg.Authority); err != nil {
		return nil, err
	}
	op, err := sdk.AccAddressFromBech32(msg.Operator)
	if err != nil {
		return nil, err
	}
	if err := m.Keeper.AdmitValidator(sdk.UnwrapSDKContext(goCtx), op, msg.Power, msg.Memo); err != nil {
		return nil, err
	}
	return &types.MsgAdmitValidatorResponse{}, nil
}

func (m msgServer) RemoveValidator(goCtx context.Context, msg *types.MsgRemoveValidator) (*types.MsgRemoveValidatorResponse, error) {
	if err := m.requireAuthority(msg.Authority); err != nil {
		return nil, err
	}
	op, err := sdk.AccAddressFromBech32(msg.Operator)
	if err != nil {
		return nil, err
	}
	if err := m.Keeper.RemoveValidator(sdk.UnwrapSDKContext(goCtx), op, msg.Reason); err != nil {
		return nil, err
	}
	return &types.MsgRemoveValidatorResponse{}, nil
}

// SetPause: guardians may only set pauses (auto-expiring); governance may set
// permanent pauses and remove any pause.
func (m msgServer) SetPause(goCtx context.Context, msg *types.MsgSetPause) (*types.MsgSetPauseResponse, error) {
	ctx := sdk.UnwrapSDKContext(goCtx)
	target := normalizeTarget(msg.Target)
	if err := types.ValidateTarget(target); err != nil {
		return nil, err
	}
	if len(msg.Reason) > 512 {
		return nil, types.ErrInvalidTarget.Wrap("reason too long")
	}
	isAuthority := msg.Signer == m.authority
	params := m.GetParams(ctx)
	isGuardian := params.IsGuardian(msg.Signer)
	if !isAuthority && !isGuardian {
		return nil, types.ErrUnauthorized.Wrap("signer is neither governance nor a guardian")
	}
	if !msg.Paused {
		if !isAuthority {
			return nil, types.ErrGuardianCannotUnset
		}
		if err := m.Pauses.Remove(ctx, target); err != nil {
			return nil, err
		}
		ctx.EventManager().EmitEvent(sdk.NewEvent("council_unpause", sdk.NewAttribute("target", target)))
		return &types.MsgSetPauseResponse{}, nil
	}
	var expires int64
	if !isAuthority {
		expires = ctx.BlockHeight() + int64(params.GuardianPauseMaxBlocks) //nolint:gosec // bounded param
	}
	if err := m.Keeper.SetPause(ctx, target, msg.Signer, msg.Reason, expires); err != nil {
		return nil, err
	}
	ctx.EventManager().EmitEvent(sdk.NewEvent("council_pause",
		sdk.NewAttribute("target", target),
		sdk.NewAttribute("by", msg.Signer),
		sdk.NewAttribute("reason", msg.Reason),
	))
	return &types.MsgSetPauseResponse{}, nil
}

func (m msgServer) UpdateParams(goCtx context.Context, msg *types.MsgUpdateParams) (*types.MsgUpdateParamsResponse, error) {
	if err := m.requireAuthority(msg.Authority); err != nil {
		return nil, err
	}
	if err := msg.Params.Validate(); err != nil {
		return nil, err
	}
	if err := m.Params.Set(sdk.UnwrapSDKContext(goCtx), msg.Params); err != nil {
		return nil, err
	}
	return &types.MsgUpdateParamsResponse{}, nil
}
