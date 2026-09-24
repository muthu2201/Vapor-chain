// Copyright (c) 2026 VaporChain / muthu2201. All rights reserved.
// Derived from cosmos/evm evmd (Apache-2.0). See NOTICE.
// Provenance: VAPOR-6eabb1be532bdef4

package config

import (
	clienthelpers "cosmossdk.io/client/v2/helpers"

	serverconfig "github.com/cosmos/cosmos-sdk/server/config"

	cosmosevmserverconfig "github.com/cosmos/evm/server/config"

	"github.com/muthu2201/vapor-chain/chain/constants"
)

func MustGetDefaultNodeHome() string {
	h, err := clienthelpers.GetNodeHomeDirectory(constants.DefaultHomeDirName)
	if err != nil {
		panic(err)
	}
	return h
}

// VaporConfig holds VaporChain-specific node options (app.toml [vaporchain]).
type VaporConfig struct {
	// BlockSTM enables optimistic parallel execution. OFF by default: keep it
	// off until the determinism suite passes on the production workload.
	BlockSTM bool `mapstructure:"block-stm"`
}

const FlagBlockSTM = "vaporchain.block-stm"

// DefaultMinGasPrice is a NON-ZERO floor (1 gwei-equivalent in acredit). Zero
// gas price turns cheap execution into free spam; with the default credit
// price (1 USDC = 1e21 acredit) a 21k-gas transfer costs ~0.00002 USDC.
const DefaultMinGasPrice = "1000000000" + constants.CreditDenom

// InitAppConfig returns the app.toml template and defaults.
func InitAppConfig(evmChainID uint64) (string, interface{}) {
	srvCfg := serverconfig.DefaultConfig()
	srvCfg.MinGasPrices = DefaultMinGasPrice
	// Validators keep ~1 day of IAVL versions and ~7 days of blocks; history
	// nodes override these (see deploy/configs/history-node.app.toml).
	srvCfg.Pruning = "custom"
	srvCfg.PruningKeepRecent = "100000"
	srvCfg.PruningInterval = "100"
	srvCfg.MinRetainBlocks = 604_800
	srvCfg.StateSync.SnapshotInterval = 1000
	srvCfg.StateSync.SnapshotKeepRecent = 2

	evmCfg := cosmosevmserverconfig.DefaultEVMConfig()
	evmCfg.EVMChainID = evmChainID

	rpcCfg := cosmosevmserverconfig.DefaultJSONRPCConfig()
	// eth_getLogs range cap protects public RPC from scraping DoS.
	rpcCfg.BlockRangeCap = 2000
	rpcCfg.LogsCap = 10000

	custom := AppConfig{
		Config:     *srvCfg,
		EVM:        *evmCfg,
		JSONRPC:    *rpcCfg,
		TLS:        *cosmosevmserverconfig.DefaultTLSConfig(),
		Vaporchain: VaporConfig{BlockSTM: false},
	}
	return AppTemplate, custom
}

type AppConfig struct {
	serverconfig.Config

	EVM        cosmosevmserverconfig.EVMConfig
	JSONRPC    cosmosevmserverconfig.JSONRPCConfig
	TLS        cosmosevmserverconfig.TLSConfig
	Vaporchain VaporConfig `mapstructure:"vaporchain"`
}

const vaporTemplate = `
###############################################################################
###                           VaporChain Options                            ###
###############################################################################

[vaporchain]

# Optimistic parallel execution (Block-STM). Keep false until the
# determinism suite has passed on your workload.
block-stm = {{ .Vaporchain.BlockSTM }}
`

const AppTemplate = serverconfig.DefaultConfigTemplate + cosmosevmserverconfig.DefaultEVMConfigTemplate + vaporTemplate
