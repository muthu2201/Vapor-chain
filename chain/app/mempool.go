// SPDX-License-Identifier: Apache-2.0 AND AGPL-3.0-only
// Copyright (c) 2026 VaporChain / muthu2201
// Derived from cosmos/evm evmd v0.7.3 (Apache-2.0, Cosmos Labs); see NOTICE.
// Provenance: VAPOR-6eabb1be532bdef4

package app

import (
	"github.com/ethereum/go-ethereum/common"

	evmmempool "github.com/cosmos/evm/mempool"
	"github.com/cosmos/evm/server"
	evmtypes "github.com/cosmos/evm/x/vm/types"

	"cosmossdk.io/log/v2"

	"github.com/cosmos/cosmos-sdk/baseapp"
	servertypes "github.com/cosmos/cosmos-sdk/server/types"
	sdk "github.com/cosmos/cosmos-sdk/types"

	"github.com/muthu2201/vapor-chain/chain/lane"
)

// lanePolicy reads the sponsored-lane configuration from x/settle params.
type lanePolicy struct{ app *VaporApp }

func (p lanePolicy) SponsoredLane(ctx sdk.Context) (map[common.Address]struct{}, uint32) {
	params := p.app.SettleKeeper.GetParams(ctx)
	return params.SponsoredSenderSet(), params.SponsoredLaneMaxBps
}

// configureEVMMempool installs the Krakatoa app-side mempool (per-sender caps,
// nonce-gap limits, replacement bump, TTL — configured in app.toml
// [evm.mempool]) and VaporChain's two-lane proposal handlers.
func (app *VaporApp) configureEVMMempool(appOpts servertypes.AppOptions, logger log.Logger) error {
	if evmtypes.GetChainConfig() == nil {
		logger.Debug("evm chain config is not set, skipping mempool configuration")
		return nil
	}

	var (
		mpConfig        = server.ResolveMempoolConfig(app.GetAnteHandler(), appOpts, logger)
		txEncoder       = evmmempool.NewTxEncoder(app.txConfig)
		evmRechecker    = evmmempool.NewTxRechecker(mpConfig.AnteHandler, txEncoder)
		cosmosRechecker = evmmempool.NewTxRechecker(mpConfig.AnteHandler, txEncoder)
		cosmosPoolMaxTx = server.GetCosmosPoolMaxTx(appOpts, logger)
		checkTxTimeout  = server.GetMempoolCheckTxTimeout(appOpts, logger)
	)

	if cosmosPoolMaxTx < 0 {
		logger.Debug("evm mempool is disabled, skipping configuration")
		return nil
	}
	if err := server.ValidateReapBounds(appOpts, mpConfig.BlockGasLimit); err != nil {
		return err
	}

	mempool := evmmempool.NewMempool(
		app.CreateQueryContext, logger, app.EVMKeeper, app.FeeMarketKeeper, app.txConfig,
		evmRechecker, cosmosRechecker, mpConfig, cosmosPoolMaxTx,
	)
	app.EVMMempool = mempool

	policy := lanePolicy{app: app}
	// Prepare and Process MUST agree on block validity or consensus halts.
	// LaneProposalTxVerifier skips ante in both (validity is FinalizeBlock's
	// job; the sponsored-lane cap is enforced separately in ProcessProposal by
	// recovering each tx's signer). See tx_verifier.go for the full rationale.
	proposalHandler := baseapp.NewDefaultProposalHandler(mempool, NewLaneProposalTxVerifier(app.BaseApp))
	proposalHandler.SetTxSelector(lane.NewSelector(policy))

	app.SetPrepareProposal(proposalHandler.PrepareProposalHandler())
	app.SetProcessProposal(lane.ProcessProposalHandler(proposalHandler.ProcessProposalHandler(), app.TxDecode, policy))
	app.SetInsertTxHandler(mempool.NewInsertTxHandler(app.TxDecode))
	app.SetReapTxsHandler(mempool.NewReapTxsHandler())
	app.SetCheckTxHandler(mempool.NewCheckTxHandler(app.TxDecode, checkTxTimeout))
	app.SetMempool(mempool)

	app.SetPrepareCheckStater(func(_ sdk.Context) {
		if !mempool.HasEventBus() {
			mempool.NotifyNewBlock()
		}
	})
	return nil
}
