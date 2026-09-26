// SPDX-License-Identifier: Apache-2.0 AND AGPL-3.0-only
// Copyright (c) 2026 VaporChain / muthu2201
// Derived from cosmos/evm evmd v0.7.3 (Apache-2.0, Cosmos Labs); see NOTICE.
// Provenance: VAPOR-6eabb1be532bdef4

package config

import (
	sdk "github.com/cosmos/cosmos-sdk/types"

	"github.com/cosmos/evm/crypto/hd"

	"github.com/muthu2201/vapor-chain/chain/constants"
)

const (
	Bech32PrefixAccAddr  = constants.Bech32Prefix
	Bech32PrefixAccPub   = constants.Bech32Prefix + sdk.PrefixPublic
	Bech32PrefixValAddr  = constants.Bech32Prefix + sdk.PrefixValidator + sdk.PrefixOperator
	Bech32PrefixValPub   = constants.Bech32Prefix + sdk.PrefixValidator + sdk.PrefixOperator + sdk.PrefixPublic
	Bech32PrefixConsAddr = constants.Bech32Prefix + sdk.PrefixValidator + sdk.PrefixConsensus
	Bech32PrefixConsPub  = constants.Bech32Prefix + sdk.PrefixValidator + sdk.PrefixConsensus + sdk.PrefixPublic
)

// SetBech32Prefixes sets vapor1..., vaporvaloper1..., vaporvalcons1... prefixes.
func SetBech32Prefixes(config *sdk.Config) {
	config.SetBech32PrefixForAccount(Bech32PrefixAccAddr, Bech32PrefixAccPub)
	config.SetBech32PrefixForValidator(Bech32PrefixValAddr, Bech32PrefixValPub)
	config.SetBech32PrefixForConsensusNode(Bech32PrefixConsAddr, Bech32PrefixConsPub)
}

// SetBip44CoinType uses coin type 60 so MetaMask, Rabby and Ledger ETH app
// derive the same address as the CLI from the same mnemonic.
func SetBip44CoinType(config *sdk.Config) {
	config.SetCoinType(hd.Bip44CoinType)
	config.SetPurpose(sdk.Purpose)
	config.SetFullFundraiserPath(hd.BIP44HDPath) //nolint:staticcheck
}
