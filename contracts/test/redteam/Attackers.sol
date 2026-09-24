// SPDX-License-Identifier: LicenseRef-VaporChain-Proprietary
// Copyright (c) 2026 VaporChain / muthu2201. All rights reserved.
// Provenance: VAPOR-6eabb1be532bdef4
pragma solidity ^0.8.28;

// Adversarial contracts deployed by scripts/redteam/redteam.sh against a live
// chain. None of this ships; each function returns whether the attack call
// succeeded so the script can assert it did NOT.

address constant SETTLE = 0x0000000000000000000000000000000000000900;

/// Tries to run Settle state changes in a context the precompile must refuse.
contract ContextAbuser {
    event Attempt(string kind, bool ok);

    /// DELEGATECALL: would make the precompile act "as" this contract.
    function viaDelegatecall(bytes calldata data) external returns (bool ok) {
        (ok,) = SETTLE.delegatecall(data);
        emit Attempt("delegatecall", ok);
    }

    /// CALLCODE: legacy variant of the same confusion.
    function viaCallcode(bytes calldata data) external returns (bool ok) {
        bytes memory d = data;
        assembly ("memory-safe") {
            ok := callcode(gas(), SETTLE, 0, add(d, 0x20), mload(d), 0, 0)
        }
        emit Attempt("callcode", ok);
    }

    /// STATICCALL into a state-changing method.
    function viaStaticcall(bytes calldata data) external returns (bool ok) {
        (ok,) = SETTLE.staticcall(data);
        emit Attempt("staticcall", ok);
    }

    /// Sends native value along with a Settle call (the precompile must reject it).
    function withValue(bytes calldata data) external payable returns (bool ok) {
        (ok,) = SETTLE.call{value: msg.value}(data);
        emit Attempt("value", ok);
    }

    receive() external payable {}
}

/// A contract (optionally attributed to its own app) trying to pull a victim's
/// funds that the victim only approved for a DIFFERENT app.
contract AllowanceThief {
    event Attempt(string kind, bool ok);

    function claimApp(uint64 appId) external returns (bool ok) {
        (ok,) = SETTLE.call(abi.encodeWithSignature("claimRegistration(uint64)", appId));
    }

    function steal(address victim, address token, uint256 amount) external returns (bool ok) {
        (ok,) = SETTLE.call(
            abi.encodeWithSignature(
                "payFrom(address,address,uint256,address,address)", victim, token, amount, address(this), address(0)
            )
        );
        emit Attempt("payFrom", ok);
    }

    function stealTab(address victim, address token, uint256 amount) external returns (bool ok) {
        (ok,) = SETTLE.call(
            abi.encodeWithSignature("tabPay(address,address,uint256,address)", victim, token, amount, address(this))
        );
        emit Attempt("tabPay", ok);
    }

    function claimOthers(uint64 appId, address token) external returns (bool ok) {
        (ok,) = SETTLE.call(abi.encodeWithSignature("claim(uint64,address)", appId, token));
        emit Attempt("claim", ok);
    }
}

/// Burns gas in a loop: probes the eth_call / estimate gas caps.
contract GasBomb {
    function burn() external pure returns (uint256 x) {
        while (true) {
            x++;
        }
    }
}
