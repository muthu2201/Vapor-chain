// Copyright (c) 2026 VaporChain / muthu2201. All rights reserved.
// Proprietary and confidential. See LICENSE at the repository root.
// Provenance: VAPOR-6eabb1be532bdef4

// Package api serves developer analytics (the blueprint's /apps/{id}/revenue
// and /apps/{id}/users) from the indexer database.
package api

import (
	"context"
	"encoding/json"
	"log/slog"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/ethereum/go-ethereum/common"

	"github.com/muthu2201/vapor-chain/services/indexer/internal/db"
)

type API struct{ db *db.DB }

func New(d *db.DB) *API { return &API{db: d} }

func writeJSON(w http.ResponseWriter, code int, v any) {
	w.Header().Set("Content-Type", "application/json")
	w.Header().Set("Cache-Control", "public, max-age=5")
	w.WriteHeader(code)
	_ = json.NewEncoder(w).Encode(v)
}

// internalError logs the cause server-side and returns a generic message:
// database errors can reveal schema and query details to a caller.
func internalError(w http.ResponseWriter, r *http.Request, err error) {
	slog.Error("indexer api query failed", "path", r.URL.Path, "err", err)
	writeJSON(w, 500, map[string]string{"error": "internal error"})
}

func intParam(r *http.Request, k string, def, max int) int {
	v, err := strconv.Atoi(r.URL.Query().Get(k))
	if err != nil || v <= 0 {
		return def
	}
	if v > max {
		return max
	}
	return v
}

func (a *API) Handler() http.Handler {
	mux := http.NewServeMux()
	mux.HandleFunc("GET /healthz", func(w http.ResponseWriter, r *http.Request) {
		if err := a.db.Pool.Ping(r.Context()); err != nil {
			writeJSON(w, 503, map[string]string{"status": "db down"})
			return
		}
		writeJSON(w, 200, map[string]string{"status": "ok"})
	})
	mux.HandleFunc("GET /v1/stats", a.stats)
	mux.HandleFunc("GET /v1/apps/{id}/revenue", a.revenue)
	mux.HandleFunc("GET /v1/apps/{id}/users", a.users)
	mux.HandleFunc("GET /v1/apps/{id}/settlements", a.settlements)
	mux.HandleFunc("GET /v1/apps/{id}/sponsored", a.sponsored)
	mux.HandleFunc("GET /v1/address/{addr}/txs", a.addressTxs)
	return cors(mux)
}

// cors: the API is public, read-only and cookie-less, so any origin may read
// it from a browser (dashboards, explorers, the reference app).
func cors(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("Origin") != "" {
			w.Header().Set("Access-Control-Allow-Origin", "*")
			w.Header().Set("Access-Control-Allow-Methods", "GET, OPTIONS")
			w.Header().Set("Access-Control-Max-Age", "600")
		}
		if r.Method == http.MethodOptions {
			w.WriteHeader(http.StatusNoContent)
			return
		}
		next.ServeHTTP(w, r)
	})
}

