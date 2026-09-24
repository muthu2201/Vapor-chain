// Copyright (c) 2026 VaporChain / muthu2201. All rights reserved.
// Provenance: VAPOR-6eabb1be532bdef4

package types

import (
	"context"

	"cosmossdk.io/core/address"

	sdk "github.com/cosmos/cosmos-sdk/types"
	stakingtypes "github.com/cosmos/cosmos-sdk/x/staking/types"
)

type AccountKeeper interface {
	AddressCodec() address.Codec
	GetModuleAddress(name string) sdk.AccAddress
}

type BankKeeper interface {
	MintCoins(ctx context.Context, moduleName string, amt sdk.Coins) error
	SendCoinsFromModuleToAccount(ctx context.Context, senderModule string, recipientAddr sdk.AccAddress, amt sdk.Coins) error
}

type StakingKeeper interface {
	GetValidator(ctx context.Context, addr sdk.ValAddress) (stakingtypes.Validator, error)
	BondDenom(ctx context.Context) (string, error)
}

type SlashingKeeper interface {
	Jail(ctx context.Context, consAddr sdk.ConsAddress) error
	Tombstone(ctx context.Context, consAddr sdk.ConsAddress) error
	IsTombstoned(ctx context.Context, consAddr sdk.ConsAddress) bool
	HasValidatorSigningInfo(ctx context.Context, consAddr sdk.ConsAddress) bool
}
