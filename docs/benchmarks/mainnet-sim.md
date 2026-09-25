<!-- Copyright (c) 2026 VaporChain / muthu2201. All rights reserved. Provenance: VAPOR-6eabb1be532bdef4 -->
# Mainnet simulation: suite, load benchmark and registry economics

One document for the full pre-testnet verification run: every CI job, every
live end-to-end suite, a load campaign with independently verified numbers, and
a measured answer to the question *"what does it cost a developer or a user to
go around the registry?"*

Everything here was measured on a running 4-validator chain with **mainnet
parameters** and the full off-chain stack (sponsor, bundler, indexer). No mocks.
Raw data: `docs/benchmarks/raw/r7-*.json`. Where a number is a projection
rather than a measurement, it says so.

> **Scope.** This is a single-machine simulation. It proves the system is
> correct and holds together under load. It does not prove geographic
> multi-operator performance, real bridge liquidity, or behaviour with real
> users. That is what the public testnet is for.

---

## 1. Verdict

| area | result |
|---|---|
| Test matrix (CI jobs + live e2e + browser e2e) | **all green on v3, in one run (exit 0)** — 32 steps; 12 Go packages under `-race`, 26 Solidity tests, 35 TS unit tests, 15 protocol checks, 15 live SDK/React e2e tests (9 of them registry economics), 5 Playwright tests (§3). GitHub CI had been red on every push for two CI-config bugs, now fixed (F-8) |
| Consensus under load | 4/4 validators in sync throughout; **0** consensus failures, **0** panics, **0** nil-prevotes / rejected proposals |
| Sponsored-lane cap (50%) | **0.5000** max share per block, verified from the chain by recovered signer over 3,008 sponsored txs |
| Fee floor (anti-spam) | **0 / 8,962** sub-floor txs admitted |
| Gas-burn conservation | over 55,870 txs: supply burned **== fees paid, to the wei** (6.930291594214504 CREDIT) |
| Settle invariants | no breach, no safe-mode pause, across the whole run |
| Sustained throughput on this box | **~186 tps** comfortable (100% inclusion, p50 finality 0.8 s); **~250–275 tps** ceiling, set by CPU (see §4) |
| Who decides which apps get free gas? | **Nobody (v3).** No attestors, biometrics, face ID or documents: base quota is bought with refundable USDC bonded behind the app and is linear in it, so fake apps gain nothing — live, one bond split over two apps bought exactly the same 25M gas as one app, and fresh apps got 0 (§5.5) |
| Does going around the registry cost real money? | **Yes, with economics v2.** Every unsponsored transaction pays for its compute: an ERC-20 transfer costs **$0.0017** (was $3.4×10⁻⁸ at v1 prices). Users of registered, verified apps pay **$0**; fake apps get **0** sponsored gas; farming sponsorship returns at most $0.90 per $1 (§5) |
| Findings | 8 total: 6 fixed (F-1, F-2, F-3, F-6, F-7, F-8), 2 open with recommendations (F-4, F-5) — §6 |

---

## 2. Environment

| | |
|---|---|
| Hardware | **one** VM, 4 vCPU Intel Xeon @ 2.10 GHz, 15 GB RAM — all 4 validators, sponsor, Alto bundler, indexer, Postgres and the load generator share it |
| Chain | `vaporchaind` @ `82f8db1` (hardened build), Cosmos SDK 0.54.4, CometBFT 0.39.4, cosmos/evm 0.7.3, Go 1.26.8 |
| Topology | 4 validators (PoA council admissions, real gentx/collect-gentxs genesis), app-side mempool, ~1 s blocks, 40M block gas |
| Economics | fee 1% (min 0.002 / max 5 USDC), split app 50 / validators 20 / relayer 10 / treasury 20, `gas_burn_bps = 10000`, min gas price 1 gwei. **v1** (load campaign, §4): `credit_price` 1e15 (1 CREDIT = $0.001), `quota_weight` 1000, registration 10 CREDIT, base quota 2M gas to every app. **v2** (current genesis, §5): `credit_price` 2e10 (1 CREDIT = $50), `quota_weight` 2, registration 0.2 CREDIT, base quota 20M gas to verified apps only, sponsor fee cap 4 gwei. **v3** (current): as v2, but base quota comes from bonded USDC (`gas_per_bonded_unit` 2,500 gas/epoch per USDC, 21-day unbonding; localnet 120 blocks) and the attestor role is removed. Localnet epoch = 60 blocks (production 86,400). Throughput and gas usage do not depend on the price |
| Off-chain | sponsor (ERC-7677) :8800, Alto bundler (ERC-4337 v0.8) :4337, indexer :8900, Postgres 16 |
| Contracts | EntryPoint v0.8 + Simple7702Account at canonical addresses (CREATE2 replay), VaporVerifyingPaymaster, VaporTokenPaymaster, VaporTokenFactory |
| Tooling | forge 1.8.3, Node 22.22, pnpm 12.6, Playwright + Chromium |

