// SPDX-License-Identifier: AGPL-3.0-only
// Copyright (c) 2026 VaporChain / muthu2201
// Provenance: VAPOR-6eabb1be532bdef4

// Package ingest follows the chain block by block. CometBFT has single-slot
// finality, so a committed height never reorgs: the indexer only has to be
// ordered and idempotent (ON CONFLICT DO NOTHING + cursor in the same tx).
package ingest

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"log/slog"
	"math/big"
	"net/http"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/common/hexutil"
	"github.com/ethereum/go-ethereum/core/types"
	"github.com/ethereum/go-ethereum/crypto"
	"github.com/ethereum/go-ethereum/rpc"
	"github.com/jackc/pgx/v5"
	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/promauto"

	"github.com/muthu2201/vapor-chain/services/indexer/internal/db"
)

var (
	mHeight = promauto.NewGauge(prometheus.GaugeOpts{Name: "vapor_indexer_height", Help: "Last indexed height"})
	mLag    = promauto.NewGauge(prometheus.GaugeOpts{Name: "vapor_indexer_lag_blocks", Help: "Chain head minus indexed height"})
	mMargin = promauto.NewGauge(prometheus.GaugeOpts{Name: "vapor_indexer_retention_margin_blocks", Help: "Indexed height minus the source node's earliest retained block; alert when small (data would be pruned before indexing)"})
	mBlocks = promauto.NewCounter(prometheus.CounterOpts{Name: "vapor_indexer_blocks_total", Help: "Blocks indexed"})
	mSettle = promauto.NewCounter(prometheus.CounterOpts{Name: "vapor_indexer_settlements_total", Help: "Settlement events indexed"})
)

var sponsoredTopic = crypto.Keccak256Hash([]byte("Sponsored(uint64,address,bytes32,uint256,uint256)"))

type Config struct {
	EVMRPC    string
	CometRPC  string
	Paymaster common.Address
	Batch     int
	Workers   int
	StartAt   int64
}

type Ingester struct {
	cfg  Config
	db   *db.DB
	rpc  *rpc.Client
	http *http.Client
	log  *slog.Logger
}

func New(cfg Config, d *db.DB, log *slog.Logger) (*Ingester, error) {
	c, err := rpc.Dial(cfg.EVMRPC)
	if err != nil {
		return nil, err
	}
	if cfg.Batch <= 0 {
		cfg.Batch = 50
	}
	if cfg.Workers <= 0 {
		cfg.Workers = 8
	}
	return &Ingester{cfg: cfg, db: d, rpc: c, http: &http.Client{Timeout: 15 * time.Second}, log: log}, nil
}

// --- CometBFT RPC

type cometEvent struct {
	Type       string `json:"type"`
	Attributes []struct {
		Key   string `json:"key"`
		Value string `json:"value"`
	} `json:"attributes"`
}

func (e cometEvent) attrs() map[string]string {
	m := make(map[string]string, len(e.Attributes))
	for _, a := range e.Attributes {
		m[a.Key] = a.Value
	}
	return m
}

func (in *Ingester) comet(ctx context.Context, path string, out any) error {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, strings.TrimRight(in.cfg.CometRPC, "/")+path, nil)
	if err != nil {
		return err
	}
	resp, err := in.http.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	body, err := io.ReadAll(io.LimitReader(resp.Body, 256<<20))
	if err != nil {
		return err
	}
	var env struct {
		Result json.RawMessage `json:"result"`
		Error  json.RawMessage `json:"error"`
	}
	if err := json.Unmarshal(body, &env); err != nil {
		return err
	}
	if len(env.Error) > 0 && string(env.Error) != "null" {
		return fmt.Errorf("comet %s: %s", path, env.Error)
	}
	return json.Unmarshal(env.Result, out)
}

type status struct {
	SyncInfo struct {
		Latest   string `json:"latest_block_height"`
		Earliest string `json:"earliest_block_height"`
	} `json:"sync_info"`
}

func (in *Ingester) heads(ctx context.Context) (latest, earliest int64, err error) {
	var s status
	if err = in.comet(ctx, "/status", &s); err != nil {
		return
	}
	latest, _ = strconv.ParseInt(s.SyncInfo.Latest, 10, 64)
	earliest, _ = strconv.ParseInt(s.SyncInfo.Earliest, 10, 64)
	return
}

// --- per-block fetch

type rpcBlock struct {
	Number       hexutil.Uint64 `json:"number"`
	Hash         common.Hash    `json:"hash"`
	Timestamp    hexutil.Uint64 `json:"timestamp"`
	GasUsed      hexutil.Uint64 `json:"gasUsed"`
	GasLimit     hexutil.Uint64 `json:"gasLimit"`
	BaseFee      *hexutil.Big   `json:"baseFeePerGas"`
	Miner        common.Address `json:"miner"`
	Transactions []rpcTx        `json:"transactions"`
}

