// Copyright (c) 2026 VaporChain / muthu2201. All rights reserved.
// Proprietary and confidential. See LICENSE at the repository root.
// Derived from cosmos/evm evmd v0.7.3 (Apache-2.0); upstream notices in NOTICE.
// Provenance: VAPOR-6eabb1be532bdef4

// Package app assembles the VaporChain state machine.
//
// What differs from upstream evmd, and why (details in ARCHITECTURE.md):
//   - No x/mint: zero inflation. The native denom is a fixed-price gas credit.
//   - Proof-of-Authority on Apache-2.0 x/staking + x/council hooks (the SDK
//     Enterprise POA module is evaluation-licensed only).
//   - x/apps + x/settle + the Settle precompile implement opt-in checkout fees.
//   - IBC transfer stack: callbacks -> ratelimit -> ibcguard -> PFM -> erc20 ->
//     transfer, wired with the v11 stack builder so SENDS also traverse rate
//     limiting and the circuit breaker (upstream evmd sends bypass middleware).
//   - IBC v2 transfer is not routed yet (Eureka comes later, deliberately).
//   - Staking/distribution/gov/slashing precompiles are not activated.
//   - Two-lane block space (sponsored lane capped) in Prepare/ProcessProposal.
//   - Block-STM is opt-in via app.toml (off by default).
package app

