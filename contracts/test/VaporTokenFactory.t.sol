// SPDX-License-Identifier: AGPL-3.0-only
// Copyright (c) 2026 VaporChain / muthu2201
// Provenance: VAPOR-6eabb1be532bdef4
pragma solidity ^0.8.28;

import {Test, Vm} from "forge-std/Test.sol";

import {VaporTokenFactory} from "../src/tokens/VaporTokenFactory.sol";
import {VaporToken} from "../src/tokens/VaporToken.sol";

contract VaporTokenFactoryTest is Test {
    VaporTokenFactory factory;
    address alice = makeAddr("alice");
    address bob = makeAddr("bob");

    function setUp() public {
        factory = new VaporTokenFactory();
    }

    function _params(address owner, uint256 supply, uint256 cap) internal pure returns (VaporTokenFactory.TokenParams memory) {
        return VaporTokenFactory.TokenParams("Vapor Game Gold", "GOLD", 18, supply, cap, owner, "ipfs://gold.json");
    }

    function test_LaunchFixedSupplyToken() public {
        vm.prank(alice);
        address t = factory.createToken(_params(alice, 1_000_000e18, 0), bytes32("s1"));
        VaporToken tok = VaporToken(t);
        assertEq(tok.totalSupply(), 1_000_000e18);
        assertEq(tok.balanceOf(alice), 1_000_000e18);
        assertTrue(tok.mintingFinished(), "cap 0 = fixed supply");
        vm.prank(alice);
        vm.expectRevert(VaporToken.MintingDisabled.selector);
        tok.mint(alice, 1);
        assertTrue(factory.isFactoryToken(t));
        assertEq(factory.tokensOf(alice)[0], t);
    }

    function test_CappedMintingThenFinish() public {
        vm.prank(alice);
        VaporToken tok = VaporToken(factory.createToken(_params(alice, 100e18, 1_000e18), bytes32("s2")));
        vm.startPrank(alice);
        tok.mint(bob, 900e18);
        vm.expectRevert(abi.encodeWithSelector(VaporToken.CapExceeded.selector, 1_000e18 + 1, 1_000e18));
        tok.mint(bob, 1);
        tok.finishMinting();
        vm.expectRevert(VaporToken.MintingDisabled.selector);
        tok.mint(bob, 0);
        vm.stopPrank();
        vm.prank(bob);
        vm.expectRevert();
        tok.mint(bob, 1); // non-owner
    }

    function test_PredictedAddressAndFrontRunProtection() public {
        VaporTokenFactory.TokenParams memory p = _params(alice, 1e18, 0);
        address predicted = factory.predictAddress(alice, p, bytes32("launch"));
        // bob uses the same params+salt first: he gets a DIFFERENT address
        vm.prank(bob);
        address bobs = factory.createToken(p, bytes32("launch"));
        assertTrue(bobs != predicted);
        vm.prank(alice);
        assertEq(factory.createToken(p, bytes32("launch")), predicted);
    }

    function test_PermitGaslessApproval() public {
        Vm.Wallet memory w = vm.createWallet("holder");
        vm.prank(w.addr);
        VaporToken tok = VaporToken(factory.createToken(_params(w.addr, 10e18, 0), bytes32("p")));
        bytes32 structHash = keccak256(abi.encode(
            keccak256("Permit(address owner,address spender,uint256 value,uint256 nonce,uint256 deadline)"),
            w.addr, bob, 5e18, tok.nonces(w.addr), block.timestamp + 1 hours));
        bytes32 digest = keccak256(abi.encodePacked("\x19\x01", tok.DOMAIN_SEPARATOR(), structHash));
        (uint8 v, bytes32 r, bytes32 s) = vm.sign(w.privateKey, digest);
        tok.permit(w.addr, bob, 5e18, block.timestamp + 1 hours, v, r, s);
        assertEq(tok.allowance(w.addr, bob), 5e18);
    }

    function test_Validation() public {
        VaporTokenFactory.TokenParams memory p = _params(alice, 1, 0);
        p.name = "";
        vm.expectRevert(VaporTokenFactory.EmptyName.selector);
        factory.createToken(p, 0);
        p = _params(address(0), 1, 0);
        vm.expectRevert(VaporTokenFactory.ZeroOwner.selector);
        factory.createToken(p, 0);
        p = _params(alice, 1, 0);
        p.decimals = 37;
        vm.expectRevert(VaporTokenFactory.BadDecimals.selector);
        factory.createToken(p, 0);
        p = _params(alice, 11, 10);
        vm.expectRevert();
        factory.createToken(p, 0);
    }

    function testFuzz_SupplyNeverExceedsCap(uint128 initial, uint128 cap, uint128 mintAmt) public {
        vm.assume(cap > 0 && initial <= cap);
        vm.prank(alice);
        VaporToken tok = VaporToken(factory.createToken(_params(alice, initial, cap), keccak256(abi.encode(initial, cap))));
        vm.prank(alice);
        try tok.mint(bob, mintAmt) {} catch {}
        assertLe(tok.totalSupply(), cap);
    }
}
