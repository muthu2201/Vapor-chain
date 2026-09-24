// SPDX-License-Identifier: Apache-2.0
// Copyright (c) 2026 VaporChain / muthu2201. Licensed under Apache-2.0.
// Provenance: VAPOR-6eabb1be532bdef4
pragma solidity ^0.8.28;

import {SettleAddress} from "../utils/SettleAddress.sol";

/// @title SwordShop — the blueprint's reference game purchase.
/// @notice "Buy sword (2 USDC)": the payment runs through Settle so the game
///         earns 50% of the fee and its players get sponsored gas.
contract SwordShop {
    uint256 public constant PRICE = 2e6; // 2 USDC (6 decimals)

    address public immutable usdc;
    address public immutable treasuryOfGame;
    mapping(address => uint256) public swords;

    event SwordsBought(address indexed buyer, uint256 qty, uint256 fee);

    error ZeroAddress();

    constructor(address usdc_, address treasury_, uint64 appId) {
        if (usdc_ == address(0) || treasury_ == address(0)) revert ZeroAddress();
        usdc = usdc_;
        treasuryOfGame = treasury_;
        require(SettleAddress.SETTLE.claimRegistration(appId), "claim failed");
    }

    function buy(uint256 qty, address referrer) external {
        // `net` is not needed: the game only records the fee it was charged
        // forge-lint: disable-next-line(unused-return)
        (uint256 fee,) = SettleAddress.SETTLE.payFrom(msg.sender, usdc, PRICE * qty, treasuryOfGame, referrer);
        swords[msg.sender] += qty;
        // SETTLE is native code moving bank coins; nothing re-enters here
        // forge-lint: disable-next-line(reentrancy-events)
        emit SwordsBought(msg.sender, qty, fee);
    }
}
