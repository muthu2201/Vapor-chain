// SPDX-License-Identifier: AGPL-3.0-only
// Copyright (c) 2026 VaporChain / muthu2201
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
		// sponsored gas by itself (base quota needs bonded capital), so the
		// fee does not have to out-price a free-gas faucet.
		RegistrationFee:         sdk.NewCoin(constants.CreditDenom, math.NewIntWithDecimal(2, 17)),
		MaxContractsPerApp:      100,
		ContractMoveDelayBlocks: 604_800, // ~7 days at 1s blocks
		EpochLengthBlocks:       86_400,  // ~1 day
		MaxGasPerEpoch:          5_000_000_000,
		DiversityTarget:         5,
		MaxReferrerBps:          1_000, // referrer may take up to 10% of the app's share
		// Base quota is bought with LOCKED CAPITAL, not granted per identity:
		// 2,500 gas per epoch per bonded USDC ~ 4.6%/yr of the bond, paid in gas
		// that can only sponsor the app's own users (1-day epochs, genesis
		// price 1 CREDIT = 50 USDC, 1 gwei). Linear in capital, so splitting
		// one bond across many fake apps yields exactly nothing extra.
		BondDenom:        "uusdc",
		GasPerBondedUnit: 2_500,
		UnbondingBlocks:  1_814_400, // ~21 days at 1s blocks
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
	if p.DiversityTarget == 0 || p.DiversityTarget > 10_000 {
		return ErrInvalidParams.Wrap("diversity_target must be in [1,10000]")
	}
	if p.MaxReferrerBps > MaxBps {
		return ErrInvalidParams.Wrap("max_referrer_bps > 10000")
	}
	if p.BondDenom != "" {
		if err := sdk.ValidateDenom(p.BondDenom); err != nil {
			return ErrInvalidParams.Wrapf("bond_denom: %v", err)
		}
		// a bond must stay locked at least through the epoch it counted toward
		if p.UnbondingBlocks < p.EpochLengthBlocks {
			return ErrInvalidParams.Wrap("unbonding_blocks must be >= epoch_length_blocks")
		}
	}
	return nil
}
