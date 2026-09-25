// Copyright (c) 2026 VaporChain / muthu2201. All rights reserved.
// Provenance: VAPOR-6eabb1be532bdef4

package types

import (
	"strings"

	"github.com/ethereum/go-ethereum/common"

	sdk "github.com/cosmos/cosmos-sdk/types"
)

const (
	// MaxFeeBps hard-caps the protocol fee at 10% no matter what governance votes.
	MaxFeeBps = 1_000
	// MaxSponsoredLaneBps: the sponsored lane can never take more than 90% of
	// a block, so paying users always have room.
	MaxSponsoredLaneBps = 9_000
)

func DefaultParams() Params {
	return Params{
		FeeBps:                        100,
		Split:                         Split{AppBps: 5_000, ValidatorBps: 2_000, RelayerBps: 1_000, TreasuryBps: 2_000},
		Assets:                        []FeeAsset{},
		SponsoredSenders:              []string{},
		SponsoredLaneMaxBps:           5_000,
		ValidatorPayoutIntervalBlocks: 86_400,
		CreditsEnabled:                true,
		MaxTabAgeBlocks:               604_800,
		GasBurnBps:                    MaxBps, // burn 100% of gas fees by default
		RelayerRecipients:             []string{},
		RelayerPayoutCap:              sdk.Coins{},
	}
}

func (s Split) Validate() error {
	if uint64(s.AppBps)+uint64(s.ValidatorBps)+uint64(s.RelayerBps)+uint64(s.TreasuryBps) != MaxBps {
		return ErrInvalidParams.Wrap("split must sum to 10000 bps")
	}
	return nil
}

func (a FeeAsset) Validate() error {
	if err := sdk.ValidateDenom(a.Denom); err != nil {
		return ErrInvalidParams.Wrapf("asset denom %q: %v", a.Denom, err)
	}
	// ERC-20-native representations ("erc20/0x...") hold balances inside
	// contract storage; Settle only moves bank-native coins so that it never
	// executes third-party token code.
	if strings.HasPrefix(a.Denom, "erc20/") {
		return ErrInvalidParams.Wrapf("asset %s: ERC-20-native tokens cannot be settlement assets", a.Denom)
	}
	for name, v := range map[string]interface{ IsNil() bool }{
		"min_fee": a.MinFee, "max_fee": a.MaxFee, "micro_threshold": a.MicroThreshold,
		"tab_settle_threshold": a.TabSettleThreshold, "quota_weight": a.QuotaWeight, "credit_price": a.CreditPrice,
	} {
		if v.IsNil() {
			return ErrInvalidParams.Wrapf("asset %s: %s is nil", a.Denom, name)
		}
	}
	if a.MinFee.IsNegative() || a.MaxFee.IsNegative() || a.MicroThreshold.IsNegative() ||
		a.TabSettleThreshold.IsNegative() || a.QuotaWeight.IsNegative() || a.CreditPrice.IsNegative() {
		return ErrInvalidParams.Wrapf("asset %s: negative value", a.Denom)
	}
	if a.MaxFee.IsPositive() && a.MinFee.GT(a.MaxFee) {
		return ErrInvalidParams.Wrapf("asset %s: min_fee > max_fee", a.Denom)
	}
	if !a.TabSettleThreshold.IsPositive() {
		return ErrInvalidParams.Wrapf("asset %s: tab_settle_threshold must be positive", a.Denom)
	}
	if a.Decimals > 36 {
		return ErrInvalidParams.Wrapf("asset %s: decimals > 36", a.Denom)
	}
	return nil
}

func validateOptionalBech32(field, v string) error {
	if v == "" {
		return nil
	}
	if _, err := sdk.AccAddressFromBech32(v); err != nil {
		return ErrInvalidParams.Wrapf("%s: %v", field, err)
	}
	return nil
}

func (p Params) Validate() error {
	if p.FeeBps > MaxFeeBps {
		return ErrInvalidParams.Wrapf("fee_bps > %d", MaxFeeBps)
	}
	if err := p.Split.Validate(); err != nil {
		return err
	}
	seen := map[string]bool{}
	for _, a := range p.Assets {
		if err := a.Validate(); err != nil {
			return err
		}
		if seen[a.Denom] {
			return ErrInvalidParams.Wrapf("duplicate asset %s", a.Denom)
		}
		seen[a.Denom] = true
	}
	if err := validateOptionalBech32("treasury_admin", p.TreasuryAdmin); err != nil {
		return err
	}
	if err := validateOptionalBech32("relayer_admin", p.RelayerAdmin); err != nil {
		return err
	}
	for _, r := range p.RelayerRecipients {
		if _, err := sdk.AccAddressFromBech32(r); err != nil {
			return ErrInvalidParams.Wrapf("relayer recipient %q", r)
		}
	}
	if err := p.RelayerPayoutCap.Validate(); err != nil {
		return ErrInvalidParams.Wrapf("relayer_payout_cap: %v", err)
	}
	ss := map[string]bool{}
	for _, s := range p.SponsoredSenders {
		if !common.IsHexAddress(s) {
			return ErrInvalidParams.Wrapf("sponsored sender %q is not a 0x address", s)
		}
		k := common.HexToAddress(s).Hex()
		if ss[k] {
			return ErrInvalidParams.Wrap("duplicate sponsored sender")
		}
		ss[k] = true
	}
	if p.SponsoredLaneMaxBps > MaxSponsoredLaneBps {
		return ErrInvalidParams.Wrapf("sponsored_lane_max_bps > %d", MaxSponsoredLaneBps)
	}
	if p.ValidatorPayoutIntervalBlocks < 10 {
		return ErrInvalidParams.Wrap("validator_payout_interval_blocks must be >= 10")
	}
	if p.MaxTabAgeBlocks < 1 {
		return ErrInvalidParams.Wrap("max_tab_age_blocks must be >= 1")
	}
	if p.GasBurnBps > MaxBps {
		return ErrInvalidParams.Wrapf("gas_burn_bps > %d", MaxBps)
	}
	return nil
}

// Asset returns the settlement asset config for denom.
func (p Params) Asset(denom string) (FeeAsset, bool) {
	for _, a := range p.Assets {
		if a.Denom == denom {
			return a, true
		}
	}
	return FeeAsset{}, false
}

// SponsoredSenderSet returns the lane's sender set keyed by checksummed hex.
func (p Params) SponsoredSenderSet() map[common.Address]struct{} {
	out := make(map[common.Address]struct{}, len(p.SponsoredSenders))
	for _, s := range p.SponsoredSenders {
		out[common.HexToAddress(s)] = struct{}{}
	}
	return out
}

func (p Params) IsRelayerRecipient(addr string) bool {
	for _, r := range p.RelayerRecipients {
		if r == addr {
			return true
		}
	}
	return false
}
