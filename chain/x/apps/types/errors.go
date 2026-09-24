// Copyright (c) 2026 VaporChain / muthu2201. All rights reserved.
// Provenance: VAPOR-6eabb1be532bdef4

package types

import errorsmod "cosmossdk.io/errors"

var (
	ErrUnauthorized     = errorsmod.Register(ModuleName, 2, "unauthorized")
	ErrAppNotFound      = errorsmod.Register(ModuleName, 3, "app not found")
	ErrInvalidProof     = errorsmod.Register(ModuleName, 4, "contract ownership proof is invalid")
	ErrAlreadyBound     = errorsmod.Register(ModuleName, 5, "contract is already attributed to this app")
	ErrTooManyContracts = errorsmod.Register(ModuleName, 6, "app reached max_contracts_per_app")
	ErrNotBound         = errorsmod.Register(ModuleName, 7, "contract is not attributed to this app")
	ErrNoPendingMove    = errorsmod.Register(ModuleName, 8, "no pending move for contract")
	ErrNoPendingClaim   = errorsmod.Register(ModuleName, 9, "contract has not claimed registration into this app")
	ErrInvalidParams    = errorsmod.Register(ModuleName, 10, "invalid params")
	ErrInvalidField     = errorsmod.Register(ModuleName, 11, "invalid field")
	ErrAppInactive      = errorsmod.Register(ModuleName, 12, "app is not active")
	ErrNotAttestor      = errorsmod.Register(ModuleName, 13, "signer is not a domain attestor")
	ErrDomainMismatch   = errorsmod.Register(ModuleName, 14, "attested domain does not match the app's domain")
	ErrMovePending      = errorsmod.Register(ModuleName, 15, "a move for this contract is already pending")
	ErrNotContract      = errorsmod.Register(ModuleName, 16, "address has no contract code")
	ErrNoPendingOwner   = errorsmod.Register(ModuleName, 17, "no pending ownership transfer to signer")
)
