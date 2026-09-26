// SPDX-License-Identifier: AGPL-3.0-only
// Copyright (c) 2026 VaporChain / muthu2201
// Provenance: VAPOR-6eabb1be532bdef4

package keeper_test

import (
	"math/rand"
	"testing"

	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/crypto"
	"github.com/stretchr/testify/require"

	"cosmossdk.io/math"

	sdk "github.com/cosmos/cosmos-sdk/types"
	authtypes "github.com/cosmos/cosmos-sdk/x/auth/types"

	"github.com/muthu2201/vapor-chain/chain/constants"
	"github.com/muthu2201/vapor-chain/chain/testutil"
	appstypes "github.com/muthu2201/vapor-chain/chain/x/apps/types"
	settlekeeper "github.com/muthu2201/vapor-chain/chain/x/settle/keeper"
	settletypes "github.com/muthu2201/vapor-chain/chain/x/settle/types"
)

func coin(a int64) sdk.Coin { return sdk.NewCoin(testutil.USDC, math.NewInt(a)) }

func bal(e *testutil.Env, a sdk.AccAddress) math.Int {
	return e.App.BankKeeper.GetBalance(e.Ctx, a, testutil.USDC).Amount
}

// registerAppWithContract registers an app owned by owner and binds a contract
// address proven with a CREATE proof (nonce 0).
func registerAppWithContract(t *testing.T, e *testutil.Env, owner sdk.AccAddress, referrerBps uint32) (appstypes.App, common.Address) {
	t.Helper()
	id, err := e.App.AppsKeeper.RegisterApp(e.Ctx, owner, owner.String(), "ipfs://app", "", referrerBps)
	require.NoError(t, err)
	contract := crypto.CreateAddress(common.BytesToAddress(owner), 0)
	_, _, err = e.App.AppsKeeper.AddContract(e.Ctx, owner, id, contract.Hex(), appstypes.OwnershipProof{Type: appstypes.PROOF_TYPE_DEPLOYER_CREATE, Nonce: 0})
	require.NoError(t, err)
	app, ok := e.App.AppsKeeper.AppOfContract(e.Ctx, contract)
	require.True(t, ok)
	return app, contract
}

func requireSolvent(t *testing.T, e *testutil.Env) {
	t.Helper()
	require.Empty(t, e.App.SettleKeeper.CheckInvariants(e.Ctx))
	recount, err := e.App.SettleKeeper.RecountLedger(e.Ctx)
	require.NoError(t, err)
	mod := e.App.AccountKeeper.GetModuleAddress(settletypes.ModuleName)
	for d, owed := range recount {
		tracked, _ := e.App.SettleKeeper.LedgerTotal.Get(e.Ctx, d)
		require.True(t, tracked.Equal(owed), "ledger drift %s: tracked %s recount %s", d, tracked, owed)
		require.True(t, e.App.BankKeeper.GetBalance(e.Ctx, mod, d).Amount.Equal(owed), "module balance != ledger for %s", d)
	}
}

func TestPayNoAppBucket(t *testing.T) {
	e := testutil.Setup(t, 3)
	payer, payee := e.Accounts[1], e.Accounts[2]
	before := bal(e, payee)
	res, err := e.App.SettleKeeper.Pay(e.Ctx, settlekeeper.PayRequest{Payer: payer, Payee: payee, Amount: coin(100_000_000)})
	require.NoError(t, err)
	require.Equal(t, int64(1_000_000), res.Fee.Int64())
	require.Equal(t, int64(99_000_000), bal(e, payee).Sub(before).Int64())
	// no app: 70% treasury, 20% validators, 10% relayers
	require.Equal(t, int64(700_000), e.App.SettleKeeper.PoolBalance(e.Ctx, settletypes.PoolTreasury, testutil.USDC).Int64())
	require.Equal(t, int64(200_000), e.App.SettleKeeper.PoolBalance(e.Ctx, settletypes.PoolValidator, testutil.USDC).Int64())
	require.Equal(t, int64(100_000), e.App.SettleKeeper.PoolBalance(e.Ctx, settletypes.PoolRelayer, testutil.USDC).Int64())
	requireSolvent(t, e)
}

