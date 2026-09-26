// SPDX-License-Identifier: AGPL-3.0-only
// Copyright (c) 2026 VaporChain / muthu2201
// Provenance: VAPOR-6eabb1be532bdef4

package app

import (
	"encoding/json"
	"time"

	"cosmossdk.io/math"

	"github.com/cosmos/cosmos-sdk/client"
	"github.com/cosmos/cosmos-sdk/codec"
	sdk "github.com/cosmos/cosmos-sdk/types"
	"github.com/cosmos/cosmos-sdk/types/module"
	banktypes "github.com/cosmos/cosmos-sdk/x/bank/types"
	distrtypes "github.com/cosmos/cosmos-sdk/x/distribution/types"
	govv1 "github.com/cosmos/cosmos-sdk/x/gov/types/v1"
	slashingtypes "github.com/cosmos/cosmos-sdk/x/slashing/types"
	stakingtypes "github.com/cosmos/cosmos-sdk/x/staking/types"

	erc20types "github.com/cosmos/evm/x/erc20/types"
	feemarkettypes "github.com/cosmos/evm/x/feemarket/types"
	evmtypes "github.com/cosmos/evm/x/vm/types"

	vaporconfig "github.com/muthu2201/vapor-chain/chain/app/config"
	"github.com/muthu2201/vapor-chain/chain/constants"
)

// GenesisState is the raw JSON genesis keyed by module name.
type GenesisState map[string]json.RawMessage

// CreditMetadata is the bank metadata of the gas credit. cosmos/evm reads the
// EVM coin's decimals from here, so it is mandatory.
func CreditMetadata() banktypes.Metadata {
	return banktypes.Metadata{
		Description: "VaporChain gas credit: fixed-price, non-redeemable prepaid compute. Chain-internal (never leaves via IBC).",
		DenomUnits: []*banktypes.DenomUnit{
			{Denom: constants.CreditDenom, Exponent: 0},
			{Denom: constants.CreditDisplayDenom, Exponent: constants.CreditDecimals},
		},
		Base:    constants.CreditDenom,
		Display: constants.CreditDisplayDenom,
		Name:    "Gas Credit",
		Symbol:  constants.CreditSymbol,
	}
}

// PowerMetadata is the bank metadata of the validator power token.
func PowerMetadata() banktypes.Metadata {
	return banktypes.Metadata{
		Description: "VaporChain validator power (Proof-of-Authority). Minted only by x/council via governance; non-transferable.",
		DenomUnits: []*banktypes.DenomUnit{
			{Denom: constants.PowerDenom, Exponent: 0},
			{Denom: constants.PowerDisplayDenom, Exponent: constants.PowerDecimals},
		},
		Base:    constants.PowerDenom,
		Display: constants.PowerDisplayDenom,
		Name:    "Validator Power",
		Symbol:  "VPOWER",
	}
}

