<!--
SPDX-License-Identifier: CC-BY-4.0
Copyright (c) 2026 VaporChain / muthu2201
Provenance: VAPOR-6eabb1be532bdef4
-->
# VaporChain contracts

Foundry workspace (solc 0.8.37, evm `prague`, via-IR, 1M optimizer runs).

| Contract | Purpose |
|---|---|
| `paymaster/VaporVerifyingPaymaster` | Sponsored gas, signed by the sponsor service; signer rotation 48h timelock, instant revoke, 7702 delegate allowlist, treasury-pinned withdrawals |
| `paymaster/VaporTokenPaymaster` | Pay gas in USDC at the **protocol** credit price read from `SETTLE.quoteCredits` (no oracle, no owner price); self-refills via `SETTLE.buyCredits` |
| `paymaster/VaporPaymasterBase` | Replaces eth-infinitism BasePaymaster so deposits/stake can only leave to the treasury |
| `tokens/VaporTokenFactory` + `VaporToken` | One-tx token launch (ERC-20 + permit + burnable + capped mint), CREATE2 front-run-proof addresses |
| `checkout/VaporCheckout` | Drop-in merchant checkout through Settle (revenue share + quota) |
| `examples/SwordShop` | Blueprint reference game purchase |
| `interfaces/ISettle` | ABI of the native Settle precompile at `0x…0900` |

Canonical ERC-4337 contracts are **not compiled here**: `deploy/canonical/*.calldata.hex` are the
exact Ethereum-mainnet CREATE2 payloads, replayed through the preinstalled deterministic deployer so
EntryPoint v0.8 lands at `0x4337084D9E255Ff0702461CF8895CE9E3b5Ff108` and Simple7702Account at
`0x4Cd241E8d1510e30b2076397afc7508Ae59C66c9` on every VaporChain network.

```bash
forge test                                # 25 unit/fuzz tests (Settle test double)
RPC_URL=... DEPLOYER_KEY=... PM_OWNER=... SPONSOR_SIGNER=... TREASURY=... USDC_TOKEN=... ./script/deploy-protocol.sh
RPC_URL=... OWNER_KEY=... USER_KEY=... ./script/e2e-settle.sh   # real precompile, live node
```
