// SPDX-License-Identifier: AGPL-3.0-only
// Copyright (c) 2026 VaporChain / muthu2201
// Provenance: VAPOR-6eabb1be532bdef4

// Package e2e runs the whole indexer pipeline against a LIVE localnet and a
// real PostgreSQL server: ingest a window of blocks into a throwaway
// database, cross-check every block with the chain's own JSON-RPC, seal the
// window into Parquet and verify the manifest, then query the HTTP API.
//
//	VAPOR_TEST_PG=postgres://postgres@localhost:5433/postgres?host=/tmp \
//	VAPOR_TEST_EVM_RPC=http://127.0.0.1:8545 VAPOR_TEST_COMET_RPC=http://127.0.0.1:26657 \
//	go test ./internal/e2e/
package e2e

import (
	"context"
	"crypto/rand"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"path/filepath"
	"strconv"
	"testing"
	"time"

	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/common/hexutil"
	"github.com/ethereum/go-ethereum/ethclient"
	"github.com/jackc/pgx/v5"
	"github.com/parquet-go/parquet-go"

	"github.com/muthu2201/vapor-chain/services/indexer/internal/api"
	"github.com/muthu2201/vapor-chain/services/indexer/internal/archive"
	"github.com/muthu2201/vapor-chain/services/indexer/internal/db"
	"github.com/muthu2201/vapor-chain/services/indexer/internal/ingest"
)

const window = 25

func env(t *testing.T, k string) string {
	v := os.Getenv(k)
	if v == "" {
		t.Skipf("%s not set: live pipeline test skipped", k)
	}
	return v
}

// freshDB creates a uniquely named database and drops it after the test.
func freshDB(t *testing.T, server string) string {
	ctx := context.Background()
	admin, err := pgx.Connect(ctx, server)
	if err != nil {
		t.Fatal(err)
	}
	b := make([]byte, 6)
	_, _ = rand.Read(b)
	name := "vapor_indexer_test_" + hex.EncodeToString(b)
	if _, err := admin.Exec(ctx, "CREATE DATABASE "+name); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		_, _ = admin.Exec(context.Background(), "DROP DATABASE IF EXISTS "+name+" WITH (FORCE)")
		_ = admin.Close(context.Background())
	})
	u, _ := url.Parse(server)
	u.Path = "/" + name
	return u.String()
}