// NewDefaultGenesisState returns VaporChain's production defaults. Network
// specific values (validators, assets, admins) are applied on top by
// scripts/genesis/build-genesis.sh.
func NewDefaultGenesisState(cdc codec.JSONCodec, mbm module.BasicManager) GenesisState {
	gen := mbm.DefaultGenesis(cdc)

	// ---- bank: metadata + SendEnabled=false for chain-internal denoms
	var bank banktypes.GenesisState
	cdc.MustUnmarshalJSON(gen[banktypes.ModuleName], &bank)
	bank.DenomMetadata = []banktypes.Metadata{CreditMetadata(), PowerMetadata()}
	bank.SendEnabled = []banktypes.SendEnabled{
		{Denom: constants.CreditDenom, Enabled: false},
		{Denom: constants.PowerDenom, Enabled: false},
	}
	gen[banktypes.ModuleName] = cdc.MustMarshalJSON(&bank)

	// ---- evm
	evmGen := evmtypes.DefaultGenesisState()
	evmGen.Params.EvmDenom = constants.CreditDenom
	evmGen.Params.ActiveStaticPrecompiles = vaporconfig.StaticPrecompileAddresses()
	evmGen.Preinstalls = evmtypes.DefaultPreinstalls
	gen[evmtypes.ModuleName] = cdc.MustMarshalJSON(evmGen)

	// ---- erc20: no wrapped native token (the credit is gas-only)
	erc20Gen := erc20types.DefaultGenesisState()
	erc20Gen.TokenPairs = []erc20types.TokenPair{}
	erc20Gen.NativePrecompiles = []string{}
	gen[erc20types.ModuleName] = cdc.MustMarshalJSON(erc20Gen)

	// ---- feemarket: EIP-1559 with a NON-ZERO floor
	fm := feemarkettypes.DefaultGenesisState()
	fm.Params.NoBaseFee = false
	fm.Params.BaseFee = math.LegacyNewDec(1_000_000_000)     // 1 gwei-equivalent
	fm.Params.MinGasPrice = math.LegacyNewDec(1_000_000_000) // floor
	gen[feemarkettypes.ModuleName] = cdc.MustMarshalJSON(fm)

	// ---- staking: PoA power token, 100 validators max, 14d unbonding
	var st stakingtypes.GenesisState
	cdc.MustUnmarshalJSON(gen[stakingtypes.ModuleName], &st)
	st.Params.BondDenom = constants.PowerDenom
	st.Params.MaxValidators = 100
	st.Params.UnbondingTime = 14 * 24 * time.Hour
	st.Params.HistoricalEntries = 10_000
	st.Params.MinCommissionRate = math.LegacyZeroDec()
	gen[stakingtypes.ModuleName] = cdc.MustMarshalJSON(&st)

	// ---- slashing: downtime jails (no burn), double-sign burns 5% + tombstone
	var sl slashingtypes.GenesisState
	cdc.MustUnmarshalJSON(gen[slashingtypes.ModuleName], &sl)
	sl.Params.SignedBlocksWindow = 10_000
	sl.Params.MinSignedPerWindow = math.LegacyMustNewDecFromStr("0.5")
	sl.Params.DowntimeJailDuration = 10 * time.Minute
	sl.Params.SlashFractionDoubleSign = math.LegacyMustNewDecFromStr("0.05")
	sl.Params.SlashFractionDowntime = math.LegacyZeroDec()
	gen[slashingtypes.ModuleName] = cdc.MustMarshalJSON(&sl)

	// ---- distribution: all gas fees to validators
	var dist distrtypes.GenesisState
	cdc.MustUnmarshalJSON(gen[distrtypes.ModuleName], &dist)
	dist.Params.CommunityTax = math.LegacyZeroDec()
	gen[distrtypes.ModuleName] = cdc.MustMarshalJSON(&dist)

	// ---- gov: validator council, 72h voting = parameter timelock
	var gv govv1.GenesisState
	cdc.MustUnmarshalJSON(gen["gov"], &gv)
	voting := 72 * time.Hour
	expedited := 24 * time.Hour
	deposit := 72 * time.Hour
	gv.Params.VotingPeriod = &voting
	gv.Params.ExpeditedVotingPeriod = &expedited
	gv.Params.MaxDepositPeriod = &deposit
	gv.Params.MinDeposit = sdk.NewCoins(sdk.NewCoin(constants.CreditDenom, math.NewIntWithDecimal(10_000, 18)))
	gv.Params.ExpeditedMinDeposit = sdk.NewCoins(sdk.NewCoin(constants.CreditDenom, math.NewIntWithDecimal(50_000, 18)))
	gv.Params.Quorum = "0.5"
	gv.Params.Threshold = "0.667"
	gv.Params.ExpeditedThreshold = "0.75"
	gen["gov"] = cdc.MustMarshalJSON(&gv)

	return gen
}

// defaultGenesisOverride makes `vaporchaind init` emit VaporChain defaults
// (credit denom, PoA staking, fee floor, ...) instead of SDK stock values, so
// an operator never has to hand-patch genesis to get a correct network.
type defaultGenesisOverride struct {
	module.AppModuleBasic
	raw json.RawMessage
}

func (o defaultGenesisOverride) DefaultGenesis(codec.JSONCodec) json.RawMessage { return o.raw }

func (o defaultGenesisOverride) ValidateGenesis(cdc codec.JSONCodec, cfg client.TxEncodingConfig, bz json.RawMessage) error {
	if v, ok := o.AppModuleBasic.(module.HasGenesisBasics); ok {
		return v.ValidateGenesis(cdc, cfg, bz)
	}
	return nil
}

// GenesisBasicManager is the BasicManager used by the `init` command.
func (app *VaporApp) GenesisBasicManager() module.BasicManager {
	defaults := NewDefaultGenesisState(app.appCodec, app.BasicModuleManager)
	out := make(module.BasicManager, len(app.BasicModuleManager))
	for name, b := range app.BasicModuleManager {
		if raw, ok := defaults[name]; ok {
			out[name] = defaultGenesisOverride{AppModuleBasic: b, raw: raw}
			continue
		}
		out[name] = b
	}
	return out
}
