// Copyright (c) 2026 VaporChain / muthu2201. All rights reserved.
// Provenance: VAPOR-6eabb1be532bdef4

package types

import (
	"github.com/cosmos/cosmos-sdk/codec"
	"github.com/cosmos/cosmos-sdk/codec/legacy"
	codectypes "github.com/cosmos/cosmos-sdk/codec/types"
	sdk "github.com/cosmos/cosmos-sdk/types"
	"github.com/cosmos/cosmos-sdk/types/msgservice"
)

func RegisterLegacyAminoCodec(cdc *codec.LegacyAmino) {
	legacy.RegisterAminoMsg(cdc, &MsgPay{}, "vapor/settle/MsgPay")
	legacy.RegisterAminoMsg(cdc, &MsgClaimRevenue{}, "vapor/settle/MsgClaimRevenue")
	legacy.RegisterAminoMsg(cdc, &MsgBuyCredits{}, "vapor/settle/MsgBuyCredits")
	legacy.RegisterAminoMsg(cdc, &MsgCloseTab{}, "vapor/settle/MsgCloseTab")
	legacy.RegisterAminoMsg(cdc, &MsgWithdrawTreasury{}, "vapor/settle/MsgWithdrawTreasury")
	legacy.RegisterAminoMsg(cdc, &MsgDisburseRelayerPool{}, "vapor/settle/MsgDisburseRelayerPool")
	legacy.RegisterAminoMsg(cdc, &MsgUpdateParams{}, "vapor/settle/MsgUpdateParams")
	cdc.RegisterConcrete(&Params{}, "vapor/x/settle/Params", nil)
}

func RegisterInterfaces(registry codectypes.InterfaceRegistry) {
	registry.RegisterImplementations((*sdk.Msg)(nil),
		&MsgPay{}, &MsgClaimRevenue{}, &MsgBuyCredits{}, &MsgCloseTab{},
		&MsgWithdrawTreasury{}, &MsgDisburseRelayerPool{}, &MsgUpdateParams{},
	)
	msgservice.RegisterMsgServiceDesc(registry, &_Msg_serviceDesc)
}
