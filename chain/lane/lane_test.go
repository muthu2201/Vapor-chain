// SPDX-License-Identifier: AGPL-3.0-only
// Copyright (c) 2026 VaporChain / muthu2201
// Provenance: VAPOR-6eabb1be532bdef4

package lane

import (
	"context"
	"encoding/binary"
	"errors"
	"testing"

	"math/big"

	"github.com/ethereum/go-ethereum/common"
	ethtypes "github.com/ethereum/go-ethereum/core/types"
	"github.com/ethereum/go-ethereum/crypto"
	"github.com/stretchr/testify/require"
	protov2 "google.golang.org/protobuf/proto"

	abci "github.com/cometbft/cometbft/abci/types"
	cmtproto "github.com/cometbft/cometbft/proto/tendermint/types"

	evmtypes "github.com/cosmos/evm/x/vm/types"

	"cosmossdk.io/log/v2"

	sdk "github.com/cosmos/cosmos-sdk/types"
)

var bundler = common.HexToAddress("0x00000000000000000000000000000000000b0b01")

type policy struct{ bps uint32 }

func (p policy) SponsoredLane(sdk.Context) (map[common.Address]struct{}, uint32) {
	return map[common.Address]struct{}{bundler: {}}, p.bps
}

// fakeTx is a minimal sdk.Tx + GasTx.
type fakeTx struct {
	msgs []sdk.Msg
	gas  uint64
}

func (t fakeTx) GetMsgs() []sdk.Msg                    { return t.msgs }
func (t fakeTx) GetMsgsV2() ([]protov2.Message, error) { return nil, nil }
func (t fakeTx) GetGas() uint64                        { return t.gas }

func ethTx(from common.Address, gas uint64) fakeTx {
	return fakeTx{msgs: []sdk.Msg{&evmtypes.MsgEthereumTx{From: from.Bytes()}}, gas: gas}
}

// encode/decode: 1 byte kind + 8 bytes gas (0 = sponsored, 1 = normal)
func enc(sponsored bool, gas uint64) []byte {
	b := make([]byte, 9)
	if !sponsored {
		b[0] = 1
	}
	binary.BigEndian.PutUint64(b[1:], gas)
	return b
}

func dec(bz []byte) (sdk.Tx, error) {
	if len(bz) != 9 {
		return nil, errors.New("bad tx")
	}
	from := bundler
	if bz[0] == 1 {
		from = common.HexToAddress("0x1234")
	}
	return ethTx(from, binary.BigEndian.Uint64(bz[1:])), nil
}

func ctxWithGas(max int64) sdk.Context {
	return sdk.NewContext(nil, cmtproto.Header{}, false, log.NewNopLogger()).
		WithConsensusParams(cmtproto.ConsensusParams{Block: &cmtproto.BlockParams{MaxGas: max, MaxBytes: 4 << 20}})
}

func TestIsSponsored(t *testing.T) {
	set := map[common.Address]struct{}{bundler: {}}
	require.True(t, IsSponsored(ethTx(bundler, 1), set))
	require.False(t, IsSponsored(ethTx(common.HexToAddress("0x1"), 1), set))
	// multi-msg txs are never classified sponsored (cannot smuggle a paying msg)
	require.False(t, IsSponsored(fakeTx{msgs: []sdk.Msg{&evmtypes.MsgEthereumTx{From: bundler.Bytes()}, &evmtypes.MsgEthereumTx{From: bundler.Bytes()}}}, set))
	require.False(t, IsSponsored(ethTx(bundler, 1), nil))
}

// TestIsSponsoredRecoversSignerWhenFromEmpty is the ProcessProposal case: a
// freshly decoded MsgEthereumTx has an EMPTY From, so classification must fall
// back to recovering the signer from the signature. A From-based check would
// silently treat every such tx as non-sponsored and never enforce the cap.
func TestIsSponsoredRecoversSignerWhenFromEmpty(t *testing.T) {
	key, err := crypto.GenerateKey()
	require.NoError(t, err)
	signerAddr := crypto.PubkeyToAddress(key.PublicKey)
	chainID := big.NewInt(779700)
	ethSigner := ethtypes.LatestSignerForChainID(chainID)

	freshMsg := func() *evmtypes.MsgEthereumTx {
		tx := ethtypes.MustSignNewTx(key, ethSigner, &ethtypes.LegacyTx{
			Nonce: 0, To: &bundler, Value: big.NewInt(0), Gas: 21000, GasPrice: big.NewInt(1e9),
		})
		msg := &evmtypes.MsgEthereumTx{}
		msg.FromEthereumTx(tx)
		msg.From = nil // a decoded-but-not-yet-ante'd tx has no From
		return msg
	}

	m := freshMsg()
	require.Empty(t, m.From, "precondition: From must be empty")
	require.True(t, IsSponsored(fakeTx{msgs: []sdk.Msg{m}, gas: 21000}, map[common.Address]struct{}{signerAddr: {}}),
		"must recover the signer and classify as sponsored")
	require.False(t, IsSponsored(fakeTx{msgs: []sdk.Msg{freshMsg()}, gas: 21000}, map[common.Address]struct{}{bundler: {}}),
		"a signer outside the set is not sponsored")
}

