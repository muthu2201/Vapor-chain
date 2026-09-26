// SPDX-License-Identifier: AGPL-3.0-only
// Copyright (c) 2026 VaporChain / muthu2201
// Provenance: VAPOR-6eabb1be532bdef4

// Package settle implements the Settle precompile at 0x…0900.
//
// WHY A PRECOMPILE (not a Solidity fee router): it is cheaper, it cannot be
// upgraded by an EOA admin (only a chain upgrade changes it), it never executes
// third-party ERC-20 code (so there is no reentrancy surface), and it shares
// state atomically with x/settle and x/apps. Every state change runs inside the
// EVM's journaled cache context: if the calling transaction reverts, so does
// the settlement.
//
// Security notes (see docs/security/red-team.md for the attack matrix):
//   - DELEGATECALL / CALLCODE into a precompile are executed READ-ONLY by the
//     patched geth fork with caller = the delegating contract, so a malicious
//     contract can never impersonate a user against Settle.
//   - Native value sent to Settle is rejected (it would be stranded).
//   - Amounts wider than 255 bits are rejected before touching math.Int.
package settle

import (
	"bytes"
	_ "embed"
	"fmt"

	"github.com/ethereum/go-ethereum/accounts/abi"
	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/core/vm"

	cmn "github.com/cosmos/evm/precompiles/common"

	storetypes "github.com/cosmos/cosmos-sdk/store/v2/types"
	sdk "github.com/cosmos/cosmos-sdk/types"

	"github.com/muthu2201/vapor-chain/chain/constants"
)

var _ vm.PrecompiledContract = &Precompile{}

var (
	//go:embed abi.json
	abiJSON []byte
	// ABI of ISettle.sol.
	ABI abi.ABI
)

func init() {
	var err error
	ABI, err = abi.JSON(bytes.NewReader(abiJSON))
	if err != nil {
		panic(fmt.Errorf("settle precompile: bad ABI: %w", err))
	}
}

// Method names.
const (
	MethodPay                 = "pay"
	MethodPayFrom             = "payFrom"
	MethodTabPay              = "tabPay"
	MethodCloseTab            = "closeTab"
	MethodApproveApp          = "approveApp"
	MethodAppAllowance        = "appAllowance"
	MethodRegisterApp         = "registerApp"
	MethodClaimRegistration   = "claimRegistration"
	MethodAcceptContractClaim = "acceptContractClaim"
	MethodBondApp             = "bondApp"
	MethodUnbondApp           = "unbondApp"
	MethodAppBond             = "appBond"
	MethodAppOf               = "appOf"
	MethodClaim               = "claim"
	MethodClaimable           = "claimable"
	MethodBuyCredits          = "buyCredits"
	MethodQuoteCredits        = "quoteCredits"
	MethodQuoteFee            = "quoteFee"
	MethodDenomOf             = "denomOf"
	MethodProvenance          = "provenance"
)

// Precompile is the Settle precompiled contract.
type Precompile struct {
	cmn.Precompile
	abi.ABI
	settle SettleKeeper
	apps   AppsKeeper
	erc20  Erc20Keeper
}

// NewPrecompile wires the precompile to the native keepers.
func NewPrecompile(settleK SettleKeeper, appsK AppsKeeper, erc20K Erc20Keeper, bankK cmn.BankKeeper) *Precompile {
	return &Precompile{
		Precompile: cmn.Precompile{
			KvGasConfig:           storetypes.KVGasConfig(),
			TransientKVGasConfig:  storetypes.TransientGasConfig(),
			ContractAddress:       common.HexToAddress(constants.SettlePrecompileAddress),
			BalanceHandlerFactory: cmn.NewBalanceHandlerFactory(bankK),
		},
		ABI:    ABI,
		settle: settleK,
		apps:   appsK,
		erc20:  erc20K,
	}
}

func (Precompile) Name() string { return "settle" }

// RequiredGas is the flat entry cost; the actual KV gas used by the native
// action is charged on top inside RunNativeAction.
func (p Precompile) RequiredGas(input []byte) uint64 {
	if len(input) < 4 {
		return 0
	}
	method, err := p.MethodById(input[:4])
	if err != nil {
		return 0
	}
	return p.Precompile.RequiredGas(input, p.IsTransaction(method))
}

func (p Precompile) Run(evm *vm.EVM, contract *vm.Contract, readonly bool) ([]byte, error) {
	if contract.Value() != nil && contract.Value().Sign() > 0 {
		return nil, vm.ErrExecutionReverted
	}
	return p.RunNativeAction(evm, contract, func(ctx sdk.Context) ([]byte, error) {
		return p.Execute(ctx, evm, contract, readonly)
	})
}

// IsTransaction reports state-changing methods.
func (Precompile) IsTransaction(method *abi.Method) bool {
	switch method.Name {
	case MethodPay, MethodPayFrom, MethodTabPay, MethodCloseTab, MethodApproveApp,
		MethodRegisterApp, MethodClaimRegistration, MethodAcceptContractClaim, MethodBondApp, MethodUnbondApp,
		MethodClaim, MethodBuyCredits:
		return true
	default:
		return false
	}
}

func (p Precompile) Execute(ctx sdk.Context, evm *vm.EVM, contract *vm.Contract, readOnly bool) ([]byte, error) {
	method, args, err := cmn.SetupABI(p.ABI, contract, readOnly, p.IsTransaction)
	if err != nil {
		return nil, err
	}
	caller := contract.Caller()
	stateDB := evm.StateDB
	switch method.Name {
	case MethodPay:
		return p.pay(ctx, stateDB, caller, method, args)
	case MethodPayFrom:
		return p.payFrom(ctx, stateDB, caller, method, args)
	case MethodTabPay:
		return p.tabPay(ctx, stateDB, caller, method, args)
	case MethodCloseTab:
		return p.closeTab(ctx, stateDB, caller, method, args)
	case MethodApproveApp:
		return p.approveApp(ctx, stateDB, caller, method, args)
	case MethodAppAllowance:
		return p.appAllowance(ctx, method, args)
	case MethodRegisterApp:
		return p.registerApp(ctx, stateDB, caller, method, args)
	case MethodClaimRegistration:
		return p.claimRegistration(ctx, stateDB, caller, method, args)
	case MethodAcceptContractClaim:
		return p.acceptContractClaim(ctx, caller, method, args)
	case MethodBondApp:
		return p.bondApp(ctx, caller, method, args)
	case MethodUnbondApp:
		return p.unbondApp(ctx, caller, method, args)
	case MethodAppBond:
		return p.appBond(ctx, method, args)
	case MethodAppOf:
		return p.appOf(ctx, method, args)
	case MethodClaim:
		return p.claim(ctx, stateDB, method, args)
	case MethodClaimable:
		return p.claimable(ctx, method, args)
	case MethodBuyCredits:
		return p.buyCredits(ctx, stateDB, caller, method, args)
	case MethodQuoteCredits:
		return p.quoteCredits(ctx, method, args)
	case MethodQuoteFee:
		return p.quoteFee(ctx, method, args)
	case MethodDenomOf:
		return p.denomOf(ctx, method, args)
	case MethodProvenance:
		return p.provenance(method)
	default:
		return nil, fmt.Errorf(cmn.ErrUnknownMethod, method.Name)
	}
}
