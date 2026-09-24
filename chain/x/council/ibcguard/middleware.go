// Copyright (c) 2026 VaporChain / muthu2201. All rights reserved.
// Proprietary and confidential. See LICENSE at the repository root.
// Provenance: VAPOR-6eabb1be532bdef4

// Package ibcguard is the ICS-20 circuit breaker. It sits in the transfer stack
// between rate limiting and PFM:
//
//	core IBC -> callbacks -> ratelimit -> ibcguard -> PFM -> erc20 -> transfer
//
// It does two things:
//  1. Hard-blocks the gas credit and validator power denoms from ever leaving
//     the chain (defence in depth on top of bank SendEnabled=false).
//  2. Enforces emergency pauses set by x/council guardians ("ibc" or
//     "ibc:<denom>") on sends AND receives. Acks and timeouts are always
//     passed through so that refunds of in-flight packets are never blocked.
package ibcguard

import (
	"fmt"
	"strings"

	errorsmod "cosmossdk.io/errors"

	sdk "github.com/cosmos/cosmos-sdk/types"

	transfertypes "github.com/cosmos/ibc-go/v11/modules/apps/transfer/types"
	clienttypes "github.com/cosmos/ibc-go/v11/modules/core/02-client/types"
	channeltypes "github.com/cosmos/ibc-go/v11/modules/core/04-channel/types"
	porttypes "github.com/cosmos/ibc-go/v11/modules/core/05-port/types"
	ibcexported "github.com/cosmos/ibc-go/v11/modules/core/exported"

	"github.com/muthu2201/vapor-chain/chain/constants"
	"github.com/muthu2201/vapor-chain/chain/x/council/types"
)

// PauseChecker is satisfied by the x/council keeper.
type PauseChecker interface {
	IsIBCPaused(ctx sdk.Context, denoms ...string) bool
}

var (
	_ porttypes.Middleware              = (*IBCMiddleware)(nil)
	_ porttypes.PacketUnmarshalerModule = (*IBCMiddleware)(nil)
)

type IBCMiddleware struct {
	app         porttypes.PacketUnmarshalerModule
	ics4Wrapper porttypes.ICS4Wrapper
	guard       PauseChecker
}

func NewIBCMiddleware(guard PauseChecker) *IBCMiddleware {
	if guard == nil {
		panic("ibcguard: nil pause checker")
	}
	return &IBCMiddleware{guard: guard}
}

// neverLeaves are base denoms that are chain-internal by design.
func neverLeaves(base string) bool {
	return base == constants.CreditDenom || base == constants.PowerDenom
}

func parse(bz []byte) (transfertypes.FungibleTokenPacketData, bool) {
	var data transfertypes.FungibleTokenPacketData
	if err := transfertypes.ModuleCdc.UnmarshalJSON(bz, &data); err != nil {
		return data, false
	}
	return data, true
}

// sendDenoms returns every spelling a guardian may have paused for an outbound
// transfer: base denom, full path and the local ibc/HASH.
func sendDenoms(data transfertypes.FungibleTokenPacketData) []string {
	d := transfertypes.ExtractDenomFromPath(data.Denom)
	return []string{d.Base, d.Path(), d.IBCDenom()}
}

// recvDenoms mirrors transfer's own logic to name the LOCAL denom that a
// received packet will credit.
func recvDenoms(packet channeltypes.Packet, data transfertypes.FungibleTokenPacketData) []string {
	d := transfertypes.ExtractDenomFromPath(data.Denom)
	out := []string{d.Base, d.Path()}
	prefix := fmt.Sprintf("%s/%s/", packet.GetSourcePort(), packet.GetSourceChannel())
	if strings.HasPrefix(data.Denom, prefix) {
		// token returning home: unwind one hop
		local := transfertypes.ExtractDenomFromPath(strings.TrimPrefix(data.Denom, prefix))
		out = append(out, local.IBCDenom())
	} else {
		local := transfertypes.ExtractDenomFromPath(fmt.Sprintf("%s/%s/%s", packet.GetDestPort(), packet.GetDestChannel(), data.Denom))
		out = append(out, local.IBCDenom())
	}
	return out
}

func (im *IBCMiddleware) OnChanOpenInit(ctx sdk.Context, order channeltypes.Order, hops []string, portID, channelID string, cp channeltypes.Counterparty, version string) (string, error) {
	return im.app.OnChanOpenInit(ctx, order, hops, portID, channelID, cp, version)
}

