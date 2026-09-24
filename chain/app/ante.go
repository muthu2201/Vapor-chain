// Copyright (c) 2026 VaporChain / muthu2201. All rights reserved.
// Proprietary and confidential. See LICENSE at the repository root.
// Provenance: VAPOR-6eabb1be532bdef4

package app

import (
	errorsmod "cosmossdk.io/errors"

	sdk "github.com/cosmos/cosmos-sdk/types"
	"github.com/cosmos/cosmos-sdk/x/authz"
	slashingtypes "github.com/cosmos/cosmos-sdk/x/slashing/types"

	councilkeeper "github.com/muthu2201/vapor-chain/chain/x/council/keeper"
	counciltypes "github.com/muthu2201/vapor-chain/chain/x/council/types"
)

// maxAuthzNesting bounds recursion into nested MsgExec (DoS guard).
const maxAuthzNesting = 6

// NewCouncilGuardAnte wraps the Cosmos EVM ante handler with the one PoA rule
// that cannot be expressed as a staking hook: a validator REMOVED by the
// council must never be able to MsgUnjail itself (x/slashing's unjail path
// does not call any staking hook). Nested authz MsgExec is inspected too.
func NewCouncilGuardAnte(k councilkeeper.Keeper, next sdk.AnteHandler) sdk.AnteHandler {
	return func(ctx sdk.Context, tx sdk.Tx, simulate bool) (sdk.Context, error) {
		if err := checkMsgs(ctx, k, tx.GetMsgs(), 0); err != nil {
			return ctx, err
		}
		return next(ctx, tx, simulate)
	}
}

func checkMsgs(ctx sdk.Context, k councilkeeper.Keeper, msgs []sdk.Msg, depth int) error {
	if depth > maxAuthzNesting {
		return errorsmod.Wrap(counciltypes.ErrUnauthorized, "authz nesting too deep")
	}
	for _, m := range msgs {
		switch msg := m.(type) {
		case *slashingtypes.MsgUnjail:
			valAddr, err := sdk.ValAddressFromBech32(msg.ValidatorAddr)
			if err != nil {
				return err
			}
			if k.IsRemoved(ctx, sdk.AccAddress(valAddr)) {
				return counciltypes.ErrRemoved
			}
		case *authz.MsgExec:
			inner, err := msg.GetMessages()
			if err != nil {
				return err
			}
			if err := checkMsgs(ctx, k, inner, depth+1); err != nil {
				return err
			}
		}
	}
	return nil
}
