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
| Test matrix (CI jobs + live e2e + browser e2e) | **all green** — 32 steps; 12 Go packages under `-race`, 26 Solidity tests, 35 TS unit tests, 15 protocol checks, 13 live SDK/React e2e tests, 5 Playwright tests (§3). GitHub CI had been red on every push for two CI-config bugs, now fixed (F-8) |
| Consensus under load | 4/4 validators in sync throughout; **0** consensus failures, **0** panics, **0** nil-prevotes / rejected proposals |
| Sponsored-lane cap (50%) | **0.5000** max share per block, verified from the chain by recovered signer over 3,008 sponsored txs |
| Fee floor (anti-spam) | **0 / 8,962** sub-floor txs admitted |
| Gas-burn conservation | over 55,870 txs: supply burned **== fees paid, to the wei** (6.930291594214504 CREDIT) |
| Settle invariants | no breach, no safe-mode pause, across the whole run |
| Sustained throughput on this box | **~186 tps** comfortable (100% inclusion, p50 finality 0.8 s); **~250–275 tps** ceiling, set by CPU (see §4) |
| Does bypassing the registry make transactions expensive? | **No — not at genesis prices.** A plain transfer costs ~$3×10⁻⁸ of gas. What the registry actually controls is onboarding, revenue share and sponsorship (§5) |
| Findings | 8 total: 4 fixed (F-3, F-6, F-7, F-8), 4 open with recommendations (F-1, F-2, F-4, F-5) — §6 |

---

## 2. Environment

| | |
|---|---|
| Hardware | **one** VM, 4 vCPU Intel Xeon @ 2.10 GHz, 15 GB RAM — all 4 validators, sponsor, Alto bundler, indexer, Postgres and the load generator share it |
| Chain | `vaporchaind` @ `82f8db1` (hardened build), Cosmos SDK 0.54.4, CometBFT 0.39.4, cosmos/evm 0.7.3, Go 1.26.8 |
| Topology | 4 validators (PoA council admissions, real gentx/collect-gentxs genesis), app-side mempool, ~1 s blocks, 40M block gas |
| Economics (genesis) | fee 1% (min 0.002 / max 5 USDC), split app 50 / validators 20 / relayer 10 / treasury 20, `gas_burn_bps = 10000`, min gas price 1 gwei, `credit_price` 1e15 acredit per uusdc (**1 CREDIT = $0.001**), registration fee 10 CREDIT, base quota 2,000,000 gas / 60-block epoch, `quota_weight` 1000 |
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
| e2e | SDK live e2e | sponsored 7702 checkout (3), token launch (1), registry economics (7) — 11 tests | PASS |
| e2e | React live e2e | hooks against the live stack under CORS — 2 tests | PASS |
| web | bootstrap, `next build`, Playwright | real Chromium vs real chain: CSP/headers, faucet validation, first-visit gasless purchase, gasless token launch, indexed dashboard — 5 tests | PASS |

¹ Go reused its test cache for `chain` (identical inputs to a full
`-race` run earlier in the same session: 8/8 packages, 12 s for `x/settle/keeper`).
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

---

## 5. Registry economics — what bypassing actually costs

Measured by `packages/sdk/test/e2e/registry-economics.test.ts` (7/7 passing;
report `raw/r7-registry-economics.json`). USD figures convert credits at the
protocol price (1 CREDIT = $0.001). One 10 USDC payment through each path:

| path | user needs gas? | user gas cost | protocol fee | payee gets | developer earns | protocol keeps |
|---|---|--:|--:|--:|--:|--:|
| **Registered** — app + attributed contract, sponsored UserOp | **no** — zero-balance user works | **$0** | $0.10 | $9.90 | **$0.05** | $0.05 (and pays $5.4×10⁻⁷ gas) |
| **Bypass: plain ERC-20 transfer** (no registry, no Settle) | **yes** — and cannot get it alone | $3.4×10⁻⁸ | $0 | $10.00 | $0 | $0 (gas burned) |
| **Bypass: Settle rails, no app** | yes | $1.2×10⁻⁷ | $0.10 | $9.90 | **$0** | **$0.10** (treasury +$0.07) |