import (
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	goruntime "runtime"

	"github.com/spf13/cast"

	// Force-load the tracer engines to trigger registration
	_ "github.com/ethereum/go-ethereum/eth/tracers/js"
	_ "github.com/ethereum/go-ethereum/eth/tracers/native"
	"github.com/ethereum/go-ethereum/common"
	gethvm "github.com/ethereum/go-ethereum/core/vm"

	abci "github.com/cometbft/cometbft/abci/types"
	cmtproto "github.com/cometbft/cometbft/proto/tendermint/types"

	dbm "github.com/cosmos/cosmos-db"
	evmante "github.com/cosmos/evm/ante"
	antetypes "github.com/cosmos/evm/ante/types"
	evmencoding "github.com/cosmos/evm/encoding"
	evmaddress "github.com/cosmos/evm/encoding/address"
	evmmempool "github.com/cosmos/evm/mempool"
	precompiletypes "github.com/cosmos/evm/precompiles/types"
	cosmosevmserver "github.com/cosmos/evm/server"
	srvflags "github.com/cosmos/evm/server/flags"
	"github.com/cosmos/evm/utils"
	"github.com/cosmos/evm/x/erc20"
	erc20keeper "github.com/cosmos/evm/x/erc20/keeper"
	erc20types "github.com/cosmos/evm/x/erc20/types"
	"github.com/cosmos/evm/x/feemarket"
	feemarketkeeper "github.com/cosmos/evm/x/feemarket/keeper"
	feemarkettypes "github.com/cosmos/evm/x/feemarket/types"
	ibccallbackskeeper "github.com/cosmos/evm/x/ibc/callbacks/keeper"
	"github.com/cosmos/evm/x/vm"
	evmkeeper "github.com/cosmos/evm/x/vm/keeper"
	vmrunner "github.com/cosmos/evm/x/vm/runner"
	evmtypes "github.com/cosmos/evm/x/vm/types"
	"github.com/cosmos/gogoproto/proto"

	ibccallbacks "github.com/cosmos/ibc-go/v11/modules/apps/callbacks"
	packetforward "github.com/cosmos/ibc-go/v11/modules/apps/packet-forward-middleware"
	packetforwardkeeper "github.com/cosmos/ibc-go/v11/modules/apps/packet-forward-middleware/keeper"
	packetforwardtypes "github.com/cosmos/ibc-go/v11/modules/apps/packet-forward-middleware/types"
	ratelimiting "github.com/cosmos/ibc-go/v11/modules/apps/rate-limiting"
	ratelimitkeeper "github.com/cosmos/ibc-go/v11/modules/apps/rate-limiting/keeper"
	ratelimittypes "github.com/cosmos/ibc-go/v11/modules/apps/rate-limiting/types"
	transfer "github.com/cosmos/ibc-go/v11/modules/apps/transfer"
	transferkeeper "github.com/cosmos/ibc-go/v11/modules/apps/transfer/keeper"
	ibctransfertypes "github.com/cosmos/ibc-go/v11/modules/apps/transfer/types"
	ibc "github.com/cosmos/ibc-go/v11/modules/core"
	porttypes "github.com/cosmos/ibc-go/v11/modules/core/05-port/types"
	ibcapi "github.com/cosmos/ibc-go/v11/modules/core/api"
	ibcexported "github.com/cosmos/ibc-go/v11/modules/core/exported"
	ibckeeper "github.com/cosmos/ibc-go/v11/modules/core/keeper"
	ibctm "github.com/cosmos/ibc-go/v11/modules/light-clients/07-tendermint"

	autocliv1 "cosmossdk.io/api/cosmos/autocli/v1"
	reflectionv1 "cosmossdk.io/api/cosmos/reflection/v1"
	"cosmossdk.io/client/v2/autocli"
	"cosmossdk.io/core/appmodule"
	"cosmossdk.io/log/v2"

	"github.com/cosmos/cosmos-sdk/baseapp"
	"github.com/cosmos/cosmos-sdk/baseapp/txnrunner"
	"github.com/cosmos/cosmos-sdk/client"
	"github.com/cosmos/cosmos-sdk/client/flags"
	"github.com/cosmos/cosmos-sdk/client/grpc/cmtservice"
	"github.com/cosmos/cosmos-sdk/client/grpc/node"
	"github.com/cosmos/cosmos-sdk/codec"
	"github.com/cosmos/cosmos-sdk/codec/types"
	"github.com/cosmos/cosmos-sdk/runtime"
	runtimeservices "github.com/cosmos/cosmos-sdk/runtime/services"
	sdkserver "github.com/cosmos/cosmos-sdk/server"
	"github.com/cosmos/cosmos-sdk/server/api"
	"github.com/cosmos/cosmos-sdk/server/config"
	servertypes "github.com/cosmos/cosmos-sdk/server/types"
	storetypes "github.com/cosmos/cosmos-sdk/store/v2/types"
	sdk "github.com/cosmos/cosmos-sdk/types"
	sdkmempool "github.com/cosmos/cosmos-sdk/types/mempool"
	"github.com/cosmos/cosmos-sdk/types/module"
	"github.com/cosmos/cosmos-sdk/types/msgservice"
	signingtypes "github.com/cosmos/cosmos-sdk/types/tx/signing"
	"github.com/cosmos/cosmos-sdk/version"
	"github.com/cosmos/cosmos-sdk/x/auth"
	authkeeper "github.com/cosmos/cosmos-sdk/x/auth/keeper"
	"github.com/cosmos/cosmos-sdk/x/auth/posthandler"
	authsims "github.com/cosmos/cosmos-sdk/x/auth/simulation"
	authtx "github.com/cosmos/cosmos-sdk/x/auth/tx"
	txmodule "github.com/cosmos/cosmos-sdk/x/auth/tx/config"
	authtypes "github.com/cosmos/cosmos-sdk/x/auth/types"
	"github.com/cosmos/cosmos-sdk/x/auth/vesting"
	vestingtypes "github.com/cosmos/cosmos-sdk/x/auth/vesting/types"
	"github.com/cosmos/cosmos-sdk/x/authz"
	authzkeeper "github.com/cosmos/cosmos-sdk/x/authz/keeper"
	authzmodule "github.com/cosmos/cosmos-sdk/x/authz/module"
	"github.com/cosmos/cosmos-sdk/x/bank"
	bankkeeper "github.com/cosmos/cosmos-sdk/x/bank/keeper"
	banktypes "github.com/cosmos/cosmos-sdk/x/bank/types"
	"github.com/cosmos/cosmos-sdk/x/consensus"
	consensusparamkeeper "github.com/cosmos/cosmos-sdk/x/consensus/keeper"
	consensusparamtypes "github.com/cosmos/cosmos-sdk/x/consensus/types"
	distr "github.com/cosmos/cosmos-sdk/x/distribution"
	distrkeeper "github.com/cosmos/cosmos-sdk/x/distribution/keeper"
	distrtypes "github.com/cosmos/cosmos-sdk/x/distribution/types"
	"github.com/cosmos/cosmos-sdk/x/evidence"
	evidencekeeper "github.com/cosmos/cosmos-sdk/x/evidence/keeper"
	evidencetypes "github.com/cosmos/cosmos-sdk/x/evidence/types"
	"github.com/cosmos/cosmos-sdk/x/feegrant"
	feegrantkeeper "github.com/cosmos/cosmos-sdk/x/feegrant/keeper"
	feegrantmodule "github.com/cosmos/cosmos-sdk/x/feegrant/module"
	"github.com/cosmos/cosmos-sdk/x/genutil"
	genutiltypes "github.com/cosmos/cosmos-sdk/x/genutil/types"
	"github.com/cosmos/cosmos-sdk/x/gov"
	govkeeper "github.com/cosmos/cosmos-sdk/x/gov/keeper"
	govtypes "github.com/cosmos/cosmos-sdk/x/gov/types"
	"github.com/cosmos/cosmos-sdk/x/slashing"
	slashingkeeper "github.com/cosmos/cosmos-sdk/x/slashing/keeper"
	slashingtypes "github.com/cosmos/cosmos-sdk/x/slashing/types"
	"github.com/cosmos/cosmos-sdk/x/staking"
	stakingkeeper "github.com/cosmos/cosmos-sdk/x/staking/keeper"
	stakingtypes "github.com/cosmos/cosmos-sdk/x/staking/types"
	"github.com/cosmos/cosmos-sdk/x/upgrade"
	upgradekeeper "github.com/cosmos/cosmos-sdk/x/upgrade/keeper"
	upgradetypes "github.com/cosmos/cosmos-sdk/x/upgrade/types"

	vaporconfig "github.com/muthu2201/vapor-chain/chain/app/config"
	"github.com/muthu2201/vapor-chain/chain/constants"
	settleprecompile "github.com/muthu2201/vapor-chain/chain/precompiles/settle"
	"github.com/muthu2201/vapor-chain/chain/x/apps"
	appskeeper "github.com/muthu2201/vapor-chain/chain/x/apps/keeper"
	appstypes "github.com/muthu2201/vapor-chain/chain/x/apps/types"
	"github.com/muthu2201/vapor-chain/chain/x/council"
	"github.com/muthu2201/vapor-chain/chain/x/council/ibcguard"
	councilkeeper "github.com/muthu2201/vapor-chain/chain/x/council/keeper"
	counciltypes "github.com/muthu2201/vapor-chain/chain/x/council/types"
	"github.com/muthu2201/vapor-chain/chain/x/settle"
	settlekeeper "github.com/muthu2201/vapor-chain/chain/x/settle/keeper"
	settletypes "github.com/muthu2201/vapor-chain/chain/x/settle/types"
)

func init() {
	// 18-decimal power token: one "vpower" = 1e18 avpower = 1 consensus power.
	sdk.DefaultPowerReduction = utils.AttoPowerReduction
	DefaultNodeHome = vaporconfig.MustGetDefaultNodeHome()
}

