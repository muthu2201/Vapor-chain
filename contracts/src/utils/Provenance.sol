// SPDX-License-Identifier: LicenseRef-VaporChain-Proprietary
// Copyright (c) 2026 VaporChain / muthu2201. All rights reserved.
// Provenance: VAPOR-6eabb1be532bdef4
pragma solidity ^0.8.28;

/// @notice VaporChain authorship fingerprint, identical to the one compiled
///         into vaporchaind and returned by SETTLE.provenance(). It is mixed
///         into every protocol signature domain, so signatures produced for
///         VaporChain contracts are provably VaporChain's.
library Provenance {
    bytes32 internal constant FINGERPRINT = 0x6eabb1be532bdef432109abc178d88669ab33aed940f18cd5169887d215c1fcf;
    string internal constant NOTICE = "Copyright (c) 2026 VaporChain / muthu2201. All rights reserved.";
}