func TestPayAttributedToAppWithReferrer(t *testing.T) {
	e := testutil.Setup(t, 4)
	owner, payee, referrer := e.Accounts[1], e.Accounts[2], e.Accounts[3]
	app, contract := registerAppWithContract(t, e, owner, 1000)
	contractAcc := sdk.AccAddress(contract.Bytes())
	// fund the contract (it pays with msg.sender semantics)
	require.NoError(t, e.App.BankKeeper.SendCoins(e.Ctx, owner, contractAcc, sdk.NewCoins(coin(200_000_000))))
	refBefore := bal(e, referrer)
	_, err := e.App.SettleKeeper.Pay(e.Ctx, settlekeeper.PayRequest{Payer: contractAcc, Payee: payee, Amount: coin(100_000_000), App: &app, Referrer: referrer})
	require.NoError(t, err)
	// blueprint worked example: app 0.45 claimable, referrer 0.05 paid, validators .2, relayers .1, treasury .2
	require.Equal(t, int64(450_000), e.App.SettleKeeper.ClaimableOf(e.Ctx, app.AppId, testutil.USDC).Int64())
	require.Equal(t, int64(50_000), bal(e, referrer).Sub(refBefore).Int64())
	require.Equal(t, int64(200_000), e.App.SettleKeeper.PoolBalance(e.Ctx, settletypes.PoolTreasury, testutil.USDC).Int64())
	requireSolvent(t, e)

	// claim goes to the revenue recipient only, and only once
	ownerBefore := bal(e, owner)
	claimed, err := e.App.SettleKeeper.ClaimRevenue(e.Ctx, app.AppId, nil)
	require.NoError(t, err)
	require.Equal(t, int64(450_000), claimed.AmountOf(testutil.USDC).Int64())
	require.Equal(t, int64(450_000), bal(e, owner).Sub(ownerBefore).Int64())
	_, err = e.App.SettleKeeper.ClaimRevenue(e.Ctx, app.AppId, nil)
	require.ErrorIs(t, err, settletypes.ErrNothingToClaim)
	requireSolvent(t, e)
}

func TestRejectsBadPayments(t *testing.T) {
	e := testutil.Setup(t, 2)
	k := e.App.SettleKeeper
	p := e.Accounts[0]
	// self payment
	_, err := k.Pay(e.Ctx, settlekeeper.PayRequest{Payer: p, Payee: p, Amount: coin(1_000_000)})
	require.ErrorIs(t, err, settletypes.ErrSelfPayment)
	// module account payee (would strand funds)
	mod := e.App.AccountKeeper.GetModuleAddress(settletypes.ModuleName)
	_, err = k.Pay(e.Ctx, settlekeeper.PayRequest{Payer: p, Payee: mod, Amount: coin(1_000_000)})
	require.ErrorIs(t, err, settletypes.ErrInvalidRecipient)
	// non-allowlisted asset (the gas credit itself)
	_, err = k.Pay(e.Ctx, settlekeeper.PayRequest{Payer: p, Payee: e.Accounts[1], Amount: sdk.NewCoin(constants.CreditDenom, math.NewInt(1))})
	require.ErrorIs(t, err, settletypes.ErrAssetNotAllowed)
	// zero amount
	_, err = k.Pay(e.Ctx, settlekeeper.PayRequest{Payer: p, Payee: e.Accounts[1], Amount: coin(0)})
	require.ErrorIs(t, err, settletypes.ErrInvalidAmount)
	// insufficient funds
	_, err = k.Pay(e.Ctx, settlekeeper.PayRequest{Payer: p, Payee: e.Accounts[1], Amount: coin(1_000_000_000_000_000)})
	require.Error(t, err)
	requireSolvent(t, e)
}

func TestGuardianPauseStopsSettle(t *testing.T) {
	e := testutil.Setup(t, 2)
	require.NoError(t, e.App.CouncilKeeper.SetPause(e.Ctx, "settle:"+testutil.USDC, e.Guardian.String(), "incident", e.Ctx.BlockHeight()+5))
	_, err := e.App.SettleKeeper.Pay(e.Ctx, settlekeeper.PayRequest{Payer: e.Accounts[0], Payee: e.Accounts[1], Amount: coin(1_000_000)})
	require.ErrorIs(t, err, settletypes.ErrPaused)
	// pause expires on its own
	for i := 0; i < 6; i++ {
		e.NextBlock(t)
	}
	_, err = e.App.SettleKeeper.Pay(e.Ctx, settlekeeper.PayRequest{Payer: e.Accounts[0], Payee: e.Accounts[1], Amount: coin(1_000_000)})
	require.NoError(t, err)
}