type rpcTx struct {
	Hash  common.Hash     `json:"hash"`
	From  common.Address  `json:"from"`
	To    *common.Address `json:"to"`
	Value *hexutil.Big    `json:"value"`
	Type  hexutil.Uint64  `json:"type"`
	Index hexutil.Uint64  `json:"transactionIndex"`
}

type fetched struct {
	height   int64
	time     time.Time
	block    rpcBlock
	receipts []*types.Receipt
	events   []cometEvent
}

func (in *Ingester) fetch(ctx context.Context, h int64) (*fetched, error) {
	f := &fetched{height: h}
	if err := in.rpc.CallContext(ctx, &f.block, "eth_getBlockByNumber", hexutil.EncodeUint64(uint64(h)), true); err != nil {
		return nil, fmt.Errorf("block %d: %w", h, err)
	}
	if err := in.rpc.CallContext(ctx, &f.receipts, "eth_getBlockReceipts", hexutil.EncodeUint64(uint64(h))); err != nil {
		return nil, fmt.Errorf("receipts %d: %w", h, err)
	}
	var br struct {
		TxsResults []struct {
			Events []cometEvent `json:"events"`
		} `json:"txs_results"`
		FinalizeBlockEvents []cometEvent `json:"finalize_block_events"`
	}
	if err := in.comet(ctx, fmt.Sprintf("/block_results?height=%d", h), &br); err != nil {
		return nil, fmt.Errorf("block_results %d: %w", h, err)
	}
	for _, t := range br.TxsResults {
		f.events = append(f.events, t.Events...)
	}
	f.events = append(f.events, br.FinalizeBlockEvents...)
	var hdr struct {
		Header struct {
			Time time.Time `json:"time"`
		} `json:"header"`
	}
	if err := in.comet(ctx, fmt.Sprintf("/header?height=%d", h), &hdr); err != nil {
		f.time = time.Unix(int64(f.block.Timestamp), 0).UTC()
	} else {
		f.time = hdr.Header.Time
	}
	return f, nil
}

func num(s string) string {
	if _, ok := new(big.Int).SetString(s, 10); ok {
		return s
	}
	return "0"
}

// appEventKinds are Cosmos events recorded for dashboards/audit.
var appEventKinds = map[string]bool{
	"app_registered": true, "app_updated": true, "app_contract_bound": true, "app_contract_move_scheduled": true,
	"app_status_changed": true, "app_ownership_transferred": true, "apps_epoch_rollover": true,
	"app_bonded": true, "app_unbonding": true, "app_unbonded": true,
	"settle_revenue_claimed": true, "settle_credits_bought": true, "settle_validator_payout": true,
	"settle_treasury_withdrawn": true, "settle_relayer_disbursed": true, "settle_invariant_broken": true,
	"settle_tab_closed": true, "council_pause": true, "council_unpause": true, "council_admit_validator": true,
	"council_remove_validator": true, "council_pause_expired": true,
}

