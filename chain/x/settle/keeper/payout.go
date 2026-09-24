// Copyright (c) 2026 VaporChain / muthu2201. All rights reserved.
// Proprietary and confidential. See LICENSE at the repository root.
// Provenance: VAPOR-6eabb1be532bdef4

package keeper

import (
	"fmt"

	"cosmossdk.io/collections"
	"cosmossdk.io/math"

	sdk "github.com/cosmos/cosmos-sdk/types"

	"github.com/muthu2201/vapor-chain/chain/x/settle/types"
)

type payee struct {
	addr   sdk.AccAddress
	weight math.Int
}

// PayoutValidators distributes the validator pool to bonded validators,
// weighted by   consensus power x uptime   where uptime comes from x/slashing's
// missed-block counter over the signing window. This replaces the blueprint's
// "monthly stipend voted by a group policy" with a trustless on-chain rule:
// no human decides who gets paid, and a validator that is offline half the
// window earns half. Rounding dust stays in the pool for the next interval.
func (k Keeper) PayoutValidators(ctx sdk.Context) error {
	params := k.GetParams(ctx)
	last, err := k.LastPayout.Get(ctx)
	if err != nil {
		last = 0
	}
	if ctx.BlockHeight()-last < params.ValidatorPayoutIntervalBlocks {
		return nil
	}
	if err := k.LastPayout.Set(ctx, ctx.BlockHeight()); err != nil {
		return err
	}
	// new relayer interval
	if err := k.RelayerPaid.Clear(ctx, nil); err != nil {
		return err
	}

	vals, err := k.stakingKeeper.GetBondedValidatorsByPower(ctx)
	if err != nil || len(vals) == 0 {
		return err
	}
	window, err := k.slashingKeeper.SignedBlocksWindow(ctx)
	if err != nil || window <= 0 {
		window = 1
	}
	pr := k.stakingKeeper.PowerReduction(ctx)
	payees := make([]payee, 0, len(vals))
	total := math.ZeroInt()
	for _, v := range vals {
		power := v.GetConsensusPower(pr)
		if power <= 0 {
			continue
		}
		uptimeBps := int64(types.MaxBps)
		if consBz, err := v.GetConsAddr(); err == nil {
			if info, err := k.slashingKeeper.GetValidatorSigningInfo(ctx, sdk.ConsAddress(consBz)); err == nil {
				missed := info.MissedBlocksCounter
				if missed > window {
					missed = window
				}
				uptimeBps = types.MaxBps - missed*types.MaxBps/window
			}
		}
		if uptimeBps <= 0 {
			continue
		}
		valAddr, err := sdk.ValAddressFromBech32(v.GetOperator())
		if err != nil {
			continue
		}
		w := math.NewInt(power).Mul(math.NewInt(uptimeBps))
		payees = append(payees, payee{addr: sdk.AccAddress(valAddr), weight: w})
		total = total.Add(w)
	}
	if !total.IsPositive() {
		return nil
	}

	type entry struct {
		denom string
		bal   math.Int
	}
	var balances []entry
	err = k.Pools.Walk(ctx, collections.NewPrefixedPairRange[string, string](types.PoolValidator), func(key collections.Pair[string, string], bal math.Int) (bool, error) {
		balances = append(balances, entry{denom: key.K2(), bal: bal})
		return false, nil
	})
	if err != nil {
		return err
	}
	for _, e := range balances {
		paidTotal := math.ZeroInt()
		for _, p := range payees {
			share := e.bal.Mul(p.weight).Quo(total)
			if !share.IsPositive() {
				continue
			}
			if err := k.bankKeeper.SendCoinsFromModuleToAccount(ctx, types.ModuleName, p.addr, sdk.NewCoins(sdk.NewCoin(e.denom, share))); err != nil {
				return err
			}
			paidTotal = paidTotal.Add(share)
		}
		if err := k.addPool(ctx, types.PoolValidator, e.denom, paidTotal.Neg()); err != nil {
			return err
		}
		ctx.EventManager().EmitEvent(sdk.NewEvent("settle_validator_payout",
			sdk.NewAttribute("denom", e.denom),
			sdk.NewAttribute("paid", paidTotal.String()),
			sdk.NewAttribute("validators", fmt.Sprint(len(payees))),
		))
	}
	return nil
}
