// Copyright (c) 2026 VaporChain / muthu2201. All rights reserved.
// Provenance: VAPOR-6eabb1be532bdef4

package types

import (
	"strings"

	"cosmossdk.io/math"

	sdk "github.com/cosmos/cosmos-sdk/types"
)

// DefaultGuardianPauseMaxBlocks is ~24h at 1s blocks: long enough for
// governance (72h voting) to be convened, short enough that a stolen guardian
// key cannot freeze payments indefinitely without re-pausing on-chain (which
// is publicly visible and alerts operators).
const DefaultGuardianPauseMaxBlocks = 86_400

// DefaultMaxPowerPerValidator is 1,000 vpower (18 decimals).
var DefaultMaxPowerPerValidator = math.NewIntWithDecimal(1000, 18)

func DefaultParams() Params {
	return Params{
		Guardians:              []string{},
		GuardianPauseMaxBlocks: DefaultGuardianPauseMaxBlocks,
		MaxPowerPerValidator:   DefaultMaxPowerPerValidator,
	}
}

func (p Params) Validate() error {
	seen := map[string]bool{}
	for _, g := range p.Guardians {
		if _, err := sdk.AccAddressFromBech32(g); err != nil {
			return ErrInvalidParams.Wrapf("guardian %q: %v", g, err)
		}
		if seen[g] {
			return ErrInvalidParams.Wrapf("duplicate guardian %s", g)
		}
		seen[g] = true
	}
	if p.GuardianPauseMaxBlocks == 0 {
		return ErrInvalidParams.Wrap("guardian_pause_max_blocks must be > 0")
	}
	if p.MaxPowerPerValidator.IsNil() || !p.MaxPowerPerValidator.IsPositive() {
		return ErrInvalidParams.Wrap("max_power_per_validator must be positive")
	}
	return nil
}

// IsGuardian reports whether addr (bech32) is a guardian.
func (p Params) IsGuardian(addr string) bool {
	for _, g := range p.Guardians {
		if g == addr {
			return true
		}
	}
	return false
}

// ValidateTarget checks the pause target grammar.
func ValidateTarget(t string) error {
	switch {
	case t == TargetSettle || t == TargetIBC:
		return nil
	case strings.HasPrefix(t, TargetSettlePrefix):
		return sdk.ValidateDenom(strings.TrimPrefix(t, TargetSettlePrefix))
	case strings.HasPrefix(t, TargetIBCPrefix):
		d := strings.TrimPrefix(t, TargetIBCPrefix)
		if d == "" || len(d) > 256 {
			return ErrInvalidTarget
		}
		return nil
	default:
		return ErrInvalidTarget.Wrapf("%q", t)
	}
}
