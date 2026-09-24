// Copyright (c) 2026 VaporChain / muthu2201. All rights reserved.
// Proprietary and confidential. See LICENSE at the repository root.
// Provenance: VAPOR-6eabb1be532bdef4

// Package rpc adds the "vapor" JSON-RPC namespace to the node.
//
// Why this exists: cosmos/evm v0.7.3 has a bug in eth_fillTransaction. Its
// SetTxDefaults estimates gas against block 0 (the genesis state) instead of
// the latest block, so any call that touches state created after genesis
// (a registered app, a deployed contract, a funded balance) reverts during
// estimation. viem's deployContract/sendTransaction call eth_fillTransaction
// first, so every wallet flow that depends on post-genesis state broke.
//
// We cannot edit the upstream module, and the upstream namespace registry
// refuses duplicate names. It does, however, register every API with
// go-ethereum's rpc.Server, which merges services that share a namespace and
// lets a later registration replace a single method. The "vapor" creator
// therefore returns two services:
//
//   - an "eth" service carrying only FillTransaction, which estimates gas at
//     the latest block and then defers to the upstream defaults;
//   - the "vapor" service (vapor_provenance, vapor_clientVersion).
//
// EnsureNamespaceOrder (called from the root command) guarantees "vapor" is
// enabled and registered after "eth", so the override always wins.
package rpc

import (
	"context"
	"errors"
	"slices"

	"github.com/ethereum/go-ethereum/common/hexutil"
	ethtypes "github.com/ethereum/go-ethereum/core/types"
	gethrpc "github.com/ethereum/go-ethereum/rpc"

	evmrpc "github.com/cosmos/evm/rpc"
	"github.com/cosmos/evm/rpc/backend"
	"github.com/cosmos/evm/rpc/stream"
	rpctypes "github.com/cosmos/evm/rpc/types"
	evmtypes "github.com/cosmos/evm/x/vm/types"

	"github.com/cosmos/cosmos-sdk/client"
	"github.com/cosmos/cosmos-sdk/server"

	"github.com/muthu2201/vapor-chain/chain/provenance"
)

// Namespace is the JSON-RPC namespace this package registers.
const Namespace = "vapor"

const apiVersion = "1.0"

func init() {
	if err := evmrpc.RegisterAPINamespace(Namespace, newAPIs); err != nil {
		panic(err)
	}
}

func newAPIs(_ *server.Context, _ client.Context, _ *stream.RPCStream, b backend.BackendI) []gethrpc.API {
	return []gethrpc.API{
		{Namespace: evmrpc.EthNamespace, Version: apiVersion, Service: &EthOverrides{backend: b}, Public: true},
		{Namespace: Namespace, Version: apiVersion, Service: &VaporAPI{}, Public: true},
	}
}

// EnsureNamespaceOrder returns the configured namespace list with "vapor"
// present whenever "eth" is, and placed after it. Order matters because
// go-ethereum's registry lets the last registration of a method win.
func EnsureNamespaceOrder(apis []string) []string {
	out := slices.DeleteFunc(slices.Clone(apis), func(s string) bool { return s == Namespace })
	if slices.Contains(out, evmrpc.EthNamespace) {
		out = append(out, Namespace)
	}
	return out
}

// ------------------------------------------------------------------- eth

// EthOverrides replaces individual eth_* methods whose upstream behaviour is
// wrong. Each method documents the upstream defect it fixes.
type EthOverrides struct {
	backend backend.BackendI
}

// FillTransaction fills in defaults (nonce, fees, gas, chain id) and returns
// the unsigned RLP, exactly like upstream, except that the gas estimate runs
// against the latest block instead of genesis.
func (e *EthOverrides) FillTransaction(args evmtypes.TransactionArgs) (*rpctypes.SignTransactionResult, error) {
	if args.From == nil {
		return nil, errors.New("from address is required")
	}
	ctx := context.Background()
	estimate := args.Gas == nil
	if estimate {
		// A placeholder gas makes upstream SetTxDefaults skip its genesis
		// estimate while it resolves nonce, fees and chain id.
		placeholder := hexutil.Uint64(1)
		args.Gas = &placeholder
	}
	args, err := e.backend.SetTxDefaults(ctx, args)
	if err != nil {
		return nil, err
	}
	if estimate {
		args.Gas = nil
		latest := rpctypes.EthLatestBlockNumber
		gas, err := e.backend.EstimateGas(ctx, args, &rpctypes.BlockNumberOrHash{BlockNumber: &latest}, nil)
		if err != nil {
			return nil, err
		}
		args.Gas = &gas
	}
	tx := args.ToTransaction(ethtypes.LegacyTxType) // upgrades to 1559/7702 when those fields are set
	raw, err := tx.MarshalBinary()
	if err != nil {
		return nil, err
	}
	return &rpctypes.SignTransactionResult{Raw: raw, Tx: tx}, nil
}

// ----------------------------------------------------------------- vapor

// VaporAPI serves chain-specific, read-only metadata.
type VaporAPI struct{}

// ProvenanceResult is returned by vapor_provenance.
type ProvenanceResult struct {
	Owner       string `json:"owner"`
	Fingerprint string `json:"fingerprint"`
	ShortID     string `json:"shortId"`
	Version     string `json:"version"`
	Commit      string `json:"commit"`
	BuildTime   string `json:"buildTime"`
}

// Provenance returns the authorship fingerprint compiled into this binary.
// Explorers and wallets use it to verify they talk to a genuine node build.
func (VaporAPI) Provenance() ProvenanceResult {
	return ProvenanceResult{
		Owner:       provenance.Owner,
		Fingerprint: provenance.Fingerprint,
		ShortID:     provenance.ShortID,
		Version:     provenance.Version,
		Commit:      provenance.GitCommit,
		BuildTime:   provenance.BuildTime,
	}
}

// ClientVersion mirrors web3_clientVersion with the provenance id appended.
func (VaporAPI) ClientVersion() string {
	return provenance.String()
}
