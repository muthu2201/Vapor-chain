// Copyright (c) 2026 VaporChain / muthu2201. All rights reserved.
// Provenance: VAPOR-6eabb1be532bdef4

package keeper_test

import (
	"testing"

	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/crypto"
	"github.com/stretchr/testify/require"

	"cosmossdk.io/math"

	sdk "github.com/cosmos/cosmos-sdk/types"

	"github.com/muthu2201/vapor-chain/chain/constants"
	"github.com/muthu2201/vapor-chain/chain/testutil"
	appskeeper "github.com/muthu2201/vapor-chain/chain/x/apps/keeper"
	"github.com/muthu2201/vapor-chain/chain/x/apps/types"
)

func create(owner sdk.AccAddress, nonce uint64) common.Address {
	return crypto.CreateAddress(common.BytesToAddress(owner), nonce)
}

func TestRegisterAppBurnsFee(t *testing.T) {
	e := testutil.Setup(t, 2)
	k := e.App.AppsKeeper
	owner := e.Accounts[0]
	supplyBefore := e.App.BankKeeper.GetSupply(e.Ctx, constants.CreditDenom).Amount
	id, err := k.RegisterApp(e.Ctx, owner, "", "ipfs://meta", "shop.example.com", 500)
	require.NoError(t, err)
	require.Equal(t, uint64(1), id)
	fee := k.GetParams(e.Ctx).RegistrationFee.Amount
	require.True(t, supplyBefore.Sub(e.App.BankKeeper.GetSupply(e.Ctx, constants.CreditDenom).Amount).Equal(fee), "registration fee must be burned")
	app, err := k.GetApp(e.Ctx, id)
	require.NoError(t, err)
	require.Equal(t, owner.String(), app.RevenueRecipient)
	require.Equal(t, types.APP_STATUS_ACTIVE, app.Status)

	_, err = k.RegisterApp(e.Ctx, owner, "", "", "", 5000) // referrer above max
	require.ErrorIs(t, err, types.ErrInvalidField)
	_, err = k.RegisterApp(e.Ctx, owner, "", "", "BAD_DOMAIN", 0)
	require.ErrorIs(t, err, types.ErrInvalidField)
	mod := e.App.AccountKeeper.GetModuleAddress("settle")
	_, err = k.RegisterApp(e.Ctx, owner, mod.String(), "", "", 0) // module account as recipient
	require.ErrorIs(t, err, types.ErrInvalidField)
}

func TestOwnershipProofs(t *testing.T) {
	e := testutil.Setup(t, 3)
	k := e.App.AppsKeeper
	owner, attacker := e.Accounts[0], e.Accounts[1]
	id, err := k.RegisterApp(e.Ctx, owner, "", "", "", 0)
	require.NoError(t, err)
	attackerApp, err := k.RegisterApp(e.Ctx, attacker, "", "", "", 0)
	require.NoError(t, err)

	// CREATE proof
	c1 := create(owner, 7)
	_, _, err = k.AddContract(e.Ctx, owner, id, c1.Hex(), types.OwnershipProof{Type: types.PROOF_TYPE_DEPLOYER_CREATE, Nonce: 7})
	require.NoError(t, err)
	// wrong nonce is rejected
	_, _, err = k.AddContract(e.Ctx, owner, id, create(owner, 9).Hex(), types.OwnershipProof{Type: types.PROOF_TYPE_DEPLOYER_CREATE, Nonce: 8})
	require.ErrorIs(t, err, types.ErrInvalidProof)
	// an attacker cannot claim someone else's contract with their own CREATE proof
	_, _, err = k.AddContract(e.Ctx, attacker, attackerApp, c1.Hex(), types.OwnershipProof{Type: types.PROOF_TYPE_DEPLOYER_CREATE, Nonce: 7})
	require.ErrorIs(t, err, types.ErrInvalidProof)
	// a non-owner cannot add contracts to someone else's app
	_, _, err = k.AddContract(e.Ctx, attacker, id, create(attacker, 0).Hex(), types.OwnershipProof{Type: types.PROOF_TYPE_DEPLOYER_CREATE})
	require.ErrorIs(t, err, types.ErrUnauthorized)

	// CREATE2 proof
	salt := common.HexToHash("0x01")
	ich := crypto.Keccak256([]byte("init code"))
	c2 := crypto.CreateAddress2(common.BytesToAddress(owner), salt, ich)
	_, _, err = k.AddContract(e.Ctx, owner, id, c2.Hex(), types.OwnershipProof{Type: types.PROOF_TYPE_DEPLOYER_CREATE2, Salt: salt.Hex(), InitCodeHash: common.Bytes2Hex(ich)})
	require.NoError(t, err)

	// OWNABLE on an address without code is rejected
	_, _, err = k.AddContract(e.Ctx, owner, id, create(owner, 99).Hex(), types.OwnershipProof{Type: types.PROOF_TYPE_OWNABLE})
	require.ErrorIs(t, err, types.ErrNotContract)

	app, ok := k.AppOfContract(e.Ctx, c1)
	require.True(t, ok)
	require.Equal(t, id, app.AppId)
	// checksum casing can never create a second binding
	_, _, err = k.AddContract(e.Ctx, owner, id, common.HexToAddress(c1.Hex()).Hex(), types.OwnershipProof{Type: types.PROOF_TYPE_DEPLOYER_CREATE, Nonce: 7})
	require.ErrorIs(t, err, types.ErrAlreadyBound)
}

