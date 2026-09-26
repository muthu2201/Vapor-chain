// SPDX-License-Identifier: AGPL-3.0-only
// Copyright (c) 2026 VaporChain / muthu2201
// Provenance: VAPOR-6eabb1be532bdef4

package keeper

import (
	"fmt"
	"math/big"

	"cosmossdk.io/collections"
	"cosmossdk.io/math"

	sdk "github.com/cosmos/cosmos-sdk/types"

	appstypes "github.com/muthu2201/vapor-chain/chain/x/apps/types"
	"github.com/muthu2201/vapor-chain/chain/x/settle/types"
)

// PayRequest is one typed payment intent. Fees attach ONLY to these explicit
// intents executed by the protocol itself — never to value inferred from
// opaque calldata (see ARCHITECTURE.md §Economic fee architecture).
type PayRequest struct {
	Payer    sdk.AccAddress
	Payee    sdk.AccAddress
	Amount   sdk.Coin
	App      *appstypes.App // nil => no-app bucket
	Referrer sdk.AccAddress // optional
	// FundsInModule is true when the amount is already escrowed in the module
	// (tab settlement); otherwise it is pulled from Payer.
	FundsInModule bool
}

type PayResult struct {
	Fee   math.Int
	Net   math.Int
	Split types.SplitResult
	AppID uint64
}

// Pay executes a settlement atomically: move funds, pay the payee net, split
// the fee into claimable/pools, record quota stats, emit events. Any error
// reverts everything (the caller's cached context / EVM snapshot).
func (k Keeper) Pay(ctx sdk.Context, req PayRequest) (PayResult, error) {
	params := k.GetParams(ctx)
	asset, err := k.checkAsset(ctx, params, req.Amount.Denom)
	if err != nil {
		return PayResult{}, err
	}
	if !req.Amount.Amount.IsPositive() {
		return PayResult{}, types.ErrInvalidAmount.Wrap("amount must be positive")
	}
	if !k.validRecipient(req.Payee) {
		return PayResult{}, types.ErrInvalidRecipient.Wrap("payee is empty, blocked or a module account")
	}
	if req.Payer.Equals(req.Payee) {
		return PayResult{}, types.ErrSelfPayment
	}
	if !req.FundsInModule {
		if err := k.bankKeeper.SendCoinsFromAccountToModule(ctx, req.Payer, types.ModuleName, sdk.NewCoins(req.Amount)); err != nil {
			return PayResult{}, err
		}
	}
	return k.distribute(ctx, params, asset, req)
}