---

## 3. The suite

One command runs all of it against a live localnet:
`scripts/bench/full-suite.sh` (stages: `go contracts frontend scans e2e web`).

| stage | step | what it proves | result |
|---|---|---|---|
| go | `chain`: build, vet, gofmt, `test -race -tags test` | settle money paths, fee/split fuzzing, gas burn, apps quota/HLL/ownership/moves, council PoA, IBC guard, lane cap + Prepare/Process agreement, RPC override (8 packages) | PASS ¹ |
| go | `services/sponsor`: build, vet, gofmt, `test -race` | sponsorship policy (foreign calls, cross-app approve, value, rogue 7702 delegate), Go↔Solidity hash parity, rate limiting (3 packages) | PASS |
| go | `services/indexer`: build, vet, gofmt, `test -race` | indexing pipeline end-to-end | PASS |
| go | `tools/loadgen`: build, vet, gofmt | load generator compiles clean | PASS |
| contracts | `forge build --sizes` | all contracts compile under the size limit | PASS |
| contracts | `forge lint src --severity high` | no high-severity lint findings | PASS (CI invocation fixed, F-8) |
| contracts | `forge test` | paymasters, token paymaster, token factory, checkout — 26 tests / 4 suites | PASS |
| frontend | `pnpm install --frozen-lockfile`, `gen:abis` | lockfile honoured; committed ABIs byte-identical to the build | PASS |
| frontend | build SDK + React, typecheck all workspaces | SDK, React and web app type-check (including the new economics test) | PASS |
| frontend | unit tests | SDK 20 + React 15 tests | PASS (CI invocation fixed, F-8) |
| scans | no committed keys; provenance watermark | the CI secrets scan and watermark scan, identical to CI | PASS |
| e2e | `contracts/script/e2e-settle.sh` | 15 protocol checks against the live precompile | PASS |
| e2e | SDK live e2e | sponsored 7702 checkout (3), token launch (1), registry economics (9) — 13 tests | PASS |
| e2e | React live e2e | hooks against the live stack under CORS — 2 tests | PASS |
| web | bootstrap, `next build`, Playwright | real Chromium vs real chain: CSP/headers, faucet validation, first-visit gasless purchase, gasless token launch, indexed dashboard — 5 tests | PASS |

¹ The chain suite runs in full under `-race`, including `TestBondedBaseQuota`.
The live suites bond 10,000 test USDC (`Settle.bondApp`) behind every app
whose users should be gasless — no attestor or special key is involved.
Two local-environment issues were found and removed along the way — `node` on
`PATH` resolving to v20 (the runner now refuses to start below Node 22) and a
corrupt Turbopack cache in `apps/web/.next` — neither is a code defect.
The economics suite ran twice on different chain states: every asserted value
was identical and the measured gas differed by ≤ 0.12%.

Live suites in more detail:

- **Protocol e2e** (`contracts/script/e2e-settle.sh`): provenance fingerprint →
  register app (fee burned) → checkout self-claims, gets no attribution until
  the owner accepts → `payOrder` reverts without `approveApp` → app-scoped
  allowance → merchant receives net (9.9 of 10 USDC) → app claimable 45,000
  (50% minus 10% referrer) → double payment reverts → claim → token launch →
  token-paymaster refill (3 USDC → 3,000 CREDIT via `buyCredits`).
- **SDK e2e** (`packages/sdk/test/e2e`): a brand-new EOA with **zero gas**
  upgrades itself with EIP-7702 and pays an order in one sponsored UserOp;
  the sponsor refuses calls outside the app; token launch; and the
  **registry-economics** suite behind §5.
- **React e2e**: the hooks against the live stack in a DOM that enforces CORS.
- **Web e2e (Playwright)**: a real browser on `next start` against the real
  chain — strict nonce CSP and security headers, faucet validation, then a
  first-time visitor creates a wallet, gets test USDC, buys an item gaslessly
  and survives a reload.