func TestSelfClaimNeedsOwnerAcceptance(t *testing.T) {
	e := testutil.Setup(t, 2)
	k := e.App.AppsKeeper
	id, err := k.RegisterApp(e.Ctx, e.Accounts[0], "", "", "", 0)
	require.NoError(t, err)
	hostile := common.HexToAddress("0x000000000000000000000000000000000000beef")
	require.NoError(t, k.ClaimRegistration(e.Ctx, hostile, id))
	// a claim alone gives NO attribution (else anyone could drain the app's quota)
	_, ok := k.AppOfContract(e.Ctx, hostile)
	require.False(t, ok)
	// only the owner may accept
	_, _, err = k.AcceptContractClaim(e.Ctx, e.Accounts[1].String(), id, hostile.Hex())
	require.ErrorIs(t, err, types.ErrUnauthorized)
	_, _, err = k.AcceptContractClaim(e.Ctx, e.Accounts[0].String(), id, hostile.Hex())
	require.NoError(t, err)
	_, ok = k.AppOfContract(e.Ctx, hostile)
	require.True(t, ok)
}

func TestContractMoveIsTimelockedAndVetoable(t *testing.T) {
	e := testutil.Setup(t, 2)
	k := e.App.AppsKeeper
	owner := e.Accounts[0]
	a1, _ := k.RegisterApp(e.Ctx, owner, "", "", "", 0)
	a2, _ := k.RegisterApp(e.Ctx, owner, "", "", "", 0)
	c := create(owner, 0)
	proof := types.OwnershipProof{Type: types.PROOF_TYPE_DEPLOYER_CREATE}
	_, _, err := k.AddContract(e.Ctx, owner, a1, c.Hex(), proof)
	require.NoError(t, err)
	pending, unlock, err := k.AddContract(e.Ctx, owner, a2, c.Hex(), proof)
	require.NoError(t, err)
	require.True(t, pending)
	require.Equal(t, e.Ctx.BlockHeight()+k.GetParams(e.Ctx).ContractMoveDelayBlocks, unlock)
	app, _ := k.AppOfContract(e.Ctx, c)
	require.Equal(t, a1, app.AppId, "attribution must not change before the timelock")

	// veto
	require.NoError(t, k.CancelContractMove(e.Ctx, owner.String(), c.Hex()))
	require.ErrorIs(t, k.CancelContractMove(e.Ctx, owner.String(), c.Hex()), types.ErrNoPendingMove)

	// re-request and let it mature
	_, unlock, err = k.AddContract(e.Ctx, owner, a2, c.Hex(), proof)
	require.NoError(t, err)
	e.Ctx = e.Ctx.WithBlockHeight(unlock)
	require.NoError(t, k.EndBlock(e.Ctx))
	app, _ = k.AppOfContract(e.Ctx, c)
	require.Equal(t, a2, app.AppId)
	a1app, _ := k.GetApp(e.Ctx, a1)
	require.Equal(t, uint32(0), a1app.ContractCount)
}

