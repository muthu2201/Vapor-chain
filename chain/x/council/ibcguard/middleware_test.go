// Copyright (c) 2026 VaporChain / muthu2201. All rights reserved.
// Provenance: VAPOR-6eabb1be532bdef4

package ibcguard

import (
	"testing"

	"github.com/stretchr/testify/require"

	cmtproto "github.com/cometbft/cometbft/proto/tendermint/types"

	"cosmossdk.io/log/v2"

	sdk "github.com/cosmos/cosmos-sdk/types"

	transfertypes "github.com/cosmos/ibc-go/v11/modules/apps/transfer/types"
	clienttypes "github.com/cosmos/ibc-go/v11/modules/core/02-client/types"
	channeltypes "github.com/cosmos/ibc-go/v11/modules/core/04-channel/types"
	porttypes "github.com/cosmos/ibc-go/v11/modules/core/05-port/types"
	ibcexported "github.com/cosmos/ibc-go/v11/modules/core/exported"
)

type guard map[string]bool

func (g guard) IsIBCPaused(_ sdk.Context, denoms ...string) bool {
	if g["*"] {
		return true
	}
	for _, d := range denoms {
		if g[d] {
			return true
		}
	}
	return false
}

type ics4 struct{ sent int }

func (w *ics4) SendPacket(sdk.Context, string, string, clienttypes.Height, uint64, []byte) (uint64, error) {
	w.sent++
	return 1, nil
}
func (w *ics4) WriteAcknowledgement(sdk.Context, ibcexported.PacketI, ibcexported.Acknowledgement) error {
	return nil
}
func (w *ics4) GetAppVersion(sdk.Context, string, string) (string, bool) { return "ics20-1", true }

type app struct {
	porttypes.IBCModule
	recv int
}

func (a *app) OnRecvPacket(sdk.Context, string, channeltypes.Packet, sdk.AccAddress) ibcexported.Acknowledgement {
	a.recv++
	return channeltypes.NewResultAcknowledgement([]byte{1})
}
func (a *app) UnmarshalPacketData(sdk.Context, string, string, []byte) (any, string, error) {
	return nil, "", nil
}

func data(denom string) []byte {
	d := transfertypes.NewFungibleTokenPacketData(denom, "1", "a", "b", "")
	return d.GetBytes()
}

func setup(g guard) (*IBCMiddleware, *ics4, *app) {
	mw := NewIBCMiddleware(g)
	w := &ics4{}
	a := &app{}
	mw.SetICS4Wrapper(w)
	mw.SetUnderlyingApplication(a)
	return mw, w, a
}

var ctx = sdk.NewContext(nil, cmtproto.Header{}, false, log.NewNopLogger())

func TestCreditAndPowerNeverLeave(t *testing.T) {
	mw, w, _ := setup(guard{})
	for _, d := range []string{"acredit", "avpower"} {
		_, err := mw.SendPacket(ctx, "transfer", "channel-0", clienttypes.ZeroHeight(), 1, data(d))
		require.Error(t, err, d)
	}
	_, err := mw.SendPacket(ctx, "transfer", "channel-0", clienttypes.ZeroHeight(), 1, data("transfer/channel-0/uusdc"))
	require.NoError(t, err)
	require.Equal(t, 1, w.sent)
}

func TestPauseBlocksBothDirectionsButNotRefunds(t *testing.T) {
	voucher := transfertypes.ExtractDenomFromPath("transfer/channel-7/uusdc").IBCDenom()
	mw, w, a := setup(guard{voucher: true})
	_, err := mw.SendPacket(ctx, "transfer", "channel-7", clienttypes.ZeroHeight(), 1, data("transfer/channel-7/uusdc"))
	require.Error(t, err)
	require.Equal(t, 0, w.sent)
	// inbound USDC arriving over channel-7 becomes the paused voucher
	pkt := channeltypes.NewPacket(data("uusdc"), 1, "transfer", "channel-99", "transfer", "channel-7", clienttypes.ZeroHeight(), 1)
	ack := mw.OnRecvPacket(ctx, "ics20-1", pkt, nil)
	require.False(t, ack.Success())
	require.Equal(t, 0, a.recv)
}