---

## 4. Load campaign

`scripts/bench/load-campaign.sh` drives `tools/loadgen` over all four
validators' JSON-RPC endpoints with 200 funded accounts, 30 s per scenario.
Every number below was **read back from the chain** (`scripts/bench/chain-window.py`),
not taken from the generator's own bookkeeping — see finding F-6 for why that
matters.

| scenario | target tps | sent | mined | chain-sustained tps | peak block | peak gas used / reserved | finality p50 / p95 | reverts |
|---|--:|--:|--:|--:|---|--:|--:|--:|
| native transfer | 200 | 5,980 | 5,980 (100%) | **185.9** | 247 tx, 1.05 s | 13% / 13% | 0.77 s / 1.3 s | 0 |
| native transfer | 500 | 13,963 | 12,497 † | **259.0** | 1,904 tx, 6.9 s | 99.96% / 99.96% | 5.3 s / 15.4 s | 0 |
| ERC-20 transfer | 300 | 8,904 | 6,174 ‡ | 253.2 (window) | 961 tx, 4.3 s | 70% / 100% | 2.9 s / 8.0 s | 0 |
| Settle `pay` | 300 | 8,690 | 8,690 (100%) | **129.8** | 160 tx, 1.0 s | **50% / 100%** | 20.5 s / 34.1 s | 0 |
| mixed (1/3 each) | 300 | 8,423 | 8,352 | **168.8** | 316 tx, 1.7 s | 53% / 100% | 11.6 s / 19.4 s | 0 |
| sponsored lane | 200 | 5,985 | 5,936 | 77.3 § | 217 tx, 1.0 s | 34% / 61% | 1.8 s / 40.4 s | 0 |
| sub-floor spam | 300 | 8,962 | **0** | — | — | — | — | — |
| native transfer | 1,000 | 17,207 | 8,192 † | **199.1** | 1,904 tx, 7.65 s | 99.96% / 99.96% | 16.8 s / 27.9 s | 0 |

† Past the knee the mempool accepts more than it can mine. At 500 tps, 2,730
of the backlog were mined during the next scenario and 1,466 accepted txs were
never mined; at 1,000 tps, 8,372 of 16,564 accepted txs were never mined. See F-4.
‡ The ERC-20 window also mined 2,730 leftover transfers from the 500-tps run;
2,730 ERC-20 txs were rejected on nonce collision with that backlog.
§ Throughput here is the lane cap working: the sponsor's 500k-gas txs are held
to 40 per block (20M gas), so the sponsored backlog drains slowly while paying
traffic flows (p50 1.8 s).

**What sets the ceiling.** A block of 1,904 transfers — the 40M gas limit — took
6.9–7.65 s to produce because all four validators execute every block on the
same 4 vCPUs. So on this box the knee (~250–275 transfers/s) is **CPU-bound,
not gas- or consensus-bound**; the gas limit alone would allow ~1,900 tx per
~1 s block. Dedicated validator hardware should move the knee substantially,
but that has to be measured on the testnet, not assumed.

**Sponsored-lane cap.** Max sponsored share of any block's gas = **0.5000**,
recomputed from the chain by recovering each tx's signer (3,008 sponsored txs).
Sponsored traffic can never take more than half the block, whatever the bundler
submits.

**Fee floor.** All 8,962 txs at `maxFeePerGas = 1 wei` were refused at mempool
entry (`max fee per gas (1) is lower than the base fee (1000000000)`); none
reached a block.

**Gas-burn conservation.** From height 1463 to 1871 the chain mined 55,870 EVM
txs paying 6,930,291,594,214,504,000 acredit in gas. The acredit supply fell by
exactly 6,930,291,594,214,504,000 over the same window
(`raw/r7-burn-conservation.json`). Every unit of gas is burned; nothing leaks
to the fee collector or distribution.

**Liveness.** After the campaign all four nodes were in sync; zero
`CONSENSUS FAILURE`, panics, nil-prevotes or rejected proposals in any
validator's log; settle never entered safe mode.

**Regression on economics v2** (fresh chain, repriced genesis;
`raw/r8-*.json`): transfer @ 200 → **182.4** chain-sustained tps, 100% inclusion;
sponsored lane → max share **0.5000** (chain-verified, per block); sub-floor spam →
**0** admitted; burn conservation over 11,974 txs → supply burned **==** fees paid
(1.882404 CREDIT, ~$94 of compute at the new price). Repricing changed nothing in
throughput, the lane cap or the burn.

