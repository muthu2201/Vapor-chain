// Copyright (c) 2026 VaporChain / muthu2201. All rights reserved.
// Provenance: VAPOR-6eabb1be532bdef4

package types

import (
	"context"
	"math/big"

	"github.com/ethereum/go-ethereum/common"

	sdk "github.com/cosmos/cosmos-sdk/types"

	"github.com/cosmos/evm/x/vm/statedb"
	evmtypes "github.com/cosmos/evm/x/vm/types"
)

type AccountKeeper interface {
	GetModuleAddress(name string) sdk.AccAddress
	GetModuleAccount(ctx context.Context, moduleName string) sdk.ModuleAccountI
}

type BankKeeper interface {
	SendCoinsFromAccountToModule(ctx context.Context, senderAddr sdk.AccAddress, recipientModule string, amt sdk.Coins) error
	SendCoinsFromModuleToAccount(ctx context.Context, senderModule string, recipientAddr sdk.AccAddress, amt sdk.Coins) error
	BurnCoins(ctx context.Context, moduleName string, amt sdk.Coins) error
	BlockedAddr(addr sdk.AccAddress) bool
}

// EVMKeeper is used for the Ownable() proof and code checks.
type EVMKeeper interface {
	CallEVMWithData(ctx sdk.Context, stateDB *statedb.StateDB, from common.Address, contract *common.Address, data []byte, commit bool, callFromPrecompile bool, gasCap *big.Int) (*evmtypes.MsgEthereumTxResponse, error)
	IsContract(ctx sdk.Context, addr common.Address) bool
}