// DefaultNodeHome is ~/.vaporchaind.
var DefaultNodeHome string

// maxIBCCallbackGas bounds EVM execution triggered by IBC callbacks.
const maxIBCCallbackGas = uint64(1_000_000)

var (
	_ runtime.AppI                = (*VaporApp)(nil)
	_ cosmosevmserver.Application = (*VaporApp)(nil)
)

// VaporApp is the VaporChain ABCI application.
type VaporApp struct {
	*baseapp.BaseApp

	legacyAmino       *codec.LegacyAmino
	appCodec          codec.Codec
	interfaceRegistry types.InterfaceRegistry
	txConfig          client.TxConfig

	pendingTxListeners []evmante.PendingTxListener

	keys  map[string]*storetypes.KVStoreKey
	oKeys map[string]*storetypes.ObjectStoreKey

	// SDK keepers
	AccountKeeper         authkeeper.AccountKeeper
	BankKeeper            bankkeeper.Keeper
	StakingKeeper         *stakingkeeper.Keeper
	SlashingKeeper        slashingkeeper.Keeper
	DistrKeeper           distrkeeper.Keeper
	GovKeeper             govkeeper.Keeper
	UpgradeKeeper         *upgradekeeper.Keeper
	AuthzKeeper           authzkeeper.Keeper
	EvidenceKeeper        evidencekeeper.Keeper
	FeeGrantKeeper        feegrantkeeper.Keeper
	ConsensusParamsKeeper consensusparamkeeper.Keeper

	// IBC keepers
	IBCKeeper       *ibckeeper.Keeper
	TransferKeeper  *transferkeeper.Keeper
	CallbackKeeper  ibccallbackskeeper.ContractKeeper
	PFMKeeper       *packetforwardkeeper.Keeper
	RateLimitKeeper *ratelimitkeeper.Keeper

	// Cosmos EVM keepers
	FeeMarketKeeper feemarketkeeper.Keeper
	EVMKeeper       *evmkeeper.Keeper
	Erc20Keeper     erc20keeper.Keeper
	EVMMempool      sdkmempool.ExtMempool

	// VaporChain keepers
	CouncilKeeper councilkeeper.Keeper
	AppsKeeper    appskeeper.Keeper
	SettleKeeper  settlekeeper.Keeper

	ModuleManager      *module.Manager
	BasicModuleManager module.BasicManager
	sm                 *module.SimulationManager
	configurator       module.Configurator
}

