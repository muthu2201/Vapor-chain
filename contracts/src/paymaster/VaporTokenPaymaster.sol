// SPDX-License-Identifier: LicenseRef-VaporChain-Proprietary
// Copyright (c) 2026 VaporChain / muthu2201. All rights reserved.
// Provenance: VAPOR-6eabb1be532bdef4
pragma solidity ^0.8.28;

import {VaporPaymasterBase} from "./VaporPaymasterBase.sol";
import {IEntryPoint} from "account-abstraction/interfaces/IEntryPoint.sol";
import {PackedUserOperation} from "account-abstraction/interfaces/PackedUserOperation.sol";
import {IERC20} from "@openzeppelin/contracts/token/ERC20/IERC20.sol";
import {SafeERC20} from "@openzeppelin/contracts/token/ERC20/utils/SafeERC20.sol";
import {Math} from "@openzeppelin/contracts/utils/math/Math.sol";

import {SettleAddress} from "../utils/SettleAddress.sol";

/// @title VaporTokenPaymaster
/// @notice Lets users pay gas in USDC. The exchange rate is NOT an oracle and
///         NOT an owner-set number: it is read live from the Settle
///         precompile's governance-fixed gas-credit price (x/settle), so the
///         paymaster can never drift from protocol pricing and there is no
///         price to manipulate.
/// @dev    Flow: validation pre-charges the maximum cost in USDC (transferFrom
///         the sender, who must have approved this paymaster); postOp refunds
///         the unused part. `refill()` converts collected USDC into gas
///         credits through SETTLE.buyCredits and deposits them into the
///         EntryPoint, so the paymaster is self-funding and permissionless to
///         top up.
///         Bundler note: validation calls the Settle precompile and a
///         bank-backed ERC-20 precompile, which ERC-7562 tracers do not know;
///         run the VaporChain bundler with safe-mode off (it is a permissioned
///         bundler protected by the sponsored-lane cap and paymaster stake).
contract VaporTokenPaymaster is VaporPaymasterBase {
    using SafeERC20 for IERC20;

    /// @notice USDC (bank-native ERC-20 precompile).
    IERC20 public immutable token;
    /// @notice Markup on top of the protocol price (bps), covers rounding and
    ///         the postOp overhead; capped at 20%.
    uint256 public markupBps;
    /// @notice Gas charged for this paymaster's own postOp.
    uint256 public constant POST_OP_OVERHEAD = 40_000;
    uint256 public constant MAX_MARKUP_BPS = 2_000;
    /// @notice Price probe size (1 USDC = 1e6 base units) keeps precision.
    uint256 private constant PROBE = 1e6;

    event GasPaidInToken(address indexed sender, uint256 tokenCharged, uint256 gasCost);
    event Refilled(uint256 tokenSpent, uint256 creditsDeposited);
    event MarkupChanged(uint256 markupBps);

    error MarkupTooHigh();
    error CreditsUnavailable();

    constructor(IEntryPoint entryPoint_, address owner_, IERC20 token_, address payable treasury_, uint256 markupBps_)
        VaporPaymasterBase(entryPoint_, owner_, treasury_)
    {
        if (markupBps_ > MAX_MARKUP_BPS) revert MarkupTooHigh();
        if (address(token_) == address(0)) revert CreditsUnavailable();
        token = token_;
        markupBps = markupBps_;
    }

    function setMarkup(uint256 bps) external onlyOwner {
        if (bps > MAX_MARKUP_BPS) revert MarkupTooHigh();
        markupBps = bps;
        emit MarkupChanged(bps);
    }

    /// @notice Credits (wei of CREDIT) bought by 1 USDC at the protocol price.
    function creditsPerToken() public view returns (uint256) {
        uint256 c = SettleAddress.SETTLE.quoteCredits(address(token), PROBE);
        if (c == 0) revert CreditsUnavailable();
        return c;
    }

    /// @notice USDC needed to cover `gasCost` wei of CREDIT, incl. markup (rounded up).
    function tokenCost(uint256 gasCost) public view returns (uint256) {
        uint256 withMarkup = Math.mulDiv(gasCost, 10_000 + markupBps, 10_000, Math.Rounding.Ceil);
        return Math.mulDiv(withMarkup, PROBE, creditsPerToken(), Math.Rounding.Ceil);
    }

    function _validatePaymasterUserOp(PackedUserOperation calldata userOp, bytes32, uint256 maxCost)
        internal
        override
        returns (bytes memory context, uint256 validationData)
    {
        // gasFees = maxPriorityFeePerGas (high 128) | maxFeePerGas (low 128)
        // forge-lint: disable-next-line(unsafe-typecast)
        uint256 maxFeePerGas = uint128(uint256(userOp.gasFees));
        uint256 maxCharge = tokenCost(maxCost + POST_OP_OVERHEAD * maxFeePerGas);
        // `from` is the UserOp sender, whose own signature the EntryPoint has
        // already validated for THIS op: only its own approved USDC is charged.
        // forge-lint: disable-next-line(arbitrary-send-erc20)
        token.safeTransferFrom(userOp.sender, address(this), maxCharge);
        return (abi.encode(userOp.sender, maxCharge), 0);
    }

    function _postOp(PostOpMode, bytes calldata context, uint256 actualGasCost, uint256 actualUserOpFeePerGas)
        internal
        override
    {
        (address sender, uint256 maxCharge) = abi.decode(context, (address, uint256));
        uint256 actual = tokenCost(actualGasCost + POST_OP_OVERHEAD * actualUserOpFeePerGas);
        if (actual > maxCharge) actual = maxCharge;
        uint256 refund = maxCharge - actual;
        if (refund > 0) token.safeTransfer(sender, refund);
        // `token` is the bank-backed USDC precompile: a transfer runs no EVM
        // code at the recipient, so nothing can re-enter before this log.
        // forge-lint: disable-next-line(reentrancy-events)
        emit GasPaidInToken(sender, actual, actualGasCost);
    }

    /// @notice Convert collected USDC into gas credits and deposit them in the
    ///         EntryPoint. Anyone may call: funds never leave the paymaster.
    function refill(uint256 tokenAmount) external {
        uint256 bal = token.balanceOf(address(this));
        if (tokenAmount > bal) tokenAmount = bal;
        uint256 credits = SettleAddress.SETTLE.buyCredits(address(token), tokenAmount);
        entryPoint.depositTo{value: credits}(address(this));
        // SETTLE is native code and depositTo only credits a balance; neither
        // calls back into this contract.
        // forge-lint: disable-next-line(reentrancy-events)
        emit Refilled(tokenAmount, credits);
    }

    /// @notice Sweep surplus USDC to the treasury.
    function sweep(uint256 amount) external onlyOwner {
        token.safeTransfer(treasury, amount);
    }

    /// @dev Receives minted credits from SETTLE.buyCredits.
    receive() external payable {}
}
