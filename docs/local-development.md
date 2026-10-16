# Local Development

This file defines the minimal development flow of Servify under Windows PowerShell, WSL, Linux, and CI, aiming to reduce the problem of "the same command behaving differently across environments".

## Recommended Way

- Windows users:
  - Recommended to run Git and editor operations in PowerShell
  - Recommended to run the repository's `sh` / `bash` scripts inside WSL Ubuntu
- Linux users:
  - Directly use the repository's `make`, `go`, `npm`, `sh` entry points
- CI:
  - Follows `.github/workflows/ci.yml`, defaults to Ubuntu + bash

## Unified Entry Points

- Build:
  - `make build`
  - `make build-knowledge-provider`
  - `build-knowledge-provider` is a generic alias of `build-weknora`, used for external knowledge provider compatibility builds
- Run services:
  - `make run`
  - `make run-cli CONFIG=./config.yml`
  - `make run-knowledge-provider CONFIG=./config.weknora.yml`
  - `make run-weknora CONFIG=./config.weknora.yml`
  - `run-cli` and `run` (cmd/server) share the same orchestration root `bootstrap.RunStandalone` (BuildRouter full routing surface); the differences are only two: the CLI does not run migrations (expects a pre-migrated database), and does not register background workers (Migrate/RegisterWorkers off in `StandaloneOptions`). The database driver and port are overridden via environment variables (`DB_DRIVER=sqlite DB_DSN=... SERVIFY_PORT=...`), so the server can be started on sqlite when there is no local Postgres
  - `run-weknora` / `run-knowledge-provider` are the same implementation as `run-cli`, only compiled with `-tags weknora` (compatibility build artifact); provider switching is driven by configuration (RAGFlow / Dify / WeKnora sections), used for WeKnora compatibility / mock regression; the knowledge base selection chain is ragflow → dify → weknora → pgvector direct configuration (lands on the pgvector default path when there is zero external configuration; for positioning clarification and selection direction see [Knowledge Base Selection Landscape](./KNOWLEDGE_BASE_LANDSCAPE.md), for RAGFlow integration see [RAGFlow Integration](./RAGFLOW_INTEGRATION.md))
- Tests:
  - `./scripts/run-tests.sh`
  - `./scripts/run-go-race-tests.sh`
  - `./scripts/run-smoke-tests.sh`
  - `make dify-acceptance`
  - `make ragflow-acceptance`
  - `make weknora-acceptance`
  - `make auth-session-acceptance`
  - `make validate-acceptance-manifest MANIFEST=./scripts/test-results/<provider>/<mode>/manifest.json`
  - `make check-acceptance-evidence`
  - `make knowledge-provider-acceptance`
  - `dify-acceptance` is used for Dify main path acceptance; default `DIFY_ACCEPTANCE_MODE=mock`
  - `weknora-acceptance` is used for WeKnora compatibility acceptance; default `WEKNORA_ACCEPTANCE_MODE=mock`
  - `auth-session-acceptance` is used for authentication and self-service session chain acceptance; default `AUTH_ACCEPTANCE_MODE=real`
  - These scripts all write evidence to `./scripts/test-results/...`
  - `scripts/test-results/**/manifest.json` can be included into git as the formal acceptance index; other response files still stay ignored by default, avoiding committing the local artifacts wholesale
  - Before real-environment acceptance, `SERVIFY_URL`, provider addresses, and dataset/credentials must be explicitly overridden
- Generated artifacts and repository hygiene:
  - `make demo-sync-sdk`
  - `make generated-assets`
  - `make release-changelog FROM=<previous-tag-or-commit> TO=HEAD`
  - `make repo-hygiene`
  - `make local-check`
  - `make release-check CONFIG=./config.yml`
  - `make clean-runtime`
  - `./scripts/verify-generated-assets.sh`

## Windows and WSL Conventions

- If you call `bash` or `sh` from PowerShell, confirm it actually points to a usable environment
- If the system's `bash.exe` points to a legacy WSL entry without a correctly bound distro, prefer:
  - `wsl -d Ubuntu -- bash -lc "<command>"`
- If `sh` comes from Git Bash / a scoop shim, simple script checks can be run directly

## Git Safe Directory

When Git inside WSL reports `detected dubious ownership`, run inside WSL:

```bash
# 在 WSL 内仓库目录执行
git config --global --add safe.directory "$(pwd)"
```

## Line Ending and Permission Bits

- The repository unifies text line ending policy through `.gitattributes`
- Shell scripts default to LF
- CI uses `bash -n` to validate script syntax
- Scripts needing execute permission are explicitly `chmod +x`-ed before running in CI or in the shell

## Runtime File Locations

- Local runtime output defaults to `./.runtime/`
- Do not place the upload directory, temporary output, or binaries in the repository root
- Build artifacts prefer output to `./bin/`
- When the local runtime directory needs cleaning, run `make clean-runtime`

## Minimal Cross-Platform Checklist

- Recommended to run first:
  - `make local-check`
  - `make dify-acceptance`
  - `make ragflow-acceptance`
  - `make weknora-acceptance`
  - and add before release: `make release-check CONFIG=./config.yml`
- `go test ./apps/server/internal/config ./apps/server/internal/handlers`
- `go test ./scripts -run "Test(WeKnoraIntegrationScript(RealModeRejectsLocalHost|MockModeWritesEvidence)|DifyIntegrationScript(RealModeRejectsLocalHost|MockModeWritesEvidence))$"`
- `sh scripts/check-repo-hygiene.sh`
- If the generated artifact flow was changed, additionally run:
  - `sh scripts/regenerate-generated-assets.sh`
  - `sh scripts/verify-generated-assets.sh`

`make local-check` will output:

- The resolved paths of the current `sh` / `bash` / `node` / `npm`
- The repo hygiene and generated asset verification results
- whether `safe.directory` already includes the current repository
- whether WSL is available, plus the current distro list

## Known Differences

- In some Windows environments, `bash` may default to the legacy WSL entry, making `/bin/bash` unavailable
- `npm` / `node` inside WSL may accidentally connect to the Windows-side install path; `which node` and `which npm` need separate checks
- PowerShell, Git Bash, and WSL do not fully agree on path formats and quote handling; complex commands should preferably be written into repository scripts
