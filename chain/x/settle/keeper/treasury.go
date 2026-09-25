// Copyright (c) 2026 VaporChain / muthu2201. All rights reserved.
// Proprietary and confidential. See LICENSE at the repository root.
// Provenance: VAPOR-6eabb1be532bdef4

package keeper

import (
	"fmt"

	"cosmossdk.io/math"

	sdk "github.com/cosmos/cosmos-sdk/types"
	authtypes "github.com/cosmos/cosmos-sdk/x/auth/types"

	appstypes "github.com/muthu2201/vapor-chain/chain/x/apps/types"
	"github.com/muthu2201/vapor-chain/chain/x/settle/types"
)

// ClaimRevenue pays an app's claimable balances to its revenue_recipient.
// Anyone may trigger it (money only ever goes to the recipient the owner
// chose). Pull-based payouts avoid reentrancy and let governance freeze a
// revoked app's balance.
func (k Keeper) ClaimRevenue(ctx sdk.Context, appID uint64, denoms []string) (sdk.Coins, error) {
	app, err := k.appsKeeper.GetApp(ctx, appID)
	if err != nil {
		return nil, err
	}
	if app.Status == appstypes.APP_STATUS_REVOKED {
		return nil, types.ErrAppRevoked
	}
	recipient, err := sdk.AccAddressFromBech32(app.RevenueRecipient)
	if err != nil {
		return nil, err
	}
	if len(denoms) == 0 {
		for _, a := range k.GetParams(ctx).Assets {
			denoms = append(denoms, a.Denom)
		}
	}
	claimed := sdk.NewCoins()
	seen := map[string]bool{}
	for _, d := range denoms {
		if seen[d] {
			continue
		}
		seen[d] = true
		amt := k.ClaimableOf(ctx, appID, d)
		if !amt.IsPositive() {
			continue
		}
		if err := k.addClaimable(ctx, appID, d, amt.Neg()); err != nil {
			return nil, err
		}
		claimed = claimed.Add(sdk.NewCoin(d, amt))
	}
	if claimed.IsZero() {
		return nil, types.ErrNothingToClaim
	}
	if err := k.bankKeeper.SendCoinsFromModuleToAccount(ctx, types.ModuleName, recipient, claimed); err != nil {
		return nil, err
	}
	ctx.EventManager().EmitEvent(sdk.NewEvent("settle_revenue_claimed",
		sdk.NewAttribute("app_id", fmt.Sprint(appID)),
		sdk.NewAttribute("recipient", recipient.String()),
		sdk.NewAttribute("amount", claimed.String()),
	))
	return claimed, nil
}

// QuoteCredits returns how many acredit `amount` of `denom` buys.
func (k Keeper) QuoteCredits(ctx sdk.Context, denom string, amount math.Int) (math.Int, error) {
	params := k.GetParams(ctx)
	asset, ok := params.Asset(denom)
	if !ok || !asset.Enabled || !params.CreditsEnabled || !asset.CreditPrice.IsPositive() {
		return math.ZeroInt(), types.ErrCreditsDisabled.Wrapf("%s", denom)
	}
	return amount.Mul(asset.CreditPrice), nil
}

// BuyCredits sells gas credits at the governance-fixed price. The payment goes
// to the treasury pool; credits are MINTED (never taken from a float), are not
// redeemable and cannot leave the chain. This is a prepaid compute voucher,
// not an investment asset: there is no market, no oracle and no redemption.
func (k Keeper) BuyCredits(ctx sdk.Context, buyer, recipient sdk.AccAddress, payment sdk.Coin) (sdk.Coin, error) {
	if !payment.Amount.IsPositive() {
		return sdk.Coin{}, types.ErrInvalidAmount
	}
	if k.councilKeeper.IsSettlePaused(ctx, payment.Denom) {
		return sdk.Coin{}, types.ErrPaused
	}
	credits, err := k.QuoteCredits(ctx, payment.Denom, payment.Amount)
	if err != nil {
		return sdk.Coin{}, err
	}
	if len(recipient) == 0 {
		recipient = buyer
	}
	if !k.validRecipient(recipient) {
		return sdk.Coin{}, types.ErrInvalidRecipient
	}
	if err := k.bankKeeper.SendCoinsFromAccountToModule(ctx, buyer, types.ModuleName, sdk.NewCoins(payment)); err != nil {
		return sdk.Coin{}, err
	}
	if err := k.addPool(ctx, types.PoolTreasury, payment.Denom, payment.Amount); err != nil {
		return sdk.Coin{}, err
	}
	out, err := k.mintCredits(ctx, recipient, credits)
	if err != nil {
		return sdk.Coin{}, err
	}
	ctx.EventManager().EmitEvent(sdk.NewEvent("settle_credits_bought",
		sdk.NewAttribute("buyer", buyer.String()),
		sdk.NewAttribute("recipient", recipient.String()),
		sdk.NewAttribute("payment", payment.String()),
		sdk.NewAttribute("credits", out.String()),
	))
	return out, nil
}

