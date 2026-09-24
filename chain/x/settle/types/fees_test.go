// Copyright (c) 2026 VaporChain / muthu2201. All rights reserved.
// Provenance: VAPOR-6eabb1be532bdef4

package types

import (
	"testing"

	"cosmossdk.io/math"
)

func usdc() FeeAsset {
	return FeeAsset{
		Denom: "uusdc", Enabled: true, MinFee: math.NewInt(2_000), MaxFee: math.NewInt(5_000_000),
		MicroThreshold: math.NewInt(250_000), TabSettleThreshold: math.NewInt(1_000_000),
		QuotaWeight: math.NewInt(1000), CreditPrice: math.NewInt(1),
	}
}

func TestComputeFeeBlueprintExamples(t *testing.T) {
	a := usdc()
	cases := []struct{ amount, fee int64 }{
		{100_000_000, 1_000_000},     // 100 USDC -> 1 USDC (blueprint worked example)
		{2_000_000, 20_000},          // 2 USDC sword -> 0.02
		{250_000, 2_500},             // at micro threshold: 1% (2500 > floor 2000)
		{200_000, 2_000},             // just above floor math: 0.2 USDC below threshold -> pure 1%
		{100_000, 1_000},             // 0.10 USDC micro payment: no floor
		{10_000, 100},                // 0.01 USDC tip: 1%, no 20% floor tax
		{1_000_000_000, 5_000_000},   // 1000 USDC capped at 5 USDC
		{1, 0},                       // dust rounds down to zero fee
	}
	for _, c := range cases {
		got := ComputeFee(math.NewInt(c.amount), a, 100)
		if !got.Equal(math.NewInt(c.fee)) {
			t.Fatalf("amount %d: fee %s want %d", c.amount, got, c.fee)
		}
	}
}

func TestFloorAppliesOnlyAboveMicroThreshold(t *testing.T) {
	a := usdc()
	a.MinFee = math.NewInt(10_000) // 0.01 floor
	if f := ComputeFee(math.NewInt(300_000), a, 100); !f.Equal(math.NewInt(10_000)) {
		t.Fatalf("floor not applied: %s", f)
	}
	if f := ComputeFee(math.NewInt(200_000), a, 100); !f.Equal(math.NewInt(2_000)) {
		t.Fatalf("floor applied below micro threshold: %s", f)
	}
}

func TestSplitWorkedExample(t *testing.T) {
	// 1 USDC fee, app referrer at max 10% of app share:
	// app 0.45, referrer 0.05, validators 0.20, relayers 0.10, treasury 0.20
	s := ComputeSplit(math.NewInt(1_000_000), Split{5000, 2000, 1000, 2000}, true, true, 1000)
	want := SplitResult{math.NewInt(450_000), math.NewInt(50_000), math.NewInt(200_000), math.NewInt(100_000), math.NewInt(200_000)}
	if !s.App.Equal(want.App) || !s.Referrer.Equal(want.Referrer) || !s.Validator.Equal(want.Validator) ||
		!s.Relayer.Equal(want.Relayer) || !s.Treasury.Equal(want.Treasury) {
		t.Fatalf("split %+v want %+v", s, want)
	}
	// no app: app share goes to treasury (100% protocol bucket)
	n := ComputeSplit(math.NewInt(1_000_000), Split{5000, 2000, 1000, 2000}, false, true, 1000)
	if !n.App.IsZero() || !n.Referrer.IsZero() || !n.Treasury.Equal(math.NewInt(700_000)) {
		t.Fatalf("no-app split %+v", n)
	}
}

// FuzzComputeSplit: conservation holds for every fee, split and referrer rate.
func FuzzComputeSplit(f *testing.F) {
	f.Add(uint64(1_000_000), uint16(5000), uint16(2000), uint16(1000), uint16(1000), true, true)
	f.Add(uint64(1), uint16(3333), uint16(3333), uint16(3333), uint16(10000), true, false)
	f.Add(uint64(999_999_999_999), uint16(0), uint16(0), uint16(0), uint16(0), false, true)
	f.Fuzz(func(t *testing.T, fee uint64, app, val, rel uint16, ref uint16, hasApp, hasRef bool) {
		a, v, r := uint32(app%10001), uint32(val%10001), uint32(rel%10001)
		if a+v+r > 10000 {
			return
		}
		sp := Split{AppBps: a, ValidatorBps: v, RelayerBps: r, TreasuryBps: 10000 - a - v - r}
		res := ComputeSplit(math.NewIntFromUint64(fee), sp, hasApp, hasRef, uint32(ref%10001))
		if !res.Total().Equal(math.NewIntFromUint64(fee)) {
			t.Fatalf("not conserved: %+v total %s fee %d", res, res.Total(), fee)
		}
		for _, x := range []math.Int{res.App, res.Referrer, res.Validator, res.Relayer, res.Treasury} {
			if x.IsNegative() {
				t.Fatalf("negative share %+v", res)
			}
		}
		if !hasApp && (!res.App.IsZero() || !res.Referrer.IsZero()) {
			t.Fatalf("no-app payment paid an app: %+v", res)
		}
	})
}

// FuzzComputeFee: 0 <= fee <= amount, fee <= max_fee, monotone in amount.
func FuzzComputeFee(f *testing.F) {
	f.Add(uint64(1_000_000), uint16(100))
	f.Add(uint64(0), uint16(1000))
	f.Add(uint64(1<<62), uint16(1))
	f.Fuzz(func(t *testing.T, amount uint64, bps uint16) {
		a := usdc()
		b := uint32(bps % (MaxFeeBps + 1))
		fee := ComputeFee(math.NewIntFromUint64(amount), a, b)
		if fee.IsNegative() || fee.GT(math.NewIntFromUint64(amount)) || fee.GT(a.MaxFee) {
			t.Fatalf("fee %s out of bounds for amount %d bps %d", fee, amount, b)
		}
		if amount < ^uint64(0) {
			if next := ComputeFee(math.NewIntFromUint64(amount+1), a, b); next.LT(fee) && amount+1 < a.MicroThreshold.Uint64() {
				t.Fatalf("fee not monotone below threshold at %d", amount)
			}
		}
	})
}

func TestParamsValidation(t *testing.T) {
	p := DefaultParams()
	p.Assets = []FeeAsset{usdc()}
	if err := p.Validate(); err != nil {
		t.Fatal(err)
	}
	bad := p
	bad.FeeBps = 1001
	if bad.Validate() == nil {
		t.Fatal("fee above 10% accepted")
	}
	bad = p
	bad.Split = Split{5000, 2000, 1000, 1999}
	if bad.Validate() == nil {
		t.Fatal("split not summing to 10000 accepted")
	}
	bad = p
	e := usdc()
	e.Denom = "erc20/0x0000000000000000000000000000000000000001"
	bad.Assets = []FeeAsset{e}
	if bad.Validate() == nil {
		t.Fatal("ERC-20-native settlement asset accepted")
	}
	bad = p
	bad.SponsoredLaneMaxBps = 9_500
	if bad.Validate() == nil {
		t.Fatal("sponsored lane above 90% accepted")
	}
}
