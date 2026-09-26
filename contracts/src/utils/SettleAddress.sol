// SPDX-License-Identifier: Apache-2.0
// Copyright (c) 2026 VaporChain / muthu2201
// Provenance: VAPOR-6eabb1be532bdef4
pragma solidity ^0.8.28;

import {ISettle} from "../interfaces/ISettle.sol";

/// @notice Address of the native Settle precompile.
library SettleAddress {
    ISettle internal constant SETTLE = ISettle(0x0000000000000000000000000000000000000900);
}
