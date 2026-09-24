// Copyright (c) 2026 VaporChain / muthu2201. All rights reserved.
// Proprietary and confidential. See LICENSE at the repository root.
// Derived from cosmos/evm evmd (Apache-2.0). See NOTICE.
// Provenance: VAPOR-6eabb1be532bdef4

package cmd

import (
	"errors"
	"fmt"
	"io"
	"os"

	"github.com/spf13/cast"
	"github.com/spf13/cobra"
	"github.com/spf13/viper"

	cmtcfg "github.com/cometbft/cometbft/config"
	cmtcli "github.com/cometbft/cometbft/libs/cli"

	dbm "github.com/cosmos/cosmos-db"
	cosmosevmcmd "github.com/cosmos/evm/client"
	evmdebug "github.com/cosmos/evm/client/debug"
	"github.com/cosmos/evm/crypto/hd"
	cosmosevmserver "github.com/cosmos/evm/server"
	srvflags "github.com/cosmos/evm/server/flags"
	"github.com/cosmos/evm/utils"

	"cosmossdk.io/log/v2"
	confixcmd "cosmossdk.io/tools/confix/cmd"

	"github.com/cosmos/cosmos-sdk/baseapp"
	"github.com/cosmos/cosmos-sdk/client"
	clientcfg "github.com/cosmos/cosmos-sdk/client/config"
	"github.com/cosmos/cosmos-sdk/client/flags"
	"github.com/cosmos/cosmos-sdk/client/pruning"
	"github.com/cosmos/cosmos-sdk/client/rpc"
	"github.com/cosmos/cosmos-sdk/client/snapshot"
	sdkserver "github.com/cosmos/cosmos-sdk/server"
	servertypes "github.com/cosmos/cosmos-sdk/server/types"
	"github.com/cosmos/cosmos-sdk/store/v2"
	snapshottypes "github.com/cosmos/cosmos-sdk/store/v2/snapshots/types"
	storetypes "github.com/cosmos/cosmos-sdk/store/v2/types"
	simtestutil "github.com/cosmos/cosmos-sdk/testutil/sims"
	sdktestutil "github.com/cosmos/cosmos-sdk/types/module/testutil"
	"github.com/cosmos/cosmos-sdk/types/tx/signing"
	"github.com/cosmos/cosmos-sdk/version"
	authcmd "github.com/cosmos/cosmos-sdk/x/auth/client/cli"
	"github.com/cosmos/cosmos-sdk/x/auth/tx"
	txmodule "github.com/cosmos/cosmos-sdk/x/auth/tx/config"
	authtypes "github.com/cosmos/cosmos-sdk/x/auth/types"
	banktypes "github.com/cosmos/cosmos-sdk/x/bank/types"
	genutilcli "github.com/cosmos/cosmos-sdk/x/genutil/client/cli"

	"github.com/muthu2201/vapor-chain/chain/app"
	vaporconfig "github.com/muthu2201/vapor-chain/chain/app/config"
	"github.com/muthu2201/vapor-chain/chain/constants"
	"github.com/muthu2201/vapor-chain/chain/provenance"
)