---

## 5. Registry economics — every unsponsored transaction pays for compute

The same live suite (`packages/sdk/test/e2e/registry-economics.test.ts`) was run
against two parameter sets: the original genesis prices (**v1**,
`raw/r7-registry-economics.json`, 7/7) and the infrastructure-cost prices now in
genesis (**v2**, `raw/r8-registry-economics.json`, 8/8). USD figures convert
credits at the protocol price.

### 5.1 What changed in v2

| parameter | v1 | **v2 (current genesis)** | why |
|---|---|---|---|
| `credit_price` (acredit per uusdc) | 1e15 — 1 CREDIT = $0.001 | **2e10 — 1 CREDIT = $50** | gas priced as infrastructure cost; a transfer costs L2-like money |
| base quota | 2M gas/epoch to **every** app | **v2:** 20M gas/epoch, attestor-verified apps only → **v3:** 2,500 gas/epoch per USDC **bonded**, no gatekeeper | closes the fake-app loophole (F-1) without trusted humans (§5.5) |
| `quota_weight` | 1000 | **2** | keeps farming unprofitable at the new price |
| sponsor `maxFeePerGas` cap | 100 gwei | **4 gwei** | bounds what earned quota is worth under congestion |
| registration fee | 10 CREDIT ($0.01) | **0.2 CREDIT ($10)** | anti-spam only — registering buys no gas |
| `Settle.bondApp` / `unbondApp` / `appBond` | — | **new (v3)** | EVM-native bonding; unbonding returns capital after 21 days |

### 5.2 One 10 USDC payment, measured on v2

| path | user needs gas? | user gas cost | protocol fee | payee gets | developer earns | protocol |
|---|---|--:|--:|--:|--:|---|
| **Registered app with quota** (bonded or earned; sponsored UserOp) | **no** — a zero-balance user works | **$0** | $0.10 | $9.90 | **$0.05** | keeps $0.05; pays $0.027 gas for the first op |
| **Bypass: plain ERC-20 transfer** | yes — and cannot get it alone | **$0.0017** | $0 | $10.00 | $0 | gas burned |
| **Bypass: Settle rails, no app** | yes | **$0.0059** | $0.10 | $9.90 | **$0** | treasury +$0.07 |
| **Fake app** (registered, no bond, no fees) | yes | same as bypass | — | — | — | **0 sponsored gas**; the sponsor refuses it |

Per-operation cost at the 1 gwei floor (measured gas; effective price 1.125 gwei):

| operation | gas | **v2 cost** | v1 cost |
|---|--:|--:|--:|
| native CREDIT transfer | 21,000 | $0.0012 | $2.4×10⁻⁸ |
| ERC-20 transfer | 30,596 | **$0.0017** | $3.4×10⁻⁸ |
| `Settle.pay` | 105,576 | $0.0059 | $1.2×10⁻⁷ |
| app registration (gas only; fee on top) | 79,270 | $0.0045 | ~$8×10⁻⁸ |
| first sponsored UserOp (7702 auth + approve + pay) | 430,198 | $0.027 — paid by the app's quota | $5.4×10⁻⁷ |

Every transaction that is not sponsored by a registered app now costs about
**50,000× more than in v1** — real USDC for the compute it uses — while users of
registered apps with quota still pay nothing.

What the suite asserts on every run (v2 additions in bold):

1. Registration burns exactly the fee (0.2 CREDIT) on top of gas.
2. **The app bonds 10,000 USDC (`bondApp`) — no approval of any kind**; a
   user holding USDC and zero credits then pays 10 USDC in one sponsored
   UserOp. The user's credit balance stays 0, the paymaster's deposit falls by
   exactly the op's `actualGasCost`, and the app's claimable rises by 50% of the fee.
3. Off-registry, the same zero-credit user can neither transfer nor call
   `buyCredits` — a third party must fund their first transaction.
4. After a third party grants 0.01 CREDIT, a plain transfer pays no protocol fee
   but **costs more than $0.0005 of gas** ($0.0017 measured) and earns the
   developer nothing.
5. `Settle.pay` without an app charges the normal fee, sends 70% of it to the
   treasury (the unclaimed 50% app share included) and **costs real gas**.
6. The sponsor refuses unregistered contracts, plain token transfers and
   `approveApp` for another app.
