// SPDX-License-Identifier: AGPL-3.0-only
// Copyright (c) 2026 VaporChain / muthu2201
// Provenance: VAPOR-6eabb1be532bdef4

package userop

import (
	"bytes"
	"encoding/json"
	"math/big"
	"testing"

	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/common/hexutil"
)

// Vectors produced by calling VaporVerifyingPaymaster.getHash on the live
// localnet deployment (chain 779700, paymaster 0xF327…01A1) with `cast call`.
// If Go and Solidity ever disagree, every sponsorship fails closed; this test
// catches that before it reaches a chain.
func TestSponsorHashMatchesSolidity(t *testing.T) {
	paymaster := common.HexToAddress("0xF32728b0f59B3Fb1f78bC1045755729aae7e01A1")
	cases := []struct {
		name, factory, factoryData string
		want                       string
	}{
		{"no initCode", "", "", "0x3f813d436f213e9472a45649a914e05705c832cae4d725e8b04cecaadc3cac56"},
		{"7702 short marker", "0x7702", "0x", "0xa9f166db224b052c71bbaa06bad35e6ea86a3a0996530f47bdaae684385ee3e5"},
		{"7702 marker + data is padded", "0x7702", "0xc0ffee", "0xafb263e556a649beaa112851f5808432126b94c466826a5f27d08d17223fd159"},
		{"7702 long marker + data", "0x7702000000000000000000000000000000000000", "0xc0ffee", "0xafb263e556a649beaa112851f5808432126b94c466826a5f27d08d17223fd159"},
		{"real factory", "0x4e59b44847b379578588920ca78fbf26c0b4956c", "0xabcdef", "0x860b2d46618bad2b177ca58dd898277f23fcc61141bdf318969311f9cfabc7b4"},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			raw := map[string]any{
				"sender":               "0x93385970124281998783D037704AAA53a559BBfd",
				"nonce":                "0x1a0d3eae3a00000000000000000",
				"callData":             "0xdeadbeef",
				"callGasLimit":         "0x20000",
				"verificationGasLimit": "0x30000",
				"preVerificationGas":   "0xc350",
				"maxFeePerGas":         "0x9df3ca80",
				"maxPriorityFeePerGas": "0xee6b280",
				"signature":            "0x",
				"paymaster":            nil, // wallets send explicit nulls
			}
			if c.factory != "" {
				raw["factory"] = c.factory
				raw["factoryData"] = c.factoryData
			}
			b, _ := json.Marshal(raw)
			var op UserOperation
			if err := json.Unmarshal(b, &op); err != nil {
				t.Fatal(err)
			}
			op.PaymasterVerificationGasLimit = (*hexutil.Big)(big.NewInt(80_000))
			op.PaymasterPostOpGasLimit = (*hexutil.Big)(big.NewInt(25_000))
			if err := op.Validate(); err != nil {
				t.Fatal(err)
			}
			got, err := SponsorHash(&op, big.NewInt(779700), paymaster, 1_800_000_000, 0, 6)
			if err != nil {
				t.Fatal(err)
			}
			if got.Hex() != c.want {
				t.Fatalf("hash %s, solidity %s (initCode %x)", got.Hex(), c.want, op.InitCode())
			}
		})
	}
}

func TestFactoryJSON(t *testing.T) {
	for _, ok := range []string{`"0x7702"`, `"0x7702000000000000000000000000000000000000"`, `"0x4e59b44847b379578588920ca78fbf26c0b4956c"`, `"0x"`} {
		var f Factory
		if err := json.Unmarshal([]byte(ok), &f); err != nil {
			t.Errorf("%s rejected: %v", ok, err)
		}
	}
	for _, bad := range []string{`"0x1234"`, `"0x7703"`, `"0x4e59b44847b379578588920ca78fbf26c0b4956c00"`, `12`} {
		var f Factory
		if err := json.Unmarshal([]byte(bad), &f); err == nil {
			t.Errorf("%s accepted", bad)
		}
	}
	var marker Factory
	_ = json.Unmarshal([]byte(`"0x7702"`), &marker)
	if !marker.IsEIP7702() {
		t.Fatal("0x7702 not recognised as marker")
	}
	if _, ok := marker.Address(); ok {
		t.Fatal("marker must not be treated as a factory contract")
	}
	// the right-aligned form is a real (precompile-range) address, not the marker
	right := Factory(common.HexToAddress("0x7702").Bytes())
	if right.IsEIP7702() {
		t.Fatal("0x000…7702 is not the EntryPoint marker")
	}
}