// distribute assumes req.Amount is in the module and not on any ledger.
func (k Keeper) distribute(ctx sdk.Context, params types.Params, asset types.FeeAsset, req PayRequest) (PayResult, error) {
	denom := req.Amount.Denom
	amount := req.Amount.Amount
	fee := types.ComputeFee(amount, asset, params.FeeBps)
	net := amount.Sub(fee)

	hasApp := req.App != nil
	var appID uint64
	var referrerBps uint32
	if hasApp {
		appID = req.App.AppId
		referrerBps = req.App.ReferrerBps
	}
	hasReferrer := len(req.Referrer) > 0 && k.validRecipient(req.Referrer) && !req.Referrer.Equals(req.Payer)
	split := types.ComputeSplit(fee, params.Split, hasApp, hasReferrer, referrerBps)
	if !split.Total().Equal(fee) {
		// unreachable by construction (fuzzed), but never distribute money on a bug
		return PayResult{}, types.ErrInvariantViolation.Wrap("split does not sum to fee")
	}

	if net.IsPositive() {
		if err := k.bankKeeper.SendCoinsFromModuleToAccount(ctx, types.ModuleName, req.Payee, sdk.NewCoins(sdk.NewCoin(denom, net))); err != nil {
			return PayResult{}, err
		}
	}
	if split.Referrer.IsPositive() {
		if err := k.bankKeeper.SendCoinsFromModuleToAccount(ctx, types.ModuleName, req.Referrer, sdk.NewCoins(sdk.NewCoin(denom, split.Referrer))); err != nil {
			return PayResult{}, err
		}
	}
	if err := k.addClaimable(ctx, appID, denom, split.App); err != nil {
		return PayResult{}, err
	}
	if err := k.addPool(ctx, types.PoolValidator, denom, split.Validator); err != nil {
		return PayResult{}, err
	}
	if err := k.addPool(ctx, types.PoolRelayer, denom, split.Relayer); err != nil {
		return PayResult{}, err
	}
	if err := k.addPool(ctx, types.PoolTreasury, denom, split.Treasury); err != nil {
		return PayResult{}, err
	}

	tot, err := k.Totals.Get(ctx, denom)
	if err != nil {
		tot = types.Totals{Denom: denom, Volume: math.ZeroInt(), Fees: math.ZeroInt()}
	}
	tot.Volume = tot.Volume.Add(amount)
	tot.Fees = tot.Fees.Add(fee)
	tot.Payments++
	if err := k.Totals.Set(ctx, denom, tot); err != nil {
		return PayResult{}, err
	}

	if hasApp {
		weight := fee.Mul(asset.QuotaWeight)
		if err := k.appsKeeper.RecordSettlement(ctx, appID, req.Payer, weight); err != nil {
			return PayResult{}, err
		}
	}

	ctx.EventManager().EmitEvent(sdk.NewEvent("settled",
		sdk.NewAttribute("app_id", fmt.Sprint(appID)),
		sdk.NewAttribute("payer", req.Payer.String()),
		sdk.NewAttribute("payee", req.Payee.String()),
		sdk.NewAttribute("denom", denom),
		sdk.NewAttribute("amount", amount.String()),
		sdk.NewAttribute("fee", fee.String()),
		sdk.NewAttribute("net", net.String()),
		sdk.NewAttribute("app_share", split.App.String()),
		sdk.NewAttribute("referrer", req.Referrer.String()),
		sdk.NewAttribute("referrer_share", split.Referrer.String()),
		sdk.NewAttribute("validator_share", split.Validator.String()),
		sdk.NewAttribute("relayer_share", split.Relayer.String()),
		sdk.NewAttribute("treasury_share", split.Treasury.String()),
	))
	return PayResult{Fee: fee, Net: net, Split: split, AppID: appID}, nil
}

// ---- app allowances (payFrom) ----

// MaxAllowance mirrors ERC-20 "infinite approval": any allowance >= 2^255 is
// never decremented (the precompile clamps type(uint256).max down to this).
var MaxAllowance = math.NewIntFromBigInt(new(big.Int).Lsh(big.NewInt(1), 255))

func (k Keeper) Allowance(ctx sdk.Context, owner string, appID uint64, denom string) math.Int {
	v, _ := getInt(ctx, k.Allowances, collections.Join3(owner, appID, denom))
	return v
}

// ApproveApp lets owner authorize one specific app (not a shared spender) to
// pull up to amount via payFrom. Scoping allowances per app means a malicious
// app can never spend what a user approved for another app.
func (k Keeper) ApproveApp(ctx sdk.Context, owner sdk.AccAddress, appID uint64, denom string, amount math.Int) error {
	if amount.IsNegative() {
		return types.ErrInvalidAmount
	}
	if _, err := k.appsKeeper.GetApp(ctx, appID); err != nil {
		return err
	}
	key := collections.Join3(owner.String(), appID, denom)
	if amount.IsZero() {
		return k.Allowances.Remove(ctx, key)
	}
	if err := k.Allowances.Set(ctx, key, amount); err != nil {
		return err
	}
	ctx.EventManager().EmitEvent(sdk.NewEvent("settle_app_approval",
		sdk.NewAttribute("owner", owner.String()),
		sdk.NewAttribute("app_id", fmt.Sprint(appID)),
		sdk.NewAttribute("denom", denom),
		sdk.NewAttribute("amount", amount.String()),
	))
	return nil
}

// SpendAllowance consumes an app allowance (no-op for infinite approvals).
func (k Keeper) SpendAllowance(ctx sdk.Context, owner sdk.AccAddress, appID uint64, denom string, amount math.Int) error {
	key := collections.Join3(owner.String(), appID, denom)
	cur, err := getInt(ctx, k.Allowances, key)
	if err != nil {
		return err
	}
	if cur.LT(amount) {
		return types.ErrAllowance.Wrapf("have %s, need %s", cur, amount)
	}
	if cur.GTE(MaxAllowance) {
		return nil
	}
	next := cur.Sub(amount)
	if next.IsZero() {
		return k.Allowances.Remove(ctx, key)
	}
	return k.Allowances.Set(ctx, key, next)
}
