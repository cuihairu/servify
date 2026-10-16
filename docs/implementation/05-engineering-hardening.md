# 05 Engineering Hardening

Scope:

- CI quality gates
- Coverage and stability
- Generated-assets consistency
- Release and versioning
- Docs-site automation

## H1 ci-quality-gates

- [x] Add a layered `go test -race` execution strategy for Go
- [x] Add coverage artifact output for Go
- [x] Add a standalone `typecheck` job for the SDK
- [x] Add a build-verification job for docs/VitePress
- [x] Add minimal static checks for shell/scripts

Acceptance:

- CI verifies not only compilability but also concurrency safety, type stability, and docs buildability

## H2 generated-assets-and-lockfiles

- [x] Freeze the list of generated assets that must be committed
- [x] Add lockfile consistency checks
- [x] Add demo SDK sync-result verification
- [x] Add API doc generation/drift checks

Acceptance:

- Local and CI no longer drift long-term over "forgotten generated-asset commits"

## H3 release-versioning

- [x] Design server version injection
- [x] Design the SDK workspace version strategy
- [x] Reserve a changelog generation script
- [x] Reserve a tag/release action

Acceptance:

- Future server and SDK releases do not need the versioning pipeline redesigned

## H4 test-pyramid

- [x] Inventory the current integration build tag coverage
- [x] Add handler-to-runtime end-to-end tests for `voice`
- [x] Add provider fallback integration tests for `ai`
- [x] Add examples smoke tests for `sdk`
- [x] Define a minimal smoke-case set for key modules

Acceptance:

- Unit tests, integration tests, and smoke tests have clear responsibilities

## H5 docs-delivery

- [x] Add the VitePress site config skeleton
- [x] Add the docs navigation and sidebar generation strategy
- [x] Add a GitHub Pages or artifact publishing pipeline
- [x] Add Mermaid rendering compatibility constraint notes

Acceptance:

- `docs/` builds and deploys reliably, instead of being just Markdown files
