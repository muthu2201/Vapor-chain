// SPDX-License-Identifier: AGPL-3.0-only
// Copyright (c) 2026 VaporChain / muthu2201
// Provenance: VAPOR-6eabb1be532bdef4

package keeper

import (
	"errors"
	"fmt"

	"cosmossdk.io/collections"
	"cosmossdk.io/core/store"
	"cosmossdk.io/log/v2"
	"cosmossdk.io/math"

	"github.com/cosmos/cosmos-sdk/codec"
	sdk "github.com/cosmos/cosmos-sdk/types"

	"github.com/muthu2201/vapor-chain/chain/x/settle/types"
)

// InvariantCheckInterval is how often (in blocks) the cheap on-chain
// conservation checks run. A breach auto-pauses Settle ("safe mode") instead
// of halting the chain, and emits settle_invariant_broken for paging.
const InvariantCheckInterval = 100

// Keeper of x/settle.
//
// Money model: every asset Settle holds lives in ONE module account. Internal
// ledgers (claimables per app, three protocol pools, open tabs) partition that
// balance. LedgerTotal[denom] is the running sum of all three ledgers and the
// invariant is  bank_balance(settle, denom) >= LedgerTotal[denom]  (">=" and
// not "==" so that a stray donation to the module can never trip safe mode).
type Keeper struct {
	cdc          codec.BinaryCodec
	storeService store.KVStoreService
	authority    string

	accountKeeper  types.AccountKeeper
	bankKeeper     types.BankKeeper
	appsKeeper     types.AppsKeeper
	councilKeeper  types.CouncilKeeper
	stakingKeeper  types.StakingKeeper
	slashingKeeper types.SlashingKeeper
	creditDenom    string

	Schema              collections.Schema
	Params              collections.Item[types.Params]
	Claimables          collections.Map[collections.Pair[uint64, string], math.Int]
	Pools               collections.Map[collections.Pair[string, string], math.Int]
	Tabs                collections.Map[string, types.Tab]
	Allowances          collections.Map[collections.Triple[string, uint64, string], math.Int]
	Totals              collections.Map[string, types.Totals]
	CreditsMinted       collections.Item[math.Int]
	GenesisCreditSupply collections.Item[math.Int]
	LedgerTotal         collections.Map[string, math.Int]
	RelayerPaid         collections.Map[string, math.Int]
	LastPayout          collections.Item[int64]
}

func NewKeeper(
	cdc codec.BinaryCodec,
	storeService store.KVStoreService,
	authority string,
	creditDenom string,
	ak types.AccountKeeper,
	bk types.BankKeeper,
	appsK types.AppsKeeper,
	councilK types.CouncilKeeper,
	sk types.StakingKeeper,
	slk types.SlashingKeeper,
) Keeper {
	if _, err := sdk.AccAddressFromBech32(authority); err != nil {
		panic(fmt.Errorf("settle: invalid authority %q: %w", authority, err))
	}
	sb := collections.NewSchemaBuilder(storeService)
	k := Keeper{
		cdc: cdc, storeService: storeService, authority: authority,
		accountKeeper: ak, bankKeeper: bk, appsKeeper: appsK, councilKeeper: councilK,
		stakingKeeper: sk, slashingKeeper: slk, creditDenom: creditDenom,
		Params:              collections.NewItem(sb, types.ParamsKey, "params", codec.CollValue[types.Params](cdc)),
		Claimables:          collections.NewMap(sb, types.ClaimablesKey, "claimables", collections.PairKeyCodec(collections.Uint64Key, collections.StringKey), sdk.IntValue),
		Pools:               collections.NewMap(sb, types.PoolsKey, "pools", collections.PairKeyCodec(collections.StringKey, collections.StringKey), sdk.IntValue),
		Tabs:                collections.NewMap(sb, types.TabsKey, "tabs", collections.StringKey, codec.CollValue[types.Tab](cdc)),
		Allowances:          collections.NewMap(sb, types.AllowancesKey, "allowances", collections.TripleKeyCodec(collections.StringKey, collections.Uint64Key, collections.StringKey), sdk.IntValue),
		Totals:              collections.NewMap(sb, types.TotalsKey, "totals", collections.StringKey, codec.CollValue[types.Totals](cdc)),
		CreditsMinted:       collections.NewItem(sb, types.CreditsMintedKey, "credits_minted", sdk.IntValue),
		GenesisCreditSupply: collections.NewItem(sb, types.GenesisCreditSupplyKey, "genesis_credit_supply", sdk.IntValue),
		LedgerTotal:         collections.NewMap(sb, types.LedgerTotalKey, "ledger_total", collections.StringKey, sdk.IntValue),
		RelayerPaid:         collections.NewMap(sb, types.RelayerPaidKey, "relayer_paid", collections.StringKey, sdk.IntValue),
		LastPayout:          collections.NewItem(sb, types.LastPayoutKey, "last_payout", collections.Int64Value),
	}
	schema, err := sb.Build()
	if err != nil {
		panic(err)
	}
	k.Schema = schema
	return k
}

