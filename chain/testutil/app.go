// SPDX-License-Identifier: AGPL-3.0-only
// Copyright (c) 2026 VaporChain / muthu2201
// Provenance: VAPOR-6eabb1be532bdef4

// Package testutil boots a real VaporApp in memory with a PoA genesis so
// keeper tests exercise exactly the production wiring (hooks, module
// accounts, blocked addresses, precompiles), not mocks.
package testutil

import (
	"encoding/json"
	"testing"
	"time"

	"github.com/ethereum/go-ethereum/common"
	"github.com/stretchr/testify/require"

	abci "github.com/cometbft/cometbft/abci/types"
	cmtproto "github.com/cometbft/cometbft/proto/tendermint/types"
	cmttypes "github.com/cometbft/cometbft/types"

	dbm "github.com/cosmos/cosmos-db"

	"cosmossdk.io/log/v2"
	"cosmossdk.io/math"

	"github.com/cosmos/cosmos-sdk/baseapp"
	"github.com/cosmos/cosmos-sdk/client/flags"
	codectypes "github.com/cosmos/cosmos-sdk/codec/types"
	cryptocodec "github.com/cosmos/cosmos-sdk/crypto/codec"
	"github.com/cosmos/cosmos-sdk/crypto/keys/secp256k1"
	"github.com/cosmos/cosmos-sdk/testutil/mock"
	simtestutil "github.com/cosmos/cosmos-sdk/testutil/sims"
	sdk "github.com/cosmos/cosmos-sdk/types"
	authtypes "github.com/cosmos/cosmos-sdk/x/auth/types"
	banktypes "github.com/cosmos/cosmos-sdk/x/bank/types"
	stakingtypes "github.com/cosmos/cosmos-sdk/x/staking/types"

	erc20types "github.com/cosmos/evm/x/erc20/types"

	"github.com/muthu2201/vapor-chain/chain/app"
	vaporconfig "github.com/muthu2201/vapor-chain/chain/app/config"
	"github.com/muthu2201/vapor-chain/chain/constants"
	counciltypes "github.com/muthu2201/vapor-chain/chain/x/council/types"
	settletypes "github.com/muthu2201/vapor-chain/chain/x/settle/types"
)

const (
	ChainID = constants.LocalnetChainID
	USDC    = "uusdc"
)

// TUSDCAddress is the deterministic ERC-20 precompile of the testnet USDC.
var TUSDCAddress = common.HexToAddress("0xb235A1c5b84eaEe836DC48DFD50325190371303F")

func init() {
	cfg := sdk.GetConfig()
	vaporconfig.SetBech32Prefixes(cfg)
	vaporconfig.SetBip44CoinType(cfg)
}

// Env is a booted app plus funded accounts.
type Env struct {
	App      *app.VaporApp
	Ctx      sdk.Context
	Accounts []sdk.AccAddress
	Admin    sdk.AccAddress
	Guardian sdk.AccAddress
	Operator sdk.AccAddress
}

// USDCAsset is the default settlement asset config used in tests (6 dp).
func USDCAsset() settletypes.FeeAsset {
	return settletypes.FeeAsset{
		Denom: USDC, Enabled: true, Symbol: "USDC", Decimals: 6,
		MinFee: math.NewInt(2_000), MaxFee: math.NewInt(5_000_000), MicroThreshold: math.NewInt(250_000),
		TabSettleThreshold: math.NewInt(1_000_000), QuotaWeight: math.NewInt(1_000),
		CreditPrice: math.NewInt(1_000_000_000_000_000),
	}
}

func newAccount() sdk.AccAddress { return sdk.AccAddress(secp256k1.GenPrivKey().PubKey().Address()) }