func TestQuotaRewardsDiversityNotWashTrading(t *testing.T) {
	params := types.DefaultParams()
	weight := math.NewInt(1_000_000_000) // same fees in both scenarios
	// honest: 1000 payments from 800 distinct payers
	honest := types.EpochStats{AppId: 1, FeeWeight: weight, Payments: 1000, Hll: types.NewHLL()}
	for i := 0; i < 800; i++ {
		honest.Hll = types.HLLAdd(honest.Hll, []byte{byte(i), byte(i >> 8), 1})
	}
	// wash: 1000 payments from 2 wallets
	wash := types.EpochStats{AppId: 2, FeeWeight: weight, Payments: 1000, Hll: types.NewHLL()}
	wash.Hll = types.HLLAdd(wash.Hll, []byte("w1"))
	wash.Hll = types.HLLAdd(wash.Hll, []byte("w2"))
	qh := appskeeper.ComputeQuota(params, honest, true)
	qw := appskeeper.ComputeQuota(params, wash, true)
	require.Equal(t, uint32(10_000), qh.DiversityBps)
	require.Less(t, qw.DiversityBps, uint32(200))
	require.Greater(t, qh.Gas, 20*qw.Gas/2, "honest app must earn far more sponsorship than a wash trader")
	require.LessOrEqual(t, qh.Gas, params.MaxGasPerEpoch)
}

func TestEpochRolloverComputesQuota(t *testing.T) {
	e := testutil.Setup(t, 2)
	k := e.App.AppsKeeper
	p := k.GetParams(e.Ctx)
	p.EpochLengthBlocks = 10
	require.NoError(t, k.Params.Set(e.Ctx, p))
	id, _ := k.RegisterApp(e.Ctx, e.Accounts[0], "", "", "", 0)
	for i := 0; i < 50; i++ {
		require.NoError(t, k.RecordSettlement(e.Ctx, id, []byte{byte(i)}, math.NewInt(100_000)))
	}
	before := k.QuotaFor(e.Ctx, id)
	require.Zero(t, before.Gas, "an unverified app gets no free base quota")
	for i := 0; i < 12; i++ {
		e.NextBlock(t)
	}
	after := k.QuotaFor(e.Ctx, id)
	require.Greater(t, after.Gas, before.Gas, "fees paid last epoch earn quota")
	require.Equal(t, uint64(1), after.Epoch)
}

// TestBaseQuotaOnlyForVerifiedApps closes the fake-app loophole: registering is
// cheap, so the protocol-sponsored base quota must require attestor-verified
// domain ownership; changing the domain drops it again, and at rollover an
// unverified app keeps only what its fees earned.
func TestBaseQuotaOnlyForVerifiedApps(t *testing.T) {
	e := testutil.Setup(t, 3)
	k := e.App.AppsKeeper
	owner, attestor := e.Accounts[0], e.Accounts[1]
	p := k.GetParams(e.Ctx)
	p.Attestors = []string{attestor.String()}
	p.AttestationThreshold = 1
	p.EpochLengthBlocks = 10
	require.NoError(t, k.SetParams(e.Ctx, p))
	require.NotZero(t, p.BaseGasPerEpoch)

	fake, err := k.RegisterApp(e.Ctx, owner, "", "", "fake.example.com", 0)
	require.NoError(t, err)
	legit, err := k.RegisterApp(e.Ctx, owner, "", "", "shop.example.com", 0)
	require.NoError(t, err)
	require.Zero(t, k.QuotaFor(e.Ctx, fake).Gas, "registration alone buys no sponsored gas")
	require.Zero(t, k.QuotaFor(e.Ctx, legit).Gas)

	// a non-attestor cannot verify, and an attestation must match the app's domain
	_, err = k.AttestDomain(e.Ctx, owner.String(), legit, "shop.example.com")
	require.ErrorIs(t, err, types.ErrNotAttestor)
	_, err = k.AttestDomain(e.Ctx, attestor.String(), legit, "other.example.com")
	require.ErrorIs(t, err, types.ErrDomainMismatch)
	verified, err := k.AttestDomain(e.Ctx, attestor.String(), legit, "shop.example.com")
	require.NoError(t, err)
	require.True(t, verified)
	require.Equal(t, p.BaseGasPerEpoch, k.QuotaFor(e.Ctx, legit).Gas)
	require.Zero(t, k.QuotaFor(e.Ctx, fake).Gas)

	// rollover: identical fee history, only the verified app gets the base on top
	for i := 0; i < 20; i++ {
		require.NoError(t, k.RecordSettlement(e.Ctx, legit, []byte{byte(i), 1}, math.NewInt(10_000)))
		require.NoError(t, k.RecordSettlement(e.Ctx, fake, []byte{byte(i), 2}, math.NewInt(10_000)))
	}
	for i := 0; i < 12; i++ {
		e.NextBlock(t)
	}
	qr, qf := k.QuotaFor(e.Ctx, legit), k.QuotaFor(e.Ctx, fake)
	require.Positive(t, qf.Gas, "fees still earn quota without verification")
	require.Equal(t, p.BaseGasPerEpoch, qr.Gas-qf.Gas, "the only difference is the verified base")

	// moving to an unattested domain resets verification
	require.NoError(t, k.UpdateApp(e.Ctx, owner.String(), legit, "", "", "new.example.com", 0))
	app, err := k.GetApp(e.Ctx, legit)
	require.NoError(t, err)
	require.False(t, app.DomainVerified)
	require.Zero(t, appskeeper.BaseQuota(p, app.DomainVerified))
}