func TestTabAutoSettlesAtThreshold(t *testing.T) {
	e := testutil.Setup(t, 3)
	payer, payee := e.Accounts[1], e.Accounts[2]
	before := bal(e, payee)
	// 99 tips of 0.01 USDC stay escrowed
	for i := 0; i < 99; i++ {
		settled, _, err := e.App.SettleKeeper.TabDeposit(e.Ctx, nil, payer, payee, coin(10_000), true)
		require.NoError(t, err)
		require.False(t, settled)
	}
	require.True(t, bal(e, payee).Equal(before))
	requireSolvent(t, e)
	// the 100th reaches 1 USDC: fee computed ON THE AGGREGATE (1% = 0.01)
	settled, res, err := e.App.SettleKeeper.TabDeposit(e.Ctx, nil, payer, payee, coin(10_000), true)
	require.NoError(t, err)
	require.True(t, settled)
	require.Equal(t, int64(10_000), res.Fee.Int64())
	require.Equal(t, int64(990_000), bal(e, payee).Sub(before).Int64())
	requireSolvent(t, e)
}

func TestTabClosePermissions(t *testing.T) {
	e := testutil.Setup(t, 4)
	payer, payee, stranger := e.Accounts[1], e.Accounts[2], e.Accounts[3]
	_, _, err := e.App.SettleKeeper.TabDeposit(e.Ctx, nil, payer, payee, coin(50_000), true)
	require.NoError(t, err)
	_, err = e.App.SettleKeeper.CloseTab(e.Ctx, stranger, 0, payer, payee, testutil.USDC)
	require.ErrorIs(t, err, settletypes.ErrUnauthorized)
	// after max age anyone may close (escrow can never be stranded)
	e.Ctx = e.Ctx.WithBlockHeight(e.Ctx.BlockHeight() + e.App.SettleKeeper.GetParams(e.Ctx).MaxTabAgeBlocks)
	res, err := e.App.SettleKeeper.CloseTab(e.Ctx, stranger, 0, payer, payee, testutil.USDC)
	require.NoError(t, err)
	require.Equal(t, int64(500), res.Fee.Int64()) // 0.05 USDC < micro threshold: pure 1%, no floor
	requireSolvent(t, e)
}

func TestAppScopedAllowance(t *testing.T) {
	e := testutil.Setup(t, 4)
	user := e.Accounts[3]
	appA, _ := registerAppWithContract(t, e, e.Accounts[1], 0)
	appB, _ := registerAppWithContract(t, e, e.Accounts[2], 0)
	k := e.App.SettleKeeper
	require.NoError(t, k.ApproveApp(e.Ctx, user, appA.AppId, testutil.USDC, math.NewInt(5_000_000)))
	// app B cannot spend what the user approved for app A
	require.ErrorIs(t, k.SpendAllowance(e.Ctx, user, appB.AppId, testutil.USDC, math.NewInt(1)), settletypes.ErrAllowance)
	require.NoError(t, k.SpendAllowance(e.Ctx, user, appA.AppId, testutil.USDC, math.NewInt(3_000_000)))
	require.Equal(t, int64(2_000_000), k.Allowance(e.Ctx, user.String(), appA.AppId, testutil.USDC).Int64())
	require.ErrorIs(t, k.SpendAllowance(e.Ctx, user, appA.AppId, testutil.USDC, math.NewInt(2_000_001)), settletypes.ErrAllowance)
	// infinite approvals are never decremented
	require.NoError(t, k.ApproveApp(e.Ctx, user, appA.AppId, testutil.USDC, settlekeeper.MaxAllowance))
	require.NoError(t, k.SpendAllowance(e.Ctx, user, appA.AppId, testutil.USDC, math.NewInt(1_000_000_000)))
	require.True(t, k.Allowance(e.Ctx, user.String(), appA.AppId, testutil.USDC).Equal(settlekeeper.MaxAllowance))
}

