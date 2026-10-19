# Admin dependency audit exemptions

Scope: `apps/admin` only (`@servify/admin`, UMI 4 / `@umijs/max` 4.7.16). The
server-side `apps/server` module has no `pnpm audit` surface and is out of
scope here.

These are **recorded exemptions, not suppressions**: nothing in the manifest
silences the findings (no `--ignore` flag, no `audit:ignore`, no registry
override to a still-vulnerable version). `pnpm audit` keeps reporting all of
them so that a future re-run stays honest. Every entry below has a registry
fact that makes it impossible to close with a dependency upgrade.

## Snapshot (2026-10-19)

```
npx -y pnpm@10.22.0 --dir apps/admin audit
3 vulnerabilities found
Severity: 1 low | 1 moderate | 1 high
```

`pnpm audit --prod` reports the same three because `@umijs/max` is declared
under `dependencies` (UMI is a build-time tool, but the app keeps it in the
prod section). All three chains are lint / test / bundler toolchain — none of
them is shipped in the served bundle (evidence below).

`react-router` closed the other two of the original five findings on
2026-10-15 via `pnpm.overrides` (`react-router@<7.18.0` → `^7.18.0`, resolved
7.18.4, commit `bdd6cd5`): GHSA-wrjc-x8rr-h8h6 (`<Link>` backslash open
redirect) and GHSA-337j-9hxr-rhxg (constructor injection). Audit went from 5
to 3 findings.

## 1. `braces` — GHSA-vfj7-8cjw-p6xm (high)

| Field | Value |
|---|---|
| Vulnerable versions | `<=3.0.3` |
| Patched versions | `<0.0.0` — no fixed release published |
| Registry latest | `3.0.3` (`npm view braces dist-tags` → `latest: 3.0.3`; published versions end at `3.0.3`) |
| Path | `@umijs/max > @umijs/lint > @typescript-eslint/eslint-plugin > @typescript-eslint/type-utils > @typescript-eslint/typescript-estree > globby > fast-glob > micromatch > braces` |

Reachability: `@umijs/lint` is only loaded by `max lint`. In a fresh `max
build` output (`dist/`, 91 files) `globby` and `micromatch` appear in 0 files,
so the glob-expansion chain is not shipped.

Why not an override: there is no patched version to pin to. Pinning
`braces@3.0.3` would pin the identical vulnerable release and change nothing.

Unblock condition: upstream publishes a `braces` release with a non-zero
patched version. Then add a `pnpm.overrides` entry, `pnpm install`, run
`typecheck` + `build`, wait for CI `Admin Checks`, and delete this entry.

## 2. `sprintf-js` — GHSA-hp3w-g68c-fv3c (moderate)

| Field | Value |
|---|---|
| Title | denial of service through unbounded precision specifiers |
| Vulnerable versions | `<=1.1.3` |
| Patched versions | `<0.0.0` — no fixed release published |
| Registry latest | `1.1.3` (no `1.2.x` line published at all; `npm view sprintf-js versions` ends at `1.1.3`) |
| Path | `@umijs/max > umi > @umijs/test > babel-jest > babel-plugin-istanbul > @istanbuljs/load-nyc-config > js-yaml > argparse > sprintf-js` |

Reachability: `@umijs/test` is umi's jest test harness. In the same build
output `sprintf`, `js-yaml` and `argparse` all appear in 0 files.

Why not an override: no version above `1.1.3` exists in the registry, so
"upgrade within a range" is not possible either — any range specifier resolves
back to the vulnerable release.

Unblock condition: upstream publishes a fixed `sprintf-js`. Same procedure as
above.

## 3. `elliptic` — GHSA-848j-6mx2-7j84 (low)

| Field | Value |
|---|---|
| Title | uses a cryptographic primitive with a risky implementation |
| Vulnerable versions | `<=6.6.1` |
| Patched versions | `<0.0.0` — no fixed release published |
| Registry latest | `6.6.1` (`npm view elliptic dist-tags` → `latest: 6.6.1`) |
| Path | `@umijs/max > umi > @umijs/bundler-webpack > node-libs-browser > crypto-browserify > browserify-sign > elliptic` |

Reachability: `node-libs-browser` is webpack's node-builtin polyfill set — a
polyfill only enters the bundle when the app actually imports the node builtin
it covers. In a fresh `max build` output `secp256k1` (elliptic's curve
implementation) appears in 0 files and `node-libs-browser` in 0 files. The
single `elliptic` substring hit is `ellipticalArcAngle` /
`ellipticalArcStartAngle` from `@ant-design/charts` geometry math — an
unrelated substring match, not the crypto package. The admin app performs no
node crypto signing.

Why not an override: same as above — `6.6.1` is both the installed and the
registry-ceiling version, and it is the vulnerable ceiling.

Unblock condition: upstream publishes a fixed `elliptic`. Same procedure as
above.

## Why an `@umijs/max` upgrade does not close any of these

`@umijs/max` 4.7.25 is the current `latest` tag against the installed 4.7.16,
but upgrading cannot resolve any of the three entries: each chain is structural
to UMI (`@umijs/lint` eslint tooling, `@umijs/test` jest harness,
`@umijs/bundler-webpack` node polyfills) and each leaf package has no fixed
release to resolve to. The blocking fact is the leaf, not the top of the
chain.

## Procedure when an upstream fix lands

1. Re-run `npx -y pnpm@10.22.0 --dir apps/admin audit` and confirm the patched
   version is published.
2. Add the narrowest `pnpm.overrides` entry that follows the pattern already in
   use (`react-router@<7.18.0`, `axios@<1`, `qs@<6.16`, ...).
3. `pnpm install`, then `run typecheck`, `run lint`, `run build`.
4. Wait for CI `Admin Checks` green.
5. Delete the matching entry in this file — the exemption is only valid while
   the patched range is `<0.0.0`.
