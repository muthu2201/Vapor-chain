// Copyright (c) 2026 VaporChain / muthu2201. All rights reserved.
// Provenance: VAPOR-6eabb1be532bdef4

package types

import errorsmod "cosmossdk.io/errors"

var (
	ErrUnauthorized        = errorsmod.Register(ModuleName, 2, "unauthorized")
	ErrNotAdmitted         = errorsmod.Register(ModuleName, 3, "operator is not admitted by the validator council")
	ErrRemoved             = errorsmod.Register(ModuleName, 4, "operator was removed by the validator council and can never validate again")
	ErrInvalidPower        = errorsmod.Register(ModuleName, 5, "invalid validator power")
	ErrInvalidTarget       = errorsmod.Register(ModuleName, 6, "invalid pause target")
	ErrForeignDelegation   = errorsmod.Register(ModuleName, 7, "only self-delegation is allowed on a proof-of-authority chain")
	ErrInvalidParams       = errorsmod.Register(ModuleName, 8, "invalid params")
	ErrProvenanceMismatch  = errorsmod.Register(ModuleName, 9, "genesis provenance does not match this binary")
	ErrPaused              = errorsmod.Register(ModuleName, 10, "target is paused by the emergency guardian")
	ErrGuardianCannotUnset = errorsmod.Register(ModuleName, 11, "guardians can pause but only governance can unpause")
)