// Setup boots a single-validator VaporApp with nAccounts funded users.
func Setup(t testing.TB, nAccounts int) *Env {
	t.Helper()
	resetEVMGlobals()
	db := dbm.NewMemDB()
	opts := simtestutil.AppOptionsMap{
		flags.FlagHome:           t.TempDir(),
		"evm.evm-chain-id":       constants.LocalnetEVMChainID,
		vaporconfig.FlagBlockSTM: false,
	}
	a := app.NewVaporApp(log.NewNopLogger(), db, nil, true, opts, baseapp.SetChainID(ChainID))
	gen := a.DefaultGenesis()
	cdc := a.AppCodec()

	env := &Env{App: a, Admin: newAccount(), Guardian: newAccount(), Operator: newAccount()}
	for i := 0; i < nAccounts; i++ {
		env.Accounts = append(env.Accounts, newAccount())
	}

	// --- validator (PoA): operator admitted, self-delegated power
	pv := mock.NewPV()
	pub, err := pv.GetPubKey()
	require.NoError(t, err)
	valSet := cmttypes.NewValidatorSet([]*cmttypes.Validator{cmttypes.NewValidator(pub, 1)})
	pk, err := cryptocodec.FromCmtPubKeyInterface(valSet.Validators[0].PubKey)
	require.NoError(t, err)
	pkAny, err := codectypes.NewAnyWithValue(pk)
	require.NoError(t, err)
	power := sdk.DefaultPowerReduction.MulRaw(100)
	valAddr := sdk.ValAddress(env.Operator)
	validator := stakingtypes.Validator{
		OperatorAddress: valAddr.String(), ConsensusPubkey: pkAny, Jailed: false, Status: stakingtypes.Bonded,
		Tokens: power, DelegatorShares: math.LegacyNewDecFromInt(power),
		Description: stakingtypes.Description{Moniker: "val0"}, UnbondingHeight: 0, UnbondingTime: time.Unix(0, 0).UTC(),
		Commission:        stakingtypes.NewCommission(math.LegacyZeroDec(), math.LegacyZeroDec(), math.LegacyZeroDec()),
		MinSelfDelegation: math.OneInt(),
	}
	var st stakingtypes.GenesisState
	cdc.MustUnmarshalJSON(gen[stakingtypes.ModuleName], &st)
	st.Validators = []stakingtypes.Validator{validator}
	st.Delegations = []stakingtypes.Delegation{stakingtypes.NewDelegation(env.Operator.String(), valAddr.String(), math.LegacyNewDecFromInt(power))}
	gen[stakingtypes.ModuleName] = cdc.MustMarshalJSON(&st)

	// --- council: admit the operator, one guardian
	var cg counciltypes.GenesisState
	cdc.MustUnmarshalJSON(gen[counciltypes.ModuleName], &cg)
	cg.Admissions = []counciltypes.Admission{{Operator: env.Operator.String(), PowerGranted: power}}
	cg.Params.Guardians = []string{env.Guardian.String()}
	cg.Params.GuardianPauseMaxBlocks = 100
	gen[counciltypes.ModuleName] = cdc.MustMarshalJSON(&cg)

	// --- settle: USDC asset, admins, fast payouts
	var sg settletypes.GenesisState
	cdc.MustUnmarshalJSON(gen[settletypes.ModuleName], &sg)
	sg.Params.Assets = []settletypes.FeeAsset{USDCAsset()}
	sg.Params.TreasuryAdmin = env.Admin.String()
	sg.Params.RelayerAdmin = env.Admin.String()
	sg.Params.RelayerRecipients = []string{env.Accounts[0].String()}
	sg.Params.RelayerPayoutCap = sdk.NewCoins(sdk.NewCoin(USDC, math.NewInt(10_000_000)))
	sg.Params.ValidatorPayoutIntervalBlocks = 10
	gen[settletypes.ModuleName] = cdc.MustMarshalJSON(&sg)

	// --- erc20: register the USDC precompile exactly like localnet
	var eg erc20types.GenesisState
	cdc.MustUnmarshalJSON(gen[erc20types.ModuleName], &eg)
	eg.TokenPairs = []erc20types.TokenPair{{Erc20Address: TUSDCAddress.Hex(), Denom: USDC, Enabled: true, ContractOwner: erc20types.OWNER_MODULE}}
	eg.DynamicPrecompiles = []string{TUSDCAddress.Hex()}
	gen[erc20types.ModuleName] = cdc.MustMarshalJSON(&eg)

	// --- bank: balances + bonded pool + USDC metadata
	var bg banktypes.GenesisState
	cdc.MustUnmarshalJSON(gen[banktypes.ModuleName], &bg)
	credits := math.NewIntWithDecimal(1_000_000, 18)
	usdc := math.NewInt(1_000_000_000_000) // 1M USDC
	fund := func(a sdk.AccAddress) {
		bg.Balances = append(bg.Balances, banktypes.Balance{Address: a.String(), Coins: sdk.NewCoins(
			sdk.NewCoin(constants.CreditDenom, credits), sdk.NewCoin(USDC, usdc))})
	}
	for _, a := range env.Accounts {
		fund(a)
	}
	fund(env.Admin)
	fund(env.Guardian)
	fund(env.Operator)
	bg.Balances = append(bg.Balances, banktypes.Balance{
		Address: authtypes.NewModuleAddress(stakingtypes.BondedPoolName).String(),
		Coins:   sdk.NewCoins(sdk.NewCoin(constants.PowerDenom, power)),
	})
	bg.DenomMetadata = append(bg.DenomMetadata, banktypes.Metadata{
		Base: USDC, Display: "usdc", Name: "Testnet USDC", Symbol: "USDC",
		DenomUnits: []*banktypes.DenomUnit{{Denom: USDC, Exponent: 0}, {Denom: "usdc", Exponent: 6}},
	})
	gen[banktypes.ModuleName] = cdc.MustMarshalJSON(&bg)

	// auth accounts
	var ag authtypes.GenesisState
	cdc.MustUnmarshalJSON(gen[authtypes.ModuleName], &ag)
	all := append([]sdk.AccAddress{env.Admin, env.Guardian, env.Operator}, env.Accounts...)
	var accs []*codectypes.Any
	for i, a := range all {
		anyAcc, err := codectypes.NewAnyWithValue(authtypes.NewBaseAccount(a, nil, uint64(i), 0))
		require.NoError(t, err)
		accs = append(accs, anyAcc)
	}
	ag.Accounts = accs
	gen[authtypes.ModuleName] = cdc.MustMarshalJSON(&ag)

	stateBytes, err := json.Marshal(gen)
	require.NoError(t, err)
	cp := simtestutil.DefaultConsensusParams
	cp.Block.MaxGas = 40_000_000
	_, err = a.InitChain(&abci.RequestInitChain{
		Validators: []abci.ValidatorUpdate{}, ConsensusParams: cp, AppStateBytes: stateBytes, ChainId: ChainID,
	})
	require.NoError(t, err)
	_, err = a.FinalizeBlock(&abci.RequestFinalizeBlock{Height: 1, Time: time.Now().UTC()})
	require.NoError(t, err)
	_, err = a.Commit()
	require.NoError(t, err)

	env.Ctx = a.NewUncachedContext(false, cmtproto.Header{Height: 2, ChainID: ChainID, Time: time.Now().UTC()})
	return env
}

// NextBlock runs the VaporChain EndBlockers and advances the context height.
func (e *Env) NextBlock(t testing.TB) {
	t.Helper()
	require.NoError(t, e.App.CouncilKeeper.PruneExpiredPauses(e.Ctx))
	require.NoError(t, e.App.AppsKeeper.EndBlock(e.Ctx))
	require.NoError(t, e.App.SettleKeeper.EndBlock(e.Ctx))
	e.Ctx = e.Ctx.WithBlockHeight(e.Ctx.BlockHeight() + 1).WithBlockTime(e.Ctx.BlockTime().Add(time.Second))
}

// GovAuthority is the governance module address (root authority).
func GovAuthority() string { return authtypes.NewModuleAddress("gov").String() }
