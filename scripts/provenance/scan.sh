#!/usr/bin/env bash
# Copyright (c) 2026 VaporChain / muthu2201. All rights reserved.
# Proprietary. See LICENSE. Provenance: VAPOR-6eabb1be532bdef4
#
# Verifies VaporChain's authorship watermarks are present and consistent, and
# can scan an arbitrary tree for leaked copies.
#
#   scripts/provenance/scan.sh            # audit this repo (human output)
#   scripts/provenance/scan.sh --ci       # audit + fail if a required marker is missing
#   scripts/provenance/scan.sh --scan DIR # hunt for our fingerprint/canary in DIR
#
# This is forensic tooling: it proves derivation, it does not disable anything.
set -uo pipefail

FINGERPRINT="6eabb1be532bdef432109abc178d88669ab33aed940f18cd5169887d215c1fcf"
SHORT="VAPOR-6eabb1be532bdef4"
CANARY="vapor-canary-f915edb4136beb9b"
OWNER="VaporChain / muthu2201"
ROOT="$(cd "$(dirname "$0")/../.." && pwd)"
c_ok='\033[1;32m'; c_bad='\033[1;31m'; c_off='\033[0m'
fail=0
ok()   { printf "${c_ok}ok${c_off}   %s\n" "$1"; }
bad()  { printf "${c_bad}MISS${c_off} %s\n" "$1"; fail=1; }

# --scan MODE: search a foreign tree for our markers
if [[ "${1:-}" == "--scan" ]]; then
  target="${2:?usage: scan.sh --scan DIR}"
  echo "Scanning $target for VaporChain provenance…"
  hits=$(grep -rlIE "$FINGERPRINT|$CANARY|$SHORT" "$target" 2>/dev/null | grep -v "$ROOT" || true)
  if [[ -n "$hits" ]]; then
    printf "${c_bad}PROVENANCE FOUND${c_off} — these files derive from VaporChain (%s):\n" "$OWNER"
    echo "$hits"
    exit 3
  fi
  echo "no VaporChain markers found"
  exit 0
fi

ci=0; [[ "${1:-}" == "--ci" ]] && ci=1
cd "$ROOT"

# 1. the fingerprint constant is the canonical one
grep -q "Fingerprint = \"$FINGERPRINT\"" chain/provenance/provenance.go \
  && ok "Go fingerprint constant" || bad "Go fingerprint constant changed or missing"

# 2. every required surface embeds the fingerprint or short id
declare -A REQUIRED=(
  ["Go binary provenance"]="chain/provenance/provenance.go:$FINGERPRINT"
  ["council genesis seal (on-chain)"]="chain/x/council/keeper/genesis.go:provenance"
  ["Settle precompile provenance()"]="chain/precompiles/settle/methods.go:Provenance|chain/precompiles/settle/fee.go:provenance|chain/precompiles/settle/settle.go:provenance"
  ["Solidity Provenance lib"]="contracts/src/utils/Provenance.sol:$FINGERPRINT"
  ["paymaster hash mixes provenance"]="contracts/src/paymaster/VaporVerifyingPaymaster.sol:Provenance"
  ["TS SDK provenance constant"]="packages/sdk/src/constants.ts:$FINGERPRINT"
  ["sponsor service hash"]="services/sponsor/internal/userop/userop.go:$FINGERPRINT"
  ["vapor JSON-RPC namespace"]="chain/rpc/vapor.go:provenance"
)
for name in "${!REQUIRED[@]}"; do
  found=0
  IFS='|' read -ra alts <<<"${REQUIRED[$name]}"
  for alt in "${alts[@]}"; do
    f="${alt%%:*}"; pat="${alt#*:}"
    [[ -f "$f" ]] && grep -qi "$pat" "$f" && { found=1; break; }
  done
  (( found )) && ok "$name" || bad "$name"
done

# 3. the canary must exist somewhere and stay unique to us
grep -rqI "$CANARY" chain/provenance/provenance.go && ok "canary present" || bad "canary missing"

# 4. source-header coverage: fraction of source files carrying the short id
scan_files() { git ls-files "$@" 2>/dev/null | grep -vE 'contracts/(lib|out|cache)/|node_modules/|\.pb\.go$|/abi/'; }
mapfile -t SRC < <(scan_files '*.go' '*.sol' '*.ts' '*.tsx' | grep -vE '_test\.go$|\.t\.sol$|\.test\.ts$')
total=${#SRC[@]}; marked=0
for f in "${SRC[@]}"; do head -6 "$f" 2>/dev/null | grep -q "$SHORT" && marked=$((marked+1)); done
pct=$(( total ? marked * 100 / total : 0 ))
printf "header coverage: %d/%d source files (%d%%)\n" "$marked" "$total" "$pct"
(( pct >= 80 )) && ok "header coverage >= 80%" || { [[ $ci == 1 ]] && bad "header coverage below 80%" || printf "     (informational)\n"; }

if (( fail )); then
  echo; printf "${c_bad}provenance check FAILED${c_off}\n"
  [[ $ci == 1 ]] && exit 1
fi
echo; ok "provenance verified ($OWNER)"
