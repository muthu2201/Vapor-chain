// Copyright (c) 2026 VaporChain / muthu2201. All rights reserved.
// Proprietary and confidential. See LICENSE at the repository root.
// Provenance: VAPOR-6eabb1be532bdef4

// Package userop models ERC-4337 v0.8 UserOperations in their JSON-RPC form
// and reproduces, byte for byte, the hash VaporVerifyingPaymaster.getHash()
// computes on-chain. If these ever diverge every signature fails closed.
package userop

import (
	"encoding/json"
	"errors"
	"fmt"
	"math/big"

	"github.com/ethereum/go-ethereum/accounts/abi"
	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/common/hexutil"
	"github.com/ethereum/go-ethereum/crypto"
)

// Provenance is the VaporChain fingerprint mixed into every sponsor hash.
var Provenance = common.HexToHash("0x6eabb1be532bdef432109abc178d88669ab33aed940f18cd5169887d215c1fcf")

// UserOperation is the ERC-7769 JSON form of a v0.8 UserOperation.
type UserOperation struct {
	Sender                        common.Address  `json:"sender"`
	Nonce                         *hexutil.Big    `json:"nonce"`
	Factory                       *common.Address `json:"factory,omitempty"`
	FactoryData                   hexutil.Bytes   `json:"factoryData,omitempty"`
	CallData                      hexutil.Bytes   `json:"callData"`
	CallGasLimit                  *hexutil.Big    `json:"callGasLimit"`
	VerificationGasLimit          *hexutil.Big    `json:"verificationGasLimit"`
	PreVerificationGas            *hexutil.Big    `json:"preVerificationGas"`
	MaxFeePerGas                  *hexutil.Big    `json:"maxFeePerGas"`
	MaxPriorityFeePerGas          *hexutil.Big    `json:"maxPriorityFeePerGas"`
	Paymaster                     *common.Address `json:"paymaster,omitempty"`
	PaymasterVerificationGasLimit *hexutil.Big    `json:"paymasterVerificationGasLimit,omitempty"`
	PaymasterPostOpGasLimit       *hexutil.Big    `json:"paymasterPostOpGasLimit,omitempty"`
	PaymasterData                 hexutil.Bytes   `json:"paymasterData,omitempty"`
	Signature                     hexutil.Bytes   `json:"signature"`
	EIP7702Auth                   json.RawMessage `json:"eip7702Auth,omitempty"`
}

func big0(b *hexutil.Big) *big.Int {
	if b == nil {
		return new(big.Int)
	}
	return b.ToInt()
}

// Validate rejects structurally broken ops before any policy work.
func (u *UserOperation) Validate() error {
	if u.Sender == (common.Address{}) {
		return errors.New("sender is required")
	}
	for name, v := range map[string]*hexutil.Big{
		"callGasLimit": u.CallGasLimit, "verificationGasLimit": u.VerificationGasLimit,
		"preVerificationGas": u.PreVerificationGas, "maxFeePerGas": u.MaxFeePerGas, "maxPriorityFeePerGas": u.MaxPriorityFeePerGas,
	} {
		if v == nil {
			return fmt.Errorf("%s is required", name)
		}
		if v.ToInt().Sign() < 0 || v.ToInt().BitLen() > 128 {
			return fmt.Errorf("%s out of range", name)
		}
	}
	if u.Nonce == nil || u.Nonce.ToInt().Sign() < 0 || u.Nonce.ToInt().BitLen() > 256 {
		return errors.New("nonce out of range")
	}
	if len(u.CallData) > 64*1024 {
		return errors.New("callData too large")
	}
	return nil
}

// InitCode returns factory || factoryData (what the bundler packs).
func (u *UserOperation) InitCode() []byte {
	if u.Factory == nil || *u.Factory == (common.Address{}) {
		return nil
	}
	return append(u.Factory.Bytes(), u.FactoryData...)
}

func pack128(hi, lo *big.Int) [32]byte {
	var out [32]byte
	hi.FillBytes(out[:16])
	lo.FillBytes(out[16:])
	return out
}

// AccountGasLimits = verificationGasLimit(16) || callGasLimit(16).
func (u *UserOperation) AccountGasLimits() [32]byte {
	return pack128(big0(u.VerificationGasLimit), big0(u.CallGasLimit))
}

// GasFees = maxPriorityFeePerGas(16) || maxFeePerGas(16).
func (u *UserOperation) GasFees() [32]byte {
	return pack128(big0(u.MaxPriorityFeePerGas), big0(u.MaxFeePerGas))
}

// PaymasterGasLimits = paymasterVerificationGasLimit(16) || postOpGasLimit(16).
func (u *UserOperation) PaymasterGasLimits() [32]byte {
	return pack128(big0(u.PaymasterVerificationGasLimit), big0(u.PaymasterPostOpGasLimit))
}

// TotalGas is the worst-case gas this op can consume (quota accounting unit).
func (u *UserOperation) TotalGas() *big.Int {
	t := new(big.Int)
	for _, v := range []*hexutil.Big{u.CallGasLimit, u.VerificationGasLimit, u.PreVerificationGas, u.PaymasterVerificationGasLimit, u.PaymasterPostOpGasLimit} {
		t.Add(t, big0(v))
	}
	return t
}

var hashArgs abi.Arguments

func init() {
	mk := func(t string) abi.Type {
		ty, err := abi.NewType(t, "", nil)
		if err != nil {
			panic(err)
		}
		return ty
	}
	for _, t := range []string{"address", "uint256", "bytes32", "bytes32", "bytes32", "uint256", "uint256", "bytes32", "uint256", "address", "uint48", "uint48", "uint64", "bytes32"} {
		hashArgs = append(hashArgs, abi.Argument{Type: mk(t)})
	}
}

// SponsorHash mirrors VaporVerifyingPaymaster.getHash(userOp, validUntil, validAfter, appId).
func SponsorHash(u *UserOperation, chainID *big.Int, paymaster common.Address, validUntil, validAfter uint64, appID uint64) (common.Hash, error) {
	agl := u.AccountGasLimits()
	pgl := u.PaymasterGasLimits()
	fees := u.GasFees()
	enc, err := hashArgs.Pack(
		u.Sender,
		big0(u.Nonce),
		crypto.Keccak256Hash(u.InitCode()),
		crypto.Keccak256Hash(u.CallData),
		agl,
		new(big.Int).SetBytes(pgl[:]),
		big0(u.PreVerificationGas),
		fees,
		chainID,
		paymaster,
		new(big.Int).SetUint64(validUntil),
		new(big.Int).SetUint64(validAfter),
		appID,
		[32]byte(Provenance),
	)
	if err != nil {
		return common.Hash{}, err
	}
	return crypto.Keccak256Hash(enc), nil
}

// PaymasterData encodes validUntil(6) | validAfter(6) | appId(8) | signature(65).
func PaymasterData(validUntil, validAfter, appID uint64, sig []byte) []byte {
	out := make([]byte, 0, 20+len(sig))
	var b8 [8]byte
	put := func(v uint64, n int) {
		for i := 0; i < 8; i++ {
			b8[7-i] = byte(v >> (8 * i))
		}
		out = append(out, b8[8-n:]...)
	}
	put(validUntil, 6)
	put(validAfter, 6)
	put(appID, 8)
	return append(out, sig...)
}