func TestCapMath(t *testing.T) {
	require.Equal(t, uint64(20_000_000), Cap(40_000_000, 5_000))
	require.Equal(t, uint64(0), Cap(0, 5_000))
	require.Equal(t, uint64(9_223_372_036_854_775_807/10_000*5_000+(9_223_372_036_854_775_807%10_000)*5_000/10_000), Cap(9_223_372_036_854_775_807, 5_000))
}

func TestSelectorEnforcesSponsoredCap(t *testing.T) {
	s := NewSelector(policy{bps: 5_000})
	ctx := context.Context(ctxWithGas(40_000_000))
	// 10 sponsored txs of 5M gas: only 4 fit in a 20M sponsored lane
	for i := 0; i < 10; i++ {
		s.SelectTxForProposal(ctx, 4<<20, 40_000_000, ethTx(bundler, 5_000_000), enc(true, 5_000_000))
	}
	require.Len(t, s.SelectedTxs(ctx), 4)
	// paying txs still fit into the remaining 20M
	for i := 0; i < 5; i++ {
		s.SelectTxForProposal(ctx, 4<<20, 40_000_000, ethTx(common.HexToAddress("0x9"), 4_000_000), enc(false, 4_000_000))
	}
	require.Len(t, s.SelectedTxs(ctx), 9)
	s.Clear()
	require.Len(t, s.SelectedTxs(ctx), 0)
}

func acceptAll(sdk.Context, *abci.RequestProcessProposal) (*abci.ResponseProcessProposal, error) {
	return &abci.ResponseProcessProposal{Status: abci.ResponseProcessProposal_ACCEPT}, nil
}

// TestProcessProposalRejectsMaliciousProposer simulates a proposer that
// ignores the lane rule: honest validators must vote the block down.
func TestProcessProposalRejectsMaliciousProposer(t *testing.T) {
	h := ProcessProposalHandler(acceptAll, dec, policy{bps: 5_000})
	ctx := ctxWithGas(40_000_000)
	var honest, malicious [][]byte
	for i := 0; i < 4; i++ {
		honest = append(honest, enc(true, 5_000_000))
	}
	honest = append(honest, enc(false, 15_000_000))
	for i := 0; i < 5; i++ { // 25M sponsored > 20M cap
		malicious = append(malicious, enc(true, 5_000_000))
	}
	r, err := h(ctx, &abci.RequestProcessProposal{Txs: honest})
	require.NoError(t, err)
	require.Equal(t, abci.ResponseProcessProposal_ACCEPT, r.Status)
	r, err = h(ctx, &abci.RequestProcessProposal{Txs: malicious})
	require.NoError(t, err)
	require.Equal(t, abci.ResponseProcessProposal_REJECT, r.Status)

	// The lane wrapper is not a validity checker: an undecodable tx is skipped
	// (it cannot be a sponsored MsgEthereumTx), so the wrapper ACCEPTs. Per-tx
	// validity belongs to the inner handler and FinalizeBlock — re-checking it
	// here is what previously halted the chain (STRESS-HALT).
	r, err = h(ctx, &abci.RequestProcessProposal{Txs: [][]byte{{0xde, 0xad}}})
	require.NoError(t, err)
	require.Equal(t, abci.ResponseProcessProposal_ACCEPT, r.Status)

	// If the INNER handler rejects (e.g. its decoder fails on the bytes), the
	// wrapper propagates that rejection unchanged.
	rejectInner := func(sdk.Context, *abci.RequestProcessProposal) (*abci.ResponseProcessProposal, error) {
		return &abci.ResponseProcessProposal{Status: abci.ResponseProcessProposal_REJECT}, nil
	}
	hr := ProcessProposalHandler(rejectInner, dec, policy{bps: 5_000})
	r, err = hr(ctx, &abci.RequestProcessProposal{Txs: honest})
	require.NoError(t, err)
	require.Equal(t, abci.ResponseProcessProposal_REJECT, r.Status)
}

func FuzzProcessProposalNeverExceedsCap(f *testing.F) {
	f.Add(uint8(10), uint32(5_000), uint64(3_000_000))
	f.Fuzz(func(t *testing.T, n uint8, bps uint32, gas uint64) {
		bps %= 9_001
		gas %= 40_000_001
		var txs [][]byte
		for i := 0; i < int(n%64); i++ {
			txs = append(txs, enc(i%2 == 0, gas))
		}
		h := ProcessProposalHandler(acceptAll, dec, policy{bps: bps})
		r, _ := h(ctxWithGas(40_000_000), &abci.RequestProcessProposal{Txs: txs})
		var sponsored uint64
		for i := range txs {
			if i%2 == 0 {
				sponsored += gas
			}
		}
		accepted := r.Status == abci.ResponseProcessProposal_ACCEPT
		if accepted && sponsored > Cap(40_000_000, bps) {
			t.Fatalf("accepted proposal with sponsored gas %d > cap %d", sponsored, Cap(40_000_000, bps))
		}
		if !accepted && sponsored <= Cap(40_000_000, bps) {
			t.Fatalf("rejected a valid proposal")
		}
	})
}
