// SPDX-License-Identifier: LicenseRef-VaporChain-Proprietary
// Copyright (c) 2026 VaporChain / muthu2201. All rights reserved.
// Provenance: VAPOR-6eabb1be532bdef4
pragma solidity ^0.8.28;

import {VaporPaymasterBase} from "./VaporPaymasterBase.sol";
import {IEntryPoint} from "account-abstraction/interfaces/IEntryPoint.sol";
import {PackedUserOperation} from "account-abstraction/interfaces/PackedUserOperation.sol";
import {UserOperationLib} from "account-abstraction/core/UserOperationLib.sol";
import {_packValidationData} from "account-abstraction/core/Helpers.sol";
import {ECDSA} from "@openzeppelin/contracts/utils/cryptography/ECDSA.sol";
import {MessageHashUtils} from "@openzeppelin/contracts/utils/cryptography/MessageHashUtils.sol";

import {Provenance} from "../utils/Provenance.sol";

/// @title VaporVerifyingPaymaster
/// @notice Sponsors gas for UserOperations approved by the VaporChain sponsor
///         service. The service signs only after checking the app's earned
///         quota in x/apps, per-user caps and the state-growth policy, so the
///         chain never gives away unbounded free gas.
/// @dev    paymasterData = validUntil(6) | validAfter(6) | appId(8) | signature(65)
///
///         Security properties:
///         - The signed hash covers every gas-relevant UserOp field, the chain
///           id, this paymaster's address and the VaporChain provenance, so a
///           signature can never be replayed on another chain/paymaster.
///         - Signer rotation is timelocked (48h) so a compromised owner key
///           cannot instantly install a rogue signer; REVOKING the signer is
///           instant (emergency stop).
///         - Withdrawals can only go to the immutable treasury.
///         - EIP-7702 senders must delegate to an allowlisted implementation,
///           blocking sponsored ops from accounts delegated to drainers.
contract VaporVerifyingPaymaster is VaporPaymasterBase {
    using UserOperationLib for PackedUserOperation;

    uint256 private constant VALID_TIMESTAMP_OFFSET = PAYMASTER_DATA_OFFSET;
    uint256 private constant APP_ID_OFFSET = VALID_TIMESTAMP_OFFSET + 12;
    uint256 private constant SIGNATURE_OFFSET = APP_ID_OFFSET + 8;
    uint256 public constant SIGNER_ROTATION_DELAY = 48 hours;

    address public signer;
    address public pendingSigner;
    uint64 public pendingSignerEta;

    /// @notice Allowlisted EIP-7702 delegate implementations.
    mapping(address => bool) public allowedDelegates;

    event Sponsored(uint64 indexed appId, address indexed sender, bytes32 indexed userOpHash, uint256 actualGasCost);
    event SignerProposed(address indexed signer, uint64 eta);
    event SignerChanged(address indexed previous, address indexed current);
    event DelegateAllowed(address indexed implementation, bool allowed);

    error SignerNotReady();
    error BadPaymasterData();

    constructor(
        IEntryPoint entryPoint_,
        address owner_,
        address signer_,
        address payable treasury_,
        address[] memory delegates
    ) VaporPaymasterBase(entryPoint_, owner_, treasury_) {
        signer = signer_;
        emit SignerChanged(address(0), signer_);
        for (uint256 i = 0; i < delegates.length; i++) {
            allowedDelegates[delegates[i]] = true;
            emit DelegateAllowed(delegates[i], true);
        }
    }

    // ------------------------------------------------------------ admin

    /// @notice Start a timelocked signer rotation.
    function proposeSigner(address next) external onlyOwner {
        pendingSigner = next;
        pendingSignerEta = uint64(block.timestamp + SIGNER_ROTATION_DELAY);
        emit SignerProposed(next, pendingSignerEta);
    }

    /// @notice Finalize a rotation once the timelock elapsed.
    function acceptSigner() external onlyOwner {
        if (pendingSigner == address(0) || block.timestamp < pendingSignerEta) revert SignerNotReady();
        emit SignerChanged(signer, pendingSigner);
        signer = pendingSigner;
        pendingSigner = address(0);
        pendingSignerEta = 0;
    }

    /// @notice Emergency stop: instantly disables all sponsorship.
    function revokeSigner() external onlyOwner {
        emit SignerChanged(signer, address(0));
        signer = address(0);
    }

    function setDelegateAllowed(address implementation, bool allowed) external onlyOwner {
        allowedDelegates[implementation] = allowed;
        emit DelegateAllowed(implementation, allowed);
    }

    // --------------------------------------------------------- signing

    /// @notice The hash the sponsor service signs (EIP-191 personal-sign of this).
    function getHash(PackedUserOperation calldata userOp, uint48 validUntil, uint48 validAfter, uint64 appId)
        public
        view
        returns (bytes32)
    {
        return keccak256(
            abi.encode(
                userOp.sender,
                userOp.nonce,
                keccak256(userOp.initCode),
                keccak256(userOp.callData),
                userOp.accountGasLimits,
                uint256(bytes32(userOp.paymasterAndData[PAYMASTER_VALIDATION_GAS_OFFSET:PAYMASTER_DATA_OFFSET])),
                userOp.preVerificationGas,
                userOp.gasFees,
                block.chainid,
                address(this),
                validUntil,
                validAfter,
                appId,
                Provenance.FINGERPRINT
            )
        );
    }

    function parsePaymasterData(bytes calldata paymasterAndData)
        public
        pure
        returns (uint48 validUntil, uint48 validAfter, uint64 appId, bytes calldata signature)
    {
        if (paymasterAndData.length < SIGNATURE_OFFSET + 64) revert BadPaymasterData();
        validUntil = uint48(bytes6(paymasterAndData[VALID_TIMESTAMP_OFFSET:VALID_TIMESTAMP_OFFSET + 6]));
        validAfter = uint48(bytes6(paymasterAndData[VALID_TIMESTAMP_OFFSET + 6:APP_ID_OFFSET]));
        appId = uint64(bytes8(paymasterAndData[APP_ID_OFFSET:SIGNATURE_OFFSET]));
        signature = paymasterAndData[SIGNATURE_OFFSET:];
    }

    function _delegateAllowed(address sender) internal view returns (bool) {
        bytes memory code = sender.code;
        // EIP-7702 delegation designator: 0xef0100 || implementation (23 bytes)
        if (code.length == 23 && code[0] == 0xef && code[1] == 0x01 && code[2] == 0x00) {
            address impl;
            assembly {
                impl := shr(96, mload(add(code, 35)))
            }
            return allowedDelegates[impl];
        }
        return true; // plain smart accounts are fine
    }

    function _validatePaymasterUserOp(PackedUserOperation calldata userOp, bytes32 userOpHash, uint256)
        internal
        view
        override
        returns (bytes memory context, uint256 validationData)
    {
        (uint48 validUntil, uint48 validAfter, uint64 appId, bytes calldata signature) =
            parsePaymasterData(userOp.paymasterAndData);
        if (signature.length != 64 && signature.length != 65) revert BadPaymasterData();

        bytes32 hash = MessageHashUtils.toEthSignedMessageHash(getHash(userOp, validUntil, validAfter, appId));
        (address recovered, ECDSA.RecoverError err,) = ECDSA.tryRecover(hash, signature);
        bool sigFailed = err != ECDSA.RecoverError.NoError || signer == address(0) || recovered != signer
            || !_delegateAllowed(userOp.sender);
        if (sigFailed) {
            return ("", _packValidationData(true, validUntil, validAfter));
        }
        return (abi.encode(appId, userOp.sender, userOpHash), _packValidationData(false, validUntil, validAfter));
    }

    /// @dev Emits a per-op event the sponsor service uses to meter quota usage.
    function _postOp(PostOpMode, bytes calldata context, uint256 actualGasCost, uint256) internal override {
        (uint64 appId, address sender, bytes32 userOpHash) = abi.decode(context, (uint64, address, bytes32));
        emit Sponsored(appId, sender, userOpHash, actualGasCost);
    }
}
