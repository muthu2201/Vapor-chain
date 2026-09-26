#!/usr/bin/env node
// SPDX-License-Identifier: AGPL-3.0-only
// Copyright (c) 2026 VaporChain / muthu2201
// Provenance: VAPOR-6eabb1be532bdef4
//
// Checks (default) or writes (--fix) the license header of every tracked
// source file, using the path -> license table in scripts/license/rules.json.
//
//   node scripts/license/headers.mjs          # CI: exit 1 if any header is missing or wrong
//   node scripts/license/headers.mjs --fix    # add or correct headers in place
//
// A header is the SPDX identifier, the copyright line, (for files derived from
// cosmos/evm) the upstream attribution, and the provenance line. Any extra
// "Copyright (c) <year> <name>" line a contributor adds is kept.
import { execFileSync } from 'node:child_process'
import { readFileSync, writeFileSync } from 'node:fs'
import { basename, dirname, join } from 'node:path'
import { fileURLToPath } from 'node:url'

const root = join(dirname(fileURLToPath(import.meta.url)), '..', '..')
const cfg = JSON.parse(readFileSync(join(root, 'scripts/license/rules.json'), 'utf8'))
const rules = cfg.rules.map((r) => ({ ...r, re: new RegExp(r.pattern) }))
const excludes = cfg.exclude.map((p) => new RegExp(p))
const fix = process.argv.includes('--fix')

const LINE = { '//': ['go', 'ts', 'tsx', 'js', 'mjs', 'cjs', 'sol', 'proto'], '#': ['sh', 'py', 'yml', 'yaml', 'toml', 'service', 'conf'], '--': ['sql'] }
const BY_NAME = { Dockerfile: '#', Makefile: '#' }

function style(path) {
  const name = basename(path)
  if (BY_NAME[name]) return { kind: 'line', tok: BY_NAME[name] }
  const ext = name.includes('.') ? name.split('.').pop() : ''
  if (ext === 'md') return { kind: 'block', open: '<!--', close: '-->' }
  if (ext === 'css') return { kind: 'block', open: '/*', close: '*/' }
  for (const [tok, exts] of Object.entries(LINE)) if (exts.includes(ext)) return { kind: 'line', tok }
  return null
}

const OURS = /^Copyright (\(c\) )?\d{4} VaporChain \/ muthu2201\b/
const HEADERISH = /^(SPDX-License-Identifier:|Copyright\b|Proprietary|Provenance: VAPOR-|Derived from cosmos\/evm)/

function wanted(path) {
  const rule = rules.find((r) => r.re.test(path))
  return { license: rule.license, derived: !!rule.derived }
}

function headerLines({ license, derived }, extra) {
  return [`SPDX-License-Identifier: ${license}`, cfg.copyright, ...extra, ...(derived ? [cfg.derivedLine] : []), cfg.provenance]
}

// Splits a single-line legacy header ("Copyright ... All rights reserved. Provenance: ...") into its
// copyright lines so a contributor's own copyright survives a rewrite.
function extraCopyrights(bodies) {
  return bodies.filter((b) => /^Copyright\b/.test(b) && !OURS.test(b))
}

function render(path, text) {
  const st = style(path)
  const want = wanted(path)
  const lines = text.split('\n')
  if (st.kind === 'line') {
    const tok = st.tok
    let i = lines[0]?.startsWith('#!') ? 1 : 0
    let j = i
    const bodies = []
    while (j < lines.length && lines[j].startsWith(tok) && HEADERISH.test(lines[j].slice(tok.length).trim())) {
      bodies.push(lines[j].slice(tok.length).trim())
      j++
    }
    const head = headerLines(want, extraCopyrights(bodies)).map((b) => `${tok} ${b}`)
    const rest = lines.slice(j)
    if (j === i && rest.length && rest[0].trim() !== '') head.push('')
    return [...lines.slice(0, i), ...head, ...rest].join('\n')
  }
  // block comment header at the top of the file (markdown, css)
  let j = 0
  const bodies = []
  if (lines[0]?.startsWith(st.open)) {
    if (lines[0].trim().endsWith(st.close) && lines[0].trim() !== st.open) {
      // legacy single-line header
      const inner = lines[0].trim().slice(st.open.length, -st.close.length).trim()
      if (/VAPOR-|SPDX-License-Identifier|Copyright/.test(inner)) {
        bodies.push(...inner.split(/(?<=\.)\s+(?=Copyright|Provenance)/))
        j = 1
      }
    } else if (lines[0].trim() === st.open) {
      const end = lines.findIndex((l, k) => k > 0 && l.trim() === st.close)
      if (end > 0 && end < 12 && lines.slice(1, end).every((l) => HEADERISH.test(l.trim()))) {
        bodies.push(...lines.slice(1, end).map((l) => l.trim()))
        j = end + 1
      }
    }
  }
  const head = [st.open, ...headerLines(want, extraCopyrights(bodies)), st.close]
  return [...head, ...lines.slice(j)].join('\n')
}

const files = execFileSync('git', ['ls-files', '-z'], { cwd: root, encoding: 'utf8' })
  .split('\0')
  .filter((f) => f && style(f) && !excludes.some((re) => re.test(f)))

const bad = []
for (const f of files) {
  let text
  try {
    text = readFileSync(join(root, f), 'utf8')
  } catch {
    continue // deleted in the working tree
  }
  const out = render(f, text)
  if (out === text) continue
  if (fix) writeFileSync(join(root, f), out)
  bad.push(`${f}  (want ${wanted(f).license})`)
}

if (fix) {
  console.log(bad.length ? `updated ${bad.length} file(s):\n  ${bad.join('\n  ')}` : 'all headers already correct')
} else if (bad.length) {
  console.error(`license headers missing or wrong in ${bad.length} file(s):\n  ${bad.join('\n  ')}`)
  console.error('\nrun: node scripts/license/headers.mjs --fix   (see CONTRIBUTING.md §9)')
  process.exit(1)
} else {
  console.log(`license headers ok (${files.length} files)`)
}
