# SPDX-License-Identifier: AGPL-3.0-only
# Copyright (c) 2026 VaporChain / muthu2201
# Provenance: VAPOR-6eabb1be532bdef4
#
# vapor-indexer image. Build from services/indexer:
#   docker build -f services/indexer/Dockerfile -t vaporchain/indexer:latest services/indexer
FROM golang:1.26.8-bookworm AS build
WORKDIR /src
COPY go.mod go.sum ./
RUN go mod download
COPY . .
ENV CGO_ENABLED=0
RUN go build -trimpath -ldflags "-s -w -buildid=" -o /out/indexer ./cmd/indexer

FROM gcr.io/distroless/static-debian12:nonroot
COPY --from=build /out/indexer /usr/local/bin/indexer
USER nonroot:nonroot
ENTRYPOINT ["indexer"]
EXPOSE 8900 9900
