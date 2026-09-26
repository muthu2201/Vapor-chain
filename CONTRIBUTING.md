<!--
SPDX-License-Identifier: CC-BY-4.0
Copyright (c) 2026 VaporChain / muthu2201
Provenance: VAPOR-6eabb1be532bdef4
-->
# Contributing to VaporChain

Thanks for helping. VaporChain is a consumer EVM appchain where users pay in
USDC with one signature and no gas token, and apps pay their users' gas
("sponsor" it) out of the revenue they earn. This guide covers how the project
is built and how to set it up and test it. It also lists which parts are open
for contributions and which need a maintainer, and how to audit the code or
report a vulnerability.

> **Status: open source, pre-audit testnet software. There is no mainnet.**
> Nothing here has been independently audited, so treat every line as unverified.
> That is also why reviews and audits are the most valuable contribution you can make.

**Security vulnerabilities:** never open a public issue. Follow
[`SECURITY.md`](./SECURITY.md).

---

## Contents

1. [Quick checklist](#1-quick-checklist)
2. [Ways to help](#2-ways-to-help)
3. [How VaporChain was built](#3-how-vaporchain-was-built)
4. [Architecture in five minutes](#4-architecture-in-five-minutes)
5. [Repository map: what is open and what is guarded](#5-repository-map-what-is-open-and-what-is-guarded)
6. [Set up your environment](#6-set-up-your-environment)
7. [Build and test](#7-build-and-test)
8. [Do not touch (read before your first PR)](#8-do-not-touch-read-before-your-first-pr)
9. [Making a change](#9-making-a-change)
10. [Auditing VaporChain](#10-auditing-vaporchain)
11. [Where to start](#11-where-to-start)
12. [Licensing of contributions](#12-licensing-of-contributions)
13. [Getting help](#13-getting-help)

---

## 1. Quick checklist

1. Read this file and [`ARCHITECTURE.md`](./ARCHITECTURE.md).
2. Pick an issue labelled `good first issue`, `help wanted` or `audit`, and
   comment to claim it. For anything not covered by an issue, open one first
   and wait for a maintainer to agree on the approach.
3. Fork, then create a branch from the default branch.
4. Make the change **with tests**. Run the checks in [§7](#7-build-and-test) for
   every area you touched.
5. Run `node scripts/license/headers.mjs --fix` to add license headers to any new files.
6. Sign off every commit (`git commit -s`, see [§12](#12-licensing-of-contributions)).
7. Open a pull request from the template. Keep it to one topic, and make the description explain *why*.

---

## 2. Ways to help

| kind | examples | who |
|---|---|---|
| **Security review / audit** | break the fee math, drain a module account, exceed the sponsored-lane cap, forge a sponsorship, farm quota | anyone; see [§10](#10-auditing-vaporchain) |
| **Tests** | add cases to existing suites, fuzz targets, property tests, red-team contracts | good first contribution, even in critical code, because tests don't change behaviour |
| **Docs** | fix unclear steps, add examples, CLI cookbooks, diagrams | good first contribution |
| **SDK / React / web app** | ergonomics, error messages, UI for existing chain features | good first contribution |
| **Tooling** | scripts, CI, load generator, indexer API | good first contribution, as long as CI permissions aren't touched |
| **Protocol changes** | anything in the "guarded" rows of [§5](#5-repository-map-what-is-open-and-what-is-guarded) | issue + design approval first |

---

## 3. How VaporChain was built

**Starting point.** The node is a fork of `evmd`, the example chain in
[cosmos/evm](https://github.com/cosmos/evm) v0.7.3 (Apache-2.0, see
[`NOTICE`](./NOTICE)). The versions are pinned together:

- Cosmos SDK 0.54.4, CometBFT 0.39.4, cosmos/evm 0.7.3 and ibc-go v11.2.0.
- go-ethereum is replaced by the Cosmos fork, `cosmos/go-ethereum v1.17.2-cosmos-1`.

Everything VaporChain-specific was added on top. The protocol pieces are:

- the modules `x/settle`, `x/apps` and `x/council`;
- the Settle precompile at `0x…0900`;
- the sponsored-lane block policy;
- a gas-burn begin-blocker;
- a small `vapor_*` JSON-RPC namespace.

The off-chain stack around it is:

- ERC-4337 v0.8 contracts and paymasters;
- a sponsor service (ERC-7677), a pinned Alto bundler and an indexer;
- a TypeScript SDK, React hooks and a Next.js reference app.

**Build order.** The phases, in order:

1. Chain skeleton.
2. Settlement and app-registry modules, and the precompile.
3. Sponsored lane.
4. Contracts.
5. Services.
6. SDK, React and the web app.
7. Testnet tooling, a stress test and a red team.
8. A drain audit of every path that moves value.
9. A 4-validator "mainnet-sim" run with mainnet parameters.
10. Economics v2: gas priced as infrastructure cost.
11. v3: sponsorship backed by bonded capital instead of human attestors.

Each phase left behind documentation of *why* it was built that way. Start with:

- [`ARCHITECTURE.md`](./ARCHITECTURE.md)
- [`docs/security/`](./docs/security/)
- [`docs/benchmarks/`](./docs/benchmarks/)

**Engineering method.** Contributions are held to the same standards:

- **Tests are the spec.** Every feature lands with unit tests. Money math is
  fuzzed: `FuzzComputeFee` and `FuzzComputeSplit`. Protocol behaviour is proven
  end to end on a real multi-validator localnet, not against mocks: the SDK,
  React and Playwright suites run against a live chain, bundler and sponsor.
- **Adversarial by default.** `contracts/test/redteam` holds attacker contracts.
  `docs/security/red-team.md` records live attack runs.
  `docs/security/hardening.md` records the drain audit, including the parts we
  found wrong and corrected.
- **Numbers come from the chain.** Load results are read back from blocks, not
  from the load generator's own counters (see `scripts/bench/`). Findings are
  numbered (F-1 … F-8 in `docs/benchmarks/mainnet-sim.md`) and stay in the docs
  after they are fixed.
- **No hidden levers.** There are no admin paths that seize funds, no
  kill-switches and no backdoors. The provenance fingerprint is attribution
  only; it has no runtime effect (see [§8](#8-do-not-touch-read-before-your-first-pr)).
- **AI-assisted, human-directed.** Much of the code was written with an AI pair
  programmer (Claude Code) under the maintainer's direction; the commit trailers
  show where. Review it the way you would review any unaudited code: trust the
  tests, not how plausible the code looks, and report anything that seems
  subtly wrong.

---

## 4. Architecture in five minutes

```
        wallet / dapp  (apps/web, @vaporchain/sdk, @vaporchain/react)
                    │  one signature (EIP-7702 + ERC-4337)
        ┌───────────┴────────────┐
   sponsor service (ERC-7677)   Alto bundler (ERC-4337 v0.8)
        └───────────┬────────────┘
              EntryPoint v0.8 ──► VaporVerifyingPaymaster / VaporTokenPaymaster
                    │
        ┌───────────┴───────────────────────────────────────────┐
        │ vaporchaind (Cosmos SDK 0.54 + cosmos/evm 0.7)         │
        │  EVM ──► Settle precompile 0x…0900 ──► x/settle        │
        │  x/apps (registry, quota, bonds)  x/council (PoA)      │
        │  sponsored-lane Prepare/ProcessProposal                │
        │  IBC: callbacks → ratelimit → PFM → ibcguard → transfer│
        └───────────┬───────────────────────────────────────────┘
              indexer (Postgres + Parquet)
```

The concepts you need:

- **Two internal denoms, neither a currency.**
  - `acredit` is the gas credit. It is bought only with USDC via
    `buyCredits`, at a governance price (1 CREDIT = 50 USDC at genesis).
    Bank sends are disabled, and 100% of gas fees are burned.
  - `avpower` is non-transferable validator power, minted only by `x/council`.
- **Settlement (`x/settle`).** Apps take USDC through `Settle.pay`, `payFrom`
  and tabs. The fee is split between the app, validators, relayers and the
  treasury using integer math that sums exactly to the fee. One module account
  holds the ledgers, and invariants auto-pause settlement if they ever break.
- **App registry and quota (`x/apps`).** Apps register, then prove they own
  their contracts. Each epoch an app gets sponsored-gas **quota** from two sources:
  - quota **earned** from the fees it pays, weighted by how many distinct users paid;
  - **base** quota, bought with capital bonded behind the app. It is linear in
    the bond, so fake apps gain nothing; see
    [`docs/security/sybil-resistance.md`](./docs/security/sybil-resistance.md).
- **Sponsored lane (`chain/lane`).** Transactions from sponsored senders may use
  at most 50% of each block's gas. The cap is enforced in both PrepareProposal
  and ProcessProposal. ProcessProposal recovers each signer from the
  transaction's signature, so it can't be spoofed.
- **PoA (`x/council`).** Only admitted validators exist, and a removed validator
  can never unjail. A guardian can pause the chain for a limited time, but only
  governance can unpause.
- **Account abstraction.** A user's EOA is upgraded with EIP-7702 inside the
  same UserOp that pays. The sponsor service signs only calls to the app's own
  contracts and only within quota. The verifying paymaster's signed hash covers
  chain id, paymaster address and provenance, so a sponsorship can't be
  replayed elsewhere.

---

## 5. Repository map: what is open and what is guarded

- **Open** — good for first contributions.
- **Sensitive** — welcome, but a maintainer reviews closely. Explain security
  implications in the PR.
- **Critical** — consensus or money. Needs a maintainer-approved issue *before*
  you code, plus a security review. Tests-only PRs are always welcome here.

| path | what it is | level | license |
|---|---|---|---|
| `docs/`, `*.md` | architecture, security, benchmarks, guides | open | CC-BY-4.0 (`docs/sdk`: Apache-2.0) |
| `packages/sdk`, `packages/react` | TypeScript SDK and React hooks (viem, wagmi) | open (ABI files generated) | Apache-2.0 |
| `apps/web` | Next.js reference app: shop, wallet, launchpad, dashboard | open (CSP / wallet storage: sensitive) | Apache-2.0 |
| `contracts/src/{checkout,examples,tokens}` | example checkout, token + factory | sensitive | Apache-2.0 |
| `contracts/src/{interfaces,utils}` | `ISettle` ABI, precompile address, provenance lib | critical (ABI) | Apache-2.0 |
| `contracts/src/paymaster` | verifying + token paymasters | **critical** | AGPL-3.0-only |
| `contracts/test`, `contracts/script` | Foundry tests, red-team attackers, deploy scripts | open (tests) / sensitive (scripts) | AGPL-3.0-only |
| `chain/x/settle` | fees, splits, ledgers, invariants, gas burn, treasury, tabs | **critical** | AGPL-3.0-only |
| `chain/x/apps` | registry, ownership proofs, quota, bonds, contract moves | **critical** | AGPL-3.0-only |
| `chain/x/council` (+ `ibcguard`) | PoA admission/removal, guardian, IBC denom guard | **critical** | AGPL-3.0-only |
| `chain/precompiles/settle` | EVM ↔ Cosmos boundary at `0x…0900` | **critical** | AGPL-3.0-only (`ISettle.sol`: Apache-2.0) |
| `chain/lane` | sponsored-lane block policy | **critical** | AGPL-3.0-only |
| `chain/app` | app wiring, ante, mempool, block hooks, upgrades | **critical** | Apache-2.0 AND AGPL-3.0-only (evmd-derived) |
| `chain/proto` | protobuf API definitions | critical (state/API compat) | Apache-2.0 |
| `chain/rpc` | `vapor_*` JSON-RPC namespace | sensitive | AGPL-3.0-only |
| `chain/provenance` | authorship fingerprint | **do not change** | AGPL-3.0-only |
| `services/sponsor` | ERC-7677 sponsor: policy + signer | **critical** | AGPL-3.0-only |
| `services/indexer` | Postgres + Parquet indexer, read-only API | open (API) / sensitive (ingest) | AGPL-3.0-only |
| `services/bundler` | pinned Alto bundler config | sensitive | AGPL-3.0-only (Alto itself GPL-3.0) |
| `scripts/localnet`, `scripts/bench`, `tools/loadgen` | localnet, full suite, load campaigns | open | AGPL-3.0-only |
| `scripts/testnet` | testnet genesis builder + economic params | critical | AGPL-3.0-only |
| `deploy/` | Docker, compose, systemd, Prometheus alerts | sensitive | AGPL-3.0-only |
| `.github/workflows` | CI | maintainers only | AGPL-3.0-only |
| `contracts/lib` | vendored OpenZeppelin, account-abstraction, forge-std | **never modify** | their own |

---

## 6. Set up your environment

The pinned versions matter. Use exactly these:

| tool | version | why pinned |
|---|---|---|
| Go | **1.26.8** (not 1.27) | a transitive dependency (`bytedance/sonic`) doesn't build on 1.27 yet |
| Foundry | 1.8.3 | `solc 0.8.37`, `evm_version = prague` (EIP-7702; Osaka is not active) |
| Node | ≥ 22 | the workspace and the web bootstrap script need it |
| pnpm | 12.6.0 | lockfile format |
| PostgreSQL | 15+ | indexer and local service stack |
| `jq`, `curl`, `cast` | any recent | localnet scripts |
| `buf`, `protoc-gen-gocosmos` (cosmos/gogoproto v1.7.2), `protoc-gen-grpc-gateway` v1.16.0 | only for `.proto` changes | code generation |

```bash
git clone https://github.com/muthu2201/Vapor-chain.git && cd Vapor-chain
pnpm install --frozen-lockfile
cd chain && make build && cd ..        # -> chain/build/vaporchaind
```

`contracts/lib` is vendored in the repository (no submodules to fetch).
A 4-validator localnet plus the service stack fits on 4 vCPUs / 8 GB RAM.

---

## 7. Build and test

Run the checks for every area your change touches. CI runs the same checks on every push and PR.

| area | command (from repo root unless noted) |
|---|---|
| chain | `cd chain && go build ./... && go vet ./... && go test -tags test -race ./...` (the `test` tag resets EVM globals between tests) |
| chain fuzz | `cd chain && make fuzz` |
| services | `cd services/sponsor && go test -race ./...`, and the same in `services/indexer` |
| Go formatting | `gofmt -l $(git ls-files '*.go' \| grep -v '\.pb\.go$')` must print nothing |
| contracts | `cd contracts && forge build --sizes && forge lint src --severity high && forge test` |
| SDK / React / web | `pnpm --filter @vaporchain/sdk build && pnpm --filter @vaporchain/react build && pnpm -r run typecheck && pnpm -r --filter './packages/**' run test` |
| license headers | `node scripts/license/headers.mjs` |
| provenance | `scripts/provenance/scan.sh --ci` |

**Live end-to-end tests** run against a real chain:

```bash
scripts/localnet/localnet.sh init 4 300 && scripts/localnet/localnet.sh start
scripts/localnet/services.sh up              # contracts, sponsor, bundler, indexer, Postgres
export OWNER=0x$(chain/build/vaporchaind keys unsafe-export-eth-key dev \
  --keyring-backend test --home .localnet/node0)
VAPOR_E2E_OWNER_KEY=$OWNER pnpm --filter @vaporchain/sdk run test:e2e
VAPOR_E2E_OWNER_KEY=$OWNER pnpm --filter @vaporchain/react run test:e2e
node apps/web/scripts/bootstrap-localnet.ts && pnpm --filter @vaporchain/web run test:e2e
```

To run everything (every CI job, every live suite and the browser tests), use
`scripts/bench/full-suite.sh`. Pass it a subset like
`STAGES="contracts e2e" scripts/bench/full-suite.sh`. Never edit a script while
it is running.

The localnet keys are **test-only**. They live in `.localnet/`, which is
git-ignored. Never reuse them anywhere else.

---

## 8. Do not touch (read before your first PR)

This list is what keeps the chain safe. A PR that breaks one of these rules is
closed, however good the rest is. If you think a rule is wrong, open an issue.

### 8.1 Generated files: never edit by hand

| file | regenerate with |
|---|---|
| `chain/x/*/types/*.pb.go`, `*.pb.gw.go` | edit the `.proto`, then `cd chain && make proto` |
| `chain/precompiles/settle/abi.json` | `cd contracts && forge build`, then `jq .abi contracts/out/ISettle.sol/ISettle.json > chain/precompiles/settle/abi.json` |
| `packages/sdk/src/abi/*.ts` | `pnpm gen:abis` (needs `contracts/out`) |
| `pnpm-lock.yaml`, `go.sum`, `services/bundler/package-lock.json` | the package manager, and only in a dependency PR |
| `contracts/deployments/*.json` | written by `scripts/localnet/services.sh` |
| `docs/benchmarks/raw/**` | measured data: never edit the numbers; re-run the benchmark |

### 8.2 Consensus and money code: needs an approved issue first

- **`chain/lane`**: PrepareProposal and ProcessProposal must stay *symmetric*
  and must not run the full ante handler. An earlier design that did halted the
  chain under load (STRESS-HALT, `docs/benchmarks/results.md`). The cap must
  keep recovering signers from signatures.
- **`chain/app`**: block-hook order is load-bearing. Settle's begin-blocker
  (the gas burn) must run **before** `x/distribution`. The ante chain
  (`ante.go`), the tx verifier, the mempool and module wiring are consensus.
- **`chain/x/settle`**: a fee split must always sum exactly to the fee, the
  ledgers must never exceed the module balance, and credits may be minted only
  in `BuyCredits`. Invariant failures pause settlement and must never halt the chain.
- **`chain/x/apps`**: quota must stay linear in the bond and in fees. Bond
  totals must equal what the module holds. The owner of a revoked app can
  always unbond. `bond_denom` must not change while bonds exist.
- **`chain/x/council`**: removal is permanent (including through authz
  `MsgExec`). The guardian can pause but never unpause. `acredit` and `avpower`
  must never leave through IBC.
- **`chain/precompiles/settle`**: DELEGATECALL and CALLCODE must be refused, as
  must state changes under STATICCALL. Native value must be rejected. Existing
  method signatures are a public ABI.
- **`contracts/src/paymaster` and `services/sponsor`**: the sponsorship hash
  layout must match between Solidity and Go. The vectors in
  `services/sponsor/internal/userop/userop_test.go` pin this parity. Signer
  rotation stays timelocked and revocation stays instant.

### 8.3 State-compatibility rules (breaking these forks the chain)

- **Protobuf:** never renumber or reuse a field. When you delete one, mark
  its number and name `reserved` (see the `reserved` lines in
  `chain/proto/vaporchain/apps/v1/apps.proto`).
- **Store keys:** never reuse a retired key prefix (`x/apps` prefix 10 is retired).
- **Error codes:** never reuse a retired code (`x/apps` codes 13–14). New codes are appended.
- **Params:** a new param needs a default, validation, and updates to both
  genesis builders: `scripts/localnet/localnet.sh`, and
  `scripts/testnet/build-genesis.sh` with `testnet.config.json`.
- **Economic defaults** move only with the maths written down in
  `docs/benchmarks/mainnet-sim.md` §5. They are `credit_price`, `quota_weight`,
  `gas_per_bonded_unit`, `sponsored_lane_max_bps`, `gas_burn_bps` and the
  registration fee. The farming bound
  `app_share + quota_weight × sponsor_max_fee ÷ credit_price < 1` must hold.
- **Any state-breaking change** needs a `ConsensusVersion` bump, a migration and
  an upgrade handler (`chain/app/upgrades.go`).
- **`ISettle.sol` exists twice.** The copies at
  `chain/precompiles/settle/ISettle.sol` and `contracts/src/interfaces/ISettle.sol`
  must stay byte-identical.
- **Frozen identifiers:** chain IDs (7797 / 77970 / 779700), the bech32 prefix
  `vapor`, the precompile address `0x…0900` and the denoms.
- **Pinned versions** (Go, SDK/CometBFT/cosmos-evm/ibc-go, the geth-fork
  `replace`, solc/EVM version, EntryPoint v0.8) are not bumped in drive-by
  PRs. An upgrade is its own issue.

### 8.4 Legal and provenance files

- Never remove or alter the following:
  - `LICENSE`, `LICENSES/`, `NOTICE`, `TRADEMARKS.md`, `scripts/license/rules.json`;
  - the SPDX, copyright and `Provenance: VAPOR-6eabb1be532bdef4` header lines;
  - `chain/provenance`, `contracts/src/utils/Provenance.sol`.

  NOTICE makes preserving them a condition of the AGPL (section 7(b)). You *may*
  add entries to `THIRD_PARTY_NOTICES.md` for new dependencies.
- The provenance fingerprint is **attribution only**. It is mixed into signature
  domains (paymaster hash, sponsor hash) and seeds a salt (`x/apps` HLL), so
  changing it also breaks signatures and state.
- `.github/workflows` and `.github/CODEOWNERS` are maintainer-only.

### 8.5 Never commit

Never commit any of these (CI scans for them):

- private keys and mnemonics;
- `.env` files other than `.env.example`;
- `.localnet/`, `secrets/`, `*.key.json`;
- RPC URLs that contain API keys;
- personal data.

---

## 9. Making a change

- **Commit messages** follow the history: `area: imperative summary`, for
  example `apps: reject bond in wrong denom` or `sdk: add bondInfo to AppsRest`.
  Explain *why* in the body.
- **Tests first for bugs.** A fix comes with a test that fails without it.
- **Style.**
  - Go: `gofmt` and `go vet`, with comments that explain intent rather than restating the code.
  - Solidity: follow the surrounding style; `forge lint src --severity high` must be clean.
  - TypeScript: strict mode, no `any` in public types, `pnpm -r run typecheck`.
- **License headers:** `node scripts/license/headers.mjs --fix` adds or
  corrects them from `scripts/license/rules.json`. You may add your own
  `Copyright (c) <year> <name>` line under the standard one; the tool keeps it.
- **Small PRs.** One topic per PR. Refactors go separately from behaviour changes.
- **Review.** `CODEOWNERS` routes critical paths to the maintainer. Expect
  questions, and expect "please split this". Maintainers usually squash-merge.
- **CI must be green.** A failing test is never "flaky" until proven so. Don't
  skip, disable or loosen tests to get green.

---

## 10. Auditing VaporChain

Reviewers and auditors are especially welcome. Suggested path:

1. **Read the security contract.**
   [`docs/security/threat-model.md`](./docs/security/threat-model.md) maps
   every claim to code and tests. Then read:
   - [`hardening.md`](./docs/security/hardening.md) (the drain audit);
   - [`red-team.md`](./docs/security/red-team.md);
   - [`sybil-resistance.md`](./docs/security/sybil-resistance.md);
   - [`mainnet-sim.md`](./docs/benchmarks/mainnet-sim.md) (findings F-1 … F-8).
2. **Try to break these invariants**, which must always hold:
   - module bank balance ≥ the sum of `x/settle` ledgers;
   - `acredit` supply ≤ genesis + minted;
   - every split sums exactly to its fee;
   - the sponsored share of each block's gas ≤ `sponsored_lane_max_bps`;
   - the `x/apps` module balance = bonds + pending unbondings;
   - nobody can spend another app's allowance or quota;
   - the guardian cannot unpause, and a removed validator cannot unjail;
   - `acredit` and `avpower` never leave via IBC;
   - a sponsorship can't be replayed on another chain or paymaster;
   - paying fees to farm quota always loses money (farming bound < 1).
3. **Write a proof of concept** with the existing harnesses:
   - Go keeper tests (`go test -tags test ./x/...`, see `chain/testutil`);
   - fuzz targets;
   - Foundry tests next to `contracts/test/redteam`;
   - a live localnet ([§7](#7-build-and-test)).
4. **Report it.**
   - Anything exploitable (loss of funds, halt, cap bypass, unauthorized mint
     or spend, signature forgery): privately via [`SECURITY.md`](./SECURITY.md).
   - Hardening ideas and non-exploitable issues: a public issue using the
     "Audit finding" template.

**Out of scope, or already known:**

- upstream dependencies (report those to the upstream project);
- `contracts/lib`;
- Alto;
- PoA's inherent trust in the validator set;
- the open findings F-4 (accepted ≠ mined under overload) and F-5
  (over-declared gas limits waste block space);
- the residual risks listed in the threat model.

---

## 11. Where to start

Good first contributions, in areas that are safe to learn in:

- **Web:** show an app's bonded capital, its base quota and pending unbondings
  on `apps/web/app/apps/[id]/page.tsx`. The SDK already exposes
  `apps.bondInfo` and `appsRest(...).bond(appId)`.
- **Docs:** a CLI cookbook for `x/apps`. Cover `vaporchaind tx apps
  register | add-contract | bond | unbond` and `vaporchaind q apps quota | bond`,
  with a real localnet transcript.
- **SDK tests:** unit tests for REST parsing in `packages/sdk/src/apps.ts`
  (`bond`, `quota`) against JSON fixtures, with no network.
- **Chain tests:** table tests for `x/apps` `Params.Validate` edge cases
  (`bond_denom`, `unbonding_blocks` against the epoch length). Tests only; no
  behaviour change.
- **Indexer:** a read-only endpoint `GET /v1/apps/{id}/bonds` for an app's
  bond history. The indexer already stores the `app_bonded`, `app_unbonding`
  and `app_unbonded` events in its `app_events` table.
- **Tooling:** add `shellcheck` for `scripts/**/*.sh` and fix what it reports.
- **SDK (help wanted):** a gas-limit helper that trims over-declared limits
  (finding F-5). Discuss the approach in the issue first.

---

## 12. Licensing of contributions

- **Inbound = outbound.** Your contribution is licensed under the license of
  the files it changes or adds, as `scripts/license/rules.json` and each file's
  SPDX header say:
  - AGPL-3.0-only for the protocol;
  - Apache-2.0 for the SDK, interfaces, examples and the web app;
  - CC-BY-4.0 for docs.

  See [`NOTICE`](./NOTICE).
- **Developer Certificate of Origin.** There is no CLA. Instead, every commit
  must be signed off (`git commit -s`), which adds
  `Signed-off-by: Your Name <you@example.com>`. By signing off you certify the
  [DCO 1.1](https://developercertificate.org/): you wrote the change, or have
  the right to submit it under these licenses. A CI check enforces this on pull
  requests. To fix a missing sign-off, run `git commit --amend -s --no-edit`
  for the last commit, or `git rebase --signoff <base>` for several.
- **AI-generated code** is welcome, but you are responsible for it. You must be
  able to explain it and certify the DCO for it, and it must meet the same test bar.
- **Trademarks.** Contributing grants no rights in the VaporChain name or logo.
  See [`TRADEMARKS.md`](./TRADEMARKS.md).

---

## 13. Getting help

- **Questions and proposals:** open a GitHub issue. Search first; someone may
  already have asked.
- **Stuck on setup:** open an issue with your OS, the tool versions from
  [§6](#6-set-up-your-environment) and the full error output.
- **Conduct:** everyone here follows the
  [Code of Conduct](./CODE_OF_CONDUCT.md).
