// SPDX-License-Identifier: AGPL-3.0-only
// Copyright (c) 2026 VaporChain / muthu2201
// Provenance: VAPOR-6eabb1be532bdef4

// Package server exposes the ERC-7677 paymaster web-service API
// (pm_getPaymasterStubData / pm_getPaymasterData) used by viem,
// permissionless.js and the VaporChain SDK.
package server

import (
	"context"
	"crypto/ecdsa"
	"encoding/json"
	"errors"
	"io"
	"log/slog"
	"math/big"
	"net"
	"net/http"
	"sync"
	"time"

	"github.com/ethereum/go-ethereum/accounts"
	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/common/hexutil"
	"github.com/ethereum/go-ethereum/crypto"
	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/promauto"
	"golang.org/x/time/rate"

	"github.com/muthu2201/vapor-chain/services/sponsor/internal/policy"
	"github.com/muthu2201/vapor-chain/services/sponsor/internal/store"
	"github.com/muthu2201/vapor-chain/services/sponsor/internal/userop"
)

const (
	PaymasterVerificationGas = 80_000
	PaymasterPostOpGas       = 25_000
)

var (
	mRequests = promauto.NewCounterVec(prometheus.CounterOpts{Name: "vapor_sponsor_requests_total", Help: "JSON-RPC requests by method and outcome"}, []string{"method", "outcome"})
	mSigned   = promauto.NewCounterVec(prometheus.CounterOpts{Name: "vapor_sponsor_signed_total", Help: "Signed sponsorships by app"}, []string{"app"})
	mGas      = promauto.NewCounterVec(prometheus.CounterOpts{Name: "vapor_sponsor_gas_reserved_total", Help: "Gas reserved by app"}, []string{"app"})
	mLatency  = promauto.NewHistogram(prometheus.HistogramOpts{Name: "vapor_sponsor_latency_seconds", Help: "Request latency", Buckets: prometheus.DefBuckets})
)

type Config struct {
	ChainID       *big.Int
	EntryPoint    common.Address
	Paymaster     common.Address
	SignerKey     *ecdsa.PrivateKey
	Validity      time.Duration
	RatePerSecond float64
	Burst         int
	// TrustedProxies are the load balancers allowed to set X-Forwarded-For.
	// Empty means the service is reached directly and XFF is ignored.
	TrustedProxies []*net.IPNet
	// CORSOrigins allowed to call from browsers; empty or "*" allows any
	// (no credentials are ever accepted, so "*" exposes nothing extra).
	CORSOrigins []string
}

type Server struct {
	cfg    Config
	policy *policy.Engine
	store  *store.Store
	log    *slog.Logger

	mu       sync.Mutex
	limiters map[string]*rate.Limiter
}

func New(cfg Config, p *policy.Engine, s *store.Store, log *slog.Logger) *Server {
	return &Server{cfg: cfg, policy: p, store: s, log: log, limiters: map[string]*rate.Limiter{}}
}

type rpcReq struct {
	JSONRPC string            `json:"jsonrpc"`
	ID      json.RawMessage   `json:"id"`
	Method  string            `json:"method"`
	Params  []json.RawMessage `json:"params"`
}

type rpcErr struct {
	Code    int    `json:"code"`
	Message string `json:"message"`
}

type rpcResp struct {
	JSONRPC string          `json:"jsonrpc"`
	ID      json.RawMessage `json:"id"`
	Result  any             `json:"result,omitempty"`
	Error   *rpcErr         `json:"error,omitempty"`
}

type pmContext struct {
	AppID json.Number `json:"appId"`
}

type pmResult struct {
	Paymaster                     common.Address `json:"paymaster"`
	PaymasterData                 hexutil.Bytes  `json:"paymasterData"`
	PaymasterVerificationGasLimit hexutil.Big    `json:"paymasterVerificationGasLimit"`
	PaymasterPostOpGasLimit       hexutil.Big    `json:"paymasterPostOpGasLimit"`
	Sponsor                       *sponsorInfo   `json:"sponsor,omitempty"`
	IsFinal                       bool           `json:"isFinal,omitempty"`
}

type sponsorInfo struct {
	Name string `json:"name"`
}

