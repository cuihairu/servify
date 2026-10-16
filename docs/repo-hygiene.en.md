> **English** | [中文](repo-hygiene.md)

# Repo Hygiene

This file defines the boundaries in the Servify repository between source code, generated artifacts, and runtime files, to avoid the workspace being polluted by temporary files, binaries, and caches.

## Content That Must Be Committed

- All source code, configuration templates, documentation, and test code
- Generated artifacts explicitly declared through `generated-assets.manifest`
- Artifacts the repository is designed as "part of the release surface", for example the committed demo SDK artifacts and API documentation artifacts

## Content That Should Not Be Committed

- Locally built binaries, such as `server` and `server.exe` in the root directory
- Runtime upload directories, temporary upload files during test execution
- Local caches, logs, coverage, temporary databases
- Ordinary build caches that package managers or build commands can rebuild reliably

## Current Explicit Boundaries

- `generated-assets.manifest`
  - Files listed here must exist and must be tracked by Git
- `uploads/`, `apps/server/uploads/`, `internal/handlers/uploads/`
  - Treated as runtime or test artifacts; must not enter version control
- `./.runtime/`
  - The root of the local runtime directory, carrying non-source content such as uploaded files and temporary runtime output
- `/server`, `/server.exe`
  - Treated as locally built binaries; must not enter version control

## Testing Conventions

- Upload-related tests should prefer `t.TempDir()` or the system temporary directory
- If a test truly depends on static samples, put them in an explicit fixture / testdata directory, not the runtime upload directory
- Tests should clean up the temporary files they create

## Naming Conventions

- `testdata/`
  - Committable, stable, reusable test samples
- `fixtures/`
  - Cross-test sample collections that need explicit semantic grouping
- `generated/`
  - Content that clearly belongs to generated artifacts and usually needs an additional generation entry explanation
- `.runtime/`
  - Local runtime output, not committed

## Generated Artifact Conventions

- Generated artifacts that must be committed need a stable generation entry and must be verifiable in CI
- When adding a generated artifact, also update:
  - The generation script or command entry
  - `generated-assets.manifest` or an equivalent verification mechanism
  - The corresponding documentation
- See `docs/generated-assets.md` for current governed generated artifacts

## Default Judgment When Adding a Directory

- If the directory content is source code, documentation, fixtures, or contract files, it should usually be committed
- If the directory content is runtime output, caches, compilation results, or downloads, it usually should not be committed
- If the directory content is in between, first pin down "whether it is rebuildable" and "whether CI should verify it"

## Default Ignore Conventions for New Directories

- New runtime directories:
  - Default to placing under `.runtime/` or the sub-app's own runtime directory
  - Default to adding to `.gitignore`
- New cache directories:
  - Default to adding to `.gitignore`
  - Prefer `.cache/`, the tool's default cache directory, or the sub-project's local cache directory for the name
- New build output directories:
  - Default not committed
  - Only when explicitly designed as a governed generated artifact may they be committed and enter the governance process
- New test sample directories:
  - Prefer `testdata/` or `fixtures/`
  - Committed by default
- New generated artifact directories:
  - If they need to be committed, add documentation, the generation command, and CI verification
  - If they do not need to be committed, add them to `.gitignore`

## Root Directory Governance

- The root directory should prioritize entry-level content, for example:
  - Top-level documentation
  - Top-level configuration examples
  - Top-level build entry points
  - Cross-application shared scripts and manifests
- In-app samples, runtime files, and module-private documentation should return to their own subdirectories as much as possible
- Before adding a root-level file, first judge whether it belongs to:
  - A whole-repository entry point
  - A cross-directory shared rule
  - The main release or build chain
- If it belongs to none of the above three, it usually should not go in the root directory
