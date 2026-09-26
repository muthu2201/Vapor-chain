// SPDX-License-Identifier: Apache-2.0
// Copyright (c) 2026 VaporChain / muthu2201
// Provenance: VAPOR-6eabb1be532bdef4
pragma solidity ^0.8.28;

import {SettleAddress} from "../utils/SettleAddress.sol";

/// @title VaporCheckout
/// @notice Drop-in merchant checkout. Deploy one per app, accept its
///         registration claim once, and every order paid through it:
///         - settles through the native Settle precompile (fee floor/cap,
///           50% revenue share to the app, referrer share),
///         - makes the app earn sponsorship quota, so its users get free gas.
/// @dev    Users authorize the APP (not this contract, not a shared spender)
///         with SETTLE.approveApp(appId, token, amount); with EIP-7702 the
///         approval and payOrder can be batched into one signature.
contract VaporCheckout {
    uint64 public immutable appId;
    address public immutable merchant;

    mapping(bytes32 => address) public paidBy;

    event OrderPaid(bytes32 indexed orderId, address indexed payer, address token, uint256 amount, uint256 fee, uint256 net, address referrer);

    error AlreadyPaid(bytes32 orderId);
    error ZeroMerchant();

    constructor(uint64 appId_, address merchant_) {
        if (merchant_ == address(0)) revert ZeroMerchant();
        appId = appId_;
        merchant = merchant_;
        // Request attribution; the app owner accepts it once.
        require(SettleAddress.SETTLE.claimRegistration(appId_), "claim failed");
    }

    /// @notice Pay an order (idempotent per orderId: double-pays revert).
    function payOrder(bytes32 orderId, address token, uint256 amount, address referrer)
        external
        returns (uint256 fee, uint256 net)
    {
        if (paidBy[orderId] != address(0)) revert AlreadyPaid(orderId);
        paidBy[orderId] = msg.sender;
        (fee, net) = SettleAddress.SETTLE.payFrom(msg.sender, token, amount, merchant, referrer);
        // SETTLE is a precompile moving bank coins (x/settle rejects erc20/
        // denoms), so no EVM code runs before this log; paidBy is set first.
        // forge-lint: disable-next-line(reentrancy-events)
        emit OrderPaid(orderId, msg.sender, token, amount, fee, net, referrer);
    }

    /// @notice Micro-payment (tips, per-second billing): escrowed in a tab and
    ///         settled in aggregate so the 1% stays 1% on tiny amounts.
    function tip(address token, uint256 amount) external returns (bool settled) {
        return SettleAddress.SETTLE.tabPay(msg.sender, token, amount, merchant);
    }
}
