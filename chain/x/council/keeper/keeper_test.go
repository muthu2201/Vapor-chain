// Copyright (c) 2026 VaporChain / muthu2201. All rights reserved.
// Provenance: VAPOR-6eabb1be532bdef4

package keeper_test

import (
	"testing"

	"github.com/stretchr/testify/require"

	"cosmossdk.io/math"

	sdk "github.com/cosmos/cosmos-sdk/types"

	"github.com/muthu2201/vapor-chain/chain/constants"
	"github.com/muthu2201/vapor-chain/chain/provenance"
	"github.com/muthu2201/vapor-chain/chain/testutil"
	"github.com/muthu2201/vapor-chain/chain/x/council/keeper"
	"github.com/muthu2201/vapor-chain/chain/x/council/types"
)

func TestAdmissionMintsNonTransferablePower(t *testing.T) {
	e := testutil.Setup(t, 2)
	k := e.App.CouncilKeeper
	op := e.Accounts[0]
	pow := math.NewIntWithDecimal(50, 18)
	require.NoError(t, k.AdmitValidator(e.Ctx, op, pow, "new operator"))
	require.True(t, e.App.BankKeeper.GetBalance(e.Ctx, op, constants.PowerDenom).Amount.Equal(pow))
	ok, err := k.IsAdmitted(e.Ctx, op)
	require.NoError(t, err)
	require.True(t, ok)
	// power is not transferable via MsgSend (SendEnabled=false)
	require.Error(t, e.App.BankKeeper.IsSendEnabledCoins(e.Ctx, sdk.NewCoin(constants.PowerDenom, math.OneInt())))
	require.Error(t, e.App.BankKeeper.IsSendEnabledCoins(e.Ctx, sdk.NewCoin(constants.CreditDenom, math.OneInt())))
	// cap per validator
	require.ErrorIs(t, k.AdmitValidator(e.Ctx, op, types.DefaultMaxPowerPerValidator, ""), types.ErrInvalidPower)
}

func TestRemovalIsPermanentAndJails(t *testing.T) {
	e := testutil.Setup(t, 1)
	k := e.App.CouncilKeeper
	require.NoError(t, k.RemoveValidator(e.Ctx, e.Operator, "key compromise"))
	require.True(t, k.IsRemoved(e.Ctx, e.Operator))
	val, err := e.App.StakingKeeper.GetValidator(e.Ctx, sdk.ValAddress(e.Operator))
	require.NoError(t, err)
	require.True(t, val.IsJailed())
	require.ErrorIs(t, k.AdmitValidator(e.Ctx, e.Operator, math.OneInt(), ""), types.ErrRemoved)
	// the staking hook refuses to create a validator for a removed operator
	require.ErrorIs(t, k.Hooks().AfterValidatorCreated(e.Ctx, sdk.ValAddress(e.Operator)), types.ErrRemoved)
}

func TestHooksEnforcePoA(t *testing.T) {
	e := testutil.Setup(t, 2)
	h := e.App.CouncilKeeper.Hooks()
	// not admitted -> cannot create a validator
	require.ErrorIs(t, h.AfterValidatorCreated(e.Ctx, sdk.ValAddress(e.Accounts[0])), types.ErrNotAdmitted)
	// only self-delegation
	require.ErrorIs(t, h.BeforeDelegationCreated(e.Ctx, e.Accounts[1], sdk.ValAddress(e.Operator)), types.ErrForeignDelegation)
	require.NoError(t, h.BeforeDelegationCreated(e.Ctx, e.Operator, sdk.ValAddress(e.Operator)))
}

func TestGuardianCanPauseButNotUnpause(t *testing.T) {
	e := testutil.Setup(t, 1)
	srv := keeper.NewMsgServerImpl(e.App.CouncilKeeper)
	_, err := srv.SetPause(e.Ctx, &types.MsgSetPause{Signer: e.Guardian.String(), Target: "ibc", Paused: true, Reason: "bridge exploit"})
	require.NoError(t, err)
	require.True(t, e.App.CouncilKeeper.IsIBCPaused(e.Ctx, "uusdc"))
	_, err = srv.SetPause(e.Ctx, &types.MsgSetPause{Signer: e.Guardian.String(), Target: "ibc", Paused: false})
	require.ErrorIs(t, err, types.ErrGuardianCannotUnset)
	_, err = srv.SetPause(e.Ctx, &types.MsgSetPause{Signer: e.Accounts[0].String(), Target: "settle", Paused: true})
	require.ErrorIs(t, err, types.ErrUnauthorized)
	_, err = srv.SetPause(e.Ctx, &types.MsgSetPause{Signer: e.Guardian.String(), Target: "evm", Paused: true})
	require.ErrorIs(t, err, types.ErrInvalidTarget)
	// governance can unpause
	_, err = srv.SetPause(e.Ctx, &types.MsgSetPause{Signer: testutil.GovAuthority(), Target: "ibc", Paused: false})
	require.NoError(t, err)
	require.False(t, e.App.CouncilKeeper.IsIBCPaused(e.Ctx, "uusdc"))
}

func TestGuardianPauseExpires(t *testing.T) {
	e := testutil.Setup(t, 1)
	srv := keeper.NewMsgServerImpl(e.App.CouncilKeeper)
	_, err := srv.SetPause(e.Ctx, &types.MsgSetPause{Signer: e.Guardian.String(), Target: "settle", Paused: true})
	require.NoError(t, err)
	require.True(t, e.App.CouncilKeeper.IsSettlePaused(e.Ctx, "uusdc"))
	e.Ctx = e.Ctx.WithBlockHeight(e.Ctx.BlockHeight() + int64(e.App.CouncilKeeper.GetParams(e.Ctx).GuardianPauseMaxBlocks))
	require.False(t, e.App.CouncilKeeper.IsSettlePaused(e.Ctx, "uusdc"))
}

func TestProvenanceRecordedAtGenesis(t *testing.T) {
	e := testutil.Setup(t, 1)
	p := e.App.CouncilKeeper.ProvenanceRecord(e.Ctx)
	require.Equal(t, provenance.Fingerprint, p.Fingerprint)
	require.Equal(t, provenance.Seal(testutil.ChainID), p.Seal)
	gs := types.DefaultGenesisState()
	gs.Provenance.Fingerprint = "00"
	require.ErrorIs(t, gs.Validate(), types.ErrProvenanceMismatch)
}
