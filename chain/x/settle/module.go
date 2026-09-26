// SPDX-License-Identifier: AGPL-3.0-only
// Copyright (c) 2026 VaporChain / muthu2201
// Provenance: VAPOR-6eabb1be532bdef4

package settle

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

	"github.com/muthu2201/vapor-chain/chain/x/settle/keeper"
	"github.com/muthu2201/vapor-chain/chain/x/settle/types"
)

const ConsensusVersion = 1

var (
	_ module.AppModuleBasic      = AppModule{}
	_ module.HasGenesis          = AppModule{}
	_ module.HasServices         = AppModule{}
	_ module.HasConsensusVersion = AppModule{}
	_ appmodule.AppModule        = AppModule{}
	_ appmodule.HasEndBlocker    = AppModule{}
	_ appmodule.HasBeginBlocker  = AppModule{}
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

func (am AppModule) BeginBlock(ctx context.Context) error {
	return am.keeper.BeginBlock(sdk.UnwrapSDKContext(ctx))
}

func (am AppModule) EndBlock(ctx context.Context) error {
	return am.keeper.EndBlock(sdk.UnwrapSDKContext(ctx))
}

// AutoCLIOptions exposes every query/tx on the CLI without hand-written cobra code.
func (AppModule) AutoCLIOptions() *autocliv1.ModuleOptions {
	return &autocliv1.ModuleOptions{
		Query: &autocliv1.ServiceCommandDescriptor{
			Service: "vaporchain.settle.v1.Query",
			RpcCommandOptions: []*autocliv1.RpcCommandOptions{
				{RpcMethod: "Params", Use: "params", Short: "Show x/settle params"},
				{RpcMethod: "QuoteFee", Use: "quote-fee [denom] [amount]", Short: "Quote the fee for a payment", PositionalArgs: []*autocliv1.PositionalArgDescriptor{{ProtoField: "denom"}, {ProtoField: "amount"}}},
				{RpcMethod: "Claimable", Use: "claimable [app-id]", Short: "Revenue claimable by an app", PositionalArgs: []*autocliv1.PositionalArgDescriptor{{ProtoField: "app_id"}}},
				{RpcMethod: "Pools", Use: "pools", Short: "Protocol pool balances"},
				{RpcMethod: "Tab", Use: "tab [app-id] [payer] [payee] [denom]", Short: "Show an open micro-payment tab", PositionalArgs: []*autocliv1.PositionalArgDescriptor{{ProtoField: "app_id"}, {ProtoField: "payer"}, {ProtoField: "payee"}, {ProtoField: "denom"}}},
				{RpcMethod: "Allowance", Use: "allowance [owner] [app-id] [denom]", Short: "Show an app allowance", PositionalArgs: []*autocliv1.PositionalArgDescriptor{{ProtoField: "owner"}, {ProtoField: "app_id"}, {ProtoField: "denom"}}},
				{RpcMethod: "Totals", Use: "totals", Short: "Lifetime settlement totals"},
				{RpcMethod: "QuoteCredits", Use: "quote-credits [denom] [amount]", Short: "Quote gas credits for a payment", PositionalArgs: []*autocliv1.PositionalArgDescriptor{{ProtoField: "denom"}, {ProtoField: "amount"}}},
			},
		},
		Tx: &autocliv1.ServiceCommandDescriptor{
			Service: "vaporchain.settle.v1.Msg",
			RpcCommandOptions: []*autocliv1.RpcCommandOptions{
				{RpcMethod: "Pay", Use: "pay [payee] [amount]", Short: "Pay through Settle (no-app bucket)", PositionalArgs: []*autocliv1.PositionalArgDescriptor{{ProtoField: "payee"}, {ProtoField: "amount"}}},
				{RpcMethod: "ClaimRevenue", Use: "claim [app-id]", Short: "Pay an app's claimable revenue to its recipient", PositionalArgs: []*autocliv1.PositionalArgDescriptor{{ProtoField: "app_id"}}},
				{RpcMethod: "BuyCredits", Use: "buy-credits [payment]", Short: "Buy gas credits at the fixed price", PositionalArgs: []*autocliv1.PositionalArgDescriptor{{ProtoField: "payment"}}},
				{RpcMethod: "CloseTab", Use: "close-tab [app-id] [payer] [payee] [denom]", Short: "Settle an open tab", PositionalArgs: []*autocliv1.PositionalArgDescriptor{{ProtoField: "app_id"}, {ProtoField: "payer"}, {ProtoField: "payee"}, {ProtoField: "denom"}}},
				{RpcMethod: "WithdrawTreasury", Use: "withdraw-treasury [recipient] [amount]", Short: "Treasury admin: withdraw", PositionalArgs: []*autocliv1.PositionalArgDescriptor{{ProtoField: "recipient"}, {ProtoField: "amount", Varargs: true}}},
				{RpcMethod: "DisburseRelayerPool", Use: "disburse-relayer [recipient] [amount]", Short: "Relayer admin: pay a relayer/bundler", PositionalArgs: []*autocliv1.PositionalArgDescriptor{{ProtoField: "recipient"}, {ProtoField: "amount"}}},
				{RpcMethod: "UpdateParams", Skip: true},
			},
		},
	}
}
