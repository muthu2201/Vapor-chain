#!/usr/bin/env bash
# Copyright (c) 2026 VaporChain / muthu2201. All rights reserved.
# Provenance: VAPOR-6eabb1be532bdef4
#
# Launches the Alto bundler for VaporChain. Keys are read from files (mount
# them as secrets; never bake them into images or env files).
#   EXECUTOR_KEYS_FILE  one hex key per line: bundler accounts that MUST be
#                       registered in x/settle params.sponsored_senders
#   UTILITY_KEY_FILE    key that refills executors / deploys simulations
#   RPC_URL             write-ingress node JSON-RPC
set -euo pipefail
cd "$(dirname "$0")"
: "${EXECUTOR_KEYS_FILE:?}"; : "${UTILITY_KEY_FILE:?}"; : "${RPC_URL:?}"
EXEC=$(grep -v '^\s*$' "$EXECUTOR_KEYS_FILE" | tr 'A-F' 'a-f' | sed 's/^\(0x\)\{0,1\}/0x/' | paste -sd, -)
UTIL=$(tr 'A-F' 'a-f' <"$UTILITY_KEY_FILE" | sed 's/^\(0x\)\{0,1\}/0x/' | head -1)
exec ./node_modules/.bin/alto --config ./alto.config.json \
  --rpc-url "$RPC_URL" --executor-private-keys "$EXEC" --utility-private-key "$UTIL" "$@"