// NewVaporApp returns a fully wired VaporChain application.
func NewVaporApp(
	logger log.Logger,
	db dbm.DB,
	_ io.Writer, // trace store (unused: store v2 traces via telemetry)
	loadLatest bool,
	appOpts servertypes.AppOptions,
	baseAppOptions ...func(*baseapp.BaseApp),
) *VaporApp {
	evmChainID := cast.ToUint64(appOpts.Get(srvflags.EVMChainID))
	encodingConfig := evmencoding.MakeConfig(evmChainID)

	appCodec := encodingConfig.Codec
	legacyAmino := encodingConfig.Amino
	interfaceRegistry := encodingConfig.InterfaceRegistry
	txConfig := encodingConfig.TxConfig
	txDecoder := encodingConfig.TxConfig.TxDecoder()

	baseAppOptions = append(baseAppOptions, baseapp.SetOptimisticExecution())

	bApp := baseapp.NewBaseApp(constants.AppName, logger, db, txDecoder, baseAppOptions...)
	bApp.SetVersion(version.Version)
	bApp.SetInterfaceRegistry(interfaceRegistry)
	bApp.SetTxEncoder(txConfig.TxEncoder())

	keys := storetypes.NewKVStoreKeys(
		authtypes.StoreKey, banktypes.StoreKey, stakingtypes.StoreKey,
		distrtypes.StoreKey, slashingtypes.StoreKey,
		govtypes.StoreKey, consensusparamtypes.StoreKey,
		upgradetypes.StoreKey, feegrant.StoreKey, evidencetypes.StoreKey, authzkeeper.StoreKey,
		// ibc
		ibcexported.StoreKey, ibctransfertypes.StoreKey, packetforwardtypes.StoreKey, ratelimittypes.StoreKey,
		// cosmos evm
		evmtypes.StoreKey, feemarkettypes.StoreKey, erc20types.StoreKey,
		// vaporchain
		counciltypes.StoreKey, appstypes.StoreKey, settletypes.StoreKey,
	)
	oKeys := storetypes.NewObjectStoreKeys(banktypes.ObjectStoreKey, evmtypes.ObjectKey)

	var nonTransientKeys []storetypes.StoreKey
	for _, k := range keys {
		nonTransientKeys = append(nonTransientKeys, k)
	}
	for _, k := range oKeys {
		nonTransientKeys = append(nonTransientKeys, k)
	}

	// The EVM meters gas itself; the SDK block gas meter would double count.
	bApp.SetDisableBlockGasMeter(true)

	if err := bApp.RegisterStreamingServices(appOpts, keys); err != nil {
		fmt.Printf("failed to load state streaming: %s", err)
		os.Exit(1)
	}

	app := &VaporApp{
		BaseApp:           bApp,
		legacyAmino:       legacyAmino,
		appCodec:          appCodec,
		txConfig:          txConfig,
		interfaceRegistry: interfaceRegistry,
		keys:              keys,
		oKeys:             oKeys,
	}

	// Governance is the root authority. Its voting period (72h) is the
	// timelock on every parameter change and upgrade.
	authAddr := authtypes.NewModuleAddress(govtypes.ModuleName).String()

	app.ConsensusParamsKeeper = consensusparamkeeper.NewKeeper(
		appCodec, runtime.NewKVStoreService(keys[consensusparamtypes.StoreKey]), authAddr, runtime.EventService{},
	)
	bApp.SetParamStore(app.ConsensusParamsKeeper.ParamsStore)

	app.AccountKeeper = authkeeper.NewAccountKeeper(
		appCodec, runtime.NewKVStoreService(keys[authtypes.StoreKey]),
		authtypes.ProtoBaseAccount, vaporconfig.GetMaccPerms(),
		evmaddress.NewEvmCodec(sdk.GetConfig().GetBech32AccountAddrPrefix()),
		sdk.GetConfig().GetBech32AccountAddrPrefix(),
		authAddr,
	)

	app.BankKeeper = bankkeeper.NewBaseKeeper(
		appCodec, runtime.NewKVStoreService(keys[banktypes.StoreKey]), app.AccountKeeper,
		vaporconfig.BlockedAddresses(), authAddr, logger,
	)
	app.BankKeeper = app.BankKeeper.WithObjStoreKey(oKeys[banktypes.ObjectStoreKey])

	enabledSignModes := append(authtx.DefaultSignModes, signingtypes.SignMode_SIGN_MODE_TEXTUAL) //nolint:gocritic
	txConfigOpts := authtx.ConfigOptions{
		EnabledSignModes:           enabledSignModes,
		TextualCoinMetadataQueryFn: txmodule.NewBankKeeperCoinMetadataQueryFn(app.BankKeeper),
	}
	txConfig, err := authtx.NewTxConfigWithOptions(appCodec, txConfigOpts)
	if err != nil {
		panic(err)
	}
	app.txConfig = txConfig

	app.StakingKeeper = stakingkeeper.NewKeeper(
		appCodec, runtime.NewKVStoreService(keys[stakingtypes.StoreKey]), app.AccountKeeper, app.BankKeeper, authAddr,
		evmaddress.NewEvmCodec(sdk.GetConfig().GetBech32ValidatorAddrPrefix()),
		evmaddress.NewEvmCodec(sdk.GetConfig().GetBech32ConsensusAddrPrefix()),
	)

	app.DistrKeeper = distrkeeper.NewKeeper(
		appCodec, runtime.NewKVStoreService(keys[distrtypes.StoreKey]), app.AccountKeeper, app.BankKeeper,
		app.StakingKeeper, authtypes.FeeCollectorName, authAddr,
	)

	app.SlashingKeeper = slashingkeeper.NewKeeper(
		appCodec, app.LegacyAmino(), runtime.NewKVStoreService(keys[slashingtypes.StoreKey]), app.StakingKeeper, authAddr,
	)

	app.FeeGrantKeeper = feegrantkeeper.NewKeeper(appCodec, runtime.NewKVStoreService(keys[feegrant.StoreKey]), app.AccountKeeper)

	// x/council turns x/staking into Proof-of-Authority.
	app.CouncilKeeper = councilkeeper.NewKeeper(
		appCodec, runtime.NewKVStoreService(keys[counciltypes.StoreKey]), authAddr,
		app.AccountKeeper, app.BankKeeper, app.StakingKeeper, app.SlashingKeeper,
	)

	app.StakingKeeper.SetHooks(stakingtypes.NewMultiStakingHooks(
		app.DistrKeeper.Hooks(), app.SlashingKeeper.Hooks(), app.CouncilKeeper.Hooks(),
	))

	app.AuthzKeeper = authzkeeper.NewKeeper(
		runtime.NewKVStoreService(keys[authzkeeper.StoreKey]), appCodec, app.MsgServiceRouter(), app.AccountKeeper,
	)

	skipUpgradeHeights := map[int64]bool{}
	for _, h := range cast.ToIntSlice(appOpts.Get(sdkserver.FlagUnsafeSkipUpgrades)) {
		skipUpgradeHeights[int64(h)] = true
	}
	homePath := cast.ToString(appOpts.Get(flags.FlagHome))
	app.UpgradeKeeper = upgradekeeper.NewKeeper(
		skipUpgradeHeights, runtime.NewKVStoreService(keys[upgradetypes.StoreKey]), appCodec, homePath, app.BaseApp, authAddr,
	)

	app.IBCKeeper = ibckeeper.NewKeeper(
		appCodec, runtime.NewKVStoreService(keys[ibcexported.StoreKey]), app.UpgradeKeeper, authAddr,
	)

	govConfig := govtypes.DefaultConfig()
	govConfig.MaxMetadataLen = 10_000
	govKeeper := govkeeper.NewKeeper(
		appCodec, runtime.NewKVStoreService(keys[govtypes.StoreKey]), app.AccountKeeper, app.BankKeeper,
		app.DistrKeeper, app.MsgServiceRouter(), govConfig, authAddr,
		govkeeper.NewDefaultCalculateVoteResultsAndVotingPower(app.StakingKeeper),
	)
	app.GovKeeper = *govKeeper.SetHooks(govtypes.NewMultiGovHooks())

	evidenceKeeper := evidencekeeper.NewKeeper(
		appCodec, runtime.NewKVStoreService(keys[evidencetypes.StoreKey]), app.StakingKeeper, app.SlashingKeeper,
		app.AccountKeeper.AddressCodec(), runtime.ProvideCometInfoService(),
	)
	app.EvidenceKeeper = *evidenceKeeper

	app.FeeMarketKeeper = feemarketkeeper.NewKeeper(
		appCodec, authtypes.NewModuleAddress(govtypes.ModuleName), keys[feemarkettypes.StoreKey],
	)

	// Transfer keeper must exist before the EVM keeper so precompiles get a
	// non-nil reference.
	app.TransferKeeper = transferkeeper.NewKeeper(
		appCodec, evmaddress.NewEvmCodec(sdk.GetConfig().GetBech32AccountAddrPrefix()),
		runtime.NewKVStoreService(keys[ibctransfertypes.StoreKey]), app.IBCKeeper.ChannelKeeper,
		app.MsgServiceRouter(), app.AccountKeeper, app.BankKeeper, authAddr,
	)

	tracer := cast.ToString(appOpts.Get(srvflags.EVMTracer))

	// Static precompiles. The Settle precompile holds POINTERS to keepers that
	// are constructed right below; it only dereferences them at runtime.
	staticPrecompiles := precompiletypes.NewStaticPrecompiles().
		WithPraguePrecompiles().
		WithP256Precompile().
		WithBech32Precompile().
		WithICS02Precompile(appCodec, app.IBCKeeper.ClientKeeper).
		WithICS20Precompile(app.BankKeeper, *app.StakingKeeper, app.TransferKeeper, app.IBCKeeper.ChannelKeeper, &app.Erc20Keeper).
		WithBankPrecompile(app.BankKeeper, &app.Erc20Keeper)
	settlePC := settleprecompile.NewPrecompile(&app.SettleKeeper, &app.AppsKeeper, &app.Erc20Keeper, app.BankKeeper)
	staticPrecompiles[settlePC.Address()] = settlePC

	app.EVMKeeper = evmkeeper.NewKeeper(
		appCodec, keys[evmtypes.StoreKey], oKeys[evmtypes.ObjectKey], nonTransientKeys,
		authtypes.NewModuleAddress(govtypes.ModuleName),
		app.AccountKeeper, app.BankKeeper, app.StakingKeeper, app.FeeMarketKeeper,
		&app.ConsensusParamsKeeper, &app.Erc20Keeper, evmChainID, tracer,
	).WithStaticPrecompiles(map[common.Address]gethvm.PrecompiledContract(staticPrecompiles))

	app.EVMKeeper.EnableVirtualFeeCollection()

	app.Erc20Keeper = erc20keeper.NewKeeper(
		keys[erc20types.StoreKey], appCodec, authtypes.NewModuleAddress(govtypes.ModuleName),
		app.AccountKeeper, app.BankKeeper, app.EVMKeeper, app.StakingKeeper, app.TransferKeeper,
	)

	app.AppsKeeper = appskeeper.NewKeeper(
		appCodec, runtime.NewKVStoreService(keys[appstypes.StoreKey]), authAddr,
		app.AccountKeeper, app.BankKeeper, app.EVMKeeper,
	)

	app.SettleKeeper = settlekeeper.NewKeeper(
		appCodec, runtime.NewKVStoreService(keys[settletypes.StoreKey]), authAddr, constants.CreditDenom,
		app.AccountKeeper, app.BankKeeper, app.AppsKeeper, app.CouncilKeeper, app.StakingKeeper, app.SlashingKeeper,
	)

	// ---------------------------------------------------------------- IBC
	app.PFMKeeper = packetforwardkeeper.NewKeeper(
		appCodec, app.AccountKeeper.AddressCodec(), runtime.NewKVStoreService(keys[packetforwardtypes.StoreKey]),
		app.TransferKeeper, app.IBCKeeper.ChannelKeeper, app.BankKeeper, authAddr,
	)
	app.RateLimitKeeper = ratelimitkeeper.NewKeeper(
		appCodec, app.AccountKeeper.AddressCodec(), runtime.NewKVStoreService(keys[ratelimittypes.StoreKey]),
		app.IBCKeeper.ChannelKeeper, app.IBCKeeper.ClientKeeper, app.BankKeeper, authAddr,
	)
	app.CallbackKeeper = ibccallbackskeeper.NewKeeper(app.AccountKeeper, app.EVMKeeper, app.Erc20Keeper)

	// Transfer stack, bottom -> top:
	//   transfer <- erc20 <- PFM <- ibcguard <- ratelimit <- callbacks <- core IBC
	// RecvPacket flows top-down; SendPacket (from transfer or a PFM forward)
	// flows bottom-up through ibcguard and ratelimit before reaching core IBC.
	transferBase := erc20.NewIBCMiddleware(app.Erc20Keeper, transfer.NewIBCModule(app.TransferKeeper))
	transferStack := porttypes.NewIBCStackBuilder(app.IBCKeeper.ChannelKeeper)
	transferStack.Base(transferBase).
		Next(packetforward.NewIBCMiddleware(app.PFMKeeper, 0, packetforwardkeeper.DefaultForwardTransferPacketTimeoutTimestamp)).
		Next(ibcguard.NewIBCMiddleware(app.CouncilKeeper)).
		Next(ratelimiting.NewIBCMiddleware(app.RateLimitKeeper)).
		Next(ibccallbacks.NewIBCMiddleware(app.CallbackKeeper, maxIBCCallbackGas))

	ibcRouter := porttypes.NewRouter()
	ibcRouter.AddRoute(ibctransfertypes.ModuleName, transferStack.Build())
	app.IBCKeeper.SetRouter(ibcRouter)
	// IBC v2 (Eureka) routes are intentionally empty until the Eureka path
	// is audited and Skip supports VaporChain on it.
	app.IBCKeeper.SetRouterV2(ibcapi.NewRouter())

	clientKeeper := app.IBCKeeper.ClientKeeper
	tmLightClientModule := ibctm.NewLightClientModule(appCodec, clientKeeper.GetStoreProvider())
	clientKeeper.AddRoute(ibctm.ModuleName, &tmLightClientModule)

	transferModule := transfer.NewAppModule(app.TransferKeeper)
	vmModule := vm.NewAppModule(app.EVMKeeper, app.AccountKeeper, app.BankKeeper, app.AccountKeeper.AddressCodec())

	// ------------------------------------------------------------ modules
	app.ModuleManager = module.NewManager(
		genutil.NewAppModule(app.AccountKeeper, app.StakingKeeper, app, app.txConfig),
		auth.NewAppModule(appCodec, app.AccountKeeper, authsims.RandomGenesisAccounts, nil),
		bank.NewAppModule(appCodec, app.BankKeeper, app.AccountKeeper, nil),
		feegrantmodule.NewAppModule(appCodec, app.AccountKeeper, app.BankKeeper, app.FeeGrantKeeper, app.interfaceRegistry),
		gov.NewAppModule(appCodec, &app.GovKeeper, app.AccountKeeper, app.BankKeeper, nil),
		slashing.NewAppModule(appCodec, app.SlashingKeeper, app.AccountKeeper, app.BankKeeper, app.StakingKeeper, nil, app.interfaceRegistry),
		distr.NewAppModule(appCodec, app.DistrKeeper, app.AccountKeeper, app.BankKeeper, app.StakingKeeper, nil),
		staking.NewAppModule(appCodec, app.StakingKeeper, app.AccountKeeper, app.BankKeeper, nil),
		upgrade.NewAppModule(app.UpgradeKeeper, app.AccountKeeper.AddressCodec()),
		evidence.NewAppModule(app.EvidenceKeeper),
		authzmodule.NewAppModule(appCodec, app.AuthzKeeper, app.AccountKeeper, app.BankKeeper, app.interfaceRegistry),
		consensus.NewAppModule(appCodec, app.ConsensusParamsKeeper),
		vesting.NewAppModule(app.AccountKeeper, app.BankKeeper),
		ibc.NewAppModule(app.IBCKeeper),
		ibctm.NewAppModule(tmLightClientModule),
		transferModule,
		packetforward.NewAppModule(app.PFMKeeper),
		ratelimiting.NewAppModule(app.RateLimitKeeper),
		vmModule,
		feemarket.NewAppModule(app.FeeMarketKeeper),
		erc20.NewAppModule(app.Erc20Keeper, app.AccountKeeper),
		council.NewAppModule(appCodec, app.CouncilKeeper),
		apps.NewAppModule(appCodec, app.AppsKeeper),
		settle.NewAppModule(appCodec, app.SettleKeeper),
	)

	app.BasicModuleManager = module.NewBasicManagerFromManager(
		app.ModuleManager,
		map[string]module.AppModuleBasic{
			genutiltypes.ModuleName:     genutil.NewAppModuleBasic(genutiltypes.DefaultMessageValidator),
			stakingtypes.ModuleName:     staking.AppModuleBasic{},
			govtypes.ModuleName:         gov.NewAppModuleBasic(nil),
			ibctransfertypes.ModuleName: transfer.AppModuleBasic{},
		},
	)
	app.BasicModuleManager.RegisterLegacyAminoCodec(legacyAmino)
	app.BasicModuleManager.RegisterInterfaces(interfaceRegistry)

	app.ModuleManager.SetOrderPreBlockers(upgradetypes.ModuleName, authtypes.ModuleName, evmtypes.ModuleName)

	app.ModuleManager.SetOrderBeginBlockers(
		ibcexported.ModuleName, ibctransfertypes.ModuleName,
		erc20types.ModuleName, feemarkettypes.ModuleName,
		evmtypes.ModuleName, // must come after feemarket
		distrtypes.ModuleName, slashingtypes.ModuleName,
		evidencetypes.ModuleName, stakingtypes.ModuleName,
		authtypes.ModuleName, banktypes.ModuleName, govtypes.ModuleName, genutiltypes.ModuleName,
		authz.ModuleName, feegrant.ModuleName, consensusparamtypes.ModuleName, vestingtypes.ModuleName,
		packetforwardtypes.ModuleName, ratelimittypes.ModuleName,
		counciltypes.ModuleName, appstypes.ModuleName, settletypes.ModuleName,
	)

	// settle before staking: validator payouts use the bonded set that
	// produced this block. feemarket last to capture full block gas.
	app.ModuleManager.SetOrderEndBlockers(
		banktypes.ModuleName, govtypes.ModuleName,
		counciltypes.ModuleName, appstypes.ModuleName, settletypes.ModuleName,
		stakingtypes.ModuleName, authtypes.ModuleName,
		evmtypes.ModuleName, erc20types.ModuleName,
		ibcexported.ModuleName, ibctransfertypes.ModuleName, packetforwardtypes.ModuleName, ratelimittypes.ModuleName,
		distrtypes.ModuleName, slashingtypes.ModuleName,
		genutiltypes.ModuleName, evidencetypes.ModuleName, authz.ModuleName,
		feegrant.ModuleName, upgradetypes.ModuleName, consensusparamtypes.ModuleName, vestingtypes.ModuleName,
		feemarkettypes.ModuleName,
	)

	// council before staking+genutil: gentxs hit the council admission hook.
	// settle after bank: it records the genesis credit supply.
	genesisModuleOrder := []string{
		authtypes.ModuleName, banktypes.ModuleName,
		counciltypes.ModuleName,
		distrtypes.ModuleName, stakingtypes.ModuleName, slashingtypes.ModuleName, govtypes.ModuleName,
		ibcexported.ModuleName,
		evmtypes.ModuleName, feemarkettypes.ModuleName, erc20types.ModuleName,
		ibctransfertypes.ModuleName, packetforwardtypes.ModuleName, ratelimittypes.ModuleName,
		appstypes.ModuleName, settletypes.ModuleName,
		genutiltypes.ModuleName, evidencetypes.ModuleName, authz.ModuleName,
		feegrant.ModuleName, upgradetypes.ModuleName, vestingtypes.ModuleName, consensusparamtypes.ModuleName,
	}
	app.ModuleManager.SetOrderInitGenesis(genesisModuleOrder...)
	app.ModuleManager.SetOrderExportGenesis(genesisModuleOrder...)

	app.configurator = module.NewConfigurator(app.appCodec, app.MsgServiceRouter(), app.GRPCQueryRouter())
	if err = app.ModuleManager.RegisterServices(app.configurator); err != nil {
		panic(fmt.Sprintf("failed to register services in module manager: %s", err.Error()))
	}

	app.RegisterUpgradeHandlers()

	autocliv1.RegisterQueryServer(app.GRPCQueryRouter(), runtimeservices.NewAutoCLIQueryService(app.ModuleManager.Modules))
	reflectionSvc, err := runtimeservices.NewReflectionService()
	if err != nil {
		panic(err)
	}
	reflectionv1.RegisterReflectionServiceServer(app.GRPCQueryRouter(), reflectionSvc)

	app.sm = module.NewSimulationManagerFromAppModules(app.ModuleManager.Modules, map[string]module.AppModuleSimulation{
		authtypes.ModuleName: auth.NewAppModule(app.appCodec, app.AccountKeeper, authsims.RandomGenesisAccounts, nil),
	})
	app.sm.RegisterStoreDecoders()

	app.MountKVStores(keys)
	app.MountObjectStores(oKeys)

	maxGasWanted := cast.ToUint64(appOpts.Get(srvflags.EVMMaxTxGasWanted))

	app.SetInitChainer(app.InitChainer)
	app.SetPreBlocker(app.PreBlocker)
	app.SetBeginBlocker(app.BeginBlocker)
	app.SetEndBlocker(app.EndBlocker)

	app.setAnteHandler(app.txConfig, maxGasWanted)

	if err := app.configureEVMMempool(appOpts, logger); err != nil {
		panic(fmt.Sprintf("failed to configure EVM mempool: %s", err.Error()))
	}

	app.setPostHandler()

	protoFiles, err := proto.MergedRegistry()
	if err != nil {
		panic(err)
	}
	if err := msgservice.ValidateProtoAnnotations(protoFiles); err != nil {
		fmt.Fprintln(os.Stderr, err.Error())
	}

	if loadLatest {
		if err := app.LoadLatestVersion(); err != nil {
			logger.Error("error on loading last version", "err", err)
			os.Exit(1)
		}
		ctx := app.NewContextLegacy(true, cmtproto.Header{Height: app.LastBlockHeight(), ChainID: app.ChainID()})
		vmModule.HydrateGlobals(ctx)
	}

	// Block-STM is opt-in. The sequential runner is the safe default.
	var runner sdk.TxRunner = txnrunner.NewDefaultRunner(txDecoder)
	if cast.ToBool(appOpts.Get(vaporconfig.FlagBlockSTM)) {
		logger.Info("Block-STM parallel execution ENABLED")
		runner = txnrunner.NewSTMRunner(
			txDecoder, nonTransientKeys,
			min(goruntime.GOMAXPROCS(0), goruntime.NumCPU()), true,
			func(ms storetypes.MultiStore) string {
				return app.EVMKeeper.GetParams(sdk.NewContext(ms, cmtproto.Header{}, false, log.NewNopLogger())).EvmDenom
			},
		)
	}
	vmrunner.SetRunner(bApp, runner)

	return app
}

