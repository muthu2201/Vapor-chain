// SPDX-License-Identifier: LicenseRef-VaporChain-Proprietary
// Copyright (c) 2026 VaporChain / muthu2201. All rights reserved.
// Provenance: VAPOR-6eabb1be532bdef4
pragma solidity ^0.8.24;

/// @title ISettle — VaporChain's native checkout precompile (0x…0900)
/// @notice Every function is executed by native Go code (x/settle + x/apps),
///         not by EVM bytecode: it cannot be upgraded by an EOA admin, it never
///         calls back into user contracts (no reentrancy surface) and it shares
///         state with the Cosmos modules.
/// @dev    Attribution is ALWAYS msg.sender's registered app. Callers cannot
///         pass an app id for payments, so nobody can steal another app's
///         revenue share or sponsorship quota.
///         `token` is the ERC-20 address of a bank-native coin registered in
///         x/erc20 (e.g. USDC.inj). Only governance-allowlisted assets work.
interface ISettle {
    /// Emitted for every settled payment.
    event Settled(
        uint64 indexed appId,
        address indexed payer,
        address indexed payee,
        address token,
        uint256 amount,
        uint256 fee,
        uint256 net,
        address referrer
    );
    event TabDeposited(uint64 indexed appId, address indexed payer, address indexed payee, address token, uint256 amount, bool settled);
    event AppApproval(address indexed owner, uint64 indexed appId, address token, uint256 amount);
    event RevenueClaimed(uint64 indexed appId, address indexed recipient, address token, uint256 amount);
    event CreditsBought(address indexed buyer, address indexed recipient, address token, uint256 paid, uint256 credits);
    event RegistrationClaimed(uint64 indexed appId, address indexed contractAddr);
    event AppRegistered(uint64 indexed appId, address indexed owner);

    // ------------------------------------------------------------------ payments
    /// @notice Pay `amount` of `token` from msg.sender to `payee`, minus the fee.
    function pay(address token, uint256 amount, address payee, address referrer) external returns (uint256 fee, uint256 net);

    /// @notice Pull from `payer` using the allowance `payer` granted to msg.sender's app.
    /// @dev msg.sender must be a contract of an ACTIVE registered app.
    function payFrom(address payer, address token, uint256 amount, address payee, address referrer)
        external
        returns (uint256 fee, uint256 net);

    /// @notice Escrow a micro-payment into a tab; auto-settles at the asset's threshold.
    /// @dev payer must be msg.sender, or have approved msg.sender's app.
    function tabPay(address payer, address token, uint256 amount, address payee) external returns (bool settled);

    /// @notice Settle an open tab (payer, payee, app owner, or anyone after max age).
    function closeTab(uint64 appId, address payer, address payee, address token) external returns (uint256 fee, uint256 net);

    // ----------------------------------------------------------------- approvals
    /// @notice Authorize ONE app (not a shared spender) to pull up to `amount` via payFrom/tabPay.
    function approveApp(uint64 appId, address token, uint256 amount) external returns (bool);
    function appAllowance(address owner, uint64 appId, address token) external view returns (uint256);

    // ---------------------------------------------------------------------- apps
    /// @notice Register an app owned by msg.sender (burns the registration fee in credits).
    function registerApp(address revenueRecipient, string calldata metadataUri, uint32 referrerBps) external returns (uint64 appId);
    /// @notice Called BY a contract (e.g. in its constructor) to request attribution to `appId`.
    ///         The app owner must accept with acceptContractClaim.
    function claimRegistration(uint64 appId) external returns (bool);
    /// @notice App owner (msg.sender) accepts a contract's pending claim.
    function acceptContractClaim(uint64 appId, address contractAddr) external returns (bool pending);
    /// @notice App owner (msg.sender) locks `amount` of `token` (the chain's bond asset, e.g. USDC)
    ///         behind the app. Base sponsorship quota is linear in bonded capital, so it needs no
    ///         identity checks: splitting one bond across many apps never yields more quota.
    function bondApp(uint64 appId, address token, uint256 amount) external returns (uint256 bonded);
    /// @notice App owner starts returning bonded capital: it stops counting toward quota now and is
    ///         released to the owner once the chain's unbonding period has passed.
    function unbondApp(uint64 appId, address token, uint256 amount) external returns (uint64 releaseHeight);
    /// @notice Capital bonded behind `appId` and the base quota (gas per epoch) it buys.
    function appBond(uint64 appId) external view returns (uint256 bonded, uint64 baseGasPerEpoch);
    function appOf(address contractAddr) external view returns (uint64 appId, bool active);

    // ------------------------------------------------------------------- revenue
    /// @notice Pay an app's claimable revenue in `token` to its revenue recipient (anyone may call).
    function claim(uint64 appId, address token) external returns (uint256 amount);
    function claimable(uint64 appId, address token) external view returns (uint256);

    // ------------------------------------------------------------------- credits
    /// @notice Buy gas credits (native CREDIT) at the governance-fixed price, sent to msg.sender.
    function buyCredits(address token, uint256 amount) external returns (uint256 credits);
    function quoteCredits(address token, uint256 amount) external view returns (uint256);

    // --------------------------------------------------------------------- views
    function quoteFee(address token, uint256 amount) external view returns (uint256 fee, uint256 net);
    function denomOf(address token) external view returns (string memory);
    /// @notice VaporChain authorship fingerprint.
    function provenance() external view returns (bytes32);
}
