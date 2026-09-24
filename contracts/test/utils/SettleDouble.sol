// SPDX-License-Identifier: LicenseRef-VaporChain-Proprietary
// Copyright (c) 2026 VaporChain / muthu2201. All rights reserved.
// Provenance: VAPOR-6eabb1be532bdef4
pragma solidity ^0.8.28;

import {IERC20} from "@openzeppelin/contracts/token/ERC20/IERC20.sol";

/// @notice TEST DOUBLE of the Settle precompile for Foundry unit tests (the
///         real precompile is native Go and only exists on a VaporChain node;
///         e2e tests in script/e2e run against the real one on localnet).
///         Semantics mirror x/settle: fee = 1% with floor/cap, attribution by
///         caller, per-app allowances, pending self-claims, fixed credit price.
contract SettleDouble {
    uint256 public constant FEE_BPS = 100;
    uint256 public constant MIN_FEE = 2_000;
    uint256 public constant MAX_FEE = 5_000_000;
    uint256 public constant MICRO = 250_000;
    uint256 public constant CREDIT_PRICE = 1e15; // acredit per micro-USDC

    mapping(address => uint64) public appOfContract;
    mapping(uint64 => mapping(address => bool)) public pendingClaim;
    mapping(address => mapping(uint64 => mapping(address => uint256))) public allowance;
    mapping(uint64 => mapping(address => uint256)) public claimableOf;
    address public treasury = address(0x7EA5);
    uint64 public nextApp = 1;
    mapping(uint64 => address) public ownerOf;

    function fee(uint256 amount) public pure returns (uint256 f) {
        f = amount * FEE_BPS / 10_000;
        if (amount >= MICRO && f < MIN_FEE) f = MIN_FEE;
        if (f > MAX_FEE) f = MAX_FEE;
        if (f > amount) f = amount;
    }

    function registerApp(address, string calldata, uint32) external returns (uint64 id) {
        id = nextApp++;
        ownerOf[id] = msg.sender;
    }

    function claimRegistration(uint64 appId) external returns (bool) {
        pendingClaim[appId][msg.sender] = true;
        return true;
    }

    function acceptContractClaim(uint64 appId, address c) external returns (bool) {
        require(ownerOf[appId] == msg.sender, "not owner");
        require(pendingClaim[appId][c], "no claim");
        appOfContract[c] = appId;
        return false;
    }

    function approveApp(uint64 appId, address token, uint256 amount) external returns (bool) {
        allowance[msg.sender][appId][token] = amount;
        return true;
    }

    function _settle(address payer, address token, uint256 amount, address payee, uint64 appId)
        internal
        returns (uint256 f, uint256 net)
    {
        f = fee(amount);
        net = amount - f;
        IERC20(token).transferFrom(payer, address(this), amount);
        IERC20(token).transfer(payee, net);
        claimableOf[appId][token] += f / 2;
        IERC20(token).transfer(treasury, f - f / 2);
    }

    function pay(address token, uint256 amount, address payee, address) external returns (uint256, uint256) {
        return _settle(msg.sender, token, amount, payee, appOfContract[msg.sender]);
    }

    function payFrom(address payer, address token, uint256 amount, address payee, address)
        external
        returns (uint256, uint256)
    {
        uint64 appId = appOfContract[msg.sender];
        require(appId != 0, "caller not an app");
        if (payer != msg.sender) {
            uint256 a = allowance[payer][appId][token];
            require(a >= amount, "allowance");
            if (a != type(uint256).max) allowance[payer][appId][token] = a - amount;
        }
        return _settle(payer, token, amount, payee, appId);
    }

    function tabPay(address payer, address token, uint256 amount, address payee) external returns (bool) {
        require(payer == msg.sender || appOfContract[msg.sender] != 0, "tab");
        IERC20(token).transferFrom(payer, payee, amount);
        return false;
    }

    function quoteCredits(address, uint256 amount) external pure returns (uint256) {
        return amount * CREDIT_PRICE;
    }

    function buyCredits(address token, uint256 amount) external returns (uint256 credits) {
        IERC20(token).transferFrom(msg.sender, treasury, amount);
        credits = amount * CREDIT_PRICE;
        (bool ok,) = msg.sender.call{value: credits}("");
        require(ok, "credit transfer");
    }

    receive() external payable {}
}
