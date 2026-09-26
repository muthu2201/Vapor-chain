// SPDX-License-Identifier: AGPL-3.0-only
// Copyright (c) 2026 VaporChain / muthu2201
// Provenance: VAPOR-6eabb1be532bdef4

// Package userop models ERC-4337 v0.8 UserOperations in their JSON-RPC form
// and reproduces, byte for byte, the hash VaporVerifyingPaymaster.getHash()
// computes on-chain. If these ever diverge every signature fails closed.
package userop

import (
	"bytes"
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
	Factory                       Factory         `json:"factory,omitempty"`
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

// UnmarshalJSON drops explicit nulls first: bundlers and wallets send
// `"paymaster": null` style fields, which the hex types would reject.
func (u *UserOperation) UnmarshalJSON(b []byte) error {
	var fields map[string]json.RawMessage
	if err := json.Unmarshal(b, &fields); err != nil {
		return err
	}
	for k, v := range fields {
		if string(bytes.TrimSpace(v)) == "null" {
			delete(fields, k)
		}
	}
	clean, err := json.Marshal(fields)
	if err != nil {
		return err
	}
	type plain UserOperation // no methods: avoids recursing into this func
	return json.Unmarshal(clean, (*plain)(u))
}

var (
	eip7702Short = []byte{0x77, 0x02}
	eip7702Long  = append([]byte{0x77, 0x02}, make([]byte, 18)...)
)

// Factory is the ERC-7769 "factory" field: a 20-byte factory address, or the
// EntryPoint v0.8 EIP-7702 marker, which clients send as the literal "0x7702"
// (or left-aligned and zero-padded to 20 bytes).
type Factory []byte

func (f *Factory) UnmarshalJSON(b []byte) error {
	var h hexutil.Bytes
	if err := json.Unmarshal(b, &h); err != nil {
		return err
	}
	if len(h) != 0 && len(h) != common.AddressLength && !bytes.Equal(h, eip7702Short) {
		return errors.New("factory must be a 20-byte address or 0x7702")
	}
	*f = Factory(h)
	return nil
}

func (f Factory) MarshalJSON() ([]byte, error) { return json.Marshal(hexutil.Bytes(f)) }

// IsEIP7702 reports whether this is the EIP-7702 marker rather than a factory.
func (f Factory) IsEIP7702() bool {
	return bytes.Equal(f, eip7702Short) || bytes.Equal(f, eip7702Long)
}

// Address returns the real factory contract, if there is one.
func (f Factory) Address() (common.Address, bool) {
	if len(f) != common.AddressLength || f.IsEIP7702() {
		return common.Address{}, false
	}
	a := common.BytesToAddress(f)
	return a, a != (common.Address{})
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

// InitCode returns the initCode bytes exactly as the bundler packs them into
// handleOps, because VaporVerifyingPaymaster.getHash hashes those raw bytes.
// Alto (and viem) keep the short "0x7702" marker as two bytes unless
// factoryData follows, in which case it is padded to 20 bytes so the
// EntryPoint's marker check still sees it.
func (u *UserOperation) InitCode() []byte {
	switch {
	case len(u.Factory) == 0:
		return nil
	case bytes.Equal(u.Factory, eip7702Short) && len(u.FactoryData) > 0:
		return append(bytes.Clone(eip7702Long), u.FactoryData...)
	case !u.Factory.IsEIP7702() && common.BytesToAddress(u.Factory) == (common.Address{}):
		return nil
	}
	return append(bytes.Clone(u.Factory), u.FactoryData...)
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
