// Copyright (c) 2026 VaporChain / muthu2201. All rights reserved.
// Provenance: VAPOR-6eabb1be532bdef4

package userop

import (
	"errors"
	"math/big"
	"strings"

	"github.com/ethereum/go-ethereum/accounts/abi"
	"github.com/ethereum/go-ethereum/common"
)

// Call is one call an account will execute.
type Call struct {
	Target common.Address
	Value  *big.Int
	Data   []byte
}

// accountABI covers BaseAccount (eth-infinitism v0.8) which SimpleAccount and
// Simple7702Account both use.
var accountABI, _ = abi.JSON(strings.NewReader(`[
 {"type":"function","name":"execute","inputs":[{"name":"target","type":"address"},{"name":"value","type":"uint256"},{"name":"data","type":"bytes"}]},
 {"type":"function","name":"executeBatch","inputs":[{"name":"calls","type":"tuple[]","components":[{"name":"target","type":"address"},{"name":"value","type":"uint256"},{"name":"data","type":"bytes"}]}]}
]`))

// ErrUnknownCallData is returned for account calldata we cannot decode;
// the sponsor fails closed on it.
var ErrUnknownCallData = errors.New("unsupported account callData (only execute/executeBatch are sponsored)")

// DecodeCalls extracts the calls from BaseAccount.execute / executeBatch.
func DecodeCalls(callData []byte) ([]Call, error) {
	if len(callData) < 4 {
		return nil, ErrUnknownCallData
	}
	m, err := accountABI.MethodById(callData[:4])
	if err != nil {
		return nil, ErrUnknownCallData
	}
	args, err := m.Inputs.Unpack(callData[4:])
	if err != nil {
		return nil, ErrUnknownCallData
	}
	switch m.Name {
	case "execute":
		return []Call{{Target: args[0].(common.Address), Value: args[1].(*big.Int), Data: args[2].([]byte)}}, nil
	case "executeBatch":
		raw := args[0].([]struct {
			Target common.Address `json:"target"`
			Value  *big.Int       `json:"value"`
			Data   []byte         `json:"data"`
		})
		out := make([]Call, 0, len(raw))
		for _, c := range raw {
			out = append(out, Call{Target: c.Target, Value: c.Value, Data: c.Data})
		}
		return out, nil
	}
	return nil, ErrUnknownCallData
}
