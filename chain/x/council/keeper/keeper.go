// Copyright (c) 2026 VaporChain / muthu2201. All rights reserved.
// Proprietary and confidential. See LICENSE at the repository root.
// Provenance: VAPOR-6eabb1be532bdef4

package keeper

import (
	"errors"
	"fmt"
	"strings"

	"cosmossdk.io/collections"
	"cosmossdk.io/core/store"
	"cosmossdk.io/log/v2"
	"cosmossdk.io/math"

	"github.com/cosmos/cosmos-sdk/codec"
	sdk "github.com/cosmos/cosmos-sdk/types"

	"github.com/muthu2201/vapor-chain/chain/constants"
	"github.com/muthu2201/vapor-chain/chain/provenance"
	"github.com/muthu2201/vapor-chain/chain/x/council/types"
)

// Keeper of x/council.
//
// WHY A CUSTOM MODULE: the Cosmos SDK Enterprise POA module is published
// under a non-commercial evaluation licence, so VaporChain implements
// Proof-of-Authority on top of Apache-2.0 x/staking instead: validator power
// is a non-transferable bond denom that only this keeper can mint, and a
// staking hook rejects any validator whose operator was not admitted. As a
// bonus x/staking keeps HistoricalInfo, which ibc-go needs for connection
// handshakes (blueprint risk #11 disappears).
type Keeper struct {
	cdc          codec.BinaryCodec
	storeService store.KVStoreService
	authority    string

	accountKeeper  types.AccountKeeper
	bankKeeper     types.BankKeeper
	stakingKeeper  types.StakingKeeper
	slashingKeeper types.SlashingKeeper

	Schema     collections.Schema
	Params     collections.Item[types.Params]
	Admissions collections.Map[string, types.Admission]
	Pauses     collections.Map[string, types.Pause]
	Provenance collections.Item[types.Provenance]
}

func NewKeeper(
	cdc codec.BinaryCodec,
	storeService store.KVStoreService,
	authority string,
	ak types.AccountKeeper,
	bk types.BankKeeper,
	sk types.StakingKeeper,
	slk types.SlashingKeeper,
) Keeper {
	if _, err := sdk.AccAddressFromBech32(authority); err != nil {
		panic(fmt.Errorf("council: invalid authority %q: %w", authority, err))
	}
	sb := collections.NewSchemaBuilder(storeService)
	k := Keeper{
		cdc:            cdc,
		storeService:   storeService,
		authority:      authority,
		accountKeeper:  ak,
		bankKeeper:     bk,
		stakingKeeper:  sk,
		slashingKeeper: slk,
		Params:         collections.NewItem(sb, types.ParamsKey, "params", codec.CollValue[types.Params](cdc)),
		Admissions:     collections.NewMap(sb, types.AdmissionsKey, "admissions", collections.StringKey, codec.CollValue[types.Admission](cdc)),
		Pauses:         collections.NewMap(sb, types.PausesKey, "pauses", collections.StringKey, codec.CollValue[types.Pause](cdc)),
		Provenance:     collections.NewItem(sb, types.ProvenanceKey, "provenance", codec.CollValue[types.Provenance](cdc)),
	}
	schema, err := sb.Build()
	if err != nil {
		panic(err)
	}
	k.Schema = schema
	return k
}

// SetSlashingKeeper breaks the construction cycle staking -> hooks -> council -> slashing.
func (k *Keeper) SetSlashingKeeper(slk types.SlashingKeeper) { k.slashingKeeper = slk }

func (k Keeper) Authority() string { return k.authority }

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

// IsAdmitted reports whether an operator may run a validator.
func (k Keeper) IsAdmitted(ctx sdk.Context, operator sdk.AccAddress) (bool, error) {
	a, err := k.Admissions.Get(ctx, operator.String())
	if errors.Is(err, collections.ErrNotFound) {
		return false, nil
	}
	if err != nil {
		return false, err
	}
	return !a.Removed, nil
}

// IsRemoved reports whether an operator has been permanently removed.
func (k Keeper) IsRemoved(ctx sdk.Context, operator sdk.AccAddress) bool {
	a, err := k.Admissions.Get(ctx, operator.String())
	return err == nil && a.Removed
}

// isPauseActive applies the guardian expiry rule.
func isPauseActive(ctx sdk.Context, p types.Pause) bool {
	return p.ExpiresHeight == 0 || ctx.BlockHeight() < p.ExpiresHeight
}

// IsPaused checks an exact target.
func (k Keeper) IsPaused(ctx sdk.Context, target string) bool {
	p, err := k.Pauses.Get(ctx, target)
	if err != nil {
		return false
	}
	return isPauseActive(ctx, p)
}

// IsSettlePaused is the check used by x/settle and the Settle precompile.
func (k Keeper) IsSettlePaused(ctx sdk.Context, denom string) bool {
	return k.IsPaused(ctx, types.TargetSettle) || k.IsPaused(ctx, types.TargetSettlePrefix+denom)
}

// IsIBCPaused is the check used by the IBC circuit-breaker middleware. Any of
// the supplied denom spellings (base, full trace path, ibc/HASH) may match.
func (k Keeper) IsIBCPaused(ctx sdk.Context, denoms ...string) bool {
	if k.IsPaused(ctx, types.TargetIBC) {
		return true
	}
	for _, d := range denoms {
		if d != "" && k.IsPaused(ctx, types.TargetIBCPrefix+d) {
			return true
		}
	}
	return false
}

