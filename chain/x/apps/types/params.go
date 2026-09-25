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
		// 0.2 credits (~10 USDC at the genesis price of 1 CREDIT = 50 USDC)
		// burned per registration: pure anti-spam. Registering buys no
		// sponsored gas by itself (BaseGasPerEpoch needs a verified domain),
		// so the fee does not have to out-price a free-gas faucet.
		RegistrationFee:         sdk.NewCoin(constants.CreditDenom, math.NewIntWithDecimal(2, 17)),
		MaxContractsPerApp:      100,
		ContractMoveDelayBlocks: 604_800, // ~7 days at 1s blocks
		EpochLengthBlocks:       86_400,  // ~1 day
		// Protocol-sponsored onboarding gas per epoch for DOMAIN-VERIFIED apps
		// only (keeper.BaseQuota): ~25 first-time gasless users a day, worth
		// ~1.1 USDC/day per vetted app at the genesis price. Unverified apps
		// get sponsorship only from the fees they generate.
		BaseGasPerEpoch:      20_000_000,
		MaxGasPerEpoch:       5_000_000_000,
		DiversityTarget:      5,
		Attestors:            []string{},
		AttestationThreshold: 1,
		MaxReferrerBps:       1_000, // referrer may take up to 10% of the app's share
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
