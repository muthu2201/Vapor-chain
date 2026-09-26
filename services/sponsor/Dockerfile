# SPDX-License-Identifier: AGPL-3.0-only
# Copyright (c) 2026 VaporChain / muthu2201
# Provenance: VAPOR-6eabb1be532bdef4
#
# vapor-sponsor image. Build from services/sponsor:
#   docker build -f services/sponsor/Dockerfile -t vaporchain/sponsor:latest services/sponsor
FROM golang:1.26.8-bookworm AS build
WORKDIR /src
COPY go.mod go.sum ./
RUN go mod download
COPY . .
ENV CGO_ENABLED=0
RUN go build -trimpath -ldflags "-s -w -buildid=" -o /out/sponsor ./cmd/sponsor

FROM gcr.io/distroless/static-debian12:nonroot
COPY --from=build /out/sponsor /usr/local/bin/sponsor
USER nonroot:nonroot
ENTRYPOINT ["sponsor"]
EXPOSE 8800 9800
