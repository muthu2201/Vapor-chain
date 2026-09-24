// Copyright (c) 2026 VaporChain / muthu2201. Licensed under the Apache License, Version 2.0 (see LICENSE).
// Provenance: VAPOR-6eabb1be532bdef4
import { defineConfig } from 'vitest/config'

export default defineConfig({
  test: {
    environment: 'happy-dom',
    testTimeout: 180_000,
    hookTimeout: 180_000,
  },
})
