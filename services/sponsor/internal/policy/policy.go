// Copyright (c) 2026 VaporChain / muthu2201. All rights reserved.
// Proprietary and confidential. See LICENSE at the repository root.
// Provenance: VAPOR-6eabb1be532bdef4

// Package policy decides whether VaporChain sponsors a UserOperation.
//
// Every free resource has a quota. An op is sponsored only if ALL hold:
//   - the app exists and is ACTIVE on-chain (x/apps);
//   - every call targets a contract attributed to that app, or is
//     SETTLE.approveApp for that same app (so sponsored gas can only be spent
//     inside the app that earned it), with zero native value;
//   - account deployment only via an allowlisted factory;
//   - EIP-7702 senders delegate to an allowlisted implementation;
//   - gas and fee caps are respected;
//   - the app's earned epoch quota (x/apps) and the sender's daily cap
//     still have room (enforced atomically in the store).
package policy

import (
	"context"
	"encoding/binary"
	"errors"
	"fmt"
	"math/big"

	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/crypto"

	"github.com/muthu2201/vapor-chain/services/sponsor/internal/chain"
	"github.com/muthu2201/vapor-chain/services/sponsor/internal/userop"
)

type Chain interface {
	App(ctx context.Context, id uint64) (chain.App, error)
	AppOfContract(ctx context.Context, c common.Address) (uint64, error)
	Quota(ctx context.Context, appID uint64) (chain.Quota, error)
	Code(ctx context.Context, a common.Address) ([]byte, error)
}

type Config struct {
	ChainID           *big.Int
	EntryPoint        common.Address
	Settle            common.Address
	MaxGasPerOp       uint64
	MaxFeePerGas      *big.Int
	SenderDailyCap    int
	NewSenderDailyCap int
	AllowedDelegates  map[common.Address]bool
	AllowedFactories  map[common.Address]bool
}

type Decision struct {
	AppID     uint64
	Epoch     uint64
	Quota     uint64
	Gas       uint64
	SenderCap int
}

// PolicyError is a client-facing rejection (JSON-RPC error with reason).
type PolicyError struct{ Reason string }

func (e *PolicyError) Error() string { return e.Reason }

func deny(f string, a ...any) error { return &PolicyError{Reason: fmt.Sprintf(f, a...)} }

// approveAppSelector = keccak256("approveApp(uint64,address,uint256)")[:4] = 0xba3014bf
var approveAppSelector = crypto.Keccak256([]byte("approveApp(uint64,address,uint256)"))[:4]

type Engine struct {
	cfg   Config
	chain Chain
}

func New(cfg Config, c Chain) *Engine { return &Engine{cfg: cfg, chain: c} }

func (e *Engine) Evaluate(ctx context.Context, op *userop.UserOperation, entryPoint common.Address, chainID *big.Int, appID uint64) (Decision, error) {
	if err := op.Validate(); err != nil {
		return Decision{}, deny("invalid userOp: %v", err)
	}
	if entryPoint != e.cfg.EntryPoint {
		return Decision{}, deny("unsupported entryPoint %s", entryPoint)
	}
	if chainID == nil || chainID.Cmp(e.cfg.ChainID) != 0 {
		return Decision{}, deny("wrong chainId")
	}
	if appID == 0 {
		return Decision{}, deny("context.appId is required")
	}
	if op.MaxFeePerGas.ToInt().Cmp(e.cfg.MaxFeePerGas) > 0 {
		return Decision{}, deny("maxFeePerGas above sponsorship cap %s", e.cfg.MaxFeePerGas)
	}
	gas := op.TotalGas()
	if !gas.IsUint64() || gas.Uint64() > e.cfg.MaxGasPerOp {
		return Decision{}, deny("total gas %s above per-op cap %d", gas, e.cfg.MaxGasPerOp)
	}

	app, err := e.chain.App(ctx, appID)
	if errors.Is(err, chain.ErrNotFound) {
		return Decision{}, deny("app %d does not exist", appID)
	}
	if err != nil {
		return Decision{}, err
	}
	if app.Status != "APP_STATUS_ACTIVE" {
		return Decision{}, deny("app %d is %s", appID, app.Status)
	}

	// account deployment
	if f := op.Factory; f != nil && *f != (common.Address{}) {
		// 0x7702 marker = EntryPoint-managed 7702 init, not a factory
		if *f != common.HexToAddress("0x7702") && !e.cfg.AllowedFactories[*f] {
			return Decision{}, deny("factory %s is not allowlisted", f)
		}
	}

	// EIP-7702 delegation policy
	code, err := e.chain.Code(ctx, op.Sender)
	if err != nil {
		return Decision{}, err
	}
	if impl, ok := chain.Delegation(code); ok && !e.cfg.AllowedDelegates[impl] {
		return Decision{}, deny("sender delegates to non-allowlisted implementation %s", impl)
	}
	if len(op.EIP7702Auth) > 0 {
		var auth struct {
			Address common.Address `json:"address"`
		}
		if err := jsonUnmarshal(op.EIP7702Auth, &auth); err != nil || !e.cfg.AllowedDelegates[auth.Address] {
			return Decision{}, deny("eip7702Auth delegates to a non-allowlisted implementation")
		}
	}

	// call targets
	calls, err := userop.DecodeCalls(op.CallData)
	if err != nil {
		return Decision{}, deny("%v", err)
	}
	if len(calls) == 0 || len(calls) > 16 {
		return Decision{}, deny("between 1 and 16 calls may be sponsored")
	}
	for _, c := range calls {
		if c.Value != nil && c.Value.Sign() != 0 {
			return Decision{}, deny("sponsored calls cannot transfer native value")
		}
		if c.Target == e.cfg.Settle {
			if len(c.Data) < 4+32 || string(c.Data[:4]) != string(approveAppSelector) {
				return Decision{}, deny("only SETTLE.approveApp may be called directly in a sponsored op")
			}
			if binary.BigEndian.Uint64(c.Data[4+24:4+32]) != appID {
				return Decision{}, deny("SETTLE.approveApp must target the sponsoring app")
			}
			continue
		}
		owner, err := e.chain.AppOfContract(ctx, c.Target)
		if err != nil {
			return Decision{}, err
		}
		if owner != appID {
			return Decision{}, deny("call target %s is not a contract of app %d", c.Target, appID)
		}
	}

	q, err := e.chain.Quota(ctx, appID)
	if err != nil {
		return Decision{}, err
	}
	senderCap := e.cfg.SenderDailyCap
	if op.Nonce.ToInt().Sign() == 0 && len(code) == 0 {
		senderCap = e.cfg.NewSenderDailyCap
	}
	return Decision{AppID: appID, Epoch: q.Epoch, Quota: q.Gas, Gas: gas.Uint64(), SenderCap: senderCap}, nil
}
