// Copyright (c) 2026 VaporChain / muthu2201. All rights reserved.
// Proprietary and confidential. See LICENSE at the repository root.
// Provenance: VAPOR-6eabb1be532bdef4

package app

import (
	"context"

	storetypes "github.com/cosmos/cosmos-sdk/store/v2/types"
	sdk "github.com/cosmos/cosmos-sdk/types"
	"github.com/cosmos/cosmos-sdk/types/module"
	upgradetypes "github.com/cosmos/cosmos-sdk/x/upgrade/types"
)

// Upgrade is one named on-chain upgrade.
type Upgrade struct {
	Name          string
	StoreUpgrades storetypes.StoreUpgrades
}

// Upgrades lists every software upgrade this binary can apply. Each entry is
// executed via a governance MsgSoftwareUpgrade -> 72h vote -> Cosmovisor swap.
// "v1.1.0" is the first planned upgrade slot; it runs module migrations only.
var Upgrades = []Upgrade{
	{Name: "v1.1.0", StoreUpgrades: storetypes.StoreUpgrades{}},
}

func (app *VaporApp) RegisterUpgradeHandlers() {
	for _, u := range Upgrades {
		name := u.Name
		app.UpgradeKeeper.SetUpgradeHandler(name,
			func(ctx context.Context, _ upgradetypes.Plan, fromVM module.VersionMap) (module.VersionMap, error) {
				sdk.UnwrapSDKContext(ctx).Logger().Info("running upgrade handler", "upgrade", name)
				return app.ModuleManager.RunMigrations(ctx, app.Configurator(), fromVM)
			},
		)
	}

	upgradeInfo, err := app.UpgradeKeeper.ReadUpgradeInfoFromDisk()
	if err != nil {
		panic(err)
	}
	if app.UpgradeKeeper.IsSkipHeight(upgradeInfo.Height) {
		return
	}
	for _, u := range Upgrades {
		if upgradeInfo.Name == u.Name {
			su := u.StoreUpgrades
			app.SetStoreLoader(upgradetypes.UpgradeStoreLoader(upgradeInfo.Height, &su))
		}
	}
}
