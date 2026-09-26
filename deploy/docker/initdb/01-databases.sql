-- SPDX-License-Identifier: AGPL-3.0-only
-- Copyright (c) 2026 VaporChain / muthu2201
-- Provenance: VAPOR-6eabb1be532bdef4
-- Separate databases so the sponsor and indexer never share a schema.
CREATE DATABASE vapor_sponsor OWNER vapor;
CREATE DATABASE vapor_indexer OWNER vapor;
