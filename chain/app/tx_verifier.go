// Copyright (c) 2026 VaporChain / muthu2201. All rights reserved.
// Proprietary and confidential. See LICENSE at the repository root.
// Derived from cosmos/evm evmd (Apache-2.0). See NOTICE.
// Provenance: VAPOR-6eabb1be532bdef4

package app

import (
	"github.com/cosmos/cosmos-sdk/baseapp"
	sdk "github.com/cosmos/cosmos-sdk/types"
)

var _ baseapp.ProposalTxVerifier = &LaneProposalTxVerifier{}

// LaneProposalTxVerifier skips the ante handler in BOTH PrepareProposal and
// ProcessProposal, so the proposer and the validators agree on exactly which
// txs form a valid block.
//
// WHY skip ante here: every tx was already ante-checked at CheckTx before it
// entered the app-side mempool, and it is re-checked on every new block. The
// authoritative, cross-node-deterministic validity check is FinalizeBlock,
// where an ante failure is recorded as a failed tx (code != 0) — it never
// halts the chain. Running the full ante inside ProcessProposal instead makes
// block acceptance depend on a linear ante replay that does not match how
// PrepareProposal builds the block from the EVM mempool under load; the two
// disagree and every validator rejects the proposer's block, halting
// consensus. The sponsored-lane cap — the one consensus policy a proposer
// could break on purpose — is still enforced on every validator by
// lane.ProcessProposalHandler, which recovers each tx's signer from its
// signature. See docs/security/redteam-results.md (STRESS-HALT).
type LaneProposalTxVerifier struct {
	*baseapp.BaseApp
}

func NewLaneProposalTxVerifier(b *baseapp.BaseApp) *LaneProposalTxVerifier {
	return &LaneProposalTxVerifier{BaseApp: b}
}

// PrepareProposalVerifyTx encodes without re-running ante.
func (v *LaneProposalTxVerifier) PrepareProposalVerifyTx(tx sdk.Tx) ([]byte, error) {
	return v.TxEncode(tx)
}

// ProcessProposalVerifyTx decodes without re-running ante.
func (v *LaneProposalTxVerifier) ProcessProposalVerifyTx(txBz []byte) (sdk.Tx, error) {
	return v.TxDecode(txBz)
}
