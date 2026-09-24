// SPDX-License-Identifier: LicenseRef-VaporChain-Proprietary
// Copyright (c) 2026 VaporChain / muthu2201. All rights reserved.
// Provenance: VAPOR-6eabb1be532bdef4
pragma solidity ^0.8.28;

import {IPaymaster} from "account-abstraction/interfaces/IPaymaster.sol";
import {IEntryPoint} from "account-abstraction/interfaces/IEntryPoint.sol";
import {PackedUserOperation} from "account-abstraction/interfaces/PackedUserOperation.sol";
import {UserOperationLib} from "account-abstraction/core/UserOperationLib.sol";
import {IERC165} from "@openzeppelin/contracts/utils/introspection/IERC165.sol";
import {Ownable} from "@openzeppelin/contracts/access/Ownable.sol";
import {Ownable2Step} from "@openzeppelin/contracts/access/Ownable2Step.sol";

/// @title VaporPaymasterBase
/// @notice Minimal paymaster base (replaces eth-infinitism's BasePaymaster).
/// @dev    Why not BasePaymaster: its withdrawTo/withdrawStake let the owner
///         send the EntryPoint deposit and stake to ANY address. VaporChain
///         pins every outflow to an immutable treasury, so a stolen owner key
///         can at worst stop sponsorship, never steal the float.
abstract contract VaporPaymasterBase is IPaymaster, Ownable2Step {
    uint256 internal constant PAYMASTER_VALIDATION_GAS_OFFSET = UserOperationLib.PAYMASTER_VALIDATION_GAS_OFFSET;
    uint256 internal constant PAYMASTER_POSTOP_GAS_OFFSET = UserOperationLib.PAYMASTER_POSTOP_GAS_OFFSET;
    uint256 internal constant PAYMASTER_DATA_OFFSET = UserOperationLib.PAYMASTER_DATA_OFFSET;

    IEntryPoint public immutable entryPoint;
    address payable public immutable treasury;

    error NotFromEntryPoint();
    error InvalidEntryPoint();
    error InvalidTreasury();

    constructor(IEntryPoint entryPoint_, address owner_, address payable treasury_) Ownable(owner_) {
        if (!IERC165(address(entryPoint_)).supportsInterface(type(IEntryPoint).interfaceId)) revert InvalidEntryPoint();
        if (treasury_ == address(0)) revert InvalidTreasury();
        entryPoint = entryPoint_;
        treasury = treasury_;
    }

    modifier onlyEntryPoint() {
        if (msg.sender != address(entryPoint)) revert NotFromEntryPoint();
        _;
    }

    /// @inheritdoc IPaymaster
    function validatePaymasterUserOp(PackedUserOperation calldata userOp, bytes32 userOpHash, uint256 maxCost)
        external
        onlyEntryPoint
        returns (bytes memory context, uint256 validationData)
    {
        return _validatePaymasterUserOp(userOp, userOpHash, maxCost);
    }

    /// @inheritdoc IPaymaster
    function postOp(PostOpMode mode, bytes calldata context, uint256 actualGasCost, uint256 actualUserOpFeePerGas)
        external
        onlyEntryPoint
    {
        _postOp(mode, context, actualGasCost, actualUserOpFeePerGas);
    }

    function _validatePaymasterUserOp(PackedUserOperation calldata userOp, bytes32 userOpHash, uint256 maxCost)
        internal
        virtual
        returns (bytes memory context, uint256 validationData);

    function _postOp(PostOpMode mode, bytes calldata context, uint256 actualGasCost, uint256 actualUserOpFeePerGas)
        internal
        virtual;

    // ------------------------------------------------ deposit & stake

    /// @notice Anyone may top up the paymaster's EntryPoint deposit.
    function deposit() public payable {
        entryPoint.depositTo{value: msg.value}(address(this));
    }

    function getDeposit() public view returns (uint256) {
        return entryPoint.balanceOf(address(this));
    }

    /// @notice Withdraw deposit — ALWAYS to the treasury.
    function withdrawDeposit(uint256 amount) external onlyOwner {
        entryPoint.withdrawTo(treasury, amount);
    }

    function addStake(uint32 unstakeDelaySec) external payable onlyOwner {
        entryPoint.addStake{value: msg.value}(unstakeDelaySec);
    }

    function unlockStake() external onlyOwner {
        entryPoint.unlockStake();
    }

    /// @notice Withdraw unlocked stake — ALWAYS to the treasury.
    function withdrawStake() external onlyOwner {
        entryPoint.withdrawStake(treasury);
    }
}
