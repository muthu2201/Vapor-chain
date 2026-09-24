// Copyright (c) 2026 VaporChain / muthu2201. All rights reserved.
// Provenance: VAPOR-6eabb1be532bdef4

package settle

import (
	"cosmossdk.io/math"

	settletypes "github.com/muthu2201/vapor-chain/chain/x/settle/types"
)

// settleFee is the same pure function x/settle uses, so quotes are exact.
func settleFee(amount math.Int, asset settletypes.FeeAsset, feeBps uint32) math.Int {
	return settletypes.ComputeFee(amount, asset, feeBps)
}
