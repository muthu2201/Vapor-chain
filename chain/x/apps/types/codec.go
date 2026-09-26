// SPDX-License-Identifier: AGPL-3.0-only
// Copyright (c) 2026 VaporChain / muthu2201
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
	legacy.RegisterAminoMsg(cdc, &MsgRegisterApp{}, "vapor/apps/MsgRegisterApp")
	legacy.RegisterAminoMsg(cdc, &MsgUpdateApp{}, "vapor/apps/MsgUpdateApp")
	legacy.RegisterAminoMsg(cdc, &MsgTransferOwnership{}, "vapor/apps/MsgTransferOwnership")
	legacy.RegisterAminoMsg(cdc, &MsgAcceptOwnership{}, "vapor/apps/MsgAcceptOwnership")
	legacy.RegisterAminoMsg(cdc, &MsgAddContract{}, "vapor/apps/MsgAddContract")
	legacy.RegisterAminoMsg(cdc, &MsgAcceptContractClaim{}, "vapor/apps/MsgAcceptContractClaim")
	legacy.RegisterAminoMsg(cdc, &MsgRemoveContract{}, "vapor/apps/MsgRemoveContract")
	legacy.RegisterAminoMsg(cdc, &MsgCancelContractMove{}, "vapor/apps/MsgCancelContractMove")
	legacy.RegisterAminoMsg(cdc, &MsgBondApp{}, "vapor/apps/MsgBondApp")
	legacy.RegisterAminoMsg(cdc, &MsgUnbondApp{}, "vapor/apps/MsgUnbondApp")
	legacy.RegisterAminoMsg(cdc, &MsgSetAppStatus{}, "vapor/apps/MsgSetAppStatus")
	legacy.RegisterAminoMsg(cdc, &MsgUpdateParams{}, "vapor/apps/MsgUpdateParams")
	cdc.RegisterConcrete(&Params{}, "vapor/x/apps/Params", nil)
}

func RegisterInterfaces(registry codectypes.InterfaceRegistry) {
	registry.RegisterImplementations((*sdk.Msg)(nil),
		&MsgRegisterApp{}, &MsgUpdateApp{}, &MsgTransferOwnership{}, &MsgAcceptOwnership{},
		&MsgAddContract{}, &MsgAcceptContractClaim{}, &MsgRemoveContract{}, &MsgCancelContractMove{},
		&MsgBondApp{}, &MsgUnbondApp{}, &MsgSetAppStatus{}, &MsgUpdateParams{},
	)
	msgservice.RegisterMsgServiceDesc(registry, &_Msg_serviceDesc)
}
