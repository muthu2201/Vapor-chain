// Copyright (c) 2026 VaporChain / muthu2201. All rights reserved.
// Derived from cosmos/evm evmd (Apache-2.0). See NOTICE.
// Provenance: VAPOR-6eabb1be532bdef4

package config

import (
	"maps"
	"sort"

	corevm "github.com/ethereum/go-ethereum/core/vm"

	cosmosevmutils "github.com/cosmos/evm/utils"
	erc20types "github.com/cosmos/evm/x/erc20/types"
	feemarkettypes "github.com/cosmos/evm/x/feemarket/types"
	vmtypes "github.com/cosmos/evm/x/vm/types"
	transfertypes "github.com/cosmos/ibc-go/v11/modules/apps/transfer/types"

	authtypes "github.com/cosmos/cosmos-sdk/x/auth/types"
	distrtypes "github.com/cosmos/cosmos-sdk/x/distribution/types"
	govtypes "github.com/cosmos/cosmos-sdk/x/gov/types"
	stakingtypes "github.com/cosmos/cosmos-sdk/x/staking/types"

	"github.com/muthu2201/vapor-chain/chain/constants"
	appstypes "github.com/muthu2201/vapor-chain/chain/x/apps/types"
	counciltypes "github.com/muthu2201/vapor-chain/chain/x/council/types"
	settletypes "github.com/muthu2201/vapor-chain/chain/x/settle/types"
)

// module account permissions. There is NO x/mint: VaporChain has zero
// inflation. The only minters are x/settle (gas credits, against payment),
// x/council (validator power, by governance), IBC transfer/erc20 (vouchers)
// and the EVM (balance mirroring).
var maccPerms = map[string][]string{
	authtypes.FeeCollectorName:     nil,
	distrtypes.ModuleName:          nil,
	transfertypes.ModuleName:       {authtypes.Minter, authtypes.Burner},
	stakingtypes.BondedPoolName:    {authtypes.Burner, authtypes.Staking},
	stakingtypes.NotBondedPoolName: {authtypes.Burner, authtypes.Staking},
	govtypes.ModuleName:            {authtypes.Burner},

	vmtypes.ModuleName:        {authtypes.Minter, authtypes.Burner},
	feemarkettypes.ModuleName: nil,
	erc20types.ModuleName:     {authtypes.Minter, authtypes.Burner},

	counciltypes.ModuleName: {authtypes.Minter},
	appstypes.ModuleName:    {authtypes.Burner},
	settletypes.ModuleName:  {authtypes.Minter, authtypes.Burner},
}

// GetMaccPerms returns a copy of the module account permissions.
func GetMaccPerms() map[string][]string { return maps.Clone(maccPerms) }

// StaticPrecompileAddresses is the ordered list of precompiles VaporChain
// activates. The staking, distribution, gov and slashing precompiles are
// deliberately NOT enabled: validator operations stay on the Cosmos side of a
// permissioned chain, which removes an EVM attack surface for free.
func StaticPrecompileAddresses() []string {
	return []string{
		vmtypes.P256PrecompileAddress,
		vmtypes.Bech32PrecompileAddress,
		vmtypes.ICS20PrecompileAddress,
		vmtypes.BankPrecompileAddress,
		vmtypes.ICS02PrecompileAddress,
		constants.SettlePrecompileAddress,
	}
}

// BlockedAddresses returns all addresses that may not receive funds through
// MsgSend/IBC: module accounts, geth precompiles and every static precompile
// (including ones VaporChain does not activate, in case governance enables
// them later).
func BlockedAddresses() map[string]bool {
	blocked := make(map[string]bool)
	accs := make([]string, 0, len(maccPerms))
	for acc := range maccPerms {
		accs = append(accs, acc)
	}
	sort.Strings(accs)
	for _, acc := range accs {
		blocked[authtypes.NewModuleAddress(acc).String()] = true
	}
	// copy before appending: never mutate the upstream global slice
	hexes := append([]string{}, vmtypes.AvailableStaticPrecompiles...)
	hexes = append(hexes, constants.SettlePrecompileAddress)
	for _, addr := range corevm.PrecompiledAddressesPrague {
		hexes = append(hexes, addr.Hex())
	}
	for _, h := range hexes {
		blocked[cosmosevmutils.Bech32StringFromHexAddress(h)] = true
	}
	return blocked
}
