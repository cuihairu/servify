# 09 Runtime And Repo Hygiene

Scope:

- Repository hygiene
- Runtime artifact boundaries
- Ignore policy
- Local development environment consistency
- Generated asset and cache governance

## R1 tracked-runtime-artifacts-cleanup

- [x] Inventory mistakenly committed binaries, upload directories, test leftovers, and cache files
- [x] Remove non-source artifacts such as `server`, `server.exe`, and runtime upload samples
- [x] Create an explicit fixture directory for sample files needed in tests
- [x] Move runtime output directories out of the source tree or switch them to temporary directories
- [x] Add cleanup scripts or test teardowns to avoid re-polluting the workspace

Acceptance:

- The repository no longer keeps dirty runtime artifacts long term
- Test samples and real runtime output have a clear boundary

## R2 ignore-and-boundary-policy

- [x] Unify the ignore policy across the root directory, `sdk/`, `docs/`, and `apps/*`
- [x] Distinguish generated assets that must be committed from build/runtime/cache files that must not
- [x] Establish rules for binaries, logs, coverage, temporary databases, and upload directories
- [x] Add documentation and maintenance rules for the generated assets manifest
- [x] Define default ignore conventions for new directories

Acceptance:

- Newcomers can quickly tell "which files should be committed and which should not"
- CI and local development no longer drift back and forth over unclear boundaries

## R3 local-dev-environment-convergence

- [x] Inventory command differences between Windows PowerShell, WSL, Linux, and CI
- [x] Provide unified entry points for common workflows, such as `make` / `npm scripts` / shell wrappers
- [x] Resolve Git safe directory, line ending, permission bit, and Node/Go path differences
- [x] Add WSL and Ubuntu initialization documentation
- [x] Establish a minimal verification checklist for cross-platform development

Acceptance:

- The same development workflow runs stably on Windows + WSL + Linux
- Common environment problems have clear documentation and fix paths

## R4 generated-assets-governance

- [x] Inventory all generated assets that must be committed and their generation entry points
- [x] Inventory all build and cache artifacts that must not be committed
- [x] Unify generation rules for demo SDK, docs API, and release/changelog assets
- [x] Add a one-command rebuild entry for generated assets
- [x] Add boundary drift explanation output in CI to reduce troubleshooting cost

Acceptance:

- Generated asset governance is no longer a scatter of scripts but a unified set of rules
- Any category of generated asset can be rebuilt and verified quickly

## R5 repo-hygiene-guardrails

- [x] Add repository hygiene checks, for example forbidding committed binaries, runtime directories, and oversized files
- [x] Add fixture / testdata / generated directory naming conventions
- [x] Add pre-commit self-check rules to the PR template or contribution docs
- [x] Add responsibility notes for root directory files to stop piling up miscellaneous files

Acceptance:

- The repository structure stays maintainable long term and does not degrade through accumulated temporary files
