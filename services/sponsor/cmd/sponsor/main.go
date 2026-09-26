// SPDX-License-Identifier: AGPL-3.0-only
// Copyright (c) 2026 VaporChain / muthu2201
// Provenance: VAPOR-6eabb1be532bdef4

// Command sponsor runs the VaporChain paymaster (ERC-7677) service.
//
// Configuration is environment-only (12-factor); secrets come from files so
// they never appear in process listings:
//
//	VAPOR_EVM_RPC            JSON-RPC of a VaporChain node (write-ingress or RPC tier)
//	VAPOR_REST               Cosmos REST API (x/apps queries)
//	VAPOR_DATABASE_URL       PostgreSQL DSN
//	VAPOR_SIGNER_KEY_FILE    file with the sponsor signer private key (hex)
//	VAPOR_PAYMASTER          VaporVerifyingPaymaster address
//	VAPOR_SETTLE             Settle precompile (default 0x…0900)
//	VAPOR_ENTRYPOINT         EntryPoint (default canonical v0.8)
//	VAPOR_ALLOWED_DELEGATES  comma list of EIP-7702 implementations
//	VAPOR_ALLOWED_FACTORIES  comma list of account factories
//	VAPOR_LISTEN             default :8800 ; VAPOR_METRICS default :9800
package main

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"math/big"
	"net/http"
	"os"
	"os/signal"
	"strconv"
	"strings"
	"syscall"
	"time"

	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/crypto"
	"github.com/ethereum/go-ethereum/ethclient"
	"github.com/prometheus/client_golang/prometheus/promhttp"

	"github.com/muthu2201/vapor-chain/services/sponsor/internal/chain"
	"github.com/muthu2201/vapor-chain/services/sponsor/internal/policy"
	"github.com/muthu2201/vapor-chain/services/sponsor/internal/reconcile"
	"github.com/muthu2201/vapor-chain/services/sponsor/internal/server"
	"github.com/muthu2201/vapor-chain/services/sponsor/internal/store"
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

func addrSet(list string) map[common.Address]bool {
	out := map[common.Address]bool{}
	for _, a := range strings.Split(list, ",") {
		if a = strings.TrimSpace(a); common.IsHexAddress(a) {
			out[common.HexToAddress(a)] = true
		}
	}
	return out
}

func splitList(list string) []string {
	var out []string
	for _, v := range strings.Split(list, ",") {
		if v = strings.TrimSpace(v); v != "" {
			out = append(out, v)
		}
	}
	return out
}

func envInt(k string, def int) int {
	if v, err := strconv.Atoi(os.Getenv(k)); err == nil {
		return v
	}
	return def
}

func main() {
	log := slog.New(slog.NewJSONHandler(os.Stdout, &slog.HandlerOptions{Level: slog.LevelInfo}))
	ctx, stop := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
	defer stop()

	keyHex, err := os.ReadFile(must("VAPOR_SIGNER_KEY_FILE"))
	if err != nil {
		log.Error("read signer key", "err", err)
		os.Exit(1)
	}
	key, err := crypto.HexToECDSA(strings.TrimPrefix(strings.TrimSpace(string(keyHex)), "0x"))
	if err != nil {
		log.Error("parse signer key", "err", err)
		os.Exit(1)
	}
	eth, err := ethclient.DialContext(ctx, must("VAPOR_EVM_RPC"))
	if err != nil {
		log.Error("dial evm rpc", "err", err)
		os.Exit(1)
	}
	chainID, err := eth.ChainID(ctx)
	if err != nil {
		log.Error("chain id", "err", err)
		os.Exit(1)
	}
	st, err := store.Open(ctx, must("VAPOR_DATABASE_URL"))
	if err != nil {
		log.Error("open store", "err", err)
		os.Exit(1)
	}
	defer st.Close()

	paymaster := common.HexToAddress(must("VAPOR_PAYMASTER"))
	entryPoint := common.HexToAddress(env("VAPOR_ENTRYPOINT", "0x4337084D9E255Ff0702461CF8895CE9E3b5Ff108"))
	// 4 gwei = 4x the fee floor. This cap bounds what an app's earned quota is
	// worth: with quota_weight 2 at the genesis credit price, sponsored gas
	// spent at <= 4 gwei returns at most 0.4 of the fee that earned it, so the
	// app share (0.5) + that rebate stays < 1 and paying fees to farm free gas
	// always loses money. During congestion above the cap sponsored ops wait
	// (they are the lower-priority lane anyway); paying users are unaffected.
	maxFee, _ := new(big.Int).SetString(env("VAPOR_MAX_FEE_PER_GAS", "4000000000"), 10)

	cc := chain.New(must("VAPOR_REST"), eth, 15*time.Second)
	pol := policy.New(policy.Config{
		ChainID: chainID, EntryPoint: entryPoint,
		Settle:            common.HexToAddress(env("VAPOR_SETTLE", "0x0000000000000000000000000000000000000900")),
		MaxGasPerOp:       uint64(envInt("VAPOR_MAX_GAS_PER_OP", 2_000_000)),
		MaxFeePerGas:      maxFee,
		SenderDailyCap:    envInt("VAPOR_SENDER_DAILY_CAP", 50),
		NewSenderDailyCap: envInt("VAPOR_NEW_SENDER_DAILY_CAP", 10),
		AllowedDelegates:  addrSet(env("VAPOR_ALLOWED_DELEGATES", "0x4Cd241E8d1510e30b2076397afc7508Ae59C66c9")),
		AllowedFactories:  addrSet(os.Getenv("VAPOR_ALLOWED_FACTORIES")),
	}, cc)
	trusted, err := server.ParseCIDRs(os.Getenv("VAPOR_TRUSTED_PROXIES"))
	if err != nil {
		log.Error("parse VAPOR_TRUSTED_PROXIES", "err", err)
		os.Exit(1)
	}
	srv := server.New(server.Config{
		ChainID: chainID, EntryPoint: entryPoint, Paymaster: paymaster, SignerKey: key,
		Validity:      time.Duration(envInt("VAPOR_VALIDITY_SECONDS", 300)) * time.Second,
		RatePerSecond: float64(envInt("VAPOR_RATE_PER_IP", 20)), Burst: envInt("VAPOR_RATE_BURST", 40),
		TrustedProxies: trusted,
		CORSOrigins:    splitList(os.Getenv("VAPOR_CORS_ORIGINS")),
	}, pol, st, log)

	go reconcile.New(eth, st, paymaster, log).Run(ctx)

	api := &http.Server{Addr: env("VAPOR_LISTEN", ":8800"), Handler: srv.Handler(),
		ReadHeaderTimeout: 5 * time.Second, ReadTimeout: 10 * time.Second, WriteTimeout: 15 * time.Second, IdleTimeout: 60 * time.Second, MaxHeaderBytes: 16 << 10}
	metrics := &http.Server{Addr: env("VAPOR_METRICS", ":9800"), Handler: promhttp.Handler(), ReadHeaderTimeout: 5 * time.Second}
	go func() { _ = metrics.ListenAndServe() }()
	go func() {
		log.Info("sponsor service listening", "addr", api.Addr, "chainId", chainID, "paymaster", paymaster, "signer", crypto.PubkeyToAddress(key.PublicKey))
		if err := api.ListenAndServe(); err != nil && !errors.Is(err, http.ErrServerClosed) {
			log.Error("listen", "err", err)
			stop()
		}
	}()
	<-ctx.Done()
	shutdown, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	_ = api.Shutdown(shutdown)
	_ = metrics.Shutdown(shutdown)
	log.Info("sponsor service stopped")
}
