// Copyright (c) 2026 VaporChain / muthu2201. All rights reserved.
// Provenance: VAPOR-6eabb1be532bdef4

package types

import (
	"context"

	"github.com/ethereum/go-ethereum/common"

	"cosmossdk.io/math"

	sdk "github.com/cosmos/cosmos-sdk/types"
	slashingtypes "github.com/cosmos/cosmos-sdk/x/slashing/types"
	stakingtypes "github.com/cosmos/cosmos-sdk/x/staking/types"

	appstypes "github.com/muthu2201/vapor-chain/chain/x/apps/types"
)

type AccountKeeper interface {
	GetModuleAddress(name string) sdk.AccAddress
}

type BankKeeper interface {
	SendCoinsFromAccountToModule(ctx context.Context, senderAddr sdk.AccAddress, recipientModule string, amt sdk.Coins) error
	SendCoinsFromModuleToAccount(ctx context.Context, senderModule string, recipientAddr sdk.AccAddress, amt sdk.Coins) error
	MintCoins(ctx context.Context, moduleName string, amt sdk.Coins) error
	GetBalance(ctx context.Context, addr sdk.AccAddress, denom string) sdk.Coin
	GetSupply(ctx context.Context, denom string) sdk.Coin
	BlockedAddr(addr sdk.AccAddress) bool
}

type AppsKeeper interface {
	AppOfContract(ctx sdk.Context, contract common.Address) (appstypes.App, bool)
	GetApp(ctx sdk.Context, appID uint64) (appstypes.App, error)
	RecordSettlement(ctx sdk.Context, appID uint64, payer []byte, feeWeight math.Int) error
}

type CouncilKeeper interface {
	IsSettlePaused(ctx sdk.Context, denom string) bool
	SetPause(ctx sdk.Context, target, setBy, reason string, expires int64) error
}

type StakingKeeper interface {
	GetBondedValidatorsByPower(ctx context.Context) ([]stakingtypes.Validator, error)
	PowerReduction(ctx context.Context) math.Int
}

type SlashingKeeper interface {
	GetValidatorSigningInfo(ctx context.Context, address sdk.ConsAddress) (slashingtypes.ValidatorSigningInfo, error)
	SignedBlocksWindow(ctx context.Context) (int64, error)
}
