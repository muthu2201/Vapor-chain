#!/usr/bin/env bash
# SPDX-License-Identifier: AGPL-3.0-only
# Copyright (c) 2026 VaporChain / muthu2201
# Provenance: VAPOR-6eabb1be532bdef4

# Regenerates gogoproto Go code for x/apps, x/settle and x/council.
# Requires: buf, protoc-gen-gocosmos (cosmos/gogoproto v1.7.2), protoc-gen-grpc-gateway v1.16.0
set -euo pipefail
cd "$(dirname "$0")/../proto"
buf dep update
buf generate --template buf.gen.gogo.yaml
cp -r github.com/muthu2201/vapor-chain/chain/* ../
rm -rf github.com
