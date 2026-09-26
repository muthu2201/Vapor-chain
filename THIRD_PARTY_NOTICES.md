<!--
SPDX-License-Identifier: CC-BY-4.0
Copyright (c) 2026 VaporChain / muthu2201
Provenance: VAPOR-6eabb1be532bdef4
-->
# Third-party notices

VaporChain depends on and, in places, derives from third-party software. This
file lists those components and their licenses. Inclusion here does not change
the license of any VaporChain file (see `NOTICE` for which license covers which
path); it records the terms of the third-party parts. Pinned versions are in `chain/go.mod`, `services/*/go.mod`,
`contracts/foundry.toml` / `contracts/lib`, and the JS lockfile.

## Chain / Go

| component | version | license | notes |
|---|---|---|---|
| github.com/cosmos/evm (evmd) | v0.7.3 | Apache-2.0 | `chain/app`, `chain/cmd` derive from the evmd example; see NOTICE |
| github.com/cosmos/cosmos-sdk | v0.54.4 | Apache-2.0 | |
| github.com/cometbft/cometbft | v0.39.4 | Apache-2.0 | |
| github.com/cosmos/ibc-go/v11 | v11.2.0 | Apache-2.0 | callbacks, ratelimit, PFM, transfer |
| github.com/cosmos/go-ethereum | v1.17.2-cosmos-1 | **LGPL-3.0** | replaces ethereum/go-ethereum; used as a library (dynamically linkable); unmodified |
| github.com/ethereum/go-ethereum | v1.17.6 | LGPL-3.0 / GPL-3.0 | client libraries in tools/services |
| github.com/parquet-go/parquet-go | v0.32.0 | Apache-2.0 | indexer archive |
| github.com/jackc/pgx/v5 | v5.11.0 | MIT | Postgres driver |
| github.com/prometheus/client_golang | v1.24.1 | Apache-2.0 | metrics |

The LGPL-3.0 geth fork is used as an imported library and is not modified; its
source at the pinned version is available from the upstream project. Replacing it
with a compatible build is possible per the LGPL.

## Contracts / Solidity

| component | version | license | notes |
|---|---|---|---|
| eth-infinitism account-abstraction | v0.8.0 | **GPL-3.0** (core) / MIT (interfaces) | our contracts import only the **MIT** interfaces and helpers (IEntryPoint, PackedUserOperation, BaseAccount, Simple7702Account, Helpers). EntryPoint/StakeManager/NonceManager/SenderCreator are GPL-3.0 and are deployed as canonical bytecode, not compiled into our contracts. |
| OpenZeppelin/openzeppelin-contracts | 5.6.x | MIT | |
| foundry-rs/forge-std | current | MIT/Apache-2.0 | test-only |

The copies in `contracts/lib` keep their Solidity sources and license files
unchanged. Their JavaScript and Python tooling manifests (`package.json`,
`package-lock.json`, `yarn.lock`, `fv-requirements.txt`) were removed because
the build never uses them.

The canonical EntryPoint v0.8 and Simple7702Account are deployed by replaying
the audited mainnet CREATE2 payloads; we distribute their addresses, not
modified source.

## Services / JS

| component | version | license | notes |
|---|---|---|---|
| @pimlico/alto | 0.0.21 | **GPL-3.0** | run as a standalone, unmodified bundler process (`services/bundler`); not linked into VaporChain code |
| viem | 2.56.x | MIT | |
| wagmi | 3.7.x | MIT | |
| next / react / react-dom | 16.x / 19.x | MIT | |
| @tanstack/react-query | 5.x | MIT | |
| tailwindcss | 4.x | MIT | |
| @playwright/test, vitest | — | Apache-2.0 / MIT | test-only |

Alto (GPL-3.0) is invoked as a separate process over JSON-RPC and is neither
modified nor statically linked into any VaporChain binary; running it alongside
VaporChain does not place VaporChain code under the GPL.

## Data / networks

- Skip:Go REST API — used as a network service for cross-chain routing.
- USDC.inj / testnet USDC stand-in — the testnet `uusdc` is a value-less
  stand-in; mainnet uses USDC over IBC.

If any attribution here is incomplete or incorrect, it is an oversight; corrections
are welcome and do not waive any third-party rights.
