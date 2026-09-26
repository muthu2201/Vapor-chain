// SPDX-License-Identifier: Apache-2.0
// Copyright (c) 2026 VaporChain / muthu2201
// Provenance: VAPOR-6eabb1be532bdef4
pragma solidity ^0.8.28;

import {VaporToken} from "./VaporToken.sol";
import {Provenance} from "../utils/Provenance.sol";

/// @title VaporTokenFactory
/// @notice One-transaction token launch for VaporChain developers.
/// @dev    Tokens are deployed with CREATE2 using salt = keccak(creator, salt),
///         so (a) addresses are predictable before launch (front-ends can show
///         them) and (b) nobody can front-run a creator's address because the
///         creator is part of the salt. The factory has no owner, no fees and
///         no upgrade path; it keeps a registry for explorers and the SDK.
contract VaporTokenFactory {
    struct TokenParams {
        string name;
        string symbol;
        uint8 decimals;
        uint256 initialSupply;
        uint256 cap; // 0 = fixed supply
        address owner;
        string metadataURI;
    }

    bytes32 public constant PROVENANCE = Provenance.FINGERPRINT;

    address[] public allTokens;
    mapping(address => address[]) private _byCreator;
    mapping(address => bool) public isFactoryToken;

    event TokenCreated(
        address indexed token, address indexed creator, address indexed owner, string name, string symbol, uint8 decimals, uint256 initialSupply, uint256 cap
    );

    error EmptyName();
    error BadDecimals();
    error ZeroOwner();

    function createToken(TokenParams calldata p, bytes32 salt) external returns (address token) {
        if (bytes(p.name).length == 0 || bytes(p.symbol).length == 0) revert EmptyName();
        if (p.decimals > 36) revert BadDecimals();
        if (p.owner == address(0)) revert ZeroOwner();
        token = address(
            new VaporToken{salt: _salt(msg.sender, salt)}(
                p.name, p.symbol, p.decimals, p.initialSupply, p.cap, p.owner, p.metadataURI
            )
        );
        allTokens.push(token);
        _byCreator[msg.sender].push(token);
        isFactoryToken[token] = true;
        // the only external call is `new VaporToken`, whose constructor is our
        // own code and makes no calls, so nothing can re-enter before this log
        // forge-lint: disable-next-line(reentrancy-events)
        emit TokenCreated(token, msg.sender, p.owner, p.name, p.symbol, p.decimals, p.initialSupply, p.cap);
    }

    /// @notice Address a creator will get for (params, salt).
    function predictAddress(address creator, TokenParams calldata p, bytes32 salt) external view returns (address) {
        // initcode = creationCode || abi.encode(constructor args), exactly what CREATE2 hashes
        bytes32 initHash = keccak256(
            bytes.concat(
                type(VaporToken).creationCode,
                abi.encode(p.name, p.symbol, p.decimals, p.initialSupply, p.cap, p.owner, p.metadataURI)
            )
        );
        return address(uint160(uint256(keccak256(abi.encodePacked(bytes1(0xff), address(this), _salt(creator, salt), initHash)))));
    }

    function tokensOf(address creator) external view returns (address[] memory) {
        return _byCreator[creator];
    }

    function totalTokens() external view returns (uint256) {
        return allTokens.length;
    }

    function _salt(address creator, bytes32 salt) private pure returns (bytes32) {
        return keccak256(abi.encode(creator, salt));
    }
}