func (app *VaporApp) setAnteHandler(txConfig client.TxConfig, maxGasWanted uint64) {
	options := evmante.HandlerOptions{
		Cdc:                    app.appCodec,
		AccountKeeper:          app.AccountKeeper,
		BankKeeper:             app.BankKeeper,
		ExtensionOptionChecker: antetypes.HasDynamicFeeExtensionOption,
		EvmKeeper:              app.EVMKeeper,
		FeegrantKeeper:         app.FeeGrantKeeper,
		IBCKeeper:              app.IBCKeeper,
		FeeMarketKeeper:        app.FeeMarketKeeper,
		SignModeHandler:        txConfig.SignModeHandler(),
		SigGasConsumer:         evmante.SigVerificationGasConsumer,
		MaxTxGasWanted:         maxGasWanted,
		DynamicFeeChecker:      true,
		PendingTxListener:      app.onPendingTx,
	}
	if err := options.Validate(); err != nil {
		panic(err)
	}
	app.SetAnteHandler(NewCouncilGuardAnte(app.CouncilKeeper, evmante.NewAnteHandler(options)))
}

func (app *VaporApp) onPendingTx(hash common.Hash) {
	for _, listener := range app.pendingTxListeners {
		listener(hash)
	}
}

// RegisterPendingTxListener is used by the JSON-RPC server.
func (app *VaporApp) RegisterPendingTxListener(listener func(common.Hash)) {
	app.pendingTxListeners = append(app.pendingTxListeners, listener)
}

