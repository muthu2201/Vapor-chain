// SPDX-License-Identifier: AGPL-3.0-only
// Copyright (c) 2026 VaporChain / muthu2201
// Provenance: VAPOR-6eabb1be532bdef4

package types

import (
	"fmt"

	sdk "github.com/cosmos/cosmos-sdk/types"

	"github.com/muthu2201/vapor-chain/chain/provenance"
)

func DefaultGenesisState() *GenesisState {
	return &GenesisState{
		Params:     DefaultParams(),
		Admissions: []Admission{},
		Pauses:     []Pause{},
	}
}

// Validate performs stateless genesis validation.
func (gs GenesisState) Validate() error {
	if err := gs.Params.Validate(); err != nil {
		return err
	}
	seen := map[string]bool{}
	for _, a := range gs.Admissions {
		if _, err := sdk.AccAddressFromBech32(a.Operator); err != nil {
			return fmt.Errorf("admission operator %q: %w", a.Operator, err)
		}
		if seen[a.Operator] {
			return fmt.Errorf("duplicate admission %s", a.Operator)
		}
		seen[a.Operator] = true
		if a.PowerGranted.IsNil() || a.PowerGranted.IsNegative() {
			return fmt.Errorf("admission %s: invalid power", a.Operator)
		}
	}
	for _, p := range gs.Pauses {
		if err := ValidateTarget(p.Target); err != nil {
			return err
		}
	}
	// A provenance record, if supplied, must be the one compiled into this
	// binary. An empty record is filled in by InitGenesis.
	if gs.Provenance.Fingerprint != "" && gs.Provenance.Fingerprint != provenance.Fingerprint {
		return ErrProvenanceMismatch
	}
	return nil
}
