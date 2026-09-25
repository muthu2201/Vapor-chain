#!/usr/bin/env bash
# Copyright (c) 2026 VaporChain / muthu2201. All rights reserved.
# Provenance: VAPOR-6eabb1be532bdef4
#
# The whole verification suite behind docs/benchmarks/mainnet-sim.md: every CI
# job, plus every live end-to-end suite against a running localnet + services
# (scripts/localnet/localnet.sh start && scripts/localnet/services.sh up).
#
#   scripts/bench/full-suite.sh [out-dir]
#   STAGES="contracts e2e" scripts/bench/full-suite.sh      # subset
#
# Stages: go contracts frontend scans e2e web. Each step's output goes to
# <out-dir>/<stage>.log; a one-line-per-step summary is printed and written to
# <out-dir>/summary.txt. Exit code is non-zero if any step failed.
set -uo pipefail
ROOT="$(cd "$(dirname "$0")/../.." && pwd)"
OUT="${1:-$ROOT/.localnet/suite}"
STAGES="${STAGES:-go contracts frontend scans e2e web}"
NET_DIR="${NET_DIR:-$ROOT/.localnet}"
BIN="${BIN:-$ROOT/chain/build/vaporchaind}"
mkdir -p "$OUT"
# the web bootstrap runs TypeScript directly and the workspace requires Node >= 22
node_major=$(node -p "process.versions.node.split('.')[0]" 2>/dev/null || echo 0)
((node_major >= 22)) || { echo "full-suite: need Node >= 22 on PATH (found $(node --version 2>/dev/null || echo none))" >&2; exit 2; }
: >"$OUT/summary.txt"
FAILED=0

step() { # stage name cmd...
  local stage="$1" name="$2"; shift 2
  local t0=$SECONDS
  { echo "################ $name"; (cd "$ROOT" && "$@"); } >>"$OUT/$stage.log" 2>&1
  local rc=$?
  ((rc)) && FAILED=1
  printf '%-10s %-44s %s (%ss)\n' "$stage" "$name" "$([[ $rc == 0 ]] && echo PASS || echo "FAIL rc=$rc")" "$((SECONDS - t0))" | tee -a "$OUT/summary.txt"
}
key() { "$BIN" keys unsafe-export-eth-key "$1" --keyring-backend test --home "$NET_DIR/node0" 2>/dev/null; }

for stage in $STAGES; do
  : >"$OUT/$stage.log"
  case $stage in
  go)
    for d in chain services/sponsor services/indexer tools/loadgen; do
      step go "$d: build" bash -c "cd $d && go build -o /dev/null ./..."
      step go "$d: vet" bash -c "cd $d && go vet ./..."
      step go "$d: gofmt" bash -c "cd $d && test -z \"\$(gofmt -l \$(git ls-files '*.go' | grep -v '\\.pb\\.go\$'))\""
      if [[ $d == chain ]]; then
        step go "$d: test -race (-tags test)" bash -c "cd $d && go test -tags test -race ./..."
      else
        step go "$d: test -race" bash -c "cd $d && go test -race ./..."
      fi
    done
    ;;
  contracts)
    step contracts "forge build --sizes" bash -c "cd contracts && forge build --sizes"
    step contracts "forge lint --severity high" bash -c "cd contracts && forge lint src --severity high"
    step contracts "forge test" bash -c "cd contracts && forge test"
    ;;
  frontend)
    step frontend "pnpm install --frozen-lockfile" pnpm install --frozen-lockfile
    step frontend "gen:abis" pnpm gen:abis
    step frontend "build sdk + react" bash -c "pnpm --filter @vaporchain/sdk build && pnpm --filter @vaporchain/react build"
    step frontend "typecheck (all workspaces)" pnpm -r run typecheck
    step frontend "unit tests (packages)" pnpm -r --filter './packages/**' run test
    ;;
  scans)
    # identical to the CI secrets-scan job
    step scans "no committed keys" bash -c '
      if git ls-files | grep -E "(^|/)\.env\.local$|\.key\.json$|/secrets/"; then exit 1; fi
      if git grep -nIE "\b(0x)?[0-9a-fA-F]{64}\b" -- ":!*_test.go" ":!*.t.sol" ":!**/test/**" ":!**/testdata/**" ":!contracts/lib/**" ":!**/deployments/**" ":!*.json" ":!docs/**" \
         | grep -viE "fingerprint|provenance|keccak|hash|salt|topic|commit|sha256|canary|VAPOR-"; then exit 1; fi'
    step scans "provenance watermark" ./scripts/provenance/scan.sh --ci
    ;;
  e2e)
    OWNER=0x$(key dev) USER=0x$(key admin)
    step e2e "protocol: contracts/script/e2e-settle.sh" bash -c "cd contracts && RPC_URL=http://127.0.0.1:8545 OWNER_KEY=$OWNER USER_KEY=$USER ./script/e2e-settle.sh"
    step e2e "sdk: sponsored checkout, token launch, registry economics" \
      env VAPOR_E2E_OWNER_KEY="$OWNER" VAPOR_E2E_REPORT="$OUT/registry-economics.json" pnpm --filter @vaporchain/sdk run test:e2e
    step e2e "react: hooks against the live stack" env VAPOR_E2E_OWNER_KEY="$OWNER" pnpm --filter @vaporchain/react run test:e2e
    ;;
  web)
    step web "bootstrap-localnet (apps + .env.local)" node apps/web/scripts/bootstrap-localnet.ts
    step web "next build" pnpm --filter @vaporchain/web run build
    step web "playwright: real browser, real chain" pnpm --filter @vaporchain/web run test:e2e
    ;;
  *) echo "unknown stage $stage" >&2; FAILED=1 ;;
  esac
done
echo "summary: $OUT/summary.txt"
exit $FAILED