func (app *VaporApp) setPostHandler() {
	postHandler, err := posthandler.NewPostHandler(posthandler.HandlerOptions{})
	if err != nil {
		panic(err)
	}
	app.SetPostHandler(postHandler)
}

func (app *VaporApp) Name() string { return app.BaseApp.Name() }

func (app *VaporApp) BeginBlocker(ctx sdk.Context) (sdk.BeginBlock, error) {
	return app.ModuleManager.BeginBlock(ctx)
}

func (app *VaporApp) EndBlocker(ctx sdk.Context) (sdk.EndBlock, error) {
	return app.ModuleManager.EndBlock(ctx)
}

func (app *VaporApp) FinalizeBlock(req *abci.RequestFinalizeBlock) (*abci.ResponseFinalizeBlock, error) {
	return app.BaseApp.FinalizeBlock(req)
}

func (app *VaporApp) Configurator() module.Configurator { return app.configurator }

func (app *VaporApp) InitChainer(ctx sdk.Context, req *abci.RequestInitChain) (*abci.ResponseInitChain, error) {
	var genesisState GenesisState
	if err := json.Unmarshal(req.AppStateBytes, &genesisState); err != nil {
		panic(err)
	}
	if err := app.UpgradeKeeper.SetModuleVersionMap(ctx, app.ModuleManager.GetVersionMap()); err != nil {
		panic(err)
	}
	return app.ModuleManager.InitGenesis(ctx, app.appCodec, genesisState)
}