func (a *API) q(ctx context.Context, sql string, args ...any) ([]map[string]any, error) {
	ctx, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()
	rows, err := a.db.Pool.Query(ctx, sql, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []map[string]any
	fields := rows.FieldDescriptions()
	for rows.Next() {
		vals, err := rows.Values()
		if err != nil {
			return nil, err
		}
		m := make(map[string]any, len(vals))
		for i, f := range fields {
			switch v := vals[i].(type) {
			case []byte:
				m[f.Name] = "0x" + common.Bytes2Hex(v)
			default:
				m[f.Name] = v
			}
		}
		out = append(out, m)
	}
	return out, rows.Err()
}

func (a *API) stats(w http.ResponseWriter, r *http.Request) {
	res, err := a.q(r.Context(), `
	  WITH last AS (SELECT height, time, tx_count FROM blocks ORDER BY height DESC LIMIT 60)
	  SELECT (SELECT max(height) FROM blocks) AS height,
	         (SELECT COALESCE(sum(tx_count),0)::FLOAT / GREATEST(EXTRACT(EPOCH FROM max(time)-min(time)),1) FROM last) AS tps_recent,
	         (SELECT count(*) FROM settlements WHERE time > now() - interval '24 hours') AS settlements_24h,
	         (SELECT COALESCE(sum(fee),0)::TEXT FROM settlements WHERE time > now() - interval '24 hours') AS fees_24h`)
	if err != nil {
		internalError(w, r, err)
		return
	}
	writeJSON(w, 200, res[0])
}

func (a *API) revenue(w http.ResponseWriter, r *http.Request) {
	id, err := strconv.ParseInt(r.PathValue("id"), 10, 64)
	if err != nil {
		writeJSON(w, 400, map[string]string{"error": "bad app id"})
		return
	}
	days := intParam(r, "days", 30, 365)
	res, err := a.q(r.Context(), `SELECT date_trunc('day', time) AS day, denom, count(*) AS payments,
	    sum(amount)::TEXT AS volume, sum(fee)::TEXT AS fees, sum(app_share)::TEXT AS app_revenue, sum(referrer_share)::TEXT AS referrer_paid
	  FROM settlements WHERE app_id=$1 AND time > now() - make_interval(days => $2)
	  GROUP BY 1,2 ORDER BY 1,2`, id, days)
	if err != nil {
		internalError(w, r, err)
		return
	}
	writeJSON(w, 200, map[string]any{"app_id": id, "days": days, "series": res})
}

func (a *API) users(w http.ResponseWriter, r *http.Request) {
	id, err := strconv.ParseInt(r.PathValue("id"), 10, 64)
	if err != nil {
		writeJSON(w, 400, map[string]string{"error": "bad app id"})
		return
	}
	days := intParam(r, "days", 30, 365)
	res, err := a.q(r.Context(), `SELECT date_trunc('day', time) AS day, count(DISTINCT payer) AS unique_payers, count(*) AS payments
	  FROM settlements WHERE app_id=$1 AND time > now() - make_interval(days => $2) GROUP BY 1 ORDER BY 1`, id, days)
	if err != nil {
		internalError(w, r, err)
		return
	}
	writeJSON(w, 200, map[string]any{"app_id": id, "days": days, "series": res})
}

func (a *API) settlements(w http.ResponseWriter, r *http.Request) {
	id, err := strconv.ParseInt(r.PathValue("id"), 10, 64)
	if err != nil {
		writeJSON(w, 400, map[string]string{"error": "bad app id"})
		return
	}
	limit := intParam(r, "limit", 50, 500)
	res, err := a.q(r.Context(), `SELECT height, time, payer, payee, denom, amount::TEXT, fee::TEXT, net::TEXT, app_share::TEXT, referrer
	  FROM settlements WHERE app_id=$1 ORDER BY height DESC, seq DESC LIMIT $2`, id, limit)
	if err != nil {
		internalError(w, r, err)
		return
	}
	writeJSON(w, 200, map[string]any{"app_id": id, "items": res})
}

func (a *API) sponsored(w http.ResponseWriter, r *http.Request) {
	id, err := strconv.ParseInt(r.PathValue("id"), 10, 64)
	if err != nil {
		writeJSON(w, 400, map[string]string{"error": "bad app id"})
		return
	}
	res, err := a.q(r.Context(), `SELECT count(*) AS ops, COALESCE(sum(gas_cost),0)::TEXT AS credits_spent,
	    count(DISTINCT sender) AS unique_senders FROM sponsored_ops WHERE app_id=$1`, id)
	if err != nil {
		internalError(w, r, err)
		return
	}
	writeJSON(w, 200, map[string]any{"app_id": id, "totals": res[0]})
}

func (a *API) addressTxs(w http.ResponseWriter, r *http.Request) {
	addr := r.PathValue("addr")
	if !common.IsHexAddress(addr) || !strings.HasPrefix(addr, "0x") {
		writeJSON(w, 400, map[string]string{"error": "0x address required"})
		return
	}
	b := common.HexToAddress(addr).Bytes()
	limit := intParam(r, "limit", 50, 500)
	res, err := a.q(r.Context(), `SELECT hash, height, sender, recipient, value::TEXT, gas_used, status FROM evm_txs
	  WHERE sender=$1 OR recipient=$1 ORDER BY height DESC, idx DESC LIMIT $2`, b, limit)
	if err != nil {
		internalError(w, r, err)
		return
	}
	writeJSON(w, 200, map[string]any{"address": addr, "items": res})
}