func (im *IBCMiddleware) OnChanOpenTry(ctx sdk.Context, order channeltypes.Order, hops []string, portID, channelID string, cp channeltypes.Counterparty, cpVersion string) (string, error) {
	return im.app.OnChanOpenTry(ctx, order, hops, portID, channelID, cp, cpVersion)
}

func (im *IBCMiddleware) OnChanOpenAck(ctx sdk.Context, portID, channelID, cpChannelID, cpVersion string) error {
	return im.app.OnChanOpenAck(ctx, portID, channelID, cpChannelID, cpVersion)
}

func (im *IBCMiddleware) OnChanOpenConfirm(ctx sdk.Context, portID, channelID string) error {
	return im.app.OnChanOpenConfirm(ctx, portID, channelID)
}

func (im *IBCMiddleware) OnChanCloseInit(ctx sdk.Context, portID, channelID string) error {
	return im.app.OnChanCloseInit(ctx, portID, channelID)
}

func (im *IBCMiddleware) OnChanCloseConfirm(ctx sdk.Context, portID, channelID string) error {
	return im.app.OnChanCloseConfirm(ctx, portID, channelID)
}

func (im *IBCMiddleware) OnRecvPacket(ctx sdk.Context, channelVersion string, packet channeltypes.Packet, relayer sdk.AccAddress) ibcexported.Acknowledgement {
	if data, ok := parse(packet.GetData()); ok {
		if im.guard.IsIBCPaused(ctx, recvDenoms(packet, data)...) {
			return channeltypes.NewErrorAcknowledgement(errorsmod.Wrapf(types.ErrPaused, "inbound %s", data.Denom))
		}
	}
	return im.app.OnRecvPacket(ctx, channelVersion, packet, relayer)
}

// Acks and timeouts are NEVER blocked: they refund senders.
func (im *IBCMiddleware) OnAcknowledgementPacket(ctx sdk.Context, channelVersion string, packet channeltypes.Packet, ack []byte, relayer sdk.AccAddress) error {
	return im.app.OnAcknowledgementPacket(ctx, channelVersion, packet, ack, relayer)
}

func (im *IBCMiddleware) OnTimeoutPacket(ctx sdk.Context, channelVersion string, packet channeltypes.Packet, relayer sdk.AccAddress) error {
	return im.app.OnTimeoutPacket(ctx, channelVersion, packet, relayer)
}

func (im *IBCMiddleware) SendPacket(ctx sdk.Context, sourcePort, sourceChannel string, timeoutHeight clienttypes.Height, timeoutTimestamp uint64, data []byte) (uint64, error) {
	if pd, ok := parse(data); ok {
		denoms := sendDenoms(pd)
		if neverLeaves(denoms[0]) && denoms[0] == denoms[2] {
			return 0, errorsmod.Wrapf(types.ErrPaused, "%s is chain-internal and can never be transferred over IBC", denoms[0])
		}
		if im.guard.IsIBCPaused(ctx, denoms...) {
			return 0, errorsmod.Wrapf(types.ErrPaused, "outbound %s", pd.Denom)
		}
	}
	return im.ics4Wrapper.SendPacket(ctx, sourcePort, sourceChannel, timeoutHeight, timeoutTimestamp, data)
}

func (im *IBCMiddleware) WriteAcknowledgement(ctx sdk.Context, packet ibcexported.PacketI, ack ibcexported.Acknowledgement) error {
	return im.ics4Wrapper.WriteAcknowledgement(ctx, packet, ack)
}

func (im *IBCMiddleware) GetAppVersion(ctx sdk.Context, portID, channelID string) (string, bool) {
	return im.ics4Wrapper.GetAppVersion(ctx, portID, channelID)
}

func (im *IBCMiddleware) UnmarshalPacketData(ctx sdk.Context, portID, channelID string, bz []byte) (any, string, error) {
	return im.app.UnmarshalPacketData(ctx, portID, channelID, bz)
}

func (im *IBCMiddleware) SetICS4Wrapper(wrapper porttypes.ICS4Wrapper) {
	if wrapper == nil {
		panic("ibcguard: nil ICS4Wrapper")
	}
	im.ics4Wrapper = wrapper
}

func (im *IBCMiddleware) SetUnderlyingApplication(app porttypes.IBCModule) {
	if im.app != nil {
		panic("ibcguard: underlying application already set")
	}
	pd, ok := app.(porttypes.PacketUnmarshalerModule)
	if !ok {
		panic(fmt.Errorf("ibcguard: underlying application must implement PacketUnmarshalerModule, got %T", app))
	}
	im.app = pd
}
