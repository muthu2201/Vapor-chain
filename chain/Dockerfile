# SPDX-License-Identifier: AGPL-3.0-only
# Copyright (c) 2026 VaporChain / muthu2201
# Provenance: VAPOR-6eabb1be532bdef4
#
# vaporchaind node image. Build from the chain/ directory:
#   docker build -f chain/Dockerfile -t vaporchain/node:$(git rev-parse --short HEAD) \
#     --build-arg GIT_COMMIT=$(git rev-parse --short HEAD) chain
FROM golang:1.26.8-bookworm AS build
WORKDIR /src
# cache modules first
COPY go.mod go.sum ./
RUN go mod download
COPY . .
ARG GIT_COMMIT=docker
ARG VERSION=docker
ARG BUILD_TIME
# CGO on: cosmos-db uses the pebble/rocksdb-free default, but goleveldb needs cgo off;
# the Makefile builds pure-Go, so mirror it.
ENV CGO_ENABLED=0
RUN go build -trimpath \
      -ldflags "-s -w -buildid= \
        -X github.com/cosmos/cosmos-sdk/version.Name=vaporchain \
        -X github.com/cosmos/cosmos-sdk/version.AppName=vaporchaind \
        -X github.com/cosmos/cosmos-sdk/version.Version=${VERSION} \
        -X github.com/cosmos/cosmos-sdk/version.Commit=${GIT_COMMIT} \
        -X github.com/muthu2201/vapor-chain/chain/provenance.GitCommit=${GIT_COMMIT} \
        -X github.com/muthu2201/vapor-chain/chain/provenance.Version=${VERSION} \
        -X github.com/muthu2201/vapor-chain/chain/provenance.BuildTime=${BUILD_TIME}" \
      -o /out/vaporchaind ./cmd/vaporchaind

FROM gcr.io/distroless/static-debian12:nonroot
COPY --from=build /out/vaporchaind /usr/local/bin/vaporchaind
# p2p, rpc, grpc, api, evm-rpc, evm-ws, prometheus
EXPOSE 26656 26657 9090 1317 8545 8546 26660
USER nonroot:nonroot
ENTRYPOINT ["vaporchaind"]
CMD ["start"]
