// SPDX-License-Identifier: AGPL-3.0-only
// Copyright (c) 2026 VaporChain / muthu2201
// Provenance: VAPOR-6eabb1be532bdef4

package main

import (
	"fmt"
	"os"

	svrcmd "github.com/cosmos/cosmos-sdk/server/cmd"
	sdk "github.com/cosmos/cosmos-sdk/types"

	vaporconfig "github.com/muthu2201/vapor-chain/chain/app/config"
	"github.com/muthu2201/vapor-chain/chain/cmd/vaporchaind/cmd"
	"github.com/muthu2201/vapor-chain/chain/constants"
)

func main() {
	cfg := sdk.GetConfig()
	vaporconfig.SetBech32Prefixes(cfg)
	vaporconfig.SetBip44CoinType(cfg)
	cfg.Seal()

	rootCmd := cmd.NewRootCmd()
	if err := svrcmd.Execute(rootCmd, "VAPORCHAIND", vaporconfig.MustGetDefaultNodeHome()); err != nil {
		fmt.Fprintln(rootCmd.OutOrStderr(), err)
		os.Exit(1)
	}
	_ = constants.BinaryName
}
