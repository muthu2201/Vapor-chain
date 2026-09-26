// SPDX-License-Identifier: AGPL-3.0-only
// Copyright (c) 2026 VaporChain / muthu2201
// Provenance: VAPOR-6eabb1be532bdef4

package types

import (
	"fmt"

	"github.com/ethereum/go-ethereum/common"

	sdk "github.com/cosmos/cosmos-sdk/types"
)

func DefaultGenesisState() *GenesisState {
	return &GenesisState{Params: DefaultParams(), NextAppId: 1}
}

func (gs GenesisState) Validate() error {
	if err := gs.Params.Validate(); err != nil {
		return err
	}
	if gs.NextAppId == 0 {
		return fmt.Errorf("next_app_id must be >= 1")
	}
	ids := map[uint64]bool{}
	for _, a := range gs.Apps {
		if a.AppId == 0 || a.AppId >= gs.NextAppId {
			return fmt.Errorf("app id %d out of range", a.AppId)
		}
		if ids[a.AppId] {
			return fmt.Errorf("duplicate app id %d", a.AppId)
		}
		ids[a.AppId] = true
	}
	seen := map[string]bool{}
	for _, b := range gs.Bindings {
		if !common.IsHexAddress(b.Contract) {
			return fmt.Errorf("binding contract %q is not hex", b.Contract)
		}
		c := NormalizeContract(b.Contract)
		if seen[c] {
			return fmt.Errorf("duplicate binding %s", c)
		}
		seen[c] = true
		if !ids[b.AppId] {
			return fmt.Errorf("binding %s references unknown app %d", c, b.AppId)
		}
	}
	bonded := map[uint64]bool{}
	for _, b := range gs.Bonds {
		if !ids[b.AppId] || bonded[b.AppId] {
			return fmt.Errorf("bond for unknown or duplicate app %d", b.AppId)
		}
		bonded[b.AppId] = true
		if b.Amount.IsNil() || !b.Amount.IsPositive() {
			return fmt.Errorf("bond for app %d must be positive", b.AppId)
		}
	}
	for _, u := range gs.Unbondings {
		if _, err := sdk.AccAddressFromBech32(u.Owner); err != nil {
			return fmt.Errorf("unbonding owner %q: %w", u.Owner, err)
		}
		if u.Amount.IsNil() || !u.Amount.IsPositive() {
			return fmt.Errorf("unbonding for app %d must be positive", u.AppId)
		}
	}
	return nil
}

// NormalizeContract returns the lowercase 0x-hex form used as store key, so
// checksum-case differences can never create duplicate bindings.
func NormalizeContract(addr string) string {
	return "0x" + common.Bytes2Hex(common.HexToAddress(addr).Bytes())
}
