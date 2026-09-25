// Copyright (c) 2026 VaporChain / muthu2201. All rights reserved.
// Proprietary and confidential. See LICENSE at the repository root.
// Provenance: VAPOR-6eabb1be532bdef4

// loadgen is VaporChain's end-to-end stress and adversarial traffic generator.
// It signs real EIP-1559 transactions with deterministic keys (derived from a
// seed so the genesis builder can pre-fund them), fans them out over several
// JSON-RPC endpoints at a target rate, and measures what the CHAIN actually
// did: inclusion rate, TPS, send->inclusion latency percentiles, block time,
// block gas utilization and, for lane tests, the sponsored share per block.
//
//	loadgen accounts --count 500 --seed vapor-load --hrp vapor
//	loadgen run --rpc http://127.0.0.1:8545,http://127.0.0.1:8555 \
//	    --scenario mixed --accounts 500 --rate 400 --duration 60s --token 0x...
package main

import (
	"context"
	"crypto/ecdsa"
	"crypto/sha256"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"math/big"
	"net/http"
	"os"
	"sort"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"github.com/btcsuite/btcd/btcutil/bech32"
	"github.com/ethereum/go-ethereum/accounts/abi"
	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/core/types"
	"github.com/ethereum/go-ethereum/crypto"
	"github.com/ethereum/go-ethereum/ethclient"
	"github.com/ethereum/go-ethereum/rpc"
)

const settleAddr = "0x0000000000000000000000000000000000000900"

func main() {
	if len(os.Args) < 2 {
		fmt.Fprintln(os.Stderr, "usage: loadgen {accounts|run} [flags]")
		os.Exit(2)
	}
	switch os.Args[1] {
	case "accounts":
		cmdAccounts(os.Args[2:])
	case "run":
		cmdRun(os.Args[2:])
	default:
		fmt.Fprintln(os.Stderr, "unknown command", os.Args[1])
		os.Exit(2)
	}
}

// deriveKey returns the i-th deterministic key for a seed. NEVER use this
// scheme for real funds: it exists so localnet genesis can fund load accounts.
func deriveKey(seed string, i int) *ecdsa.PrivateKey {
	for ctr := 0; ; ctr++ {
		h := sha256.Sum256([]byte(fmt.Sprintf("%s/%d/%d", seed, i, ctr)))
		k, err := crypto.ToECDSA(h[:])
		if err == nil {
			return k
		}
	}
}

func toBech32(hrp string, addr common.Address) string {
	conv, err := bech32.ConvertBits(addr.Bytes(), 8, 5, true)
	if err != nil {
		panic(err)
	}
	s, err := bech32.Encode(hrp, conv)
	if err != nil {
		panic(err)
	}
	return s
}

func cmdAccounts(args []string) {
	fs := flag.NewFlagSet("accounts", flag.ExitOnError)
	count := fs.Int("count", 100, "number of accounts")
	seed := fs.String("seed", "vapor-load", "derivation seed")
	hrp := fs.String("hrp", "vapor", "bech32 prefix ('' prints hex)")
	_ = fs.Parse(args)
	for i := 0; i < *count; i++ {
		addr := crypto.PubkeyToAddress(deriveKey(*seed, i).PublicKey)
		if *hrp == "" {
			fmt.Println(addr.Hex())
		} else {
			fmt.Println(toBech32(*hrp, addr))
		}
	}
}

type sentTx struct {
	at        time.Time
	sponsored bool
}

type stats struct {
	sent, accepted, rejected atomic.Int64
	rejectReasons            sync.Map // reason -> *atomic.Int64
}

func (s *stats) reject(err error) {
	s.rejected.Add(1)
	reason := err.Error()
	if len(reason) > 80 {
		reason = reason[:80]
	}
	v, _ := s.rejectReasons.LoadOrStore(reason, new(atomic.Int64))
	v.(*atomic.Int64).Add(1)
}