func (s *Server) limiter(ip string) *rate.Limiter {
	s.mu.Lock()
	defer s.mu.Unlock()
	l, ok := s.limiters[ip]
	if !ok {
		if len(s.limiters) >= maxLimiters {
			s.evictIdleLocked()
		}
		l = rate.NewLimiter(rate.Limit(s.cfg.RatePerSecond), s.cfg.Burst)
		s.limiters[ip] = l
	}
	return l
}

// maxLimiters bounds memory. Keys are real peer addresses (see ClientIP), so
// reaching it needs that many distinct source IPs.
const maxLimiters = 100_000

// evictIdleLocked drops limiters whose bucket is full again (clients that
// have been quiet), so a flood of new IPs cannot reset the budget of clients
// that are actively being throttled. Falls back to a full reset only if every
// tracked client is mid-burst.
func (s *Server) evictIdleLocked() {
	burst := float64(s.cfg.Burst)
	for k, l := range s.limiters {
		if l.Tokens() >= burst {
			delete(s.limiters, k)
		}
	}
	if len(s.limiters) >= maxLimiters {
		s.limiters = map[string]*rate.Limiter{}
	}
}

func (s *Server) Handler() http.Handler {
	mux := http.NewServeMux()
	mux.HandleFunc("/healthz", func(w http.ResponseWriter, _ *http.Request) { w.WriteHeader(http.StatusOK) })
	mux.HandleFunc("/readyz", func(w http.ResponseWriter, r *http.Request) {
		if err := s.store.Ping(r.Context()); err != nil {
			http.Error(w, "db down", http.StatusServiceUnavailable)
			return
		}
		w.WriteHeader(http.StatusOK)
	})
	mux.HandleFunc("/", s.handleRPC)
	return CORS(s.cfg.CORSOrigins, "POST, OPTIONS", mux)
}

func (s *Server) handleRPC(w http.ResponseWriter, r *http.Request) {
	start := time.Now()
	defer func() { mLatency.Observe(time.Since(start).Seconds()) }()
	w.Header().Set("Content-Type", "application/json")
	if r.Method != http.MethodPost {
		http.Error(w, `{"error":"POST only"}`, http.StatusMethodNotAllowed)
		return
	}
	if !s.limiter(ClientIP(r, s.cfg.TrustedProxies)).Allow() {
		mRequests.WithLabelValues("any", "rate_limited").Inc()
		http.Error(w, `{"jsonrpc":"2.0","error":{"code":-32005,"message":"rate limited"}}`, http.StatusTooManyRequests)
		return
	}
	body, err := io.ReadAll(io.LimitReader(r.Body, 256<<10))
	if err != nil {
		http.Error(w, "bad body", http.StatusBadRequest)
		return
	}
	var req rpcReq
	if err := json.Unmarshal(body, &req); err != nil {
		_ = json.NewEncoder(w).Encode(rpcResp{JSONRPC: "2.0", Error: &rpcErr{Code: -32700, Message: "parse error"}})
		return
	}
	ctx, cancel := context.WithTimeout(r.Context(), 8*time.Second)
	defer cancel()
	res, rerr := s.dispatch(ctx, &req)
	outcome := "ok"
	if rerr != nil {
		outcome = "error"
	}
	mRequests.WithLabelValues(req.Method, outcome).Inc()
	_ = json.NewEncoder(w).Encode(rpcResp{JSONRPC: "2.0", ID: req.ID, Result: res, Error: rerr})
}

