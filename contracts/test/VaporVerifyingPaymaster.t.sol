// SPDX-License-Identifier: LicenseRef-VaporChain-Proprietary
// Copyright (c) 2026 VaporChain / muthu2201. All rights reserved.
// Provenance: VAPOR-6eabb1be532bdef4
pragma solidity ^0.8.28;

import {Test, Vm} from "forge-std/Test.sol";
import {EntryPoint} from "account-abstraction/core/EntryPoint.sol";
import {IEntryPoint} from "account-abstraction/interfaces/IEntryPoint.sol";
import {IPaymaster} from "account-abstraction/interfaces/IPaymaster.sol";
import {PackedUserOperation} from "account-abstraction/interfaces/PackedUserOperation.sol";
import {SimpleAccountFactory} from "account-abstraction/accounts/SimpleAccountFactory.sol";
import {BaseAccount} from "account-abstraction/core/BaseAccount.sol";
import {Simple7702Account} from "account-abstraction/accounts/Simple7702Account.sol";
import {MessageHashUtils} from "@openzeppelin/contracts/utils/cryptography/MessageHashUtils.sol";

import {VaporVerifyingPaymaster} from "../src/paymaster/VaporVerifyingPaymaster.sol";
import {VaporPaymasterBase} from "../src/paymaster/VaporPaymasterBase.sol";

contract Counter {
    uint256 public n;

    function inc() external {
        n++;
    }
}