func (app *VaporApp) PreBlocker(ctx sdk.Context, _ *abci.RequestFinalizeBlock) (*sdk.ResponsePreBlock, error) {
	return app.ModuleManager.PreBlock(ctx)
}

func (app *VaporApp) LoadHeight(height int64) error { return app.LoadVersion(height) }

func (app *VaporApp) LegacyAmino() *codec.LegacyAmino           { return app.legacyAmino }
func (app *VaporApp) AppCodec() codec.Codec                     { return app.appCodec }
func (app *VaporApp) InterfaceRegistry() types.InterfaceRegistry { return app.interfaceRegistry }
func (app *VaporApp) TxConfig() client.TxConfig                 { return app.txConfig }
func (app *VaporApp) GetTxConfig() client.TxConfig              { return app.txConfig }

// DefaultGenesis returns VaporChain's default genesis (see genesis.go).
func (app *VaporApp) DefaultGenesis() map[string]json.RawMessage {
	return NewDefaultGenesisState(app.appCodec, app.BasicModuleManager)
}

func (app *VaporApp) GetKey(storeKey string) *storetypes.KVStoreKey { return app.keys[storeKey] }

func (app *VaporApp) SimulationManager() *module.SimulationManager { return app.sm }

func (app *VaporApp) RegisterAPIRoutes(apiSvr *api.Server, apiConfig config.APIConfig) {
	clientCtx := apiSvr.ClientCtx
	authtx.RegisterGRPCGatewayRoutes(clientCtx, apiSvr.GRPCGatewayRouter)
	cmtservice.RegisterGRPCGatewayRoutes(clientCtx, apiSvr.GRPCGatewayRouter)
	node.RegisterGRPCGatewayRoutes(clientCtx, apiSvr.GRPCGatewayRouter)
	app.BasicModuleManager.RegisterGRPCGatewayRoutes(clientCtx, apiSvr.GRPCGatewayRouter)
	if err := sdkserver.RegisterSwaggerAPI(apiSvr.ClientCtx, apiSvr.Router, apiConfig.Swagger); err != nil {
		panic(err)
	}
}

