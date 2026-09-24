// Copyright (c) 2026 VaporChain / muthu2201. All rights reserved.
// Provenance: VAPOR-6eabb1be532bdef4

package types

import (
	"cosmossdk.io/math"

	sdk "github.com/cosmos/cosmos-sdk/types"

	"github.com/muthu2201/vapor-chain/chain/constants"
)

const (
	// MaxBps is 100%.
	MaxBps = 10_000
	// MaxMetadataURILen / MaxDomainLen bound state growth per app.
	MaxMetadataURILen = 512
	MaxDomainLen      = 253
)

func DefaultParams() Params {
	return Params{
		// 10 credits (~0.01 USDC at the default credit price) burned per
		// registration: negligible for a real developer, expensive for a spammer
		// registering millions of apps.
		RegistrationFee:         sdk.NewCoin(constants.CreditDenom, math.NewIntWithDecimal(10, 18)),
		MaxContractsPerApp:      100,
		ContractMoveDelayBlocks: 604_800, // ~7 days at 1s blocks
		EpochLengthBlocks:       86_400,  // ~1 day
		BaseGasPerEpoch:         2_000_000,
		MaxGasPerEpoch:          5_000_000_000,
		DiversityTarget:         5,
		Attestors:               []string{},
		AttestationThreshold:    1,
		MaxReferrerBps:          1_000, // referrer may take up to 10% of the app's share
	}
}

func (p Params) Validate() error {
	if err := p.RegistrationFee.Validate(); err != nil {
		return ErrInvalidParams.Wrapf("registration_fee: %v", err)
	}
	if p.MaxContractsPerApp == 0 || p.MaxContractsPerApp > 10_000 {
		return ErrInvalidParams.Wrap("max_contracts_per_app must be in [1,10000]")
	}
	if p.ContractMoveDelayBlocks < 0 {
		return ErrInvalidParams.Wrap("contract_move_delay_blocks < 0")
	}
	if p.EpochLengthBlocks < 10 {
		return ErrInvalidParams.Wrap("epoch_length_blocks must be >= 10")
	}
	if p.MaxGasPerEpoch < p.BaseGasPerEpoch {
		return ErrInvalidParams.Wrap("max_gas_per_epoch < base_gas_per_epoch")
	}
	if p.DiversityTarget == 0 || p.DiversityTarget > 10_000 {
		return ErrInvalidParams.Wrap("diversity_target must be in [1,10000]")
	}
	seen := map[string]bool{}
	for _, a := range p.Attestors {
		if _, err := sdk.AccAddressFromBech32(a); err != nil {
			return ErrInvalidParams.Wrapf("attestor %q", a)
		}
		if seen[a] {
			return ErrInvalidParams.Wrap("duplicate attestor")
		}
		seen[a] = true
	}
	if p.AttestationThreshold == 0 {
		return ErrInvalidParams.Wrap("attestation_threshold must be >= 1")
	}
	if p.MaxReferrerBps > MaxBps {
		return ErrInvalidParams.Wrap("max_referrer_bps > 10000")
	}
	return nil
}

func (p Params) IsAttestor(addr string) bool {
	for _, a := range p.Attestors {
		if a == addr {
			return true
		}
	}
	return false
}
