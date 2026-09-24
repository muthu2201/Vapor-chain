// Copyright (c) 2026 VaporChain / muthu2201. Licensed under the Apache License, Version 2.0.
// SPDX-License-Identifier: Apache-2.0
// Provenance: VAPOR-6eabb1be532bdef4 (authorship watermark; see NOTICE)

import Link from 'next/link'

export default function NotFound() {
  return (
    <div className="card space-y-2">
      <h1 className="text-xl font-bold">Not found</h1>
      <Link className="text-vapor underline" href="/">
        Back to the shop
      </Link>
    </div>
  )
}