func TestRevokedAppLosesAttribution(t *testing.T) {
	e := testutil.Setup(t, 1)
	k := e.App.AppsKeeper
	owner := e.Accounts[0]
	id, _ := k.RegisterApp(e.Ctx, owner, "", "", "", 0)
	c := create(owner, 0)
	_, _, err := k.AddContract(e.Ctx, owner, id, c.Hex(), types.OwnershipProof{Type: types.PROOF_TYPE_DEPLOYER_CREATE})
	require.NoError(t, err)
	require.NoError(t, k.SetAppStatus(e.Ctx, id, types.APP_STATUS_REVOKED))
	_, ok := k.AppOfContract(e.Ctx, c)
	require.False(t, ok)
	require.ErrorIs(t, k.SetAppStatus(e.Ctx, id, types.APP_STATUS_ACTIVE), types.ErrAppInactive, "revocation is final")
}

// TestContractMoveDroppedWhenDestinationFull proves the finalize-time cap check:
// a scheduled move must not push the destination app over MaxContractsPerApp if
// the app filled up between scheduling and unlock.
func TestContractMoveDroppedWhenDestinationFull(t *testing.T) {
	e := testutil.Setup(t, 2)
	k := e.App.AppsKeeper
	p := k.GetParams(e.Ctx)
	p.MaxContractsPerApp = 1
	require.NoError(t, k.SetParams(e.Ctx, p))
	owner := e.Accounts[0]
	a1, _ := k.RegisterApp(e.Ctx, owner, "", "", "", 0)
	a2, _ := k.RegisterApp(e.Ctx, owner, "", "", "", 0)
	proof := types.OwnershipProof{Type: types.PROOF_TYPE_DEPLOYER_CREATE}
	c := create(owner, 0)
	_, _, err := k.AddContract(e.Ctx, owner, a1, c.Hex(), proof)
	require.NoError(t, err)
	// schedule the move a1 -> a2 (a2 empty, cap ok at schedule time)
	pending, unlock, err := k.AddContract(e.Ctx, owner, a2, c.Hex(), types.OwnershipProof{Type: types.PROOF_TYPE_DEPLOYER_CREATE})
	require.NoError(t, err)
	require.True(t, pending)
	// now fill a2 to the cap with a different contract
	c2 := create(owner, 1)
	_, _, err = k.AddContract(e.Ctx, owner, a2, c2.Hex(), types.OwnershipProof{Type: types.PROOF_TYPE_DEPLOYER_CREATE, Nonce: 1})
	require.NoError(t, err)
	// mature the move: it must be DROPPED, not exceed the cap
	e.Ctx = e.Ctx.WithBlockHeight(unlock)
	require.NoError(t, k.EndBlock(e.Ctx))
	app, _ := k.AppOfContract(e.Ctx, c)
	require.Equal(t, a1, app.AppId, "contract must stay with its current app when the destination is full")
	a2app, _ := k.GetApp(e.Ctx, a2)
	require.Equal(t, uint32(1), a2app.ContractCount, "destination must not exceed the cap")
}
