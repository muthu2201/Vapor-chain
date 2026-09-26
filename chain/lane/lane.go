// SPDX-License-Identifier: AGPL-3.0-only
// Copyright (c) 2026 VaporChain / muthu2201
// Provenance: VAPOR-6eabb1be532bdef4

// Package lane implements VaporChain's two-lane block space policy.
//
// Transactions sent by registered bundlers/sponsors (x/settle
// params.sponsored_senders) are "sponsored": the user did not pay for their
// gas, a sponsor did, usually out of an earned quota. Free resources attract
// abuse, so the SPONSORED lane is capped at sponsored_lane_max_bps of block
// gas (default 50%). Paying transactions always keep the rest of the block.
//
// The rule is enforced twice:
//   - PrepareProposal: the proposer skips sponsored txs once the lane is full.
//   - ProcessProposal: every validator REJECTS a proposal whose sponsored gas
//     exceeds the cap, so a malicious proposer cannot ignore the rule.
//
// Classification is deterministic: a tx is sponsored iff its single message is
// a MsgEthereumTx whose From (already verified against the signature by the
// ante handler during ProcessProposal) is in the sponsored sender set. Gas is
// measured as the declared gas limit, exactly like the SDK's block gas check.
package lane

import (
	"context"

	"github.com/ethereum/go-ethereum/common"
	ethtypes "github.com/ethereum/go-ethereum/core/types"

	abci "github.com/cometbft/cometbft/abci/types"
	cmttypes "github.com/cometbft/cometbft/types"

	evmtypes "github.com/cosmos/evm/x/vm/types"

	"github.com/cosmos/cosmos-sdk/baseapp"
	sdk "github.com/cosmos/cosmos-sdk/types"
)

// Policy supplies the current lane configuration from state.
type Policy interface {
	// SponsoredLane returns the sponsored sender set and the cap in bps.
	SponsoredLane(ctx sdk.Context) (map[common.Address]struct{}, uint32)
}

// IsSponsored classifies a tx by recovering the Ethereum signer from its
// signature and testing membership in the sponsored-sender set.
//
// It recovers the signer rather than trusting MsgEthereumTx.From, because From
// is only populated after the ante handler runs signature verification. In
// PrepareProposal the mempool's txs already carry From (set at CheckTx), but in
// ProcessProposal a freshly decoded tx has an EMPTY From, so a From-based check
// would silently classify every tx as non-sponsored and never enforce the cap.
// Signature recovery is deterministic and unspoofable: a tx counts as sponsored
// only if a sponsored sender actually signed it, so a malicious proposer can
// neither hide sponsored gas under the cap nor forge extra headroom.
func IsSponsored(tx sdk.Tx, senders map[common.Address]struct{}) bool {
	if len(senders) == 0 || tx == nil {
		return false
	}
	msgs := tx.GetMsgs()
	if len(msgs) != 1 {
		return false
	}
	eth, ok := msgs[0].(*evmtypes.MsgEthereumTx)
	if !ok {
		return false
	}
	// Fast path: From already recovered (mempool txs verified at CheckTx).
	if len(eth.From) == 20 {
		_, ok = senders[common.BytesToAddress(eth.From)]
		return ok
	}
	ethTx := eth.AsTransaction()
	from, err := ethtypes.Sender(ethtypes.LatestSignerForChainID(ethTx.ChainId()), ethTx)
	if err != nil {
		return false
	}
	_, ok = senders[from]
	return ok
}

func txGas(tx sdk.Tx) uint64 {
	if g, ok := tx.(baseapp.GasTx); ok {
		return g.GetGas()
	}
	return 0
}

// Cap returns the sponsored-lane gas cap for a block.
func Cap(maxBlockGas uint64, bps uint32) uint64 {
	if maxBlockGas == 0 {
		return 0
	}
	// maxBlockGas * bps / 10_000 without overflow for any realistic block gas
	return maxBlockGas/10_000*uint64(bps) + (maxBlockGas%10_000)*uint64(bps)/10_000
}

// Selector is a baseapp.TxSelector that behaves exactly like the SDK default
// selector (byte + gas limits) and additionally enforces the sponsored cap.
type Selector struct {
	policy Policy

	totalTxBytes uint64
	totalTxGas   uint64
	sponsoredGas uint64
	selectedTxs  [][]byte

	// cached per proposal
	loaded  bool
	senders map[common.Address]struct{}
	bps     uint32
}

