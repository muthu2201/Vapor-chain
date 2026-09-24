// Copyright (c) 2026 VaporChain / muthu2201. All rights reserved.
// Proprietary and confidential. See LICENSE at the repository root.
// Provenance: VAPOR-6eabb1be532bdef4

// Package constants holds every chain-wide identifier in one place so that a
// network (localnet / testnet / mainnet) is fully described by a handful of
// values. Keeping them here (instead of scattered literals) is what lets the
// genesis builder, the CLI, the precompile and the tests agree byte-for-byte.
package constants

const (
	// AppName is the ABCI application name reported by the node.
	AppName = "vaporchain"
	// BinaryName is the daemon binary.
	BinaryName = "vaporchaind"
	// DefaultHomeDirName is the node home under $HOME.
	DefaultHomeDirName = ".vaporchaind"

	// Bech32Prefix is the account prefix (vapor1...). EVM hex addresses and
	// bech32 addresses share the same 20 bytes (eth_secp256k1 keys).
	Bech32Prefix = "vapor"

	// CreditDenom is the 18-decimal native gas token ("gas credit"). It is the
	// EVM denom. It is minted ONLY by x/settle at a governance-fixed USDC
	// price, is not redeemable, and has bank SendEnabled=false so it can never
	// leave the chain over IBC nor be moved with MsgSend. EVM value transfers
	// (needed for ERC-4337 EntryPoint deposits) still work because the EVM
	// mints/burns balances directly rather than going through MsgSend.
	CreditDenom        = "acredit"
	CreditDisplayDenom = "credit"
	CreditSymbol       = "CREDIT"
	CreditDecimals     = 18

	// PowerDenom is the non-transferable validator-power token used as the
	// x/staking bond denom. It turns Apache-2.0 x/staking into a Proof-of-
	// Authority set: only x/council (via governance) can mint it, and it has
	// SendEnabled=false so it cannot be bought, sold or bridged.
	PowerDenom        = "avpower"
	PowerDisplayDenom = "vpower"
	PowerDecimals     = 18

	// EVM (EIP-155) chain IDs. Checked against chainid.network on 2026-09-24:
	// none of these collide with a registered chain.
	MainnetEVMChainID  uint64 = 7797
	TestnetEVMChainID  uint64 = 77970
	LocalnetEVMChainID uint64 = 779700

	// Cosmos (CometBFT) chain IDs.
	MainnetChainID  = "vaporchain-1"
	TestnetChainID  = "vapor-testnet-1"
	LocalnetChainID = "vapor-local-1"

	// SettlePrecompileAddress is the native Settle precompile. 0x0900 sits
	// outside every range used by go-ethereum (0x01-0x11, 0x0100 P256) and
	// cosmos/evm (0x0400, 0x0800-0x0807), verified in app/precompiles.go.
	SettlePrecompileAddress = "0x0000000000000000000000000000000000000900"
)

// EVMChainIDForChain maps a Cosmos chain-id to its EVM chain id so that a node
// can never be started with a mismatched pair (a classic replay foot-gun).
func EVMChainIDForChain(chainID string) (uint64, bool) {
	switch chainID {
	case MainnetChainID:
		return MainnetEVMChainID, true
	case TestnetChainID:
		return TestnetEVMChainID, true
	case LocalnetChainID:
		return LocalnetEVMChainID, true
	default:
		return 0, false
	}
}