// NewRootCmd creates the vaporchaind root command.
func NewRootCmd() *cobra.Command {
	// pre-instantiate the app to obtain the encoding config and autocli opts
	tempApp := app.NewVaporApp(log.NewNopLogger(), dbm.NewMemDB(), nil, true, simtestutil.EmptyAppOptions{})

	encodingConfig := sdktestutil.TestEncodingConfig{
		InterfaceRegistry: tempApp.InterfaceRegistry(),
		Codec:             tempApp.AppCodec(),
		TxConfig:          tempApp.GetTxConfig(),
		Amino:             tempApp.LegacyAmino(),
	}
	initClientCtx := client.Context{}.
		WithCodec(encodingConfig.Codec).
		WithInterfaceRegistry(encodingConfig.InterfaceRegistry).
		WithTxConfig(encodingConfig.TxConfig).
		WithLegacyAmino(encodingConfig.Amino).
		WithInput(os.Stdin).
		WithAccountRetriever(authtypes.AccountRetriever{}).
		WithBroadcastMode(flags.FlagBroadcastMode).
		WithHomeDir(vaporconfig.MustGetDefaultNodeHome()).
		WithViper("VAPORCHAIND").
		WithKeyringOptions(hd.EthSecp256k1Option()).
		WithLedgerHasProtobuf(true)

	rootCmd := &cobra.Command{
		Use:   constants.BinaryName,
		Short: "VaporChain — low-cost consumer EVM appchain with USDC-native checkout",
		Long:  "VaporChain node daemon.\n" + provenance.Notice,
		PersistentPreRunE: func(cmd *cobra.Command, _ []string) error {
			cmd.SetOut(cmd.OutOrStdout())
			cmd.SetErr(cmd.ErrOrStderr())

			initClientCtx = initClientCtx.WithCmdContext(cmd.Context())
			initClientCtx, err := client.ReadPersistentCommandFlags(initClientCtx, cmd.Flags())
			if err != nil {
				return err
			}
			initClientCtx, err = clientcfg.ReadFromClientConfig(initClientCtx)
			if err != nil {
				return err
			}
			if !initClientCtx.Offline {
				enabledSignModes := append(tx.DefaultSignModes, signing.SignMode_SIGN_MODE_TEXTUAL) //nolint:gocritic
				txConfig, err := tx.NewTxConfigWithOptions(initClientCtx.Codec, tx.ConfigOptions{
					EnabledSignModes:           enabledSignModes,
					TextualCoinMetadataQueryFn: txmodule.NewGRPCCoinMetadataQueryFn(initClientCtx),
				})
				if err != nil {
					return err
				}
				initClientCtx = initClientCtx.WithTxConfig(txConfig)
			}
			if err := client.SetCmdClientContextHandler(initClientCtx, cmd); err != nil {
				return err
			}
			customAppTemplate, customAppConfig := vaporconfig.InitAppConfig(constants.LocalnetEVMChainID)
			return sdkserver.InterceptConfigsPreRunHandler(cmd, customAppTemplate, customAppConfig, initCometConfig())
		},
	}

	initRootCmd(rootCmd, tempApp)

	autoCliOpts := tempApp.AutoCliOpts()
	initClientCtx, _ = clientcfg.ReadFromClientConfig(initClientCtx)
	autoCliOpts.ClientCtx = initClientCtx
	if err := autoCliOpts.EnhanceRootCommand(rootCmd); err != nil {
		panic(err)
	}
	return rootCmd
}

// initCometConfig: ~1s blocks, bounded p2p fan-out, indexer on.
func initCometConfig() *cmtcfg.Config {
	cfg := cmtcfg.DefaultConfig()
	// The Krakatoa app-side mempool owns ordering, per-sender caps and nonce
	// gaps; CometBFT must run in "app" mempool mode.
	cfg.Mempool.Type = cmtcfg.MempoolTypeApp
	cfg.Consensus.TimeoutPropose = cfg.Consensus.TimeoutPropose / 3
	cfg.P2P.MaxNumInboundPeers = 60
	cfg.P2P.MaxNumOutboundPeers = 20
	return cfg
}

