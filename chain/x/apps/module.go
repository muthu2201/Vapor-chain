// SPDX-License-Identifier: AGPL-3.0-only
// Copyright (c) 2026 VaporChain / muthu2201
// Provenance: VAPOR-6eabb1be532bdef4

package apps

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

	"github.com/muthu2201/vapor-chain/chain/x/apps/keeper"
	"github.com/muthu2201/vapor-chain/chain/x/apps/types"
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
	return am.keeper.EndBlock(sdk.UnwrapSDKContext(ctx))
}

// AutoCLIOptions exposes every query/tx on the CLI without hand-written cobra code.
func (AppModule) AutoCLIOptions() *autocliv1.ModuleOptions {
	return &autocliv1.ModuleOptions{
		Query: &autocliv1.ServiceCommandDescriptor{
			Service: "vaporchain.apps.v1.Query",
			RpcCommandOptions: []*autocliv1.RpcCommandOptions{
				{RpcMethod: "Params", Use: "params", Short: "Show x/apps params"},
				{RpcMethod: "App", Use: "app [app-id]", Short: "Show an app", PositionalArgs: []*autocliv1.PositionalArgDescriptor{{ProtoField: "app_id"}}},
				{RpcMethod: "Apps", Use: "apps", Short: "List apps"},
				{RpcMethod: "AppByContract", Use: "app-by-contract [0x-contract]", Short: "Resolve a contract's app", PositionalArgs: []*autocliv1.PositionalArgDescriptor{{ProtoField: "contract"}}},
				{RpcMethod: "Contracts", Use: "contracts [app-id]", Short: "List an app's contracts", PositionalArgs: []*autocliv1.PositionalArgDescriptor{{ProtoField: "app_id"}}},
				{RpcMethod: "Quota", Use: "quota [app-id]", Short: "Show an app's sponsorship quota", PositionalArgs: []*autocliv1.PositionalArgDescriptor{{ProtoField: "app_id"}}},
				{RpcMethod: "PendingMoves", Use: "pending-moves", Short: "List timelocked contract moves"},
				{RpcMethod: "PendingClaims", Use: "pending-claims [app-id]", Short: "List contract self-claims awaiting acceptance", PositionalArgs: []*autocliv1.PositionalArgDescriptor{{ProtoField: "app_id"}}},
				{RpcMethod: "CurrentEpoch", Use: "epoch", Short: "Show the current quota epoch"},
				{RpcMethod: "Bond", Use: "bond [app-id]", Short: "Show capital bonded behind an app, its base quota and pending unbondings", PositionalArgs: []*autocliv1.PositionalArgDescriptor{{ProtoField: "app_id"}}},
			},
		},
		Tx: &autocliv1.ServiceCommandDescriptor{
			Service: "vaporchain.apps.v1.Msg",
			RpcCommandOptions: []*autocliv1.RpcCommandOptions{
				{
					RpcMethod: "RegisterApp", Use: "register [revenue-recipient] [metadata-uri]", Short: "Register an app (burns the registration fee)",
					PositionalArgs: []*autocliv1.PositionalArgDescriptor{{ProtoField: "revenue_recipient"}, {ProtoField: "metadata_uri"}},
				},
				{
					RpcMethod: "AddContract", Use: "add-contract [app-id] [0x-contract]",
					Short: "Attribute a contract you deployed or own to your app",
					Long: "Proves ownership with --proof, one of:\n" +
						`  {"type":"PROOF_TYPE_DEPLOYER_CREATE","nonce":"7"}                         you deployed it with CREATE at that nonce` + "\n" +
						`  {"type":"PROOF_TYPE_DEPLOYER_CREATE2","salt":"0x..","init_code_hash":"0x.."}  you deployed it with CREATE2` + "\n" +
						`  {"type":"PROOF_TYPE_OWNABLE"}                                            owner() returns your address`,
					PositionalArgs: []*autocliv1.PositionalArgDescriptor{{ProtoField: "app_id"}, {ProtoField: "contract"}},
				},
				{
					RpcMethod: "UpdateApp", Use: "update-app [app-id]", Short: "Change an app's revenue recipient, metadata or referrer share",
					PositionalArgs: []*autocliv1.PositionalArgDescriptor{{ProtoField: "app_id"}},
				},
				{
					RpcMethod: "AcceptContractClaim", Use: "accept-claim [app-id] [0x-contract]", Short: "Accept a contract's self-registration",
					PositionalArgs: []*autocliv1.PositionalArgDescriptor{{ProtoField: "app_id"}, {ProtoField: "contract"}},
				},
				{
					RpcMethod: "RemoveContract", Use: "remove-contract [app-id] [0x-contract]", Short: "Detach a contract",
					PositionalArgs: []*autocliv1.PositionalArgDescriptor{{ProtoField: "app_id"}, {ProtoField: "contract"}},
				},
				{
					RpcMethod: "CancelContractMove", Use: "cancel-move [0x-contract]", Short: "Veto a pending contract move",
					PositionalArgs: []*autocliv1.PositionalArgDescriptor{{ProtoField: "contract"}},
				},
				{
					RpcMethod: "TransferOwnership", Use: "transfer-ownership [app-id] [new-owner]", Short: "Start a two-step ownership transfer",
					PositionalArgs: []*autocliv1.PositionalArgDescriptor{{ProtoField: "app_id"}, {ProtoField: "new_owner"}},
				},
				{
					RpcMethod: "AcceptOwnership", Use: "accept-ownership [app-id]", Short: "Accept an ownership transfer",
					PositionalArgs: []*autocliv1.PositionalArgDescriptor{{ProtoField: "app_id"}},
				},
				{
					RpcMethod: "BondApp", Use: "bond [app-id] [amount]", Short: "Lock capital behind your app for base sponsorship quota (e.g. 1000000000uusdc)",
					PositionalArgs: []*autocliv1.PositionalArgDescriptor{{ProtoField: "app_id"}, {ProtoField: "amount"}},
				},
				{
					RpcMethod: "UnbondApp", Use: "unbond [app-id] [amount]", Short: "Start returning bonded capital (released after unbonding_blocks)",
					PositionalArgs: []*autocliv1.PositionalArgDescriptor{{ProtoField: "app_id"}, {ProtoField: "amount"}},
				},
				{RpcMethod: "SetAppStatus", Skip: true},
				{RpcMethod: "UpdateParams", Skip: true},
			},
		},
	}
}
