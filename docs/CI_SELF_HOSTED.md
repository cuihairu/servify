# GitHub Hosted CI

The repository now runs on GitHub Hosted Runner and no longer depends on repository-private self-hosted runners.

## Current Runtime Environment

- Runner: `ubuntu-latest`
- Workflow: `.github/workflows/ci.yml`
- Triggers:
  - push to `main`
  - pull requests against `main`
  - manual `workflow_dispatch`

## Current CI Checks

### Go checks

- `gofmt` format verification
- `go mod tidy` drift check
- `go vet`
- Unit tests and coverage gate
- Standard binary build
- WeKnora compatibility tag build

### Module checks

- `internal/modules/...` build and tests
- `internal/platform/...` build and tests
- `internal/handlers/...` build and tests
- `cmd/server` entrypoint build

### SDK checks

- `sdk` dependency install
- SDK lint
- SDK test
- SDK build
- Workspace drift check after syncing the SDK into the demo

### Docs checks

- swag regeneration and generated-artifact consistency verification
- VitePress documentation site build

### Script checks

- Syntax and allowlist verification of scripts under `scripts/` (including tests)

### Smoke checks

- Service smoke boot and basic endpoint checks

### Android probe

- Android SDK build (AAR artifact: `servify-sdk-aar`)

### iOS checks

- `ios-swift`: Swift source test job
- `ios-macos`: macOS Darwin face verification, producing the `servify-kit-xcframework` artifact and passing the size gate (`check-ios-sdk-size.sh`)

### Admin checks

- `apps/admin` build and checks

### AI Golden Set (real LLM)

- Job named `golden-real`, runs only on a manual `workflow_dispatch` (it needs real LLM credentials, so push/PR pipelines skip it)

### Integration

- Brings up the integration environment with `docker compose` using the base + weknora compatibility mock double overlay (`docker-compose.yml` + `docker-compose.weknora.yml`; the Dify service is not included)
- Health-check polling
- Runs `scripts/test-weknora-integration.sh`
- Migration watermark reconciliation: reads the head version of the migration chain dynamically to verify `schema_migrations` (not hard-coded)
- Backup & restore drill: pg_dump/pg_restore rehearsal
- Dumps compose logs on failure

CI itself has no deployment job; the release surface is the manual `release.yml` (`workflow_dispatch` triggered).

## Design Trade-offs

- No extra runner hosts to maintain, which reduces operations cost
- CI fails explicitly instead of masking problems through error-swallowing logic in `make test`
- Go, SDK and integration are split into separate jobs, which makes failures easier to localize
- CI only checks and verifies, it does not deploy; release goes through the manual release workflow

## Checks That Could Be Added Next

- Minimal Mermaid render smoke check
- OpenAPI/contract drift check
- Finer-grained integration matrix