var _ baseapp.TxSelector = (*Selector)(nil)

func NewSelector(p Policy) *Selector { return &Selector{policy: p} }

func (s *Selector) SelectedTxs(_ context.Context) [][]byte {
	out := make([][]byte, len(s.selectedTxs))
	copy(out, s.selectedTxs)
	return out
}

func (s *Selector) Clear() {
	s.totalTxBytes, s.totalTxGas, s.sponsoredGas = 0, 0, 0
	s.selectedTxs = nil
	s.loaded, s.senders, s.bps = false, nil, 0
}

func (s *Selector) SelectTxForProposal(ctx context.Context, maxTxBytes, maxBlockGas uint64, memTx sdk.Tx, txBz []byte) bool {
	if !s.loaded {
		s.senders, s.bps = s.policy.SponsoredLane(sdk.UnwrapSDKContext(ctx))
		s.loaded = true
	}
	txSize := uint64(cmttypes.ComputeProtoSizeForTxs([]cmttypes.Tx{txBz}))
	gas := txGas(memTx)
	sponsored := IsSponsored(memTx, s.senders)

	fitsBytes := txSize+s.totalTxBytes <= maxTxBytes
	fitsGas := maxBlockGas == 0 || gas+s.totalTxGas <= maxBlockGas
	fitsLane := !sponsored || maxBlockGas == 0 || gas+s.sponsoredGas <= Cap(maxBlockGas, s.bps)

	if fitsBytes && fitsGas && fitsLane {
		s.totalTxBytes += txSize
		s.totalTxGas += gas
		if sponsored {
			s.sponsoredGas += gas
		}
		s.selectedTxs = append(s.selectedTxs, txBz)
	}
	return s.totalTxBytes >= maxTxBytes || (maxBlockGas > 0 && s.totalTxGas >= maxBlockGas)
}

// ProcessProposalHandler wraps the inner handler and REJECTS a proposal only
// when the sponsored lane exceeds its cap. It does NOT re-run the ante handler:
// per-tx validity (nonces, balances, signatures) is enforced at CheckTx (block
// entry) and again deterministically in FinalizeBlock, where an invalid tx is
// recorded as a failed tx — it never halts the chain. Re-validating here with a
// full ante caused a liveness halt, because PrepareProposal (which builds the
// block from the EVM mempool) and a linear ante replay in ProcessProposal do
// not agree on the exact validity of every tx under load. The lane cap is a
// consensus POLICY a proposer could violate maliciously, so it is the one thing
// that must be checked on every validator.
func ProcessProposalHandler(inner sdk.ProcessProposalHandler, decode sdk.TxDecoder, p Policy) sdk.ProcessProposalHandler {
	return func(ctx sdk.Context, req *abci.RequestProcessProposal) (*abci.ResponseProcessProposal, error) {
		resp, err := inner(ctx, req)
		if err != nil || resp.Status != abci.ResponseProcessProposal_ACCEPT {
			return resp, err
		}
		var maxBlockGas uint64
		if b := ctx.ConsensusParams().Block; b != nil && b.MaxGas > 0 {
			maxBlockGas = uint64(b.MaxGas)
		}
		if maxBlockGas == 0 {
			return resp, nil
		}
		senders, bps := p.SponsoredLane(ctx)
		if len(senders) == 0 {
			return resp, nil
		}
		limit := Cap(maxBlockGas, bps)
		var sponsored uint64
		for _, bz := range req.Txs {
			tx, err := decode(bz)
			if err != nil {
				// Not a decodable SDK tx, hence not a sponsored MsgEthereumTx.
				// Its validity is FinalizeBlock's problem, not ours.
				continue
			}
			if IsSponsored(tx, senders) {
				sponsored += txGas(tx)
				if sponsored > limit {
					ctx.Logger().Info("rejecting proposal: sponsored lane over cap", "sponsored_gas", sponsored, "cap", limit)
					return &abci.ResponseProcessProposal{Status: abci.ResponseProcessProposal_REJECT}, nil
				}
			}
		}
		return resp, nil
	}
}