7. **Three fresh registrations have 0 quota and a sponsored call from one is
   refused; the bonded app's base is exactly bond × rate (25M gas for 10,000 USDC).**
8. **Farm bound, from the live params:** app share 0.5 + sponsored-gas rebate at
   the sponsor's cap 0.4 = **0.90 < 1**. Every dollar of fees paid to farm
   sponsored gas returns at most 90 cents (10 cents at the fee floor).
9. **Sybil-proof (v3):** one app bonding 10,000 USDC and two apps bonding 5,000
   each get the same total quota (25,000,000 = 25,000,000 gas). Unbonding drops
   quota to 0 at once while the capital stays locked until the release height;
   on chain, EndBlock at that height returned exactly the 5,000 USDC
   (`app_unbonded`).

### 5.3 The limits, stated plainly

- **The price is the same for everyone.** A user of an unregistered contract and
  an unsponsored user of a registered one pay the same gas. The registry's edge
  is sponsorship (its users pay $0) and revenue share, not a surcharge on
  outsiders. That keeps the EVM standard — wallets, tooling and gas estimation
  work unchanged — and it cannot be dodged by routing calls through a proxy,
  because gas is charged for every opcode no matter which contract runs it.
- **Unregistered users can still transact without credits** by paying gas in
  USDC through the token paymaster (priced from `quoteCredits`, plus a 10% markup
  to the treasury). They pay real money for the compute, which is the point.
- **For a merchant, the protocol fee still dominates** (1% of $10 = $0.10 vs
  $0.0017 of gas). Going around the registry saves the fee but gives up
  gasless onboarding and the 50% revenue share, and now pays for its compute.
- `credit_price`, `quota_weight`, base quota and the sponsor cap are
  governance/ops parameters. Keep
  `app_share + quota_weight × sponsor_max_fee ÷ credit_price < 1` whenever any
  of them changes; the economics suite checks it from live params on every run.

### 5.4 Setting the price from infrastructure spend

`credit_price = 1e12 ÷ P`, where `P` is USD per CREDIT; at the 1 gwei floor a
unit of gas costs `P × 10⁻⁹` USD. To recover infrastructure cost:

```
P ≈ monthly infra spend ÷ (gas sold per month × 10⁻⁹)
```

Example: $10,000/month (4 validators, RPC, bundler, indexer) and 200 billion gas
sold per month (~6.5 million ERC-20 transfers, ~2.5 tx/s on average) gives
**P ≈ $50**, the genesis value. More traffic at the same `P` over-recovers cost
and governance can lower it; the fee floor still rises with congestion
(EIP-1559). Bonded base quota costs the protocol at most
`gas_per_bonded_unit × 10⁻⁹ × P` per bonded USDC per epoch: 2,500 × 10⁻⁹ × $50 =
$0.000125, i.e. **~4.6% of the bond per year** in gas on the production
86,400-block epoch (the localnet uses 60-block epochs for speed). That gas can
only sponsor the app's own users, so total exposure is bounded by
`rate × total bonded` and governance can lower the rate at any time.

### 5.5 v3: no gatekeepers — base quota bought with bonded capital

v2 closed the fake-app loophole with human attestors. v3 removes them. The
research and the rejected alternatives (biometric personhood, passkeys/Face ID,
document KYC, social-graph and ceremony-based personhood, stamp aggregators,
DNSSEC proofs, proof of work, token-curated registries) are in
`docs/security/sybil-resistance.md`. The adopted mechanism is the one ERC-4337
uses against fake paymasters: **make the attacker lock capital**.

| measured live (`raw/r9-registry-economics.json`, 9/9) | result |
|---|---|
| 3 fresh registrations, no bond | **0** gas each; sponsored call refused |
| 10,000 USDC bonded | **25,000,000** gas/epoch (= bond × 2,500 / USDC) |
| same 10,000 split over two apps | **25,000,000** gas total — splitting gains nothing |
| unbond | quota → **0** immediately; capital locked 116 more blocks, then returned by EndBlock (`app_unbonded`, 5,000 USDC) |
| bond yield, paid only as sponsorship gas | ~**4.6%/yr** at the floor (≤ ~18% at the sponsor cap) |
| farming via fees | still ≤ **$0.90** back per $1 |

## 6. Findings

