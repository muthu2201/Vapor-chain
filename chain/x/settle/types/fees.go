// Copyright (c) 2026 VaporChain / muthu2201. All rights reserved.
// Proprietary and confidential. See LICENSE at the repository root.
// Provenance: VAPOR-6eabb1be532bdef4

package types

import "cosmossdk.io/math"

// ComputeFee applies the settlement fee formula (all integer math):
//
//	fee = amount * fee_bps / 10_000                (rounded down)
//	if amount >= micro_threshold: fee = max(fee, min_fee)
//	fee = min(fee, max_fee, amount)
//
// Below micro_threshold the floor is NOT applied: a 1% cut of a 0.01 USDC tip
// is tiny, and a 0.002 floor on it would be a 20% tax. Tabs aggregate micro
// payments so the percentage applies to the total.
func ComputeFee(amount math.Int, asset FeeAsset, feeBps uint32) math.Int {
	if !amount.IsPositive() {
		return math.ZeroInt()
	}
	fee := amount.Mul(math.NewInt(int64(feeBps))).Quo(math.NewInt(MaxBps))
	if !asset.MicroThreshold.IsNil() && amount.GTE(asset.MicroThreshold) && !asset.MinFee.IsNil() && fee.LT(asset.MinFee) {
		fee = asset.MinFee
	}
	if !asset.MaxFee.IsNil() && asset.MaxFee.IsPositive() && fee.GT(asset.MaxFee) {
		fee = asset.MaxFee
	}
	if fee.GT(amount) {
		fee = amount
	}
	return fee
}

// SplitResult is the division of one fee. Invariant (tested by fuzzing):
// App + Referrer + Validator + Relayer + Treasury == fee, all >= 0.
type SplitResult struct {
	App       math.Int
	Referrer  math.Int
	Validator math.Int
	Relayer   math.Int
	Treasury  math.Int
}

// Total returns the sum of every share.
func (s SplitResult) Total() math.Int {
	return s.App.Add(s.Referrer).Add(s.Validator).Add(s.Relayer).Add(s.Treasury)
}

// ComputeSplit divides a fee. Rounding dust always goes to the treasury so
// the parts sum to the fee exactly. Without an app, the app share goes to the
// treasury ("no-app bucket = 100% protocol"). The referrer share is carved
// out of the app share (never out of validators/relayers/treasury).
func ComputeSplit(fee math.Int, split Split, hasApp bool, hasReferrer bool, referrerBps uint32) SplitResult {
	bps := func(b uint32) math.Int { return fee.Mul(math.NewInt(int64(b))).Quo(math.NewInt(MaxBps)) }
	r := SplitResult{
		App:       bps(split.AppBps),
		Referrer:  math.ZeroInt(),
		Validator: bps(split.ValidatorBps),
		Relayer:   bps(split.RelayerBps),
	}
	r.Treasury = fee.Sub(r.App).Sub(r.Validator).Sub(r.Relayer)
	if !hasApp {
		r.Treasury = r.Treasury.Add(r.App)
		r.App = math.ZeroInt()
		return r
	}
	if hasReferrer && referrerBps > 0 {
		r.Referrer = r.App.Mul(math.NewInt(int64(referrerBps))).Quo(math.NewInt(MaxBps))
		r.App = r.App.Sub(r.Referrer)
	}
	return r
}
