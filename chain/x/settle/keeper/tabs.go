// Copyright (c) 2026 VaporChain / muthu2201. All rights reserved.
// Proprietary and confidential. See LICENSE at the repository root.
// Provenance: VAPOR-6eabb1be532bdef4

package keeper

import (
	"fmt"

	"cosmossdk.io/math"

	sdk "github.com/cosmos/cosmos-sdk/types"

	appstypes "github.com/muthu2201/vapor-chain/chain/x/apps/types"
	"github.com/muthu2201/vapor-chain/chain/x/settle/types"
)

// Tabs batch micro-payments: each deposit is escrowed immediately (no credit
// risk for the payee), and the fee is computed once on the aggregate when the
// tab reaches tab_settle_threshold or is closed. This keeps a 1% fee a 1% fee
// even for 0.01 USDC tips, and turns N settlements into one.

// TabKey is the store key of a tab.
func TabKey(appID uint64, payer, payee sdk.AccAddress, denom string) string {
	return fmt.Sprintf("%d|%s|%s|%s", appID, payer.String(), payee.String(), denom)
}

// TabDeposit escrows amount into the (app, payer, payee, denom) tab and
// auto-settles it once the threshold is reached. Returns (settled, result).
func (k Keeper) TabDeposit(ctx sdk.Context, app *appstypes.App, payer, payee sdk.AccAddress, coin sdk.Coin, pullFromPayer bool) (bool, PayResult, error) {
	params := k.GetParams(ctx)
	asset, err := k.checkAsset(ctx, params, coin.Denom)
	if err != nil {
		return false, PayResult{}, err
	}
	if !coin.Amount.IsPositive() {
		return false, PayResult{}, types.ErrInvalidAmount
	}
	if !k.validRecipient(payee) || payer.Equals(payee) {
		return false, PayResult{}, types.ErrInvalidRecipient
	}
	if pullFromPayer {
		if err := k.bankKeeper.SendCoinsFromAccountToModule(ctx, payer, types.ModuleName, sdk.NewCoins(coin)); err != nil {
			return false, PayResult{}, err
		}
	}
	var appID uint64
	if app != nil {
		appID = app.AppId
	}
	key := TabKey(appID, payer, payee, coin.Denom)
	tab, err := k.Tabs.Get(ctx, key)
	if err != nil {
		tab = types.Tab{AppId: appID, Payer: payer.String(), Payee: payee.String(), Denom: coin.Denom, Amount: math.ZeroInt(), OpenedHeight: ctx.BlockHeight()}
	}
	tab.Amount = tab.Amount.Add(coin.Amount)
	tab.Entries++
	if err := k.ledger(ctx, coin.Denom, coin.Amount); err != nil {
		return false, PayResult{}, err
	}
	if tab.Amount.GTE(asset.TabSettleThreshold) {
		res, err := k.settleTab(ctx, key, tab, app)
		return true, res, err
	}
	if err := k.Tabs.Set(ctx, key, tab); err != nil {
		return false, PayResult{}, err
	}
	ctx.EventManager().EmitEvent(sdk.NewEvent("settle_tab_deposit",
		sdk.NewAttribute("app_id", fmt.Sprint(appID)),
		sdk.NewAttribute("payer", payer.String()),
		sdk.NewAttribute("payee", payee.String()),
		sdk.NewAttribute("denom", coin.Denom),
		sdk.NewAttribute("amount", coin.Amount.String()),
		sdk.NewAttribute("tab_total", tab.Amount.String()),
	))
	return false, PayResult{}, nil
}

// settleTab pays out an escrowed tab (funds already in the module).
func (k Keeper) settleTab(ctx sdk.Context, key string, tab types.Tab, app *appstypes.App) (PayResult, error) {
	if err := k.Tabs.Remove(ctx, key); err != nil {
		return PayResult{}, err
	}
	// the escrow leaves the tab ledger; distribute() re-books fee shares
	if err := k.ledger(ctx, tab.Denom, tab.Amount.Neg()); err != nil {
		return PayResult{}, err
	}
	payer := sdk.MustAccAddressFromBech32(tab.Payer)
	payee := sdk.MustAccAddressFromBech32(tab.Payee)
	params := k.GetParams(ctx)
	asset, ok := params.Asset(tab.Denom)
	if !ok {
		// asset delisted while the tab was open: refund the payer in full
		return PayResult{Fee: math.ZeroInt(), Net: math.ZeroInt()}, k.bankKeeper.SendCoinsFromModuleToAccount(ctx, types.ModuleName, payer, sdk.NewCoins(sdk.NewCoin(tab.Denom, tab.Amount)))
	}
	if app == nil && tab.AppId != appstypes.NoApp {
		if a, err := k.appsKeeper.GetApp(ctx, tab.AppId); err == nil && a.Status == appstypes.APP_STATUS_ACTIVE {
			app = &a
		}
	}
	res, err := k.distribute(ctx, params, asset, PayRequest{
		Payer: payer, Payee: payee, Amount: sdk.NewCoin(tab.Denom, tab.Amount), App: app, FundsInModule: true,
	})
	if err != nil {
		return PayResult{}, err
	}
	ctx.EventManager().EmitEvent(sdk.NewEvent("settle_tab_closed",
		sdk.NewAttribute("app_id", fmt.Sprint(tab.AppId)),
		sdk.NewAttribute("payer", tab.Payer),
		sdk.NewAttribute("payee", tab.Payee),
		sdk.NewAttribute("denom", tab.Denom),
		sdk.NewAttribute("total", tab.Amount.String()),
		sdk.NewAttribute("entries", fmt.Sprint(tab.Entries)),
	))
	return res, nil
}

// CloseTab settles a tab on request. Allowed callers: the payer, the payee,
// the app owner, or anyone once the tab is older than max_tab_age_blocks
// (so escrow can never be stranded).
func (k Keeper) CloseTab(ctx sdk.Context, sender sdk.AccAddress, appID uint64, payer, payee sdk.AccAddress, denom string) (PayResult, error) {
	key := TabKey(appID, payer, payee, denom)
	tab, err := k.Tabs.Get(ctx, key)
	if err != nil {
		return PayResult{}, types.ErrTabNotFound
	}
	allowed := sender.Equals(payer) || sender.Equals(payee)
	var app *appstypes.App
	if appID != appstypes.NoApp {
		if a, err := k.appsKeeper.GetApp(ctx, appID); err == nil {
			if a.Owner == sender.String() {
				allowed = true
			}
			if a.Status == appstypes.APP_STATUS_ACTIVE {
				app = &a
			}
		}
	}
	if ctx.BlockHeight()-tab.OpenedHeight >= k.GetParams(ctx).MaxTabAgeBlocks {
		allowed = true
	}
	if !allowed {
		return PayResult{}, types.ErrUnauthorized.Wrap("only payer, payee, app owner (or anyone after max_tab_age_blocks) may close a tab")
	}
	return k.settleTab(ctx, key, tab, app)
}