func (k Keeper) mintCredits(ctx sdk.Context, to sdk.AccAddress, amount math.Int) (sdk.Coin, error) {
	coin := sdk.NewCoin(k.creditDenom, amount)
	if err := k.bankKeeper.MintCoins(ctx, types.ModuleName, sdk.NewCoins(coin)); err != nil {
		return sdk.Coin{}, err
	}
	if err := k.bankKeeper.SendCoinsFromModuleToAccount(ctx, types.ModuleName, to, sdk.NewCoins(coin)); err != nil {
		return sdk.Coin{}, err
	}
	minted, err := k.CreditsMinted.Get(ctx)
	if err != nil {
		minted = math.ZeroInt()
	}
	return coin, k.CreditsMinted.Set(ctx, minted.Add(amount))
}

// WithdrawTreasury moves treasury-pool funds (governance or treasury_admin).
func (k Keeper) WithdrawTreasury(ctx sdk.Context, signer string, recipient sdk.AccAddress, amount sdk.Coins) error {
	params := k.GetParams(ctx)
	if signer != k.authority && (params.TreasuryAdmin == "" || signer != params.TreasuryAdmin) {
		return types.ErrUnauthorized.Wrap("only governance or treasury_admin")
	}
	if !amount.IsValid() || amount.IsZero() {
		return types.ErrInvalidAmount
	}
	if !k.validRecipient(recipient) {
		return types.ErrInvalidRecipient
	}
	for _, c := range amount {
		if k.PoolBalance(ctx, types.PoolTreasury, c.Denom).LT(c.Amount) {
			return types.ErrInsufficientPool.Wrapf("treasury %s", c.Denom)
		}
		if err := k.addPool(ctx, types.PoolTreasury, c.Denom, c.Amount.Neg()); err != nil {
			return err
		}
	}
	if err := k.bankKeeper.SendCoinsFromModuleToAccount(ctx, types.ModuleName, recipient, amount); err != nil {
		return err
	}
	ctx.EventManager().EmitEvent(sdk.NewEvent("settle_treasury_withdrawn",
		sdk.NewAttribute("signer", signer), sdk.NewAttribute("recipient", recipient.String()), sdk.NewAttribute("amount", amount.String())))
	return nil
}