type worker struct {
	key    *ecdsa.PrivateKey
	addr   common.Address
	nonce  uint64
	client *ethclient.Client
	mu     sync.Mutex
}

var (
	erc20ABI, _  = abi.JSON(strings.NewReader(`[{"type":"function","name":"transfer","inputs":[{"name":"to","type":"address"},{"name":"amount","type":"uint256"}],"outputs":[{"type":"bool"}]}]`))
	settleABI, _ = abi.JSON(strings.NewReader(`[{"type":"function","name":"pay","inputs":[{"name":"token","type":"address"},{"name":"amount","type":"uint256"},{"name":"payee","type":"address"},{"name":"referrer","type":"address"}],"outputs":[{"type":"uint256"},{"type":"uint256"}]}]`))
)

type report struct {
	Scenario      string           `json:"scenario"`
	DurationSec   float64          `json:"duration_sec"`
	TargetRate    int              `json:"target_rate_tps"`
	Sent          int64            `json:"sent"`
	Accepted      int64            `json:"accepted_by_mempool"`
	Rejected      int64            `json:"rejected_by_mempool"`
	RejectReasons map[string]int64 `json:"reject_reasons"`
	Included      int              `json:"included_onchain"`
	Failed        int              `json:"reverted_onchain"`
	InclusionRate float64          `json:"inclusion_rate"`
	// TPS is included / send window. It overstates throughput whenever txs keep
	// landing long after sending stops (high latency past the knee); use
	// TPSSustained, which divides by the real inclusion span, for capacity.
	TPS               float64            `json:"onchain_tps"`
	TPSSustained      float64            `json:"onchain_tps_sustained"`
	InclusionSpanSec  float64            `json:"inclusion_span_sec"`
	LatencyMs         map[string]float64 `json:"latency_ms"`
	Blocks            int                `json:"blocks_observed"`
	BlockTimeMs       map[string]float64 `json:"block_time_ms"`
	MaxTxPerBlock     int                `json:"max_tx_per_block"`
	GasUtilization    map[string]float64 `json:"block_gas_utilization"`
	MaxSponsoredShare float64            `json:"max_sponsored_gas_share"`
}

func pct(xs []float64, p float64) float64 {
	if len(xs) == 0 {
		return 0
	}
	sort.Float64s(xs)
	idx := int(float64(len(xs)-1) * p)
	return xs[idx]
}

