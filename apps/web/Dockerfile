# Copyright (c) 2026 VaporChain / muthu2201. All rights reserved.
# Proprietary. See LICENSE. Provenance: VAPOR-6eabb1be532bdef4
#
# Reference web app. Build from the REPO ROOT (needs the pnpm workspace):
#   docker build -f apps/web/Dockerfile -t vaporchain/web:latest .
# NEXT_PUBLIC_* are inlined at build time; pass them as build args per network.
FROM node:22-bookworm-slim AS build
RUN corepack enable && corepack prepare pnpm@12.6.0 --activate
WORKDIR /repo
COPY pnpm-workspace.yaml pnpm-lock.yaml package.json ./
COPY packages ./packages
COPY apps/web ./apps/web
COPY scripts/gen-abis.mjs ./scripts/gen-abis.mjs
COPY contracts/deployments ./contracts/deployments
RUN pnpm install --frozen-lockfile --filter @vaporchain/web...
RUN pnpm --filter @vaporchain/sdk build && pnpm --filter @vaporchain/react build
# NEXT_PUBLIC_* must be present at build; supply via --build-arg or an env file
ARG NEXT_PUBLIC_VAPOR_NETWORK_NAME
ENV NEXT_TELEMETRY_DISABLED=1
RUN pnpm --filter @vaporchain/web build

FROM node:22-bookworm-slim AS run
WORKDIR /app
ENV NODE_ENV=production
RUN useradd -m next
COPY --from=build --chown=next /repo/apps/web/.next/standalone ./
COPY --from=build --chown=next /repo/apps/web/.next/static ./apps/web/.next/static
COPY --from=build --chown=next /repo/apps/web/public ./apps/web/public
USER next
EXPOSE 3000
ENV PORT=3000 HOSTNAME=0.0.0.0
CMD ["node", "apps/web/server.js"]