func (app *VaporApp) RegisterTxService(clientCtx client.Context) {
	authtx.RegisterTxService(app.GRPCQueryRouter(), clientCtx, app.Simulate, app.interfaceRegistry)
}

func (app *VaporApp) RegisterTendermintService(clientCtx client.Context) {
	cmtservice.RegisterTendermintService(clientCtx, app.GRPCQueryRouter(), app.interfaceRegistry, app.Query)
}

func (app *VaporApp) RegisterNodeService(clientCtx client.Context, cfg config.Config) {
	node.RegisterNodeService(clientCtx, app.GRPCQueryRouter(), cfg, func() int64 {
		return app.CommitMultiStore().EarliestVersion()
	})
}

func (app *VaporApp) GetMempool() sdkmempool.ExtMempool { return app.EVMMempool }

func (app *VaporApp) GetAnteHandler() sdk.AnteHandler { return app.AnteHandler() }

// Close shuts down the mempool and the BaseApp.
func (app *VaporApp) Close() error {
	var err error
	if m, ok := app.EVMMempool.(*evmmempool.Mempool); ok && m != nil {
		app.Logger().Info("Shutting down mempool")
		err = m.Close()
	}
	err = errors.Join(err, app.BaseApp.Close())
	if err == nil {
		app.Logger().Info("Application gracefully shutdown")
	} else {
		app.Logger().Error("Application shutdown with errors", "error", err)
	}
	return err
}

// AutoCliOpts returns the autocli options for the app.
func (app *VaporApp) AutoCliOpts() autocli.AppOptions {
	modules := make(map[string]appmodule.AppModule)
	for _, m := range app.ModuleManager.Modules {
		if withName, ok := m.(module.HasName); ok {
			if am, ok := withName.(appmodule.AppModule); ok {
				modules[withName.Name()] = am
			}
		}
	}
	return autocli.AppOptions{
		Modules:               modules,
		ModuleOptions:         runtimeservices.ExtractAutoCLIOptions(app.ModuleManager.Modules),
		AddressCodec:          evmaddress.NewEvmCodec(sdk.GetConfig().GetBech32AccountAddrPrefix()),
		ValidatorAddressCodec: evmaddress.NewEvmCodec(sdk.GetConfig().GetBech32ValidatorAddrPrefix()),
		ConsensusAddressCodec: evmaddress.NewEvmCodec(sdk.GetConfig().GetBech32ConsensusAddrPrefix()),
	}
}