func cometLatest(t *testing.T, comet string) int64 {
	resp, err := http.Get(comet + "/status")
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	var s struct {
		Result struct {
			SyncInfo struct {
				Latest string `json:"latest_block_height"`
			} `json:"sync_info"`
		} `json:"result"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&s); err != nil {
		t.Fatal(err)
	}
	h, _ := strconv.ParseInt(s.Result.SyncInfo.Latest, 10, 64)
	return h
}

func TestPipelineAgainstLiveChain(t *testing.T) {
	server := env(t, "VAPOR_TEST_PG")
	evm := env(t, "VAPOR_TEST_EVM_RPC")
	comet := env(t, "VAPOR_TEST_COMET_RPC")
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Minute)
	defer cancel()
	log := slog.New(slog.NewTextHandler(io.Discard, nil))

	d, err := db.Open(ctx, freshDB(t, server))
	if err != nil {
		t.Fatal(err)
	}
	defer d.Close()

	start := cometLatest(t, comet) - window
	in, err := ingest.New(ingest.Config{EVMRPC: evm, CometRPC: comet, StartAt: start, Batch: 10, Workers: 4}, d, log)
	if err != nil {
		t.Fatal(err)
	}
	runCtx, stop := context.WithCancel(ctx)
	done := make(chan error, 1)
	go func() { done <- in.Run(runCtx) }()
	end := start + window - 1
	for {
		cur, _ := d.Cursor(ctx, "blocks")
		if cur >= end {
			break
		}
		if ctx.Err() != nil {
			t.Fatalf("ingest did not reach %d (cursor %d)", end, cur)
		}
		time.Sleep(200 * time.Millisecond)
	}
	stop()
	<-done

	// 1. every block in the window is present, contiguous and matches the chain
	eth, err := ethclient.DialContext(ctx, evm)
	if err != nil {
		t.Fatal(err)
	}
	for h := start; h <= end; h++ {
		var hash []byte
		var txCount int32
		var gasUsed int64
		if err := d.Pool.QueryRow(ctx, `SELECT hash, tx_count, gas_used FROM blocks WHERE height=$1`, h).Scan(&hash, &txCount, &gasUsed); err != nil {
			t.Fatalf("block %d missing: %v", h, err)
		}
		// Compare with the RPC's own "hash" field. On cosmos/evm that is the
		// CometBFT block hash, NOT keccak(rlp(header)), so go-ethereum's
		// types.Block.Hash() (which recomputes it) never matches.
		var blk struct {
			Hash    common.Hash    `json:"hash"`
			GasUsed hexutil.Uint64 `json:"gasUsed"`
			Txs     []common.Hash  `json:"transactions"`
		}
		if err := eth.Client().CallContext(ctx, &blk, "eth_getBlockByNumber", hexutil.EncodeBig(bigInt(h)), false); err != nil {
			t.Fatal(err)
		}
		if common.BytesToHash(hash) != blk.Hash {
			t.Fatalf("block %d hash %x != chain %s", h, hash, blk.Hash)
		}
		if int(txCount) != len(blk.Txs) || uint64(gasUsed) != uint64(blk.GasUsed) {
			t.Fatalf("block %d: indexed txs=%d gas=%d, chain txs=%d gas=%d", h, txCount, gasUsed, len(blk.Txs), blk.GasUsed)
		}
	}
	var evmTxs int
	_ = d.Pool.QueryRow(ctx, `SELECT count(*) FROM evm_txs WHERE height BETWEEN $1 AND $2`, start, end).Scan(&evmTxs)

	// 2. seal the window into Parquet; files must hash to the manifest and
	//    hold exactly the indexed rows
	dir := t.TempDir()
	ar := archive.New(d, dir, window, log)
	if err := ar.Seal(ctx, start, end); err != nil {
		t.Fatal(err)
	}
	seg := filepath.Join(dir, fmt.Sprintf("%012d-%012d", start, end))
	var m archive.Manifest
	bz, err := os.ReadFile(filepath.Join(seg, "manifest.json"))
	if err != nil {
		t.Fatal(err)
	}
	if err := json.Unmarshal(bz, &m); err != nil {
		t.Fatal(err)
	}
	for name, want := range m.Files {
		f, err := os.ReadFile(filepath.Join(seg, name))
		if err != nil {
			t.Fatal(err)
		}
		sum := sha256.Sum256(f)
		if hex.EncodeToString(sum[:]) != want {
			t.Fatalf("%s sha256 mismatch", name)
		}
	}
	blocks, err := parquet.ReadFile[archive.BlockRow](filepath.Join(seg, "blocks.parquet"))
	if err != nil {
		t.Fatal(err)
	}
	if len(blocks) != window || m.Rows["blocks"] != window || blocks[0].Height != start || blocks[window-1].Height != end {
		t.Fatalf("parquet blocks: %d rows [%d..%d], manifest %d", len(blocks), blocks[0].Height, blocks[len(blocks)-1].Height, m.Rows["blocks"])
	}
	if m.Rows["txs"] != evmTxs {
		t.Fatalf("parquet txs %d != indexed %d", m.Rows["txs"], evmTxs)
	}
	// sealing again is a no-op (idempotent restart)
	if err := ar.Seal(ctx, start, end); err != nil {
		t.Fatal(err)
	}

	// 3. the HTTP API serves the indexed data, with CORS and without leaking
	//    database errors
	srv := httptest.NewServer(api.New(d).Handler())
	defer srv.Close()
	var stats map[string]any
	getJSON(t, srv.URL+"/v1/stats", &stats)
	cur, _ := d.Cursor(ctx, "blocks") // ingest may have run past `end` before stop
	if h, _ := stats["height"].(float64); int64(h) != cur || cur < end {
		t.Fatalf("stats height %v, cursor %d, window end %d", stats["height"], cur, end)
	}
	req, _ := http.NewRequest(http.MethodGet, srv.URL+"/v1/apps/1/sponsored", nil)
	req.Header.Set("Origin", "https://shop.example")
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	resp.Body.Close()
	if resp.StatusCode != 200 || resp.Header.Get("Access-Control-Allow-Origin") != "*" {
		t.Fatalf("sponsored: %d ACAO=%q", resp.StatusCode, resp.Header.Get("Access-Control-Allow-Origin"))
	}
	for _, bad := range []string{"/v1/apps/notanumber/revenue", "/v1/address/0xnothex/txs"} {
		r, err := http.Get(srv.URL + bad)
		if err != nil {
			t.Fatal(err)
		}
		r.Body.Close()
		if r.StatusCode != 400 {
			t.Fatalf("%s: status %d, want 400", bad, r.StatusCode)
		}
	}
	d.Close() // DB gone: the API must answer with a generic error, not SQL details
	r, err := http.Get(srv.URL + "/v1/apps/1/revenue")
	if err != nil {
		t.Fatal(err)
	}
	body, _ := io.ReadAll(r.Body)
	r.Body.Close()
	if r.StatusCode != 500 || string(body) != "{\"error\":\"internal error\"}\n" {
		t.Fatalf("db-down response leaks details: %d %s", r.StatusCode, body)
	}
}

func getJSON(t *testing.T, u string, out any) {
	t.Helper()
	r, err := http.Get(u)
	if err != nil {
		t.Fatal(err)
	}
	defer r.Body.Close()
	if r.StatusCode != 200 {
		b, _ := io.ReadAll(r.Body)
		t.Fatalf("%s: %d %s", u, r.StatusCode, b)
	}
	if err := json.NewDecoder(r.Body).Decode(out); err != nil {
		t.Fatal(err)
	}
}
