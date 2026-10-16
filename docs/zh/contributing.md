# Contributing

## Before opening a PR

- Run the smallest relevant test set for your change
- Prefer `make local-check` before opening a PR when environment/tooling changed
- Prefer `make release-check CONFIG=./config.yml` before release-oriented or deployment-facing changes
- If you touched generated assets, rebuild them and verify the diff is intentional
- If you touched runtime/build boundaries, run `sh scripts/check-repo-hygiene.sh`
- If you touched handler-to-module wiring, run `sh scripts/check-module-boundaries.sh`
- If you changed a locked boundary, update `scripts/module-boundaries.rules` and the corresponding `docs/implementation/10-*.md` docs together
- If you changed workflow, architecture, or backlog scope, update the relevant docs

## Repo hygiene

- Do not commit local binaries such as `server` or `server.exe`
- Do not commit runtime upload directories or local `.runtime/` contents
- Put reusable static test samples in `testdata/`
- Use `t.TempDir()` for ephemeral test output

## Generated assets

- Files listed in `generated-assets.manifest` must stay committed and reproducible
- Newly introduced generated assets need a documented generation command and CI verification path
- Preferred rebuild entrypoint: `sh scripts/regenerate-generated-assets.sh`