func initRootCmd(rootCmd *cobra.Command, vApp *app.VaporApp) {
	defaultNodeHome := vaporconfig.MustGetDefaultNodeHome()
	sdkAppCreator := func(l log.Logger, d dbm.DB, ao servertypes.AppOptions) servertypes.Application {
		return newApp(l, d, ao)
	}
	rootCmd.AddCommand(
		genutilcli.InitCmd(vApp.GenesisBasicManager(), defaultNodeHome),
		genutilcli.Commands(vApp.TxConfig(), vApp.BasicModuleManager, defaultNodeHome),
		cmtcli.NewCompletionCmd(rootCmd, true),
		evmdebug.Cmd(),
		confixcmd.ConfigCommand(),
		pruning.Cmd(sdkAppCreator, defaultNodeHome),
		snapshot.Cmd(sdkAppCreator),
		provenanceCmd(),
	)

	cosmosevmserver.AddCommands(rootCmd, cosmosevmserver.NewDefaultStartOptions(newApp, defaultNodeHome), appExport, addModuleInitFlags)

	rootCmd.AddCommand(cosmosevmcmd.KeyCommands(defaultNodeHome, true))
	rootCmd.AddCommand(sdkserver.StatusCommand(), queryCommand(), txCommand())

	if _, err := srvflags.AddTxFlags(rootCmd); err != nil {
		panic(err)
	}
}

func provenanceCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "provenance",
		Short: "Print the VaporChain authorship fingerprint compiled into this binary",
		RunE: func(cmd *cobra.Command, _ []string) error {
			fmt.Fprintln(cmd.OutOrStdout(), provenance.String())
			fmt.Fprintln(cmd.OutOrStdout(), "fingerprint="+provenance.Fingerprint)
			fmt.Fprintln(cmd.OutOrStdout(), "cosmos-sdk="+version.Version)
			return nil
		},
	}
}

func addModuleInitFlags(_ *cobra.Command) {}

func queryCommand() *cobra.Command {
	cmd := &cobra.Command{
		Use: "query", Aliases: []string{"q"}, Short: "Querying subcommands",
		DisableFlagParsing: false, SuggestionsMinimumDistance: 2, RunE: client.ValidateCmd,
	}
	cmd.AddCommand(
		rpc.QueryEventForTxCmd(), rpc.ValidatorCommand(),
		authcmd.QueryTxsByEventsCmd(), authcmd.QueryTxCmd(),
		sdkserver.QueryBlockCmd(), sdkserver.QueryBlockResultsCmd(),
	)
	cmd.PersistentFlags().String(flags.FlagChainID, "", "The network chain ID")
	return cmd
}

func txCommand() *cobra.Command {
	cmd := &cobra.Command{
		Use: "tx", Short: "Transactions subcommands",
		DisableFlagParsing: false, SuggestionsMinimumDistance: 2, RunE: client.ValidateCmd,
	}
	cmd.AddCommand(
		authcmd.GetSignCommand(), authcmd.GetSignBatchCommand(),
		authcmd.GetMultiSignCommand(), authcmd.GetMultiSignBatchCmd(),
		authcmd.GetValidateSignaturesCommand(), authcmd.GetBroadcastCommand(),
		authcmd.GetEncodeCommand(), authcmd.GetDecodeCommand(), authcmd.GetSimulateCmd(),
	)
	cmd.PersistentFlags().String(flags.FlagChainID, "", "The network chain ID")
	return cmd
}