func TestBuyCreditsMintsAtFixedPriceAndInvariantHolds(t *testing.T) {
	e := testutil.Setup(t, 2)
	buyer := e.Accounts[1]
	before := e.App.BankKeeper.GetBalance(e.Ctx, buyer, constants.CreditDenom).Amount
	out, err := e.App.SettleKeeper.BuyCredits(e.Ctx, buyer, nil, coin(1_000_000)) // 1 USDC
	require.NoError(t, err)
	// 1 USDC = 1e21 acredit = 1000 CREDIT (blueprint default price)
	require.Equal(t, "1000000000000000000000", out.Amount.String())
	require.True(t, e.App.BankKeeper.GetBalance(e.Ctx, buyer, constants.CreditDenom).Amount.Sub(before).Equal(out.Amount))
	require.Equal(t, int64(1_000_000), e.App.SettleKeeper.PoolBalance(e.Ctx, settletypes.PoolTreasury, testutil.USDC).Int64())
	requireSolvent(t, e)
}

func TestTreasuryAndRelayerControls(t *testing.T) {
	e := testutil.Setup(t, 3)
	k := e.App.SettleKeeper
	// build pool balances
	for i := 0; i < 20; i++ {
		_, err := k.Pay(e.Ctx, settlekeeper.PayRequest{Payer: e.Accounts[1], Payee: e.Accounts[2], Amount: coin(100_000_000)})
		require.NoError(t, err)
	}
	// strangers cannot withdraw
	require.ErrorIs(t, k.WithdrawTreasury(e.Ctx, e.Accounts[1].String(), e.Accounts[1], sdk.NewCoins(coin(1))), settletypes.ErrUnauthorized)
	// admin can, within balance
	require.NoError(t, k.WithdrawTreasury(e.Ctx, e.Admin.String(), e.Admin, sdk.NewCoins(coin(1_000_000))))
	require.Error(t, k.WithdrawTreasury(e.Ctx, e.Admin.String(), e.Admin, sdk.NewCoins(coin(1_000_000_000_000))))
	// relayer pool: only allowlisted recipients, capped per interval
	_, err := k.DisburseRelayerPool(e.Ctx, e.Admin.String(), e.Accounts[2], coin(100_000), false)
	require.ErrorIs(t, err, settletypes.ErrInvalidRecipient)
	_, err = k.DisburseRelayerPool(e.Ctx, e.Admin.String(), e.Accounts[0], coin(1_000_000), true) // as credits
	require.NoError(t, err)
	_, err = k.DisburseRelayerPool(e.Ctx, e.Admin.String(), e.Accounts[0], coin(1_000_000), false)
	require.NoError(t, err)
	requireSolvent(t, e)
}

func TestValidatorPayoutByPowerAndUptime(t *testing.T) {
	e := testutil.Setup(t, 3)
	k := e.App.SettleKeeper
	_, err := k.Pay(e.Ctx, settlekeeper.PayRequest{Payer: e.Accounts[1], Payee: e.Accounts[2], Amount: coin(100_000_000)})
	require.NoError(t, err)
	opBefore := bal(e, e.Operator)
	e.Ctx = e.Ctx.WithBlockHeight(e.Ctx.BlockHeight() + 20)
	require.NoError(t, k.PayoutValidators(e.Ctx))
	// single validator with full uptime receives the whole validator pool
	require.Equal(t, int64(200_000), bal(e, e.Operator).Sub(opBefore).Int64())
	require.True(t, k.PoolBalance(e.Ctx, settletypes.PoolValidator, testutil.USDC).IsZero())
	requireSolvent(t, e)
}

