<!-- Copyright 2026 VaporChain / muthu2201. Licensed under Apache-2.0. Provenance: VAPOR-6eabb1be532bdef4 -->
# Third-party notices (SDK distribution)

This public SDK distribution depends on the following third-party software. Their
licenses govern those components; the Apache-2.0 license of this distribution
applies to VaporChain's own SDK, hooks, example contracts and docs.

## JavaScript / TypeScript

| component | license |
|---|---|
| viem | MIT |
| wagmi | MIT |
| @tanstack/react-query | MIT |
| next, react, react-dom | MIT |
| tailwindcss, @tailwindcss/postcss | MIT |
| @playwright/test | Apache-2.0 (test only) |
| vitest, @testing-library/react, happy-dom, fake-indexeddb | MIT (test only) |
| typescript | Apache-2.0 (dev only) |

## Solidity (example contracts)

| component | license | notes |
|---|---|---|
| OpenZeppelin/openzeppelin-contracts | MIT | ERC-20, permit, utils |
| eth-infinitism/account-abstraction | MIT interfaces / GPL-3.0 core | example contracts import only the MIT interfaces (IEntryPoint, PackedUserOperation, etc.) |
| foundry-rs/forge-std | MIT / Apache-2.0 | test only |

Install these with `forge install` (see README); they are not vendored here.

## Network services

- **Skip:Go** REST API — used by the SDK's bridging helpers as a network service.

The `@vaporchain/sdk` ABI files include the ABIs of protocol contracts (e.g. the
verifying paymaster) purely as data for decoding on-chain events; the protocol
contract source is not part of this distribution.
