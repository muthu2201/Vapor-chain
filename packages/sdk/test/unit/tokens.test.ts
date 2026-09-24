// Copyright (c) 2026 VaporChain / muthu2201. Licensed under the Apache License, Version 2.0 (see LICENSE).
// Provenance: VAPOR-6eabb1be532bdef4
import { describe, expect, it } from 'vitest'
import { tokenSalt } from '../../src/index.js'

const owner = '0x47A951946a6647aB34A78809626ad9a374ECd267'

describe('tokenSalt', () => {
  it('is deterministic so predict and launch agree', () => {
    expect(tokenSalt({ name: 'Gem', symbol: 'GEM', owner })).toBe(tokenSalt({ name: 'Gem', symbol: 'GEM', owner }))
  })
  it('does not depend on address checksum casing', () => {
    expect(tokenSalt({ name: 'Gem', symbol: 'GEM', owner })).toBe(tokenSalt({ name: 'Gem', symbol: 'GEM', owner: owner.toLowerCase() as `0x${string}` }))
  })
  it('changes with any identity field or an explicit salt', () => {
    const base = tokenSalt({ name: 'Gem', symbol: 'GEM', owner })
    expect(tokenSalt({ name: 'Gem2', symbol: 'GEM', owner })).not.toBe(base)
    expect(tokenSalt({ name: 'Gem', symbol: 'GEM2', owner })).not.toBe(base)
    expect(tokenSalt({ name: 'Gem', symbol: 'GEM', owner, salt: 'v2' })).not.toBe(base)
  })
})
