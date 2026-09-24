// Copyright (c) 2026 VaporChain / muthu2201. All rights reserved.
// Proprietary and confidential. See LICENSE at the repository root.
// Provenance: VAPOR-6eabb1be532bdef4

// Command indexer follows a VaporChain HISTORY node (never a validator) and
// keeps full history in PostgreSQL + Parquet, with a developer analytics API.
//
//	VAPOR_EVM_RPC, VAPOR_COMET_RPC, VAPOR_DATABASE_URL (required)
//	VAPOR_PAYMASTER           verifying paymaster (sponsored-op analytics)
//	VAPOR_ARCHIVE_DIR         Parquet archive root (default ./archive)
//	VAPOR_ARCHIVE_EVERY       blocks per segment (default 10000)
//	VAPOR_START_HEIGHT        first height when the database is empty
//	VAPOR_LISTEN (:8900)      VAPOR_METRICS (:9900)
package main

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"net/http"
	"os"
	"os/signal"
	"strconv"
	"syscall"
	"time"

	"github.com/ethereum/go-ethereum/common"
	"github.com/prometheus/client_golang/prometheus/promhttp"

	"github.com/muthu2201/vapor-chain/services/indexer/internal/api"
	"github.com/muthu2201/vapor-chain/services/indexer/internal/archive"
	"github.com/muthu2201/vapor-chain/services/indexer/internal/db"
	"github.com/muthu2201/vapor-chain/services/indexer/internal/ingest"
)

func env(k, def string) string {
	if v := os.Getenv(k); v != "" {
		return v
	}
	return def
}

func must(k string) string {
	v := os.Getenv(k)
	if v == "" {
		fmt.Fprintf(os.Stderr, "missing required env %s\n", k)
		os.Exit(2)
	}
	return v
}

func main() {
	log := slog.New(slog.NewJSONHandler(os.Stdout, nil))
	ctx, stop := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
	defer stop()

	d, err := db.Open(ctx, must("VAPOR_DATABASE_URL"))
	if err != nil {
		log.Error("db", "err", err)
		os.Exit(1)
	}
	defer d.Close()
	start, _ := strconv.ParseInt(env("VAPOR_START_HEIGHT", "1"), 10, 64)
	every, _ := strconv.ParseInt(env("VAPOR_ARCHIVE_EVERY", "10000"), 10, 64)

	in, err := ingest.New(ingest.Config{
		EVMRPC: must("VAPOR_EVM_RPC"), CometRPC: must("VAPOR_COMET_RPC"),
		Paymaster: common.HexToAddress(os.Getenv("VAPOR_PAYMASTER")), StartAt: start,
	}, d, log)
	if err != nil {
		log.Error("ingest", "err", err)
		os.Exit(1)
	}
	dir := env("VAPOR_ARCHIVE_DIR", "./archive")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		log.Error("archive dir", "err", err)
		os.Exit(1)
	}
	go archive.New(d, dir, every, log).Run(ctx)
	go func() {
		if err := in.Run(ctx); err != nil && !errors.Is(err, context.Canceled) {
			log.Error("ingest stopped", "err", err)
			stop()
		}
	}()

	srv := &http.Server{Addr: env("VAPOR_LISTEN", ":8900"), Handler: api.New(d).Handler(), ReadHeaderTimeout: 5 * time.Second, WriteTimeout: 15 * time.Second}
	met := &http.Server{Addr: env("VAPOR_METRICS", ":9900"), Handler: promhttp.Handler(), ReadHeaderTimeout: 5 * time.Second}
	go func() { _ = met.ListenAndServe() }()
	go func() {
		log.Info("indexer API listening", "addr", srv.Addr)
		if err := srv.ListenAndServe(); err != nil && !errors.Is(err, http.ErrServerClosed) {
			log.Error("listen", "err", err)
			stop()
		}
	}()
	<-ctx.Done()
	sctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	_ = srv.Shutdown(sctx)
	_ = met.Shutdown(sctx)
}