func TestInitCodeDoesNotAliasInput(t *testing.T) {
	op := UserOperation{Factory: Factory(common.HexToAddress("0x01").Bytes()), FactoryData: []byte{1, 2}}
	ic := op.InitCode()
	ic[0] = 0xff
	if op.Factory[0] == 0xff {
		t.Fatal("InitCode mutated the op's factory")
	}
}

func TestPaymasterDataLayout(t *testing.T) {
	sig := bytes.Repeat([]byte{0xab}, 65)
	d := PaymasterData(0x010203040506, 0x0a0b0c0d0e0f, 0x1122334455667788, sig)
	want := append(common.FromHex("0x010203040506"+"0a0b0c0d0e0f"+"1122334455667788"), sig...)
	if !bytes.Equal(d, want) {
		t.Fatalf("got %x want %x", d, want)
	}
}

func TestValidateRejectsOutOfRange(t *testing.T) {
	big129 := (*hexutil.Big)(new(big.Int).Lsh(big.NewInt(1), 128))
	one := (*hexutil.Big)(big.NewInt(1))
	op := UserOperation{
		Sender: common.HexToAddress("0x01"), Nonce: one, CallGasLimit: big129,
		VerificationGasLimit: one, PreVerificationGas: one, MaxFeePerGas: one, MaxPriorityFeePerGas: one,
	}
	if op.Validate() == nil {
		t.Fatal("129-bit gas limit accepted")
	}
	op.CallGasLimit = one
	op.CallData = make([]byte, 64*1024+1)
	if op.Validate() == nil {
		t.Fatal("oversized callData accepted")
	}
}

func TestDecodeCalls(t *testing.T) {
	target := common.HexToAddress("0xb235A1c5b84eaEe836DC48DFD50325190371303F")
	exec, err := accountABI.Pack("execute", target, big.NewInt(7), []byte{0xde, 0xad})
	if err != nil {
		t.Fatal(err)
	}
	calls, err := DecodeCalls(exec)
	if err != nil || len(calls) != 1 || calls[0].Target != target || calls[0].Value.Int64() != 7 {
		t.Fatalf("execute: %v %+v", err, calls)
	}

	type call struct {
		Target common.Address
		Value  *big.Int
		Data   []byte
	}
	batch, err := accountABI.Pack("executeBatch", []call{{target, big.NewInt(0), []byte{1}}, {common.HexToAddress("0x02"), big.NewInt(3), nil}})
	if err != nil {
		t.Fatal(err)
	}
	calls, err = DecodeCalls(batch)
	if err != nil || len(calls) != 2 || calls[1].Target != common.HexToAddress("0x02") || calls[1].Value.Int64() != 3 {
		t.Fatalf("executeBatch: %v %+v", err, calls)
	}

	for _, bad := range [][]byte{nil, {1, 2, 3}, {0xde, 0xad, 0xbe, 0xef, 0}, exec[:40]} {
		if _, err := DecodeCalls(bad); err == nil {
			t.Errorf("accepted %x", bad)
		}
	}
}

// FuzzDecodeCalls: arbitrary calldata must never panic the sponsor.
func FuzzDecodeCalls(f *testing.F) {
	exec, _ := accountABI.Pack("execute", common.Address{}, big.NewInt(0), []byte{})
	f.Add(exec)
	f.Add([]byte{0x34, 0xfc, 0xd5, 0xbe})
	f.Fuzz(func(t *testing.T, data []byte) {
		_, _ = DecodeCalls(data)
	})
}