What was asserted, step by step:

1. **Registration burns exactly the fee** — 10 CREDIT left the owner on top of
   68,449 gas.
2. **Registered path:** a user holding USDC and *zero* credits paid 10 USDC in
   one sponsored UserOp (430,616 gas including the EIP-7702 authorization). The
   user's credit balance stayed 0; the paymaster's EntryPoint deposit fell by
   exactly the op's `actualGasCost`; the app's claimable rose by 50% of the fee.
3. **The onboarding wall:** the same zero-credit user, off-registry, could
   neither send a plain USDC transfer nor call `buyCredits` —
   both rejected for insufficient funds for gas. Off the registry, a new user
   cannot make a first transaction without a third party handing them credits.
4. **Bypass after funding:** once a third party sent 0.01 CREDIT (enough for
   ~290 transfers), a plain transfer delivered the full 10 USDC with no protocol
   fee, cost $3.4×10⁻⁸ of gas, and earned the developer nothing.
5. **Using the rails without registering:** `Settle.pay` from an EOA charged
   the normal 1% fee, and the treasury received **70%** of it — the 50% app
   share with no app to receive it goes to the treasury (`fees.go`).
6. **The sponsor cannot be ridden from off-registry:** a checkout that
   self-claimed an app but was never accepted, a plain token transfer, and an
   `approveApp` for a different app were all refused
   (`sponsorship denied: call target … is not a contract of app 7`, `… SETTLE.approveApp
   must target the sponsoring app`).
7. **What one registration buys:** three fresh registrations each had the full
   base quota (2,000,000 gas per epoch) immediately, with no payments (F-1).

### The honest answer

At genesis prices **bypassing the registry is not expensive** for anyone once
they hold credits: gas is about three millionths of a cent. For a merchant,
the registered path is actually 1% *more* expensive per payment, half of
which comes back to the developer. What the registry really controls is:

- **Onboarding.** Registered apps take users from zero with no gas at all;
  off-registry, every new user hits a wall that only a third party can clear.
- **Revenue.** Registered developers earn 50% of the fees their contracts
  generate; using the rails without registering hands that half to the treasury.
- **Sponsorship.** The protocol paymaster only pays for calls into a
  registered, accepted app's own contracts.

The gas burn is exact, but at 1 CREDIT = $0.001 it captures almost nothing.
Making bypass *expensive* means making gas cost real money, which is a repricing
decision — and it is coupled to two other parameters in a way that is easy to
get dangerously wrong.

### Repricing is coupled (projection from the measured gas figures)

Let `P` be the USD price of 1 CREDIT (set through `credit_price` = 1e12 / P
acredit per uusdc). With the measured 30,596 gas per transfer at 1.125 gwei:

| 1 CREDIT = | ERC-20 transfer costs | earned-quota farm ratio R (quota_weight 1000) | base quota worth, per app per day |
|---|--:|--:|--:|
| **$0.001 (genesis)** | $3.4×10⁻⁸ | 0.0011 (≈900× unprofitable) | $0.0032 |
| $1 | $3.4×10⁻⁵ | **1.1 (break-even farming)** | $3.24 |
| $10 | $3.4×10⁻⁴ | **11** | $32 |
| $100 | $0.0034 | **112** | $324 |

`R = quota_weight × gas_price ÷ credit_price` is the sponsored gas an app earns
per unit of fee it pays. Above 1, paying fees to farm quota is profitable and
the paymaster drains. So any repricing must, in the same change:

1. scale `quota_weight` so R stays well below 1 (at $100/CREDIT, `quota_weight = 1`
   gives R ≈ 0.11);