// TestRandomOperationsConserveFunds is a property test: an adversarial mix of
// every money-moving operation never breaks solvency or ledger accounting.
func TestRandomOperationsConserveFunds(t *testing.T) {
	e := testutil.Setup(t, 8)
	k := e.App.SettleKeeper
	rng := rand.New(rand.NewSource(20260924))
	var apps []appstypes.App
	for i := 0; i < 3; i++ {
		a, contract := registerAppWithContract(t, e, e.Accounts[i], uint32(rng.Intn(1001)))
		require.NoError(t, e.App.BankKeeper.SendCoins(e.Ctx, e.Accounts[i], sdk.AccAddress(contract.Bytes()), sdk.NewCoins(coin(10_000_000_000))))
		apps = append(apps, a)
	}
	pick := func() sdk.AccAddress { return e.Accounts[rng.Intn(len(e.Accounts))] }
	for i := 0; i < 3000; i++ {
		amt := int64(rng.Intn(50_000_000) + 1)
		switch rng.Intn(8) {
		case 0, 1, 2:
			var app *appstypes.App
			if rng.Intn(2) == 0 {
				a := apps[rng.Intn(len(apps))]
				app = &a
			}
			var ref sdk.AccAddress
			if rng.Intn(3) == 0 {
				ref = pick()
			}
			_, _ = k.Pay(e.Ctx, settlekeeper.PayRequest{Payer: pick(), Payee: pick(), Amount: coin(amt), App: app, Referrer: ref})
		case 3:
			_, _, _ = k.TabDeposit(e.Ctx, nil, pick(), pick(), coin(int64(rng.Intn(300_000)+1)), true)
		case 4:
			_, _ = k.ClaimRevenue(e.Ctx, apps[rng.Intn(len(apps))].AppId, nil)
		case 5:
			_, _ = k.BuyCredits(e.Ctx, pick(), nil, coin(int64(rng.Intn(2_000_000)+1)))
		case 6:
			_ = k.WithdrawTreasury(e.Ctx, e.Admin.String(), e.Admin, sdk.NewCoins(coin(int64(rng.Intn(100_000)+1))))
		case 7:
			e.Ctx = e.Ctx.WithBlockHeight(e.Ctx.BlockHeight() + int64(rng.Intn(15)))
			require.NoError(t, k.PayoutValidators(e.Ctx))
		}
		if i%250 == 0 {
			requireSolvent(t, e)
		}
	}
	requireSolvent(t, e)
}

// TestBurnGasFees verifies the deflationary gas burn: the credit denom that
// accrues in the fee collector is destroyed (not recirculated), by the
// governance-set fraction, before validators are paid.
func TestBurnGasFees(t *testing.T) {
	fund := func(e *testutil.Env, amt int64) {
		coins := sdk.NewCoins(sdk.NewCoin(constants.CreditDenom, math.NewInt(amt)))
		require.NoError(t, e.App.BankKeeper.MintCoins(e.Ctx, settletypes.ModuleName, coins))
		require.NoError(t, e.App.BankKeeper.SendCoinsFromModuleToModule(e.Ctx, settletypes.ModuleName, authtypes.FeeCollectorName, coins))
	}
	feeColl := func(e *testutil.Env) math.Int {
		addr := e.App.AccountKeeper.GetModuleAddress(authtypes.FeeCollectorName)
		return e.App.BankKeeper.GetBalance(e.Ctx, addr, constants.CreditDenom).Amount
	}
	supply := func(e *testutil.Env) math.Int {
		return e.App.BankKeeper.GetSupply(e.Ctx, constants.CreditDenom).Amount
	}

	// 100% burn (default): fee collector emptied, supply drops by the full amount.
	e := testutil.Setup(t, 1)
	fund(e, 1_000_000)
	s0 := supply(e)
	e.App.SettleKeeper.BurnGasFees(e.Ctx)
	require.True(t, feeColl(e).IsZero(), "fee collector should be emptied at 100%")
	require.Equal(t, s0.SubRaw(1_000_000), supply(e), "supply should drop by the burned amount")

	// 50% burn: half destroyed, half left for distribution.
	e2 := testutil.Setup(t, 1)
	p := e2.App.SettleKeeper.GetParams(e2.Ctx)
	p.GasBurnBps = 5_000
	require.NoError(t, e2.App.SettleKeeper.SetParams(e2.Ctx, p))
	fund(e2, 1_000_000)
	s2 := supply(e2)
	e2.App.SettleKeeper.BurnGasFees(e2.Ctx)
	require.Equal(t, math.NewInt(500_000), feeColl(e2), "half should remain for validators")
	require.Equal(t, s2.SubRaw(500_000), supply(e2), "supply should drop by half")

	// disabled: nothing burned.
	e3 := testutil.Setup(t, 1)
	p3 := e3.App.SettleKeeper.GetParams(e3.Ctx)
	p3.GasBurnBps = 0
	require.NoError(t, e3.App.SettleKeeper.SetParams(e3.Ctx, p3))
	fund(e3, 1_000_000)
	s3 := supply(e3)
	e3.App.SettleKeeper.BurnGasFees(e3.Ctx)
	require.Equal(t, math.NewInt(1_000_000), feeColl(e3), "nothing burned when disabled")
	require.Equal(t, s3, supply(e3))
}