// SetPause is used by the msg server and by x/settle's safe-mode (invariant
// breach auto-pause).
func (k Keeper) SetPause(ctx sdk.Context, target, setBy, reason string, expires int64) error {
	if err := types.ValidateTarget(target); err != nil {
		return err
	}
	return k.Pauses.Set(ctx, target, types.Pause{
		Target: target, SetBy: setBy, SetHeight: ctx.BlockHeight(), ExpiresHeight: expires, Reason: reason,
	})
}

// AdmitValidator allowlists `operator` and mints `power` of the non-transferable
// power denom to it. Admission is additive (top-ups are allowed) but a removed
// operator can never be re-admitted.
func (k Keeper) AdmitValidator(ctx sdk.Context, operator sdk.AccAddress, power math.Int, memo string) error {
	params := k.GetParams(ctx)
	if power.IsNil() || !power.IsPositive() || power.GT(params.MaxPowerPerValidator) {
		return types.ErrInvalidPower.Wrapf("power must be in (0, %s]", params.MaxPowerPerValidator)
	}
	adm, err := k.Admissions.Get(ctx, operator.String())
	switch {
	case errors.Is(err, collections.ErrNotFound):
		adm = types.Admission{Operator: operator.String(), PowerGranted: math.ZeroInt(), AdmittedHeight: ctx.BlockHeight()}
	case err != nil:
		return err
	case adm.Removed:
		return types.ErrRemoved
	}
	total := adm.PowerGranted.Add(power)
	if total.GT(params.MaxPowerPerValidator) {
		return types.ErrInvalidPower.Wrapf("cumulative power %s exceeds cap %s", total, params.MaxPowerPerValidator)
	}
	adm.PowerGranted = total
	adm.Memo = memo
	coins := sdk.NewCoins(sdk.NewCoin(constants.PowerDenom, power))
	if err := k.bankKeeper.MintCoins(ctx, types.ModuleName, coins); err != nil {
		return err
	}
	if err := k.bankKeeper.SendCoinsFromModuleToAccount(ctx, types.ModuleName, operator, coins); err != nil {
		return err
	}
	if err := k.Admissions.Set(ctx, operator.String(), adm); err != nil {
		return err
	}
	ctx.EventManager().EmitEvent(sdk.NewEvent("council_admit_validator",
		sdk.NewAttribute("operator", operator.String()),
		sdk.NewAttribute("power", power.String()),
		sdk.NewAttribute("total_power", total.String()),
	))
	return nil
}

// RemoveValidator permanently bans an operator and forces its validator out of
// the active set (jail + tombstone when a signing record exists).
func (k Keeper) RemoveValidator(ctx sdk.Context, operator sdk.AccAddress, reason string) error {
	adm, err := k.Admissions.Get(ctx, operator.String())
	if errors.Is(err, collections.ErrNotFound) {
		return types.ErrNotAdmitted
	}
	if err != nil {
		return err
	}
	if adm.Removed {
		return types.ErrRemoved
	}
	adm.Removed = true
	adm.Memo = reason
	if err := k.Admissions.Set(ctx, operator.String(), adm); err != nil {
		return err
	}

	val, err := k.stakingKeeper.GetValidator(ctx, sdk.ValAddress(operator))
	if err == nil {
		consBz, err := val.GetConsAddr()
		if err != nil {
			return err
		}
		consAddr := sdk.ConsAddress(consBz)
		// staking panics when jailing an already-jailed validator: check first.
		if !val.IsJailed() {
			if err := k.slashingKeeper.Jail(ctx, consAddr); err != nil {
				return err
			}
		}
		if k.slashingKeeper.HasValidatorSigningInfo(ctx, consAddr) && !k.slashingKeeper.IsTombstoned(ctx, consAddr) {
			if err := k.slashingKeeper.Tombstone(ctx, consAddr); err != nil {
				return err
			}
		}
	}
	ctx.EventManager().EmitEvent(sdk.NewEvent("council_remove_validator",
		sdk.NewAttribute("operator", operator.String()),
		sdk.NewAttribute("reason", reason),
	))
	return nil
}

// PruneExpiredPauses deletes guardian pauses whose expiry passed. Bounded by
// the (tiny) number of pause entries.
func (k Keeper) PruneExpiredPauses(ctx sdk.Context) error {
	var expired []string
	err := k.Pauses.Walk(ctx, nil, func(key string, p types.Pause) (bool, error) {
		if !isPauseActive(ctx, p) {
			expired = append(expired, key)
		}
		return false, nil
	})
	if err != nil {
		return err
	}
	for _, key := range expired {
		if err := k.Pauses.Remove(ctx, key); err != nil {
			return err
		}
		ctx.EventManager().EmitEvent(sdk.NewEvent("council_pause_expired", sdk.NewAttribute("target", key)))
	}
	return nil
}

// ProvenanceRecord returns the immutable on-chain authorship record.
func (k Keeper) ProvenanceRecord(ctx sdk.Context) types.Provenance {
	p, err := k.Provenance.Get(ctx)
	if err != nil {
		return types.Provenance{}
	}
	return p
}

// DefaultProvenance builds the record compiled into this binary.
func DefaultProvenance(chainID string) types.Provenance {
	return types.Provenance{
		Fingerprint: provenance.Fingerprint,
		Seal:        provenance.Seal(chainID),
		Owner:       provenance.Owner,
		Notice:      provenance.Notice,
	}
}

// normalizeTarget trims whitespace; targets are case-sensitive denoms.
func normalizeTarget(t string) string { return strings.TrimSpace(t) }
