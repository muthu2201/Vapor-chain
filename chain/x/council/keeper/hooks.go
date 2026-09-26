// SPDX-License-Identifier: AGPL-3.0-only
// Copyright (c) 2026 VaporChain / muthu2201
// Provenance: VAPOR-6eabb1be532bdef4

package keeper

import (
	"context"

	"cosmossdk.io/math"

	sdk "github.com/cosmos/cosmos-sdk/types"
	stakingtypes "github.com/cosmos/cosmos-sdk/x/staking/types"

	"github.com/muthu2201/vapor-chain/chain/x/council/types"
)

// Hooks enforce the Proof-of-Authority rules INSIDE x/staking, so they apply
// no matter how a staking message arrives (tx, authz MsgExec, gentx, or a
// precompile). Only admission-time hooks return errors; hooks that run inside
// EndBlock (bonding/unbonding) never do, because an error there would halt
// the chain.
type Hooks struct{ k Keeper }

var _ stakingtypes.StakingHooks = Hooks{}

func (k Keeper) Hooks() Hooks { return Hooks{k: k} }

func (h Hooks) AfterValidatorCreated(ctx context.Context, valAddr sdk.ValAddress) error {
	sdkCtx := sdk.UnwrapSDKContext(ctx)
	op := sdk.AccAddress(valAddr)
	if h.k.IsRemoved(sdkCtx, op) {
		return types.ErrRemoved
	}
	ok, err := h.k.IsAdmitted(sdkCtx, op)
	if err != nil {
		return err
	}
	if !ok {
		return types.ErrNotAdmitted.Wrapf("operator %s", op)
	}
	return nil
}

// BeforeDelegationCreated only allows self-delegation, so a validator's power
// is exactly what the council granted it.
func (h Hooks) BeforeDelegationCreated(_ context.Context, delAddr sdk.AccAddress, valAddr sdk.ValAddress) error {
	if !delAddr.Equals(sdk.AccAddress(valAddr)) {
		return types.ErrForeignDelegation
	}
	return nil
}

func (Hooks) BeforeValidatorModified(context.Context, sdk.ValAddress) error { return nil }
func (Hooks) AfterValidatorRemoved(context.Context, sdk.ConsAddress, sdk.ValAddress) error {
	return nil
}
func (Hooks) AfterValidatorBonded(context.Context, sdk.ConsAddress, sdk.ValAddress) error { return nil }
func (Hooks) AfterValidatorBeginUnbonding(context.Context, sdk.ConsAddress, sdk.ValAddress) error {
	return nil
}
func (Hooks) BeforeDelegationSharesModified(context.Context, sdk.AccAddress, sdk.ValAddress) error {
	return nil
}
func (Hooks) BeforeDelegationRemoved(context.Context, sdk.AccAddress, sdk.ValAddress) error {
	return nil
}
func (Hooks) AfterDelegationModified(context.Context, sdk.AccAddress, sdk.ValAddress) error {
	return nil
}
func (Hooks) BeforeValidatorSlashed(context.Context, sdk.ValAddress, math.LegacyDec) error {
	return nil
}
func (Hooks) AfterUnbondingInitiated(context.Context, uint64) error { return nil }