func cmdRun(args []string) {
	fs := flag.NewFlagSet("run", flag.ExitOnError)
	rpcs := fs.String("rpc", "http://127.0.0.1:8545", "comma-separated JSON-RPC endpoints")
	scenario := fs.String("scenario", "transfer", "transfer|erc20|settle|mixed|sponsored|lowfee")
	nAcc := fs.Int("accounts", 100, "number of sender accounts")
	seed := fs.String("seed", "vapor-load", "derivation seed")
	rate := fs.Int("rate", 100, "target tx/s")
	dur := fs.Duration("duration", 30*time.Second, "send duration")
	token := fs.String("token", "", "ERC-20 (bank-native) token for erc20/settle scenarios")
	sponsorKeyHex := fs.String("sponsor-key", "", "hex private key of a registered sponsored sender (sponsored scenario)")
	gasLimit := fs.Uint64("gas", 0, "override gas limit per tx")
	out := fs.String("out", "", "write JSON report here")
	comet := fs.String("comet", "http://127.0.0.1:26657", "CometBFT RPC for nanosecond block times")
	_ = fs.Parse(args)

	ctx := context.Background()
	endpoints := strings.Split(*rpcs, ",")
	clients := make([]*ethclient.Client, len(endpoints))
	for i, e := range endpoints {
		c, err := ethclient.Dial(strings.TrimSpace(e))
		if err != nil {
			fatal("dial %s: %v", e, err)
		}
		clients[i] = c
	}
	chainID, err := clients[0].ChainID(ctx)
	if err != nil {
		fatal("chain id: %v", err)
	}
	head, err := clients[0].HeaderByNumber(ctx, nil)
	if err != nil {
		fatal("head: %v", err)
	}
	startHeight := head.Number.Uint64()

	var workers []*worker
	if *scenario == "sponsored" {
		if *sponsorKeyHex == "" {
			fatal("--sponsor-key required for sponsored scenario")
		}
		k, err := crypto.HexToECDSA(strings.TrimPrefix(*sponsorKeyHex, "0x"))
		if err != nil {
			fatal("sponsor key: %v", err)
		}
		workers = append(workers, &worker{key: k, addr: crypto.PubkeyToAddress(k.PublicKey), client: clients[0]})
	}
	for i := 0; i < *nAcc; i++ {
		k := deriveKey(*seed, i)
		workers = append(workers, &worker{key: k, addr: crypto.PubkeyToAddress(k.PublicKey), client: clients[i%len(clients)]})
	}
	// fetch starting nonces in parallel
	var wg sync.WaitGroup
	sem := make(chan struct{}, 32)
	for _, w := range workers {
		wg.Add(1)
		sem <- struct{}{}
		go func(w *worker) {
			defer wg.Done()
			defer func() { <-sem }()
			n, err := w.client.PendingNonceAt(ctx, w.addr)
			if err == nil {
				w.nonce = n
			}
		}(w)
	}
	wg.Wait()

	tip := big.NewInt(1_000_000_000)
	feeCap := new(big.Int).Mul(big.NewInt(1_000_000_000), big.NewInt(20))
	if *scenario == "lowfee" {
		// deliberately under the fee floor: every tx must be rejected
		feeCap = big.NewInt(1)
		tip = big.NewInt(1)
	}
	tokenAddr := common.HexToAddress(*token)
	settle := common.HexToAddress(settleAddr)

	var sent sync.Map // hash -> sentTx
	var st stats

	build := func(w *worker, i int) (*types.Transaction, bool) {
		to := workers[(i*7919+1)%len(workers)].addr
		if to == w.addr {
			to = common.HexToAddress("0x000000000000000000000000000000000000dEaD")
		}
		sc := *scenario
		isSponsor := sc == "sponsored" && w == workers[0]
		if sc == "sponsored" && !isSponsor {
			sc = "transfer" // regular paying users competing for block space
		}
		if sc == "mixed" {
			switch i % 3 {
			case 0:
				sc = "transfer"
			case 1:
				sc = "erc20"
			default:
				sc = "settle"
			}
		}
		var data []byte
		value := big.NewInt(0)
		dst := to
		gas := uint64(21_000)
		switch sc {
		case "transfer", "lowfee", "sponsored":
			value = big.NewInt(1_000_000_000_000) // 1e-6 CREDIT
			if isSponsor {
				gas = 500_000 // bundler handleOps-sized gas limit
			}
		case "erc20":
			data, _ = erc20ABI.Pack("transfer", to, big.NewInt(1000))
			dst = tokenAddr
			gas = 120_000
		case "settle":
			data, _ = settleABI.Pack("pay", tokenAddr, big.NewInt(1_000_000), to, common.Address{})
			dst = settle
			gas = 250_000
		}
		if *gasLimit > 0 && (*scenario != "sponsored" || isSponsor) {
			gas = *gasLimit
		}
		w.mu.Lock()
		n := w.nonce
		w.nonce++
		w.mu.Unlock()
		tx := types.NewTx(&types.DynamicFeeTx{
			ChainID: chainID, Nonce: n, GasTipCap: tip, GasFeeCap: feeCap, Gas: gas, To: &dst, Value: value, Data: data,
		})
		signed, err := types.SignTx(tx, types.LatestSignerForChainID(chainID), w.key)
		if err != nil {
			fatal("sign: %v", err)
		}
		return signed, isSponsor
	}

	fmt.Fprintf(os.Stderr, "loadgen: scenario=%s accounts=%d rate=%d/s duration=%s endpoints=%d chain=%s start=%d\n",
		*scenario, len(workers), *rate, *dur, len(clients), chainID, startHeight)

	interval := time.Second / time.Duration(max(*rate, 1))
	deadline := time.Now().Add(*dur)
	ticker := time.NewTicker(interval)
	sendSem := make(chan struct{}, 256)
	i := 0
	var sendWG sync.WaitGroup
	for time.Now().Before(deadline) {
		<-ticker.C
		var w *worker
		if *scenario == "sponsored" && i%2 == 0 {
			w = workers[0]
		} else {
			w = workers[i%len(workers)]
		}
		tx, sponsored := build(w, i)
		i++
		st.sent.Add(1)
		sendSem <- struct{}{}
		sendWG.Add(1)
		go func(w *worker, tx *types.Transaction, sponsored bool) {
			defer sendWG.Done()
			defer func() { <-sendSem }()
			t0 := time.Now()
			if err := w.client.SendTransaction(ctx, tx); err != nil {
				st.reject(err)
				return
			}
			st.accepted.Add(1)
			sent.Store(tx.Hash(), sentTx{at: t0, sponsored: sponsored})
		}(w, tx, sponsored)
	}
	ticker.Stop()
	sendWG.Wait()
	sendEnd := time.Now()

	// drain: watch blocks until everything accepted is included or 30s pass
	rc, _ := rpc.Dial(strings.TrimSpace(endpoints[0]))
	var (
		included, failed int
		lat              []float64
		blockTimes       []float64
		gasUtil          []float64
		maxTx            int
		maxSponsored     float64
		lastBT           time.Time
		blocks           int
	)
	type inclusion struct {
		height uint64
		at     time.Time
	}
	var incl []inclusion
	seen := map[common.Hash]bool{}
	pending := st.accepted.Load()
	cursor := startHeight + 1
	drainDeadline := time.Now().Add(45 * time.Second)
	for time.Now().Before(drainDeadline) {
		h, err := clients[0].BlockNumber(ctx)
		if err != nil || h < cursor {
			time.Sleep(300 * time.Millisecond)
			continue
		}
		for ; cursor <= h; cursor++ {
			blk, err := clients[0].BlockByNumber(ctx, new(big.Int).SetUint64(cursor))
			if err != nil {
				break
			}
			blocks++
			bt := blockTime(*comet, cursor, blk.Time())
			if !lastBT.IsZero() {
				blockTimes = append(blockTimes, float64(bt.Sub(lastBT).Milliseconds()))
			}
			lastBT = bt
			if blk.GasLimit() > 0 {
				gasUtil = append(gasUtil, float64(blk.GasUsed())/float64(blk.GasLimit()))
			}
			maxTx = max(maxTx, len(blk.Transactions()))
			var sponsoredGas, totalGas uint64
			var receipts []*types.Receipt
			_ = rc.CallContext(ctx, &receipts, "eth_getBlockReceipts", fmt.Sprintf("0x%x", cursor))
			rmap := map[common.Hash]*types.Receipt{}
			for _, r := range receipts {
				rmap[r.TxHash] = r
			}
			for _, tx := range blk.Transactions() {
				totalGas += tx.Gas()
				v, ok := sent.Load(tx.Hash())
				if !ok || seen[tx.Hash()] {
					continue
				}
				seen[tx.Hash()] = true
				s := v.(sentTx)
				if s.sponsored {
					sponsoredGas += tx.Gas()
				}
				included++
				if r, ok := rmap[tx.Hash()]; ok && r.Status == 0 {
					failed++
				}
				incl = append(incl, inclusion{height: cursor, at: s.at})
			}
			if blk.GasLimit() > 0 && sponsoredGas > 0 {
				maxSponsored = max(maxSponsored, float64(sponsoredGas)/float64(blk.GasLimit()))
			}
		}
		if int64(included) >= pending && time.Since(sendEnd) > 3*time.Second {
			break
		}
	}
	// Finality latency: a tx in block H is final when H is committed, which is
	// (to within a few ms) the header time of block H+1. When the drain window
	// ends while txs are still landing, the newest included block has no
	// successor yet: wait for it rather than measuring against a missing header.
	var lastIncl uint64
	for _, in := range incl {
		lastIncl = max(lastIncl, in.height)
	}
	for wait := time.Now().Add(15 * time.Second); lastIncl > 0 && time.Now().Before(wait); time.Sleep(300 * time.Millisecond) {
		if h, err := clients[0].BlockNumber(ctx); err == nil && h > lastIncl {
			break
		}
	}
	for _, in := range incl {
		lat = append(lat, float64(blockTime(*comet, in.height+1, 0).Sub(in.at).Milliseconds()))
	}
	elapsed := sendEnd.Sub(deadline.Add(-*dur)).Seconds()
	rep := report{
		Scenario: *scenario, DurationSec: elapsed, TargetRate: *rate,
		Sent: st.sent.Load(), Accepted: st.accepted.Load(), Rejected: st.rejected.Load(),
		RejectReasons: map[string]int64{}, Included: included, Failed: failed,
		Blocks: blocks, MaxTxPerBlock: maxTx, MaxSponsoredShare: maxSponsored,
		LatencyMs:      map[string]float64{"p50": pct(lat, 0.5), "p95": pct(lat, 0.95), "p99": pct(lat, 0.99), "max": pct(lat, 1)},
		BlockTimeMs:    map[string]float64{"p50": pct(blockTimes, 0.5), "p95": pct(blockTimes, 0.95), "max": pct(blockTimes, 1)},
		GasUtilization: map[string]float64{"p50": pct(gasUtil, 0.5), "max": pct(gasUtil, 1)},
	}
	st.rejectReasons.Range(func(k, v any) bool { rep.RejectReasons[k.(string)] = v.(*atomic.Int64).Load(); return true })
	if rep.Accepted > 0 {
		rep.InclusionRate = float64(included) / float64(rep.Accepted)
	}
	if elapsed > 0 {
		rep.TPS = float64(included) / (elapsed + 1)
	}
	// sustained: from the first send to the commit of the last block holding
	// one of our txs (block H is committed at the header time of H+1)
	if lastIncl > 0 {
		if span := blockTime(*comet, lastIncl+1, 0).Sub(deadline.Add(-*dur)).Seconds(); span > 0 {
			rep.InclusionSpanSec = span
			rep.TPSSustained = float64(included) / span
		}
	}
	bz, _ := json.MarshalIndent(rep, "", "  ")
	fmt.Println(string(bz))
	if *out != "" {
		if err := os.WriteFile(*out, bz, 0o644); err != nil {
			fatal("write report: %v", err)
		}
	}
}

var btCache sync.Map

// blockTime returns the CometBFT header time (ns precision); EVM block
// timestamps are truncated to seconds and would make latency meaningless.
func blockTime(comet string, height uint64, fallback uint64) time.Time {
	if v, ok := btCache.Load(height); ok {
		return v.(time.Time)
	}
	t := time.Unix(int64(fallback), 0)
	if comet != "" {
		if resp, err := http.Get(fmt.Sprintf("%s/header?height=%d", comet, height)); err == nil {
			var out struct {
				Result struct {
					Header struct {
						Time time.Time `json:"time"`
					} `json:"header"`
				} `json:"result"`
			}
			if json.NewDecoder(resp.Body).Decode(&out) == nil && !out.Result.Header.Time.IsZero() {
				t = out.Result.Header.Time
				// cache real header times only: a height that does not exist yet
				// must be fetched again rather than pinned to the fallback
				btCache.Store(height, t)
			}
			resp.Body.Close()
		}
	}
	return t
}

func fatal(f string, a ...any) {
	fmt.Fprintf(os.Stderr, "loadgen: "+f+"\n", a...)
	os.Exit(1)
}

var _ = errors.New
