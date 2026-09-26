// SPDX-License-Identifier: AGPL-3.0-only
// Copyright (c) 2026 VaporChain / muthu2201
// Provenance: VAPOR-6eabb1be532bdef4
pragma solidity ^0.8.28;

import {Test, Vm} from "forge-std/Test.sol";
import {EntryPoint} from "account-abstraction/core/EntryPoint.sol";
import {IEntryPoint} from "account-abstraction/interfaces/IEntryPoint.sol";
import {PackedUserOperation} from "account-abstraction/interfaces/PackedUserOperation.sol";
import {SimpleAccountFactory} from "account-abstraction/accounts/SimpleAccountFactory.sol";
import {BaseAccount} from "account-abstraction/core/BaseAccount.sol";
import {IERC20} from "@openzeppelin/contracts/token/ERC20/IERC20.sol";

import {VaporTokenPaymaster} from "../src/paymaster/VaporTokenPaymaster.sol";
import {SettleDouble} from "./utils/SettleDouble.sol";
import {TestUSDC} from "./utils/TestUSDC.sol";

contract VaporTokenPaymasterTest is Test {
    address constant EP = 0x4337084D9E255Ff0702461CF8895CE9E3b5Ff108;
    address constant SETTLE = 0x0000000000000000000000000000000000000900;

    EntryPoint ep;
    VaporTokenPaymaster pm;
    TestUSDC usdc;
    SettleDouble settle;
    SimpleAccountFactory factory;
    Vm.Wallet ownerW = vm.createWallet("owner");
    address payable treasury = payable(makeAddr("treasury"));
    address bundler = makeAddr("bundler");
    address account;

    function setUp() public {
        deployCodeTo("EntryPoint.sol:EntryPoint", EP);
        ep = EntryPoint(payable(EP));
        deployCodeTo("SettleDouble.sol:SettleDouble", SETTLE);
        settle = SettleDouble(payable(SETTLE));
        vm.deal(SETTLE, 1_000_000 ether);
        usdc = new TestUSDC();
        factory = new SimpleAccountFactory(IEntryPoint(EP));
        pm = new VaporTokenPaymaster(IEntryPoint(EP), address(this), IERC20(address(usdc)), treasury, 1_000);
        vm.deal(address(this), 100 ether);
        pm.deposit{value: 10 ether}();
        vm.prank(address(ep.senderCreator()));
        account = address(factory.createAccount(ownerW.addr, 0));
        usdc.mint(account, 100e6);
        vm.prank(account);
        usdc.approve(address(pm), type(uint256).max);
    }

    function _op() internal view returns (PackedUserOperation memory op) {
        op.sender = account;
        op.nonce = ep.getNonce(account, 0);
        op.callData = abi.encodeCall(BaseAccount.execute, (address(0xBEEF), 0, ""));
        op.accountGasLimits = bytes32(abi.encodePacked(uint128(300_000), uint128(100_000)));
        op.preVerificationGas = 50_000;
        op.gasFees = bytes32(abi.encodePacked(uint128(1 gwei), uint128(2 gwei)));
        op.paymasterAndData = abi.encodePacked(address(pm), uint128(150_000), uint128(80_000));
        bytes32 h = ep.getUserOpHash(op);
        (uint8 v, bytes32 r, bytes32 s) = vm.sign(ownerW.privateKey, h);
        op.signature = abi.encodePacked(r, s, v);
    }

    function test_UserPaysGasInUSDCAtProtocolPrice() public {
        // 1 USDC buys 1e21 wei of CREDIT (x/settle default price) -> 1000 CREDIT
        assertEq(pm.creditsPerToken(), 1e21);
        uint256 before = usdc.balanceOf(account);
        PackedUserOperation[] memory ops = new PackedUserOperation[](1);
        ops[0] = _op();
        vm.prank(bundler, bundler);
        ep.handleOps(ops, payable(bundler));
        uint256 spent = before - usdc.balanceOf(account);
        assertGt(spent, 0, "charged");
        assertEq(usdc.balanceOf(address(pm)), spent, "paymaster keeps exactly the charge after refund");
        // a ~200k gas op at 2 gwei costs ~4e14 wei = ~0.0000004 USDC -> 1 micro-unit, rounded up
        assertLe(spent, 2, "gas in USDC is fractions of a cent");
    }

    function test_RefillBuysCreditsAndDeposits() public {
        usdc.mint(address(pm), 5e6);
        uint256 dep = pm.getDeposit();
        vm.prank(address(pm));
        usdc.approve(SETTLE, type(uint256).max);
        pm.refill(5e6);
        assertEq(pm.getDeposit() - dep, 5e6 * 1e15, "5 USDC -> 5000 CREDIT deposited");
    }

    function testFuzz_TokenCostNeverUndercharges(uint96 gasCost) public view {
        uint256 cost = pm.tokenCost(gasCost);
        // value of charged USDC in credits must cover the gas plus markup
        assertGe(cost * pm.creditsPerToken() / 1e6, uint256(gasCost) * 11_000 / 10_000);
    }

    function test_MarkupCappedAndOwnerOnly() public {
        vm.expectRevert(VaporTokenPaymaster.MarkupTooHigh.selector);
        pm.setMarkup(2_001);
        vm.prank(makeAddr("stranger"));
        vm.expectRevert();
        pm.setMarkup(10);
    }

    function test_SweepOnlyToTreasury() public {
        usdc.mint(address(pm), 1e6);
        pm.sweep(1e6);
        assertEq(usdc.balanceOf(treasury), 1e6);
    }
}