contract VaporVerifyingPaymasterTest is Test {
    address constant CANONICAL_EP = 0x4337084D9E255Ff0702461CF8895CE9E3b5Ff108;

    EntryPoint ep;
    VaporVerifyingPaymaster pm;
    SimpleAccountFactory factory;
    Simple7702Account impl7702;
    Counter counter;

    Vm.Wallet signerW = vm.createWallet("sponsor-signer");
    Vm.Wallet ownerW = vm.createWallet("account-owner");
    address payable treasury = payable(makeAddr("treasury"));
    address pmOwner = makeAddr("pm-owner");
    address bundler = makeAddr("bundler");

    function setUp() public {
        // the real EntryPoint v0.8, placed at its canonical address
        deployCodeTo("EntryPoint.sol:EntryPoint", CANONICAL_EP);
        ep = EntryPoint(payable(CANONICAL_EP));
        factory = new SimpleAccountFactory(IEntryPoint(CANONICAL_EP));
        impl7702 = new Simple7702Account();
        counter = new Counter();
        address[] memory delegates = new address[](1);
        delegates[0] = address(impl7702);
        pm = new VaporVerifyingPaymaster(IEntryPoint(CANONICAL_EP), pmOwner, signerW.addr, treasury, delegates);
        vm.deal(address(this), 1000 ether);
        pm.deposit{value: 100 ether}();
        vm.deal(bundler, 1 ether);
    }

    // ----------------------------------------------------------- helpers

    function _pmData(uint48 until, uint48 after_, uint64 appId, bytes memory sig) internal view returns (bytes memory) {
        return abi.encodePacked(address(pm), uint128(200_000), uint128(50_000), until, after_, appId, sig);
    }

    function _op(address sender, bytes memory initCode, bytes memory callData) internal view returns (PackedUserOperation memory op) {
        op.sender = sender;
        op.nonce = ep.getNonce(sender, 0);
        op.initCode = initCode;
        op.callData = callData;
        op.accountGasLimits = bytes32(abi.encodePacked(uint128(500_000), uint128(200_000)));
        op.preVerificationGas = 60_000;
        op.gasFees = bytes32(abi.encodePacked(uint128(1 gwei), uint128(2 gwei)));
    }

    function _sponsor(PackedUserOperation memory op, uint48 until, uint48 after_, uint64 appId, uint256 key)
        internal
        view
        returns (PackedUserOperation memory)
    {
        op.paymasterAndData = _pmData(until, after_, appId, new bytes(65));
        bytes32 h = MessageHashUtils.toEthSignedMessageHash(pm.getHash(op, until, after_, appId));
        (uint8 v, bytes32 r, bytes32 s) = vm.sign(key, h);
        op.paymasterAndData = _pmData(until, after_, appId, abi.encodePacked(r, s, v));
        return op;
    }

    function _signAccount(PackedUserOperation memory op, uint256 key) internal view returns (PackedUserOperation memory) {
        bytes32 h = ep.getUserOpHash(op);
        (uint8 v, bytes32 r, bytes32 s) = vm.sign(key, h);
        op.signature = abi.encodePacked(r, s, v);
        return op;
    }

    function _handle(PackedUserOperation memory op) internal {
        PackedUserOperation[] memory ops = new PackedUserOperation[](1);
        ops[0] = op;
        vm.prank(bundler, bundler);
        ep.handleOps(ops, payable(bundler));
    }

    function _newAccountOp() internal view returns (PackedUserOperation memory op, address sender) {
        sender = factory.getAddress(ownerW.addr, 0);
        bytes memory initCode = abi.encodePacked(address(factory), abi.encodeCall(factory.createAccount, (ownerW.addr, 0)));
        bytes memory call = abi.encodeCall(BaseAccount.execute, (address(counter), 0, abi.encodeCall(Counter.inc, ())));
        op = _op(sender, initCode, call);
    }

    // ------------------------------------------------------------- tests

    function test_SponsorsNewAccountDeploymentAndCall() public {
        (PackedUserOperation memory op,) = _newAccountOp();
        op = _sponsor(op, uint48(block.timestamp + 600), 0, 7, signerW.privateKey);
        op = _signAccount(op, ownerW.privateKey);
        uint256 depBefore = pm.getDeposit();
        vm.expectEmit(true, true, false, false, address(pm));
        emit VaporVerifyingPaymaster.Sponsored(7, op.sender, bytes32(0), 0, 0);
        _handle(op);
        assertEq(counter.n(), 1, "call executed");
        assertLt(pm.getDeposit(), depBefore, "paymaster paid");
    }

    function test_RejectsForeignSigner() public {
        (PackedUserOperation memory op,) = _newAccountOp();
        Vm.Wallet memory evil = vm.createWallet("evil");
        op = _sponsor(op, uint48(block.timestamp + 600), 0, 7, evil.privateKey);
        op = _signAccount(op, ownerW.privateKey);
        vm.expectRevert(abi.encodeWithSelector(IEntryPoint.FailedOp.selector, 0, "AA34 signature error"));
        _handle(op);
    }

    function test_RejectsExpiredSponsorship() public {
        vm.warp(1_000_000);
        (PackedUserOperation memory op,) = _newAccountOp();
        op = _sponsor(op, uint48(block.timestamp - 1), 0, 7, signerW.privateKey);
        op = _signAccount(op, ownerW.privateKey);
        vm.expectRevert(abi.encodeWithSelector(IEntryPoint.FailedOp.selector, 0, "AA32 paymaster expired or not due"));
        _handle(op);
    }

    function test_SignatureNotReplayableAcrossChains() public {
        (PackedUserOperation memory op,) = _newAccountOp();
        op = _sponsor(op, uint48(block.timestamp + 600), 0, 7, signerW.privateKey);
        op = _signAccount(op, ownerW.privateKey);
        vm.chainId(1); // same op, other chain
        op = _signAccount(op, ownerW.privateKey); // account re-signs; sponsor sig stays
        vm.expectRevert(abi.encodeWithSelector(IEntryPoint.FailedOp.selector, 0, "AA34 signature error"));
        _handle(op);
    }

    function test_TamperedCallDataInvalidatesSponsorship() public {
        (PackedUserOperation memory op,) = _newAccountOp();
        op = _sponsor(op, uint48(block.timestamp + 600), 0, 7, signerW.privateKey);
        op.callData = abi.encodeCall(BaseAccount.execute, (address(0xdead), 1 ether, ""));
        op = _signAccount(op, ownerW.privateKey);
        vm.expectRevert(abi.encodeWithSelector(IEntryPoint.FailedOp.selector, 0, "AA34 signature error"));
        _handle(op);
    }

    function test_RevokeIsInstantRotationIsTimelocked() public {
        vm.prank(pmOwner);
        pm.revokeSigner();
        (PackedUserOperation memory op,) = _newAccountOp();
        op = _sponsor(op, uint48(block.timestamp + 600), 0, 7, signerW.privateKey);
        op = _signAccount(op, ownerW.privateKey);
        vm.expectRevert(abi.encodeWithSelector(IEntryPoint.FailedOp.selector, 0, "AA34 signature error"));
        _handle(op);

        Vm.Wallet memory next = vm.createWallet("next");
        vm.startPrank(pmOwner);
        pm.proposeSigner(next.addr);
        vm.expectRevert(VaporVerifyingPaymaster.SignerNotReady.selector);
        pm.acceptSigner();
        vm.warp(block.timestamp + 48 hours);
        pm.acceptSigner();
        vm.stopPrank();
        assertEq(pm.signer(), next.addr);
    }

    function test_ZeroSignerRejectedAndRotationCancellable() public {
        address[] memory none = new address[](0);
        vm.expectRevert(VaporVerifyingPaymaster.ZeroSigner.selector);
        new VaporVerifyingPaymaster(IEntryPoint(CANONICAL_EP), pmOwner, address(0), treasury, none);

        vm.startPrank(pmOwner);
        vm.expectRevert(VaporVerifyingPaymaster.ZeroSigner.selector);
        pm.proposeSigner(address(0));

        // a rotation started by a leaked key can be aborted before it lands
        pm.proposeSigner(makeAddr("rogue"));
        pm.cancelSignerRotation();
        vm.warp(block.timestamp + 48 hours);
        vm.expectRevert(VaporVerifyingPaymaster.SignerNotReady.selector);
        pm.acceptSigner();
        vm.stopPrank();
        assertEq(pm.signer(), signerW.addr);

        vm.expectRevert();
        pm.cancelSignerRotation(); // not owner
    }

    function test_OnlyOwnerAdmin() public {
        vm.expectRevert();
        pm.revokeSigner();
        vm.expectRevert();
        pm.proposeSigner(address(1));
        vm.expectRevert();
        pm.setDelegateAllowed(address(1), true);
    }

    function test_WithdrawalsOnlyReachTreasury() public {
        vm.prank(pmOwner);
        pm.withdrawDeposit(10 ether);
        assertEq(treasury.balance, 10 ether);
        vm.expectRevert(); // not owner
        pm.withdrawDeposit(1 ether);
    }

    function test_ValidateOnlyFromEntryPoint() public {
        (PackedUserOperation memory op,) = _newAccountOp();
        vm.expectRevert(VaporPaymasterBase.NotFromEntryPoint.selector);
        pm.validatePaymasterUserOp(op, bytes32(0), 1);
        vm.expectRevert(VaporPaymasterBase.NotFromEntryPoint.selector);
        pm.postOp(IPaymaster.PostOpMode.opSucceeded, abi.encode(uint64(1), address(1), bytes32(0)), 0, 0);
    }

    // ------------------------------------------------------------ 7702

    function _op7702(Vm.Wallet memory eoa) internal view returns (PackedUserOperation memory op) {
        bytes memory call = abi.encodeCall(BaseAccount.execute, (address(counter), 0, abi.encodeCall(Counter.inc, ())));
        op = _op(eoa.addr, "", call);
    }

    function test_Sponsors7702DelegatedEOA() public {
        Vm.Wallet memory eoa = vm.createWallet("eoa-7702");
        vm.signAndAttachDelegation(address(impl7702), eoa.privateKey);
        // Simple7702Account hardcodes the canonical EntryPoint
        assertEq(address(Simple7702Account(payable(eoa.addr)).entryPoint()), CANONICAL_EP);
        PackedUserOperation memory op = _op7702(eoa);
        op = _sponsor(op, uint48(block.timestamp + 600), 0, 9, signerW.privateKey);
        op = _signAccount(op, eoa.privateKey);
        _handle(op);
        assertEq(counter.n(), 1);
    }

    function test_RefusesEOADelegatedToUnlistedImplementation() public {
        Simple7702Account rogue = new Simple7702Account(); // same code, NOT allowlisted
        Vm.Wallet memory eoa = vm.createWallet("eoa-rogue");
        vm.signAndAttachDelegation(address(rogue), eoa.privateKey);
        PackedUserOperation memory op = _op7702(eoa);
        op = _sponsor(op, uint48(block.timestamp + 600), 0, 9, signerW.privateKey);
        op = _signAccount(op, eoa.privateKey);
        vm.expectRevert(abi.encodeWithSelector(IEntryPoint.FailedOp.selector, 0, "AA34 signature error"));
        _handle(op);
    }

    function testFuzz_ParsePaymasterData(uint48 until, uint48 after_, uint64 appId, bytes32 r, bytes32 s, uint8 v) public view {
        bytes memory data = _pmData(until, after_, appId, abi.encodePacked(r, s, v));
        (uint48 u, uint48 a, uint64 id, bytes memory sig) = pm.parsePaymasterData(data);
        assertEq(u, until);
        assertEq(a, after_);
        assertEq(id, appId);
        assertEq(sig, abi.encodePacked(r, s, v));
    }
}
