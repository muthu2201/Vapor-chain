// SPDX-License-Identifier: LicenseRef-VaporChain-Proprietary
// Copyright (c) 2026 VaporChain / muthu2201. All rights reserved.
// Provenance: VAPOR-6eabb1be532bdef4
pragma solidity ^0.8.28;

import {Test} from "forge-std/Test.sol";

import {VaporCheckout} from "../src/checkout/VaporCheckout.sol";
import {SwordShop} from "../src/examples/SwordShop.sol";
import {SettleDouble} from "./utils/SettleDouble.sol";
import {TestUSDC} from "./utils/TestUSDC.sol";

contract VaporCheckoutTest is Test {
    address constant SETTLE = 0x0000000000000000000000000000000000000900;
    SettleDouble settle;
    TestUSDC usdc;
    address appOwner = makeAddr("app-owner");
    address merchant = makeAddr("merchant");
    address user = makeAddr("user");
    uint64 appId;

    function setUp() public {
        deployCodeTo("SettleDouble.sol:SettleDouble", SETTLE);
        settle = SettleDouble(payable(SETTLE));
        usdc = new TestUSDC();
        usdc.mint(user, 1_000e6);
        vm.prank(user);
        usdc.approve(SETTLE, type(uint256).max);
        vm.prank(appOwner);
        appId = settle.registerApp(appOwner, "", 0);
    }

    function test_CheckoutRequiresAcceptedClaimAndAppApproval() public {
        VaporCheckout c = new VaporCheckout(appId, merchant);
        vm.prank(user);
        vm.expectRevert("caller not an app");
        c.payOrder("order-1", address(usdc), 10e6, address(0));
        vm.prank(appOwner);
        settle.acceptContractClaim(appId, address(c));
        vm.prank(user);
        vm.expectRevert("allowance");
        c.payOrder("order-1", address(usdc), 10e6, address(0));
        vm.prank(user);
        settle.approveApp(appId, address(usdc), 25e6);
        vm.prank(user);
        (uint256 fee, uint256 net) = c.payOrder("order-1", address(usdc), 10e6, address(0));
        assertEq(fee, 100_000);
        assertEq(net, 9_900_000);
        assertEq(usdc.balanceOf(merchant), 9_900_000);
        assertEq(c.paidBy("order-1"), user);
        vm.prank(user);
        vm.expectRevert(abi.encodeWithSelector(VaporCheckout.AlreadyPaid.selector, bytes32("order-1")));
        c.payOrder("order-1", address(usdc), 10e6, address(0));
        assertEq(settle.allowance(user, appId, address(usdc)), 15e6);
    }

    function test_SwordShopBlueprintFlow() public {
        SwordShop shop = new SwordShop(address(usdc), merchant, appId);
        vm.prank(appOwner);
        settle.acceptContractClaim(appId, address(shop));
        vm.startPrank(user);
        settle.approveApp(appId, address(usdc), type(uint256).max);
        shop.buy(3, address(0));
        vm.stopPrank();
        assertEq(shop.swords(user), 3);
        assertEq(usdc.balanceOf(merchant), 6e6 - 60_000); // 1% of 6 USDC
    }
}