// newApp creates the application for `start`.
func newApp(logger log.Logger, db dbm.DB, appOpts servertypes.AppOptions) cosmosevmserver.Application {
	var cache storetypes.MultiStorePersistentCache
	if cast.ToBool(appOpts.Get(sdkserver.FlagInterBlockCache)) {
		cache = store.NewCommitKVStoreCacheManager()
	}
	pruningOpts, err := sdkserver.GetPruningOptionsFromFlags(appOpts)
	if err != nil {
		panic(err)
	}
	chainID, err := getChainIDFromOpts(appOpts)
	if err != nil {
		panic(err)
	}
	// Refuse to start with an EVM chain id that does not match the network:
	// a mismatch would make every signed EVM tx replayable elsewhere.
	if expected, known := constants.EVMChainIDForChain(chainID); known {
		if got := cast.ToUint64(appOpts.Get(srvflags.EVMChainID)); got != expected {
			panic(fmt.Sprintf("app.toml [evm] evm-chain-id is %d but network %s requires %d", got, chainID, expected))
		}
	}
	snapshotStore, err := sdkserver.GetSnapshotStore(appOpts)
	if err != nil {
		panic(err)
	}
	snapshotOptions := snapshottypes.NewSnapshotOptions(
		cast.ToUint64(appOpts.Get(sdkserver.FlagStateSyncSnapshotInterval)),
		cast.ToUint32(appOpts.Get(sdkserver.FlagStateSyncSnapshotKeepRecent)),
	)
	baseappOptions := []func(*baseapp.BaseApp){
		baseapp.SetPruning(pruningOpts),
		baseapp.SetMinGasPrices(cast.ToString(appOpts.Get(sdkserver.FlagMinGasPrices))),
		baseapp.SetQueryGasLimit(cast.ToUint64(appOpts.Get(sdkserver.FlagQueryGasLimit))),
		baseapp.SetHaltHeight(cast.ToUint64(appOpts.Get(sdkserver.FlagHaltHeight))),
		baseapp.SetHaltTime(cast.ToUint64(appOpts.Get(sdkserver.FlagHaltTime))),
		baseapp.SetMinRetainBlocks(cast.ToUint64(appOpts.Get(sdkserver.FlagMinRetainBlocks))),
		baseapp.SetInterBlockCache(cache),
		baseapp.SetTrace(cast.ToBool(appOpts.Get(sdkserver.FlagTrace))),
		baseapp.SetIndexEvents(cast.ToStringSlice(appOpts.Get(sdkserver.FlagIndexEvents))),
		baseapp.SetSnapshot(snapshotStore, snapshotOptions),
		baseapp.SetIAVLCacheSize(cast.ToInt(appOpts.Get(sdkserver.FlagIAVLCacheSize))),
		baseapp.SetIAVLDisableFastNode(cast.ToBool(appOpts.Get(sdkserver.FlagDisableIAVLFastNode))),
		baseapp.SetChainID(chainID),
	}
	return app.NewVaporApp(logger, db, nil, true, appOpts, baseappOptions...)
}

func appExport(
	logger log.Logger, db dbm.DB, height int64, forZeroHeight bool, jailAllowedAddrs []string,
	appOpts servertypes.AppOptions, modulesToExport []string,
) (servertypes.ExportedApp, error) {
	homePath, ok := appOpts.Get(flags.FlagHome).(string)
	if !ok || homePath == "" {
		return servertypes.ExportedApp{}, errors.New("application home not set")
	}
	viperAppOpts, ok := appOpts.(*viper.Viper)
	if !ok {
		return servertypes.ExportedApp{}, errors.New("appOpts is not viper.Viper")
	}
	viperAppOpts.Set(sdkserver.FlagInvCheckPeriod, 1)
	chainID, err := getChainIDFromOpts(viperAppOpts)
	if err != nil {
		return servertypes.ExportedApp{}, err
	}
	var vApp *app.VaporApp
	if height != -1 {
		vApp = app.NewVaporApp(logger, db, nil, false, viperAppOpts, baseapp.SetChainID(chainID))
		if err := vApp.LoadHeight(height); err != nil {
			return servertypes.ExportedApp{}, err
		}
	} else {
		vApp = app.NewVaporApp(logger, db, nil, true, viperAppOpts, baseapp.SetChainID(chainID))
	}
	return vApp.ExportAppStateAndValidators(forZeroHeight, jailAllowedAddrs, modulesToExport)
}

func getChainIDFromOpts(appOpts servertypes.AppOptions) (string, error) {
	chainID := cast.ToString(appOpts.Get(flags.FlagChainID))
	if chainID == "" {
		return utils.GetChainIDFromHome(cast.ToString(appOpts.Get(flags.FlagHome)))
	}
	return chainID, nil
}

var (
	_ = io.Discard
	_ = banktypes.ModuleName
)
