> **English** | [中文](generated-assets.md)

# Generated Assets

This file explains which generated artifacts in Servify are committed to the repository, their generation entry points, and how CI verifies them.

## Currently Controlled Generated Assets

`generated-assets.manifest` currently contains:

- `apps/demo-sdk/servify-sdk.esm.js`
- `apps/demo-sdk/servify-sdk.umd.js`
- `apps/demo-sdk/index.d.ts`
- `docs/generated/api/docs.go`
- `docs/generated/api/swagger.json`
- `docs/generated/api/swagger.yaml`

## Unified Rebuild Entry Point

Recommended:

```bash
sh scripts/regenerate-generated-assets.sh
```

It runs, in order:

1. Sync the demo SDK artifacts
2. Regenerate the API docs
3. Verify `generated-assets.manifest`

## Per-Item Generation Entry Points

### Demo SDK

Command:

```bash
make demo-sync-sdk
```

Or:

```bash
sh scripts/sync-sdk-to-demo.sh
```

Sources:

- `sdk/packages/vanilla/dist/index.esm.js`
- `sdk/packages/vanilla/dist/index.js`
- `sdk/packages/vanilla/dist/index.d.ts`

Output:

- `apps/demo-sdk/servify-sdk.esm.js`
- `apps/demo-sdk/servify-sdk.umd.js`
- `apps/demo-sdk/index.d.ts`

### API Docs

Command:

```bash
go run github.com/swaggo/swag/cmd/swag@v1.16.6 init -g apps/server/cmd/server/main.go -o docs/generated/api
```

Output:

- `docs/generated/api/docs.go`
- `docs/generated/api/swagger.json`
- `docs/generated/api/swagger.yaml`

### Release Changelog Draft

Command:

```bash
make release-changelog FROM=<previous-tag-or-commit> TO=HEAD
```

Or:

```bash
sh scripts/generate-changelog.sh <previous-tag-or-commit> HEAD > ./.runtime/release/RELEASE_CHANGELOG.md
```

Output:

- `./.runtime/release/RELEASE_CHANGELOG.md`
- The `RELEASE_CHANGELOG.md` inside the GitHub release workflow artifact

Rules:

- This is a release artifact, not a controlled committed generated asset
- It does not go into `generated-assets.manifest`
- By default it is written to `.runtime/` or a workflow artifact, not back into the repository root

## Verification Entry Points

Existence and Git tracking verification:

```bash
sh scripts/verify-generated-assets.sh
```

Working-tree drift verification:

- In CI, an annotated drift check runs after generation and prints:
  - Which paths changed
  - `git diff --stat`
  - The recommended rebuild command
- Locally, after a rebuild, run:

```bash
git diff --exit-code -- apps/demo-sdk docs/generated/api
```

## Rules When Adding New Generated Assets

- Must be reproducibly rebuildable
- Must have an explicit generation command
- Must decide whether to include it in `generated-assets.manifest`
- Must add the corresponding documentation
- If CI needs to block drift, a verification step must be added
- If it is merely a transient artifact of the release process, it should be written to `.runtime/` or an artifact rather than committed to the repository
