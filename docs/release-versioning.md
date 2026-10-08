# Release Versioning

## Server version injection

- Single source of truth: `apps/server/internal/version`
- Build metadata is injected with `-ldflags` from the root `Makefile`
- Injected fields:
  - `Version`
  - `Commit`
  - `BuildTime`
- `servify` server handlers and `servify-cli version` both read the same package

## SDK workspace version strategy

- `sdk/package.json` is the workspace version source of truth
- Publishable packages share the exact same version as the workspace root:
  - `@servify/core`
  - `@servify/react`
  - `@servify/vue`
  - `@servify/vanilla`
- Internal cross-package dependencies are pinned to the exact same version, not a loose range
- `@servify/api-client` and `@servify/app-core` currently share the workspace version (`1.0.0`) and have no standalone publish entry: neither declares `private`, and no publish flow targets them (only the `sdk` workspace root sets `private: true`)
- Use:
  - `npm -C sdk run version:sync`
  - `npm -C sdk run version:check`

## Changelog and release flow

- Draft changelog generation is provided by `scripts/generate-changelog.sh`
- Local draft file generation is provided by `make release-changelog`
- Local release draft output defaults to `./.runtime/release/RELEASE_CHANGELOG.md`
- Manual release preparation is reserved in `.github/workflows/release.yml`
- Current release workflow prepares metadata and artifacts; it does not publish packages automatically
- `RELEASE_CHANGELOG.md` is treated as a release artifact, not a committed generated asset
