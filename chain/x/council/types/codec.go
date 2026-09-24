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

// RegisterLegacyAminoCodec registers amino names so Ledger (SIGN_MODE_LEGACY_AMINO_JSON) signing works.
func RegisterLegacyAminoCodec(cdc *codec.LegacyAmino) {
	legacy.RegisterAminoMsg(cdc, &MsgAdmitValidator{}, "vapor/council/MsgAdmitValidator")
	legacy.RegisterAminoMsg(cdc, &MsgRemoveValidator{}, "vapor/council/MsgRemoveValidator")
	legacy.RegisterAminoMsg(cdc, &MsgSetPause{}, "vapor/council/MsgSetPause")
	legacy.RegisterAminoMsg(cdc, &MsgUpdateParams{}, "vapor/council/MsgUpdateParams")
	cdc.RegisterConcrete(&Params{}, "vapor/x/council/Params", nil)
}

// RegisterInterfaces registers the Msg implementations with the interface registry.
func RegisterInterfaces(registry codectypes.InterfaceRegistry) {
	registry.RegisterImplementations((*sdk.Msg)(nil),
		&MsgAdmitValidator{},
		&MsgRemoveValidator{},
		&MsgSetPause{},
		&MsgUpdateParams{},
	)
	msgservice.RegisterMsgServiceDesc(registry, &_Msg_serviceDesc)
}
