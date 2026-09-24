# Copyright (c) 2026 VaporChain / muthu2201. All rights reserved.
# Provenance: VAPOR-6eabb1be532bdef4
#
# Reproducible release build: -trimpath, fixed build id, CGO off by default
# (goleveldb/pebble backends), ldflags embed commit + time + provenance.

BINARY      := vaporchaind
PKG         := github.com/muthu2201/vapor-chain/chain
COMMIT      := $(shell git rev-parse --short=12 HEAD 2>/dev/null || echo unknown)
VERSION     ?= $(shell git describe --tags --always 2>/dev/null || echo v1.0.0)
BUILD_TIME  := $(shell date -u +%Y-%m-%dT%H:%M:%SZ)
BUILDDIR    ?= $(CURDIR)/build

LDFLAGS := -s -w -buildid= \
  -X github.com/cosmos/cosmos-sdk/version.Name=vaporchain \
  -X github.com/cosmos/cosmos-sdk/version.AppName=$(BINARY) \
  -X github.com/cosmos/cosmos-sdk/version.Version=$(VERSION) \
  -X github.com/cosmos/cosmos-sdk/version.Commit=$(COMMIT) \
  -X $(PKG)/provenance.GitCommit=$(COMMIT) \
  -X $(PKG)/provenance.BuildTime=$(BUILD_TIME) \
  -X $(PKG)/provenance.Version=$(VERSION)

.PHONY: build install test test-race fuzz lint proto clean

build:
	@mkdir -p $(BUILDDIR)
	go build -trimpath -ldflags '$(LDFLAGS)' -o $(BUILDDIR)/$(BINARY) ./cmd/vaporchaind

install:
	go install -trimpath -ldflags '$(LDFLAGS)' ./cmd/vaporchaind

test:
	go test -count=1 ./...

test-race:
	go test -race -count=1 ./x/... ./lane/... ./precompiles/...

fuzz:
	go test -run=^$$ -fuzz=FuzzComputeSplit -fuzztime=60s ./x/settle/types
	go test -run=^$$ -fuzz=FuzzComputeFee -fuzztime=60s ./x/settle/types

lint:
	go vet ./...
	gofmt -l . | (! grep .)

proto:
	./scripts/protogen.sh

clean:
	rm -rf $(BUILDDIR)
