// Copyright (c) 2026 VaporChain / muthu2201. All rights reserved.
// Provenance: VAPOR-6eabb1be532bdef4

package settle

import (
	"github.com/ethereum/go-ethereum/common"

	"cosmossdk.io/math"

	sdk "github.com/cosmos/cosmos-sdk/types"

	erc20types "github.com/cosmos/evm/x/erc20/types"

	appstypes "github.com/muthu2201/vapor-chain/chain/x/apps/types"
	settlekeeper "github.com/muthu2201/vapor-chain/chain/x/settle/keeper"
	settletypes "github.com/muthu2201/vapor-chain/chain/x/settle/types"
)

// SettleKeeper is the subset of x/settle used by the precompile.
type SettleKeeper interface {
	GetParams(ctx sdk.Context) settletypes.Params
	Pay(ctx sdk.Context, req settlekeeper.PayRequest) (settlekeeper.PayResult, error)
	TabDeposit(ctx sdk.Context, app *appstypes.App, payer, payee sdk.AccAddress, coin sdk.Coin, pullFromPayer bool) (bool, settlekeeper.PayResult, error)
	CloseTab(ctx sdk.Context, sender sdk.AccAddress, appID uint64, payer, payee sdk.AccAddress, denom string) (settlekeeper.PayResult, error)
	ApproveApp(ctx sdk.Context, owner sdk.AccAddress, appID uint64, denom string, amount math.Int) error
	SpendAllowance(ctx sdk.Context, owner sdk.AccAddress, appID uint64, denom string, amount math.Int) error
	Allowance(ctx sdk.Context, owner string, appID uint64, denom string) math.Int
	ClaimRevenue(ctx sdk.Context, appID uint64, denoms []string) (sdk.Coins, error)
	ClaimableOf(ctx sdk.Context, appID uint64, denom string) math.Int
	BuyCredits(ctx sdk.Context, buyer, recipient sdk.AccAddress, payment sdk.Coin) (sdk.Coin, error)
	QuoteCredits(ctx sdk.Context, denom string, amount math.Int) (math.Int, error)
}

// AppsKeeper is the subset of x/apps used by the precompile.
type AppsKeeper interface {
	AppOfContract(ctx sdk.Context, contract common.Address) (appstypes.App, bool)
	GetApp(ctx sdk.Context, appID uint64) (appstypes.App, error)
	ClaimRegistration(ctx sdk.Context, contract common.Address, appID uint64) error
	AcceptContractClaim(ctx sdk.Context, owner string, appID uint64, contractHex string) (bool, int64, error)
	RegisterApp(ctx sdk.Context, owner sdk.AccAddress, recipient, metadataURI, domain string, referrerBps uint32) (uint64, error)
	UpdateApp(ctx sdk.Context, owner string, appID uint64, recipient, metadataURI, domain string, referrerBps uint32) error
}

// Erc20Keeper resolves ERC-20 addresses to bank denoms.
type Erc20Keeper interface {
	GetTokenPairID(ctx sdk.Context, token string) []byte
	GetTokenPair(ctx sdk.Context, id []byte) (erc20types.TokenPair, bool)
}
