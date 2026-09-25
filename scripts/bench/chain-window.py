#!/usr/bin/env python3
# Copyright (c) 2026 VaporChain / muthu2201. All rights reserved.
# Provenance: VAPOR-6eabb1be532bdef4
#
# Objective throughput for a height window, read from the chain itself (no
# trust in the load generator's own bookkeeping):
#
#   scripts/bench/chain-window.py <from_height> <to_height> [evm_rpc] [comet_rpc]
#
# sustained_tps = EVM txs in the window / (commit time of the last non-empty
# block - header time of the first block). Block H is committed at the header
# time of H+1. Also reports the peak block, gas used vs the 40M limit, and the
# gas *reserved* by declared limits (CometBFT packs blocks by gas wanted, so
# over-declared limits cap tx/block below what gas used would allow).
import json
import sys
import urllib.request
from datetime import datetime


def call(url, method, params):
    body = json.dumps({"jsonrpc": "2.0", "id": 1, "method": method, "params": params}).encode()
    req = urllib.request.Request(url, body, {"content-type": "application/json"})
    return json.load(urllib.request.urlopen(req))["result"]


def header_time(comet, h):
    r = json.load(urllib.request.urlopen(f"{comet}/header?height={h}"))["result"]["header"]["time"]
    # CometBFT emits RFC3339 with nanoseconds; keep microseconds for datetime
    base, frac = r.rstrip("Z").split(".") if "." in r else (r.rstrip("Z"), "0")
    return datetime.fromisoformat(f"{base}.{frac[:6].ljust(6, '0')}+00:00").timestamp()


def main():
    h0, h1 = int(sys.argv[1]), int(sys.argv[2])
    evm = sys.argv[3] if len(sys.argv) > 3 else "http://127.0.0.1:8545"
    comet = sys.argv[4] if len(sys.argv) > 4 else "http://127.0.0.1:26657"
    # optional: comma-separated sponsored senders; reports the max per-block
    # share of gas (by declared limit, as the lane cap counts it) they took
    sponsors = {a.lower() for a in (sys.argv[5].split(",") if len(sys.argv) > 5 else []) if a}
    max_sponsored = 0.0
    sponsored_txs = 0
    txs = gas_used = gas_reserved = 0
    peak_txs = peak_h = 0
    peak_used = peak_reserved = 0.0
    last_nonempty = None
    nonempty = 0
    for h in range(h0, h1 + 1):
        b = call(evm, "eth_getBlockByNumber", [hex(h), True])
        n = len(b["transactions"])
        limit = int(b["gasLimit"], 16)
        used = int(b["gasUsed"], 16)
        reserved = sum(int(t["gas"], 16) for t in b["transactions"])
        txs += n
        gas_used += used
        gas_reserved += reserved
        if n:
            nonempty += 1
            last_nonempty = h
        if n > peak_txs:
            peak_txs, peak_h = n, h
        if limit:
            peak_used = max(peak_used, used / limit)
            peak_reserved = max(peak_reserved, reserved / limit)
            if sponsors:
                sp = [t for t in b["transactions"] if t["from"].lower() in sponsors]
                sponsored_txs += len(sp)
                max_sponsored = max(max_sponsored, sum(int(t["gas"], 16) for t in sp) / limit)
    out = {"from_height": h0, "to_height": h1, "blocks": h1 - h0 + 1, "nonempty_blocks": nonempty, "evm_txs": txs}
    if last_nonempty is not None:
        t0 = header_time(comet, h0)
        t1 = header_time(comet, last_nonempty + 1)
        span = t1 - t0
        out.update({
            "span_sec": round(span, 3),
            "sustained_tps": round(txs / span, 1) if span > 0 else None,
            "peak_block": {"height": peak_h, "txs": peak_txs,
                           "block_time_ms": round((header_time(comet, peak_h + 1) - header_time(comet, peak_h)) * 1000)},
            "peak_gas_used_ratio": round(peak_used, 4),
            "peak_gas_reserved_ratio": round(peak_reserved, 4),
            "gas_used_per_tx": gas_used // txs if txs else 0,
            "gas_reserved_per_tx": gas_reserved // txs if txs else 0,
        })
        if sponsors:
            out.update({"sponsored_txs": sponsored_txs, "max_sponsored_gas_share": round(max_sponsored, 4)})
    print(json.dumps(out))


if __name__ == "__main__":
    main()