2. stop handing an unconditional base quota to every registration (F-1);
3. re-denominate the registration fee, faucet amounts and paymaster floats,
   which are all set in credits.

---

## 6. Findings

| id | finding | status |
|---|---|---|
| F-1 | **Base quota is Sybil-farmable.** `QuotaFor` gives every ACTIVE app `base_gas_per_epoch` unconditionally; registration costs $0.01. The fee is recouped in ~3 days of base quota; ~600 registrations ($6) fill the entire 50% sponsored lane every epoch at the protocol paymaster's expense (~$1.94/day at genesis prices, ×1000 per 1000× repricing), crowding out legitimate apps' gasless UX. | **open** — see recommendations |
| F-2 | **At genesis prices, bypassing the registry is cheap and the gas burn captures ~nothing.** Mechanism correct, magnitude negligible. | **open** — pricing decision |
| F-3 | **Credits move peer-to-peer as EVM value.** Bank sends are disabled, but ERC-4337 needs native value transfers, so holders can trade credits OTC. Supply still only grows via `buyCredits`, and the protocol never redeems. The docs claimed "non-transferable". | **fixed** — ARCHITECTURE, threat model and hardening docs corrected |
| F-4 | **Accepted ≠ mined under overload.** Past the knee, the EVM pool (geth `legacypool` semantics: `account-slots 16`, `global-slots 5120`, `global-queue 1024` per node) returns success and later truncates; after the 1,000-tps run, 2,470 txs sat queued behind nonce gaps. Not a safety issue, but a wallet sees a hash that never lands. | **open** — ops + client guidance below |
| F-5 | **Over-declared gas limits halve block capacity.** Blocks are packed by declared gas: Settle `pay` sent with 250k used 125k, so blocks filled at 160 tx with 50% of gas unused. | **open** — guidance (the SDK already estimates) |
| F-6 | **The load generator overstated throughput past the knee.** `onchain_tps` divided everything mined — including a long drain tail — by the 30 s send window (e.g. settle: reported 270.9, chain-verified 129.8). The earlier `results.md` used the same formula. | **fixed** — `onchain_tps_sustained` added; campaign now embeds a chain-verified window per run |
| F-7 | **Service stack could not start in a root container** (`initdb` ran as root). | **fixed** in `0056321` |
| F-8 | **GitHub CI had failed on every push since the first full run**, from two workflow bugs, not code: `forge lint --severity high src` (forge 1.8.3 parses `src` as a second severity) and an unquoted `--filter ./packages/**` that the shell expanded, so pnpm ran a "script" named `./packages/sdk`. The same unquoted glob was in the root `package.json` scripts. | **fixed** — `forge lint src --severity high`, filters quoted; both reproduced and verified |

### Recommendations

- **F-1 — pick one:**
  (a) gate base quota on `DomainVerified` (the attestor mechanism already
  exists in `x/apps`), so a Sybil needs an attestation per app;
  (b) replace the per-epoch base with a one-time starter grant;
  (c) at minimum, raise the registration fee well above the base quota's value
  over a long horizon (at genesis prices, 10,000 CREDIT = $10 covers ~8 years).
  (a) keeps gasless-from-day-one for real apps and closes the farm.
- **F-2 — decide the gas price.** Either keep gas near-free and position the
  registry on onboarding plus revenue share, or reprice to L2-like levels
  (~$0.001–0.005 per transfer ⇒ 1 CREDIT ≈ $30–150) together with the coupled
  changes in §5. Do not reprice alone.
- **F-4:** rate-limit at the public RPC gateway so overload gets an immediate
  429 instead of a silent drop; size `global-slots`/`account-slots` on RPC
  nodes to a few blocks of capacity; clients should treat a tx hash as
  "submitted", wait for a receipt with a timeout, and rebroadcast/replace (the
  bundler already does this for UserOps).
- **F-5:** keep Settle gas estimates tight (the SDK uses `eth_estimateGas`);
  flag integrations that hard-code large gas limits.

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