// DisburseRelayerPool pays bundlers/IBC relayers from the relayer pool, only to
// allowlisted recipients and within the per-interval cap (so a stolen
// relayer_admin key can drain at most one interval's cap).
func (k Keeper) DisburseRelayerPool(ctx sdk.Context, signer string, recipient sdk.AccAddress, amount sdk.Coin, asCredits bool) (sdk.Coin, error) {
	params := k.GetParams(ctx)
	isAuthority := signer == k.authority
	if !isAuthority && (params.RelayerAdmin == "" || signer != params.RelayerAdmin) {
		return sdk.Coin{}, types.ErrUnauthorized.Wrap("only governance or relayer_admin")
	}
	if !amount.Amount.IsPositive() {
		return sdk.Coin{}, types.ErrInvalidAmount
	}
	if !params.IsRelayerRecipient(recipient.String()) {
		return sdk.Coin{}, types.ErrInvalidRecipient.Wrap("recipient is not an allowlisted relayer/bundler")
	}
	if k.PoolBalance(ctx, types.PoolRelayer, amount.Denom).LT(amount.Amount) {
		return sdk.Coin{}, types.ErrInsufficientPool.Wrapf("relayer %s", amount.Denom)
	}
	if !isAuthority {
		paid, _ := getInt(ctx, k.RelayerPaid, amount.Denom)
		limit := params.RelayerPayoutCap.AmountOf(amount.Denom)
		if paid.Add(amount.Amount).GT(limit) {
			return sdk.Coin{}, types.ErrRelayerCap.Wrapf("paid %s + %s > cap %s", paid, amount.Amount, limit)
		}
		if _, err := addInt(ctx, k.RelayerPaid, amount.Denom, amount.Amount); err != nil {
			return sdk.Coin{}, err
		}
	}
	if err := k.addPool(ctx, types.PoolRelayer, amount.Denom, amount.Amount.Neg()); err != nil {
		return sdk.Coin{}, err
	}
	var out sdk.Coin
	if asCredits {
		credits, err := k.QuoteCredits(ctx, amount.Denom, amount.Amount)
		if err != nil {
			return sdk.Coin{}, err
		}
		// the asset stays in the module as credit-sale revenue of the treasury
		if err := k.addPool(ctx, types.PoolTreasury, amount.Denom, amount.Amount); err != nil {
			return sdk.Coin{}, err
		}
		if out, err = k.mintCredits(ctx, recipient, credits); err != nil {
			return sdk.Coin{}, err
		}
	} else {
		if err := k.bankKeeper.SendCoinsFromModuleToAccount(ctx, types.ModuleName, recipient, sdk.NewCoins(amount)); err != nil {
			return sdk.Coin{}, err
		}
		out = amount
	}
	ctx.EventManager().EmitEvent(sdk.NewEvent("settle_relayer_disbursed",
		sdk.NewAttribute("signer", signer), sdk.NewAttribute("recipient", recipient.String()),
		sdk.NewAttribute("amount", amount.String()), sdk.NewAttribute("paid", out.String())))
	return out, nil
}

// BurnGasFees permanently destroys a governance-set fraction (gas_burn_bps) of
// the EVM gas fees (credit denom) that accumulated in the fee collector during
// the previous block, BEFORE x/distribution allocates them to validators
// (x/settle is ordered ahead of x/distribution in the begin-blockers).
//
// WHY BURN GAS: it turns the gas credit into a deflationary, bought-and-consumed
// compute voucher. Credits are minted ONLY by BuyCredits at the governance
// price (USDC -> treasury) and can never be sold back (SendEnabled=false), so
// burning what is spent forces the circulating supply to be replenished by more
// USDC purchases. Every unit of computation on the chain — by a registered app
// or a completely independent deployment — therefore converts, over time, into
// protocol (treasury) revenue. Validators are compensated from the USDC
// validator pool (see PayoutValidators), not from gas, so burning gas costs
// them nothing. It never fails a block: any error is logged and skipped.
func (k Keeper) BurnGasFees(ctx sdk.Context) {
	bps := k.GetParams(ctx).GasBurnBps
	if bps == 0 {
		return
	}
	feeCollector := k.accountKeeper.GetModuleAddress(authtypes.FeeCollectorName)
	if feeCollector == nil {
		return
	}
	bal := k.bankKeeper.GetBalance(ctx, feeCollector, k.creditDenom).Amount
	if !bal.IsPositive() {
		return
	}
	burn := bal
	if bps < types.MaxBps {
		burn = bal.Mul(math.NewInt(int64(bps))).Quo(math.NewInt(types.MaxBps))
	}
	if !burn.IsPositive() {
		return
	}
	coins := sdk.NewCoins(sdk.NewCoin(k.creditDenom, burn))
	if err := k.bankKeeper.SendCoinsFromModuleToModule(ctx, authtypes.FeeCollectorName, types.ModuleName, coins); err != nil {
		k.Logger(ctx).Error("gas burn: move from fee collector failed", "err", err)
		return
	}
	if err := k.bankKeeper.BurnCoins(ctx, types.ModuleName, coins); err != nil {
		k.Logger(ctx).Error("gas burn: burn failed", "err", err)
		return
	}
	ctx.EventManager().EmitEvent(sdk.NewEvent("settle_gas_burned",
		sdk.NewAttribute("denom", k.creditDenom),
		sdk.NewAttribute("amount", burn.String()),
	))
}