func (s *Server) dispatch(ctx context.Context, req *rpcReq) (any, *rpcErr) {
	switch req.Method {
	case "pm_supportedEntryPoints":
		return []common.Address{s.cfg.EntryPoint}, nil
	case "pm_getPaymasterStubData", "pm_getPaymasterData":
		if len(req.Params) < 3 {
			return nil, &rpcErr{Code: -32602, Message: "params: [userOp, entryPoint, chainId, context]"}
		}
		var op userop.UserOperation
		var ep common.Address
		var cid hexutil.Big
		var pctx pmContext
		if json.Unmarshal(req.Params[0], &op) != nil || json.Unmarshal(req.Params[1], &ep) != nil || json.Unmarshal(req.Params[2], &cid) != nil {
			return nil, &rpcErr{Code: -32602, Message: "invalid params"}
		}
		if len(req.Params) > 3 {
			_ = json.Unmarshal(req.Params[3], &pctx)
		}
		appID, _ := pctx.AppID.Int64()
		if appID <= 0 {
			return nil, &rpcErr{Code: -32602, Message: "context.appId is required"}
		}
		// the gas limits are ours to set; hash/cap them as the chain will see them
		op.PaymasterVerificationGasLimit = (*hexutil.Big)(big.NewInt(PaymasterVerificationGas))
		op.PaymasterPostOpGasLimit = (*hexutil.Big)(big.NewInt(PaymasterPostOpGas))
		dec, err := s.policy.Evaluate(ctx, &op, ep, cid.ToInt(), uint64(appID))
		if err != nil {
			var pe *policy.PolicyError
			if errors.As(err, &pe) {
				return nil, &rpcErr{Code: -32602, Message: "sponsorship denied: " + pe.Reason}
			}
			s.log.Error("policy evaluation failed", "err", err)
			return nil, &rpcErr{Code: -32603, Message: "upstream chain query failed"}
		}
		if req.Method == "pm_getPaymasterStubData" {
			return s.result(userop.PaymasterData(0, 0, dec.AppID, stubSignature()), false), nil
		}
		return s.sign(ctx, &op, dec)
	default:
		return nil, &rpcErr{Code: -32601, Message: "method not found"}
	}
}

// stubSignature has the right length and never recovers to the signer.
func stubSignature() []byte {
	sig := make([]byte, 65)
	for i := range sig {
		sig[i] = 0xff
	}
	sig[64] = 0x1c
	return sig
}

func (s *Server) result(pmData []byte, final bool) pmResult {
	return pmResult{
		Paymaster:                     s.cfg.Paymaster,
		PaymasterData:                 pmData,
		PaymasterVerificationGasLimit: hexutil.Big(*big.NewInt(PaymasterVerificationGas)),
		PaymasterPostOpGasLimit:       hexutil.Big(*big.NewInt(PaymasterPostOpGas)),
		Sponsor:                       &sponsorInfo{Name: "VaporChain"},
		IsFinal:                       final,
	}
}

func (s *Server) sign(ctx context.Context, op *userop.UserOperation, dec policy.Decision) (any, *rpcErr) {
	validUntil := uint64(time.Now().Add(s.cfg.Validity).Unix())
	err := s.store.Reserve(ctx, store.Reservation{
		AppID: dec.AppID, Epoch: dec.Epoch, Quota: dec.Quota, Sender: op.Sender.Bytes(),
		Nonce: op.Nonce.ToInt().String(), Gas: dec.Gas, SenderCap: dec.SenderCap, ValidUntil: int64(validUntil),
	})
	switch {
	case errors.Is(err, store.ErrQuotaExceeded), errors.Is(err, store.ErrSenderCap), errors.Is(err, store.ErrAlreadySigned):
		return nil, &rpcErr{Code: -32602, Message: "sponsorship denied: " + err.Error()}
	case err != nil:
		s.log.Error("reserve failed", "err", err)
		return nil, &rpcErr{Code: -32603, Message: "internal error"}
	}
	h, err := userop.SponsorHash(op, s.cfg.ChainID, s.cfg.Paymaster, validUntil, 0, dec.AppID)
	if err != nil {
		return nil, &rpcErr{Code: -32603, Message: "hash error"}
	}
	sig, err := crypto.Sign(accounts.TextHash(h.Bytes()), s.cfg.SignerKey)
	if err != nil {
		return nil, &rpcErr{Code: -32603, Message: "sign error"}
	}
	sig[64] += 27
	mSigned.WithLabelValues(itoa(dec.AppID)).Inc()
	mGas.WithLabelValues(itoa(dec.AppID)).Add(float64(dec.Gas))
	return s.result(userop.PaymasterData(validUntil, 0, dec.AppID, sig), true), nil
}

func itoa(v uint64) string { return new(big.Int).SetUint64(v).String() }
