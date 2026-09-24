// Copyright (c) 2026 VaporChain / muthu2201. All rights reserved.
// Provenance: VAPOR-6eabb1be532bdef4

package types

import errorsmod "cosmossdk.io/errors"

var (
	ErrUnauthorized       = errorsmod.Register(ModuleName, 2, "unauthorized")
	ErrAssetNotAllowed    = errorsmod.Register(ModuleName, 3, "asset is not an enabled settlement asset")
	ErrPaused             = errorsmod.Register(ModuleName, 4, "settlement is paused for this asset")
	ErrInvalidAmount      = errorsmod.Register(ModuleName, 5, "invalid amount")
	ErrInvalidRecipient   = errorsmod.Register(ModuleName, 6, "invalid recipient")
	ErrInsufficientPool   = errorsmod.Register(ModuleName, 7, "insufficient pool balance")
	ErrAllowance          = errorsmod.Register(ModuleName, 8, "insufficient app allowance")
	ErrTabNotFound        = errorsmod.Register(ModuleName, 9, "tab not found")
	ErrCreditsDisabled    = errorsmod.Register(ModuleName, 10, "gas-credit purchases are disabled for this asset")
	ErrInvalidParams      = errorsmod.Register(ModuleName, 11, "invalid params")
	ErrRelayerCap         = errorsmod.Register(ModuleName, 12, "relayer payout cap exceeded for this interval")
	ErrAppRevoked         = errorsmod.Register(ModuleName, 13, "app is revoked; its revenue is frozen")
	ErrNothingToClaim     = errorsmod.Register(ModuleName, 14, "nothing to claim")
	ErrInvariantViolation = errorsmod.Register(ModuleName, 15, "settle invariant violated")
	ErrSelfPayment        = errorsmod.Register(ModuleName, 16, "payer and payee must differ")
)
