// Copyright (c) 2026 VaporChain / muthu2201. All rights reserved.
// Derived from cosmos/evm evmd (Apache-2.0). See NOTICE.
// Provenance: VAPOR-6eabb1be532bdef4

package app

import (
	"github.com/cosmos/cosmos-sdk/baseapp"
	sdk "github.com/cosmos/cosmos-sdk/types"
)

var _ baseapp.ProposalTxVerifier = &NoCheckProposalTxVerifier{}

// NoCheckProposalTxVerifier skips re-running the ante handler in
// PrepareProposal: every tx in the app-side mempool already passed CheckTx and
// is re-checked by the mempool on every new block. ProcessProposal still runs
// the full ante handler on every tx (that is what makes the sponsored-lane
// classification tamper-proof).
type NoCheckProposalTxVerifier struct {
	*baseapp.BaseApp
}

func NewNoCheckProposalTxVerifier(b *baseapp.BaseApp) *NoCheckProposalTxVerifier {
	return &NoCheckProposalTxVerifier{BaseApp: b}
}

func (txv *NoCheckProposalTxVerifier) PrepareProposalVerifyTx(tx sdk.Tx) ([]byte, error) {
	return txv.TxEncode(tx)
}