func (k Keeper) Authority() string   { return k.authority }
func (k Keeper) CreditDenom() string { return k.creditDenom }

func (k Keeper) Logger(ctx sdk.Context) log.Logger {
	return ctx.Logger().With("module", "x/"+types.ModuleName)
}

func (k Keeper) GetParams(ctx sdk.Context) types.Params {
	p, err := k.Params.Get(ctx)
	if err != nil {
		return types.DefaultParams()
	}
	return p
}

// SetParams validates and stores params (used by tests and genesis; the Msg
// path validates then calls Params.Set directly).
func (k Keeper) SetParams(ctx sdk.Context, p types.Params) error {
	if err := p.Validate(); err != nil {
		return err
	}
	return k.Params.Set(ctx, p)
}

func (k Keeper) moduleAddr() sdk.AccAddress {
	return k.accountKeeper.GetModuleAddress(types.ModuleName)
}

func getInt[K any](ctx sdk.Context, m collections.Map[K, math.Int], key K) (math.Int, error) {
	v, err := m.Get(ctx, key)
	if errors.Is(err, collections.ErrNotFound) {
		return math.ZeroInt(), nil
	}
	return v, err
}

// addInt adds delta (may be negative) to m[key]; it refuses to go negative and
// deletes zero entries so state does not accumulate empty rows.
func addInt[K any](ctx sdk.Context, m collections.Map[K, math.Int], key K, delta math.Int) (math.Int, error) {
	cur, err := getInt(ctx, m, key)
	if err != nil {
		return cur, err
	}
	next := cur.Add(delta)
	if next.IsNegative() {
		return cur, types.ErrInsufficientPool.Wrapf("ledger would go negative (%s + %s)", cur, delta)
	}
	if next.IsZero() {
		return next, m.Remove(ctx, key)
	}
	return next, m.Set(ctx, key, next)
}

func (k Keeper) ledger(ctx sdk.Context, denom string, delta math.Int) error {
	_, err := addInt(ctx, k.LedgerTotal, denom, delta)
	return err
}

func (k Keeper) addPool(ctx sdk.Context, pool, denom string, delta math.Int) error {
	if delta.IsZero() {
		return nil
	}
	if _, err := addInt(ctx, k.Pools, collections.Join(pool, denom), delta); err != nil {
		return err
	}
	return k.ledger(ctx, denom, delta)
}

func (k Keeper) addClaimable(ctx sdk.Context, appID uint64, denom string, delta math.Int) error {
	if delta.IsZero() {
		return nil
	}
	if _, err := addInt(ctx, k.Claimables, collections.Join(appID, denom), delta); err != nil {
		return err
	}
	return k.ledger(ctx, denom, delta)
}

// PoolBalance returns an internal pool balance.
func (k Keeper) PoolBalance(ctx sdk.Context, pool, denom string) math.Int {
	v, _ := getInt(ctx, k.Pools, collections.Join(pool, denom))
	return v
}

// ClaimableOf returns revenue owed to an app in one denom.
func (k Keeper) ClaimableOf(ctx sdk.Context, appID uint64, denom string) math.Int {
	v, _ := getInt(ctx, k.Claimables, collections.Join(appID, denom))
	return v
}

// checkAsset returns the enabled, un-paused asset config for denom.
func (k Keeper) checkAsset(ctx sdk.Context, params types.Params, denom string) (types.FeeAsset, error) {
	asset, ok := params.Asset(denom)
	if !ok || !asset.Enabled {
		return asset, types.ErrAssetNotAllowed.Wrapf("%s", denom)
	}
	if k.councilKeeper.IsSettlePaused(ctx, denom) {
		return asset, types.ErrPaused.Wrapf("%s", denom)
	}
	return asset, nil
}

func (k Keeper) validRecipient(addr sdk.AccAddress) bool {
	return len(addr) > 0 && !k.bankKeeper.BlockedAddr(addr) && !addr.Equals(k.moduleAddr())
}