| id | finding | status |
|---|---|---|
| F-1 | **Base quota was Sybil-farmable.** `QuotaFor` gave every ACTIVE app `base_gas_per_epoch` unconditionally while registration cost $0.01. On the localnet's 60-block epoch, ~600 registrations ($6) could fill the whole 50% sponsored lane every epoch at the protocol paymaster's expense. On the production 86,400-block epoch the per-app leak is ~1,440× slower, but it is unbounded in time and scales with any repricing of gas. | **fixed** — v2 gated it on human-attested domains; **v3 replaced that with capital-bonded quota** (`keeper.BaseQuota`, `TestBondedBaseQuota`): no gatekeeper, linear in capital, live: fresh apps get 0, a split bond buys exactly what one bond buys |
| F-2 | **At v1 prices, bypassing the registry was cheap and the gas burn captured ~nothing** ($3.4×10⁻⁸ per transfer). | **fixed (v2)** — gas priced as infrastructure cost: $0.0017 per ERC-20 transfer, measured; coupled params retuned (§5) |
| F-3 | **Credits move peer-to-peer as EVM value.** Bank sends are disabled, but ERC-4337 needs native value transfers, so holders can trade credits OTC. Supply still only grows via `buyCredits`, and the protocol never redeems. The docs claimed "non-transferable". | **fixed** — ARCHITECTURE, threat model and hardening docs corrected |
| F-4 | **Accepted ≠ mined under overload.** Past the knee, the EVM pool (geth `legacypool` semantics: `account-slots 16`, `global-slots 5120`, `global-queue 1024` per node) returns success and later truncates; after the 1,000-tps run, 2,470 txs sat queued behind nonce gaps. Not a safety issue, but a wallet sees a hash that never lands. | **open** — ops + client guidance below |
| F-5 | **Over-declared gas limits halve block capacity.** Blocks are packed by declared gas: Settle `pay` sent with 250k used 125k, so blocks filled at 160 tx with 50% of gas unused. | **open** — guidance (the SDK already estimates) |
| F-6 | **The load generator overstated throughput past the knee.** `onchain_tps` divided everything mined — including a long drain tail — by the 30 s send window (e.g. settle: reported 270.9, chain-verified 129.8). The earlier `results.md` used the same formula. | **fixed** — `onchain_tps_sustained` added; campaign now embeds a chain-verified window per run. A second loadgen bug found on the v2 run is fixed too: when the drain window ended mid-inclusion, `blockTime` cached a Unix-epoch fallback for a not-yet-produced block, zeroing the sustained rate and corrupting latency for the last block; it now waits for the successor block and caches real header times only |
| F-7 | **Service stack could not start in a root container** (`initdb` ran as root). | **fixed** in `0056321` |
| F-8 | **GitHub CI had failed on every push since the first full run**, from two workflow bugs, not code: `forge lint --severity high src` (forge 1.8.3 parses `src` as a second severity) and an unquoted `--filter ./packages/**` that the shell expanded, so pnpm ran a "script" named `./packages/sdk`. The same unquoted glob was in the root `package.json` scripts. | **fixed** — `forge lint src --severity high`, filters quoted; both reproduced and verified |

### Recommendations

- **F-4:** rate-limit at the public RPC gateway so overload gets an immediate
  429 instead of a silent drop; size `global-slots`/`account-slots` on RPC
  nodes to a few blocks of capacity; clients should treat a tx hash as
  "submitted", wait for a receipt with a timeout, and rebroadcast/replace (the
  bundler already does this for UserOps).
- **F-5:** keep Settle gas estimates tight (the SDK uses `eth_estimateGas`);
  flag integrations that hard-code large gas limits.
- **Bond rate:** `gas_per_bonded_unit` sets how much sponsorship a bonded USDC
  buys (≈4.6%/yr of the bond in gas today). Raise it to make onboarding
  cheaper for apps, lower it to cut protocol exposure; it never affects
  Sybil resistance, which comes from linearity.

---

## 7. Reproduce

```bash
# chain + services
(cd chain && make build)
scripts/localnet/localnet.sh reset && scripts/localnet/localnet.sh start
scripts/localnet/services.sh up
# fund load accounts (or init with N_LOAD_ACCOUNTS=200), then:
scripts/bench/load-campaign.sh docs/benchmarks/raw 200
# every CI job + every live e2e
scripts/bench/full-suite.sh
# one window, chain-verified, with the lane share for a sponsored sender
scripts/bench/chain-window.py <from> <to> http://127.0.0.1:8545 http://127.0.0.1:26657 <sponsor_hex>
```
