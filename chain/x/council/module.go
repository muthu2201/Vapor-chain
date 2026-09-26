// SPDX-License-Identifier: AGPL-3.0-only
// Copyright (c) 2026 VaporChain / muthu2201
// Provenance: VAPOR-6eabb1be532bdef4

package council

import (
	"context"
	"encoding/json"
	"fmt"

	"github.com/grpc-ecosystem/grpc-gateway/runtime"

	autocliv1 "cosmossdk.io/api/cosmos/autocli/v1"
	"cosmossdk.io/core/appmodule"

	"github.com/cosmos/cosmos-sdk/client"
	"github.com/cosmos/cosmos-sdk/codec"
	codectypes "github.com/cosmos/cosmos-sdk/codec/types"
	sdk "github.com/cosmos/cosmos-sdk/types"
	"github.com/cosmos/cosmos-sdk/types/module"

	"github.com/muthu2201/vapor-chain/chain/x/council/keeper"
	"github.com/muthu2201/vapor-chain/chain/x/council/types"
)

const ConsensusVersion = 1

var (
	_ module.AppModuleBasic      = AppModule{}
	_ module.HasGenesis          = AppModule{}
	_ module.HasServices         = AppModule{}
	_ module.HasConsensusVersion = AppModule{}
	_ appmodule.AppModule        = AppModule{}
	_ appmodule.HasEndBlocker    = AppModule{}
)

type AppModule struct {
	cdc    codec.Codec
	keeper keeper.Keeper
}

func NewAppModule(cdc codec.Codec, k keeper.Keeper) AppModule {
	return AppModule{cdc: cdc, keeper: k}
}

func (AppModule) Name() string                                      { return types.ModuleName }
func (AppModule) IsAppModule()                                      {}
func (AppModule) IsOnePerModuleType()                               {}
func (AppModule) ConsensusVersion() uint64                          { return ConsensusVersion }
func (AppModule) RegisterLegacyAminoCodec(c *codec.LegacyAmino)     { types.RegisterLegacyAminoCodec(c) }
func (AppModule) RegisterInterfaces(r codectypes.InterfaceRegistry) { types.RegisterInterfaces(r) }

func (AppModule) RegisterGRPCGatewayRoutes(c client.Context, mux *runtime.ServeMux) {
	if err := types.RegisterQueryHandlerClient(context.Background(), mux, types.NewQueryClient(c)); err != nil {
		panic(err)
	}
}

func (am AppModule) RegisterServices(cfg module.Configurator) {
	types.RegisterMsgServer(cfg.MsgServer(), keeper.NewMsgServerImpl(am.keeper))
	types.RegisterQueryServer(cfg.QueryServer(), keeper.NewQueryServerImpl(am.keeper))
}

func (AppModule) DefaultGenesis(cdc codec.JSONCodec) json.RawMessage {
	return cdc.MustMarshalJSON(types.DefaultGenesisState())
}

func (AppModule) ValidateGenesis(cdc codec.JSONCodec, _ client.TxEncodingConfig, bz json.RawMessage) error {
	var gs types.GenesisState
	if err := cdc.UnmarshalJSON(bz, &gs); err != nil {
		return fmt.Errorf("failed to unmarshal %s genesis: %w", types.ModuleName, err)
	}
	return gs.Validate()
}

func (am AppModule) InitGenesis(ctx sdk.Context, cdc codec.JSONCodec, bz json.RawMessage) {
	var gs types.GenesisState
	cdc.MustUnmarshalJSON(bz, &gs)
	if err := am.keeper.InitGenesis(ctx, gs); err != nil {
		panic(err)
	}
}

func (am AppModule) ExportGenesis(ctx sdk.Context, cdc codec.JSONCodec) json.RawMessage {
	gs, err := am.keeper.ExportGenesis(ctx)
	if err != nil {
		panic(err)
	}
	return cdc.MustMarshalJSON(gs)
}

func (am AppModule) EndBlock(ctx context.Context) error {
	return am.keeper.PruneExpiredPauses(sdk.UnwrapSDKContext(ctx))
}

// AutoCLIOptions exposes every query/tx on the CLI without hand-written cobra code.
func (AppModule) AutoCLIOptions() *autocliv1.ModuleOptions {
	return &autocliv1.ModuleOptions{
		Query: &autocliv1.ServiceCommandDescriptor{
			Service: "vaporchain.council.v1.Query",
			RpcCommandOptions: []*autocliv1.RpcCommandOptions{
				{RpcMethod: "Params", Use: "params", Short: "Show council params"},
				{RpcMethod: "Admissions", Use: "admissions", Short: "List admitted validator operators"},
				{RpcMethod: "Pauses", Use: "pauses", Short: "List active emergency pauses"},
				{RpcMethod: "Provenance", Use: "provenance", Short: "Show the on-chain authorship record"},
			},
		},
		Tx: &autocliv1.ServiceCommandDescriptor{
			Service: "vaporchain.council.v1.Msg",
			RpcCommandOptions: []*autocliv1.RpcCommandOptions{
				{
					RpcMethod: "SetPause", Use: "set-pause [target] [paused] [reason]",
					Short:          "Pause (guardian/gov) or unpause (gov) a target: settle | settle:<denom> | ibc | ibc:<denom>",
					PositionalArgs: []*autocliv1.PositionalArgDescriptor{{ProtoField: "target"}, {ProtoField: "paused"}, {ProtoField: "reason"}},
					FlagOptions:    map[string]*autocliv1.FlagOptions{"signer": {Name: "signer"}},
				},
				{RpcMethod: "AdmitValidator", Skip: true},
				{RpcMethod: "RemoveValidator", Skip: true},
				{RpcMethod: "UpdateParams", Skip: true},
			},
		},
	}
}
