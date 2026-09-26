// SPDX-License-Identifier: Apache-2.0
// Copyright (c) 2026 VaporChain / muthu2201
// Provenance: VAPOR-6eabb1be532bdef4
pragma solidity ^0.8.28;

import {ERC20} from "@openzeppelin/contracts/token/ERC20/ERC20.sol";
import {ERC20Permit} from "@openzeppelin/contracts/token/ERC20/extensions/ERC20Permit.sol";
import {ERC20Burnable} from "@openzeppelin/contracts/token/ERC20/extensions/ERC20Burnable.sol";
import {Ownable} from "@openzeppelin/contracts/access/Ownable.sol";
import {Ownable2Step} from "@openzeppelin/contracts/access/Ownable2Step.sol";

/// @title VaporToken
/// @notice The standard token template used by VaporTokenFactory: ERC-20 +
///         EIP-2612 permit (gasless approvals, pairs well with sponsored
///         UserOps) + burnable + optional owner minting under a hard cap.
/// @dev    Deliberately NO pausing, blacklisting, fee-on-transfer, rebasing or
///         upgradeability: tokens launched through the factory behave exactly
///         like plain ERC-20s, which is what wallets, DEXes and x/erc20
///         registration expect. `mintingFinished` can be set once and is final.
contract VaporToken is ERC20, ERC20Permit, ERC20Burnable, Ownable2Step {
    uint8 private immutable _decimals;
    /// @notice Hard cap on total supply (0 = supply fixed at creation).
    uint256 public immutable cap;
    bool public mintingFinished;
    /// @notice Optional metadata (logo/website JSON) set at creation.
    string public metadataURI;

    error MintingDisabled();
    error CapExceeded(uint256 supply, uint256 cap);

    event MintingFinished();

    constructor(
        string memory name_,
        string memory symbol_,
        uint8 decimals_,
        uint256 initialSupply,
        uint256 cap_,
        address owner_,
        string memory metadataURI_
    ) ERC20(name_, symbol_) ERC20Permit(name_) Ownable(owner_) {
        _decimals = decimals_;
        cap = cap_;
        metadataURI = metadataURI_;
        if (cap_ != 0 && initialSupply > cap_) revert CapExceeded(initialSupply, cap_);
        if (cap_ == 0) {
            mintingFinished = true;
        }
        if (initialSupply > 0) _mint(owner_, initialSupply);
    }

    function decimals() public view override returns (uint8) {
        return _decimals;
    }

    /// @notice Owner mint, only while minting is open and under the cap.
    function mint(address to, uint256 amount) external onlyOwner {
        if (mintingFinished) revert MintingDisabled();
        if (totalSupply() + amount > cap) revert CapExceeded(totalSupply() + amount, cap);
        _mint(to, amount);
    }

    /// @notice Permanently close minting (irreversible, builds holder trust).
    function finishMinting() external onlyOwner {
        mintingFinished = true;
        emit MintingFinished();
    }
}