func (in *Ingester) write(ctx context.Context, batch []*fetched) error {
	return pgx.BeginFunc(ctx, in.db.Pool, func(tx pgx.Tx) error {
		for _, f := range batch {
			b := f.block
			var baseFee *string
			if b.BaseFee != nil {
				s := b.BaseFee.ToInt().String()
				baseFee = &s
			}
			if _, err := tx.Exec(ctx, `INSERT INTO blocks(height,hash,time,tx_count,gas_used,gas_limit,base_fee,proposer)
				VALUES ($1,$2,$3,$4,$5,$6,$7,$8) ON CONFLICT DO NOTHING`,
				f.height, b.Hash.Bytes(), f.time, len(b.Transactions), int64(b.GasUsed), int64(b.GasLimit), baseFee, b.Miner.Bytes()); err != nil {
				return err
			}
			rcpt := map[common.Hash]*types.Receipt{}
			for _, r := range f.receipts {
				rcpt[r.TxHash] = r
			}
			for _, t := range b.Transactions {
				r := rcpt[t.Hash]
				var gasUsed int64
				var st int16
				var contract []byte
				if r != nil {
					gasUsed = int64(r.GasUsed)
					st = int16(r.Status)
					if r.ContractAddress != (common.Address{}) {
						contract = r.ContractAddress.Bytes()
					}
				}
				var to []byte
				if t.To != nil {
					to = t.To.Bytes()
				}
				val := "0"
				if t.Value != nil {
					val = t.Value.ToInt().String()
				}
				if _, err := tx.Exec(ctx, `INSERT INTO evm_txs(hash,height,idx,sender,recipient,value,gas_used,status,tx_type,contract)
					VALUES ($1,$2,$3,$4,$5,$6,$7,$8,$9,$10) ON CONFLICT DO NOTHING`,
					t.Hash.Bytes(), f.height, int(t.Index), t.From.Bytes(), to, val, gasUsed, st, int16(t.Type), contract); err != nil {
					return err
				}
			}
			for _, r := range f.receipts {
				for _, l := range r.Logs {
					var tp [4][]byte
					for i := 0; i < len(l.Topics) && i < 4; i++ {
						tp[i] = l.Topics[i].Bytes()
					}
					if _, err := tx.Exec(ctx, `INSERT INTO evm_logs(height,tx_hash,log_index,address,topic0,topic1,topic2,topic3,data)
						VALUES ($1,$2,$3,$4,$5,$6,$7,$8,$9) ON CONFLICT DO NOTHING`,
						f.height, l.TxHash.Bytes(), int(l.Index), l.Address.Bytes(), tp[0], tp[1], tp[2], tp[3], l.Data); err != nil {
						return err
					}
					if l.Address == in.cfg.Paymaster && len(l.Topics) == 4 && l.Topics[0] == sponsoredTopic && len(l.Data) >= 64 {
						if _, err := tx.Exec(ctx, `INSERT INTO sponsored_ops(height,log_index,app_id,sender,user_op_hash,gas_cost,fee_per_gas)
							VALUES ($1,$2,$3,$4,$5,$6,$7) ON CONFLICT DO NOTHING`,
							f.height, int(l.Index), new(big.Int).SetBytes(l.Topics[1].Bytes()).Int64(),
							common.BytesToAddress(l.Topics[2].Bytes()).Bytes(), l.Topics[3].Bytes(),
							new(big.Int).SetBytes(l.Data[:32]).String(), new(big.Int).SetBytes(l.Data[32:64]).String()); err != nil {
							return err
						}
					}
				}
			}
			for seq, ev := range f.events {
				a := ev.attrs()
				switch {
				case ev.Type == "settled":
					appID, _ := strconv.ParseInt(a["app_id"], 10, 64)
					if _, err := tx.Exec(ctx, `INSERT INTO settlements(height,seq,time,app_id,payer,payee,denom,amount,fee,net,app_share,referrer,referrer_share,validator_share,relayer_share,treasury_share)
						VALUES ($1,$2,$3,$4,$5,$6,$7,$8,$9,$10,$11,$12,$13,$14,$15,$16) ON CONFLICT DO NOTHING`,
						f.height, seq, f.time, appID, a["payer"], a["payee"], a["denom"], num(a["amount"]), num(a["fee"]), num(a["net"]),
						num(a["app_share"]), a["referrer"], num(a["referrer_share"]), num(a["validator_share"]), num(a["relayer_share"]), num(a["treasury_share"])); err != nil {
						return err
					}
					mSettle.Inc()
				case appEventKinds[ev.Type]:
					var appID *int64
					if v, err := strconv.ParseInt(a["app_id"], 10, 64); err == nil {
						appID = &v
					}
					js, _ := json.Marshal(a)
					if _, err := tx.Exec(ctx, `INSERT INTO app_events(height,seq,kind,app_id,attrs) VALUES ($1,$2,$3,$4,$5) ON CONFLICT DO NOTHING`,
						f.height, seq, ev.Type, appID, js); err != nil {
						return err
					}
				}
			}
		}
		return db.SetCursor(ctx, tx, "blocks", batch[len(batch)-1].height)
	})
}

// Run indexes forever (until ctx is cancelled).
func (in *Ingester) Run(ctx context.Context) error {
	cur, err := in.db.Cursor(ctx, "blocks")
	if err != nil {
		return err
	}
	next := cur + 1
	for ctx.Err() == nil {
		latest, earliest, err := in.heads(ctx)
		if err != nil {
			in.log.Warn("status failed", "err", err)
			time.Sleep(time.Second)
			continue
		}
		if next == 1 {
			next = max(in.cfg.StartAt, earliest, 1)
		}
		if next < earliest {
			// history already pruned on the source node: page loudly, skip ahead
			in.log.Error("DATA GAP: source node pruned blocks not yet indexed; point VAPOR_COMET_RPC at a history node or the archive",
				"missing_from", next, "missing_to", earliest-1)
			next = earliest
		}
		mLag.Set(float64(latest - next + 1))
		if next > latest {
			time.Sleep(400 * time.Millisecond)
			continue
		}
		end := min(next+int64(in.cfg.Batch)-1, latest)
		batch := make([]*fetched, end-next+1)
		var wg sync.WaitGroup
		var firstErr error
		var mu sync.Mutex
		sem := make(chan struct{}, in.cfg.Workers)
		for h := next; h <= end; h++ {
			wg.Add(1)
			sem <- struct{}{}
			go func(h int64) {
				defer wg.Done()
				defer func() { <-sem }()
				f, err := in.fetch(ctx, h)
				mu.Lock()
				defer mu.Unlock()
				if err != nil && firstErr == nil {
					firstErr = err
				}
				batch[h-next] = f
			}(h)
		}
		wg.Wait()
		if firstErr != nil {
			in.log.Warn("fetch failed; retrying", "err", firstErr)
			time.Sleep(time.Second)
			continue
		}
		if err := in.write(ctx, batch); err != nil {
			in.log.Error("write failed; retrying", "err", err)
			time.Sleep(time.Second)
			continue
		}
		mBlocks.Add(float64(len(batch)))
		mHeight.Set(float64(end))
		mMargin.Set(float64(end - earliest))
		next = end + 1
	}
	return ctx.Err()
}
