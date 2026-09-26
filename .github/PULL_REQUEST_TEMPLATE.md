## What and why

<!-- What does this change, and why? Link the issue: "Closes #123". -->

## Area and risk

<!-- Tick the areas you touched. Critical paths (CONTRIBUTING.md §5) need an approved issue first. -->

- [ ] docs / examples
- [ ] SDK / React / web app
- [ ] services (sponsor, indexer, bundler)
- [ ] contracts
- [ ] chain: **critical** (x/settle, x/apps, x/council, precompile, lane, app wiring)
- [ ] tooling / scripts / CI

## How it was tested

<!-- Commands you ran and their results. Bug fixes need a test that fails without the fix. -->

## Checklist

- [ ] One topic per PR; tests added or updated
- [ ] The checks in CONTRIBUTING.md §7 pass for every area touched
- [ ] No generated file edited by hand (CONTRIBUTING.md §8.1); regenerated with the tooling
- [ ] No state-compatibility rule broken (CONTRIBUTING.md §8.3), or an upgrade/migration is included
- [ ] License headers are present and correct (`node scripts/license/headers.mjs`)
- [ ] Every commit is signed off (`git commit -s`, DCO)
- [ ] No keys, `.env` files, `.localnet/` data or personal data
- [ ] This is **not** a security vulnerability fix disclosed in public (use SECURITY.md)
