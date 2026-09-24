<!-- Copyright (c) 2026 VaporChain / muthu2201. All rights reserved. Provenance: VAPOR-6eabb1be532bdef4 -->
# Deploying VaporChain

This directory holds everything needed to run VaporChain in production. It is
deliberately split by concern: **consensus nodes** run under systemd +
Cosmovisor, and the **off-chain stack** (sponsor, bundler, indexer, Postgres,
web) runs from Docker Compose (or its own systemd units). The two never share
a lifecycle — you can restart the sponsor without touching a validator.

> Nothing here contains secrets. Every key is a file you mount, `0600`,
> owned by `root:vapor`. See [SECURITY](../docs/security/threat-model.md).

## 1. Build images

```bash
export TAG=$(git rev-parse --short HEAD)
docker build -f chain/Dockerfile           -t vaporchain/node:$TAG    --build-arg GIT_COMMIT=$TAG --build-arg VERSION=$TAG chain
docker build -f services/sponsor/Dockerfile -t vaporchain/sponsor:$TAG services/sponsor
docker build -f services/indexer/Dockerfile -t vaporchain/indexer:$TAG services/indexer
docker build -f services/bundler/Dockerfile -t vaporchain/bundler:$TAG services/bundler
docker build -f apps/web/Dockerfile         -t vaporchain/web:$TAG     .   # from repo root
```

The node also builds without Docker: `cd chain && make build` → `build/vaporchaind`.

## 2. Run a node (systemd + Cosmovisor)

```bash
useradd --system --home /var/lib/vaporchain vapor
mkdir -p /var/lib/vaporchain/cosmovisor/genesis/bin
cp chain/build/vaporchaind /var/lib/vaporchain/cosmovisor/genesis/bin/
# cosmovisor from cosmossdk.io/tools/cosmovisor
install cosmovisor /usr/local/bin/
vaporchaind init <moniker> --chain-id vapor-testnet-1 --home /var/lib/vaporchain
# drop in the network genesis.json + persistent_peers, then:
cp deploy/systemd/vaporchaind.service /etc/systemd/system/
systemctl enable --now vaporchaind
```

Cosmovisor applies governance upgrades automatically:
`DAEMON_ALLOW_DOWNLOAD_BINARIES=false` (upgrade binaries are placed under
`cosmovisor/upgrades/<name>/bin/` by your release pipeline, never downloaded).
The upgrade names come from `chain/app/upgrades.go`.

### app.toml essentials (production)

| setting | value | why |
|---|---|---|
| `minimum-gas-prices` | `1000000000acredit` | fee floor; matches feemarket base fee |
| `[json-rpc] api` | `eth,net,web3,vapor` | `vapor` carries the `eth_fillTransaction` fix (see chain/rpc) |
| `[json-rpc] enable` | validators: `false` | validators never expose RPC; run separate RPC nodes |
| `pruning` | `custom` (keep 100000) | bounded disk; run ≥1 **history node** with `pruning=nothing` for the indexer |
| `[vaporchain] block-stm` | optional | parallel EVM execution; off by default |
| `enabled-unsafe-cors` | `false` | put CORS on your gateway, not the node |

## 3. Run the off-chain stack (Docker Compose)

```bash
cd deploy/docker
cp .env.example .env          # set VAPOR_EVM_RPC/_REST/_COMET to YOUR node, VAPOR_PAYMASTER
mkdir -p secrets && chmod 700 secrets
#   secrets/pg_password         random string
#   secrets/sponsor_signer      the paymaster's sponsor signer key (0x…)
#   secrets/executor_keys       one bundler EOA key per line (must be in
#                               x/settle sponsored_senders)
#   secrets/utility_key         bundler refill/deploy key
chmod 600 secrets/*
docker compose --env-file .env up -d          # add: --profile web  to also run the app
```

`postgres:18` initialises `vapor_sponsor` and `vapor_indexer` on first boot
(`initdb/`). The indexer must reach a node whose pruning window is wider than
its lag — point `VAPOR_COMET` at a **history node**; the
`RetentionMarginLow` alert fires long before data is lost.

## 4. Observability

`deploy/prometheus/vaporchain.rules.yml` has alerts for consensus (node down,
stalled height, validators below fault tolerance, missed blocks), mempool
backlog, and the sponsor/indexer services. Merge `scrape.example.yml` into your
Prometheus. Node metrics: CometBFT `:26660`. Services: sponsor `:9800`,
indexer `:9900`.

## 5. Contracts

Protocol contracts (EntryPoint v0.8 + Simple7702Account at canonical
addresses, the two paymasters, the token factory) deploy with
`contracts/script/deploy-protocol.sh` (see its header for env). The output is
`contracts/deployments/<evm-chain-id>.json`, which the SDK, services and web
app all read.

## Ports

| service | port |
|---|---|
| node p2p / rpc / grpc / api | 26656 / 26657 / 9090 / 1317 |
| node evm-rpc / ws / prometheus | 8545 / 8546 / 26660 |
| sponsor api / metrics | 8800 / 9800 |
| bundler | 4337 |
| indexer api / metrics | 8900 / 9900 |
| web | 3000 |
