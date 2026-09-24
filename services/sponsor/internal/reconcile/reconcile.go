// Copyright (c) 2026 VaporChain / muthu2201. All rights reserved.
// Proprietary and confidential. See LICENSE at the repository root.
// Provenance: VAPOR-6eabb1be532bdef4

// Package reconcile turns worst-case reservations into actual usage: it
// follows the paymaster's Sponsored events (gas actually burned) and releases
// reservations whose signatures expired unused.
package reconcile

import (
	"context"
	"log/slog"
	"math/big"
	"time"

	"github.com/ethereum/go-ethereum"
	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/crypto"
	"github.com/ethereum/go-ethereum/ethclient"

	"github.com/muthu2201/vapor-chain/services/sponsor/internal/store"
)

var sponsoredTopic = crypto.Keccak256Hash([]byte("Sponsored(uint64,address,bytes32,uint256,uint256)"))

type Reconciler struct {
	eth       *ethclient.Client
	store     *store.Store
	paymaster common.Address
	log       *slog.Logger
	maxRange  uint64
}

func New(eth *ethclient.Client, s *store.Store, paymaster common.Address, log *slog.Logger) *Reconciler {
	return &Reconciler{eth: eth, store: s, paymaster: paymaster, log: log, maxRange: 1000}
}

func (r *Reconciler) Run(ctx context.Context) {
	t := time.NewTicker(2 * time.Second)
	defer t.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-t.C:
			if err := r.step(ctx); err != nil {
				r.log.Warn("reconcile step failed", "err", err)
			}
			if n, err := r.store.ExpireUnused(ctx, time.Now().Unix()); err == nil && n > 0 {
				r.log.Info("released expired sponsorship reservations", "count", n)
			}
		}
	}
}

func (r *Reconciler) step(ctx context.Context) error {
	head, err := r.eth.BlockNumber(ctx)
	if err != nil {
		return err
	}
	from, err := r.store.Cursor(ctx, "sponsored")
	if err != nil {
		return err
	}
	if from == 0 {
		from = head
	}
	for from <= head {
		to := min(from+r.maxRange-1, head)
		logs, err := r.eth.FilterLogs(ctx, ethereum.FilterQuery{
			FromBlock: new(big.Int).SetUint64(from), ToBlock: new(big.Int).SetUint64(to),
			Addresses: []common.Address{r.paymaster}, Topics: [][]common.Hash{{sponsoredTopic}},
		})
		if err != nil {
			return err
		}
		for _, l := range logs {
			if len(l.Topics) < 3 || len(l.Data) < 64 {
				continue
			}
			appID := new(big.Int).SetBytes(l.Topics[1].Bytes()).Uint64()
			sender := common.BytesToAddress(l.Topics[2].Bytes())
			cost := new(big.Int).SetBytes(l.Data[:32])
			fee := new(big.Int).SetBytes(l.Data[32:64])
			var gas uint64
			if fee.Sign() > 0 {
				gas = new(big.Int).Div(cost, fee).Uint64()
			}
			if err := r.store.Settle(ctx, appID, sender.Bytes(), gas); err != nil {
				return err
			}
		}
		if err := r.store.SetCursor(ctx, "sponsored", to+1); err != nil {
			return err
		}
		from = to + 1
	}
	return nil
}
