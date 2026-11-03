# Admin dependency audit exemptions

Scope: `apps/admin` only (`@servify/admin`, UMI 4 / `@umijs/max` 4.7.30). The
server-side `apps/server` module has no `pnpm audit` surface and is out of
scope here.

These are **recorded exemptions, not suppressions**: nothing in the manifest
silences the findings (no `--ignore` flag, no `audit:ignore`, no registry
override to a still-vulnerable version). `pnpm audit` keeps reporting all of
them so that a future re-run stays honest. Every entry below has a registry
fact that makes it impossible to close with a dependency upgrade.

## Snapshot (2026-11-03)

```
corepack pnpm@10.22.0 --dir apps/admin audit --prod
3 vulnerabilities found
Severity: 1 moderate, 2 low
```

`pnpm audit --prod` reports the same three because `@umijs/max` is declared
under `dependencies` (UMI is a build-time tool, but the app keeps it in the
prod section). All remaining chains are bundler/lint toolchain — none of them
is shipped in the served bundle (evidence below).

`braces` (GHSA-vfj7-8cjw-p6xm, high, formerly entry 1) left this ledger on
2026-10-23: the advisory was **withdrawn** upstream as not practically
exploitable, so `pnpm audit` stopped reporting it. The installed `3.0.3` and
the registry ceiling are unchanged — no override or upgrade was involved.

On 2026-10-29 the Dependabot security batch for this directory closed four
of its six findings via `pnpm.overrides` re-resolution (`@hono/node-server`
→ 2.1.5, `qs` → 6.17.1, `fast-uri` → 3.1.9, `body-parser` → 1.20.9; the
sixth, `@vitest/mocker` 5.0.5, lives in `sdk`). `deepmerge` is the one
advisory of the batch with no published fix and joins this ledger below.

`react-router` closed two of the original five findings on 2026-10-15 via
`pnpm.overrides` (`react-router@<7.18.0` → `^7.18.0`, resolved 7.18.4,
commit `bdd6cd5`): GHSA-wrjc-x8rr-h8h6 (`<Link>` backslash open redirect) and
GHSA-337j-9hxr-rhxg (constructor injection). Audit went from 5 to 3 findings.

`sprintf-js` (GHSA-hp3w-g68c-fv3c, moderate) closed on 2026-10-19 by
**dependency replacement** rather than a patched release — it has none: the
only consumer of the vulnerable line (`js-yaml@3.15.2` → `argparse@1.0.10` →
`sprintf-js@1.0.3`, all reachable only through `@umijs/test`'s jest harness)
was overridden off it. `argparse@2` has zero dependencies, so pinning
`@istanbuljs/load-nyc-config>js-yaml` to `^4.3.2` drops `js-yaml@3`,
`argparse@1` and `sprintf-js` together. Safe because
`@istanbuljs/load-nyc-config@1.1.0` calls `require('js-yaml').load(...)`, the
API js-yaml 4 keeps (`safeLoad`, dropped in 4, was never used). `js-yaml@3`
had exactly one consumer in the tree before the override, and the lockfile
diff only removes those four packages. Audit went from 3 to 2 findings.

## 1. `elliptic` — GHSA-848j-6mx2-7j84 (low)

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

Why not a dependency replacement: `elliptic` reaches the tree through **two**
sibling packages under `crypto-browserify` — `browserify-sign@4.2.6` and
`create-ecdh@4.0.4` both depend on it (lockfile snapshots, 2026-10-19). Both
siblings are the ECDSA/ECDH implementations themselves, so no released
version of either drops `elliptic`; and `node-libs-browser@2.2.1` (registry
latest) exists precisely to provide this crypto polyfill set, so there is no
polyfill-free version to override it to. The leaf is structural to
webpack's node-polyfill layer.

Unblock condition: upstream publishes a fixed `elliptic`. Same procedure as
above.

## 2. `deepmerge` — GHSA-gcqx-w48m-w2h4 (low)

| Field | Value |
|---|---|
| Title | mergeObject() allows prototype poisoning of returned objects |
| Vulnerable versions | `>=4.2.0 <=4.3.1` |
| Patched versions | `<0.0.0` — no fixed release published |
| Registry latest | `4.3.1` (`npm view deepmerge@4.3.2` → E404; published versions end at `4.3.1`) |
| Path | `@umijs/max > umi > @umijs/bundler-webpack > @svgr/plugin-svgo > deepmerge` (audit-reported; `fork-ts-checker-webpack-plugin@8.0.0` declares the same `^4.2.2` and resolves the same `4.3.1`) |

Reachability: both consumers are webpack build steps. `@svgr/plugin-svgo`
optimizes imported `.svg` assets at compile time; `fork-ts-checker-webpack-plugin`
is the parallel TypeScript type-checker. In a fresh `max build` output
(`dist/`, 91 files) `deepmerge` appears in 0 files — the plugin is never
bundled into the served app.

Why not an override: no patched version exists to pin to. The last
non-vulnerable 4.x is `4.1.2`, but both consumers declare `deepmerge: ^4.2.2`
(`npm view @svgr/plugin-svgo@6.5.1 dependencies.deepmerge`,
`npm view fork-ts-checker-webpack-plugin@8.0.0 dependencies.deepmerge`), so a
pin would force a version below both consumers' declared ranges — a
compatibility gamble this ledger does not take for a low-severity,
build-time-only finding.

Why not a dependency replacement: `deepmerge` is the merge utility both
plugins call directly (`deepmerge`/`mergeAll`), a leaf with no drop-in
sibling in either chain; the only other instance in the tree is
`deepmerge@1.5.2` (via `webpack-5-chain`), an unrelated major with a different
surface that the `^4.2.2` consumers cannot adopt.

Unblock condition: upstream publishes a `deepmerge` release above `4.3.1`.
Then add a `pnpm.overrides` entry, `pnpm install`, run `typecheck` + `build`,
wait for CI `Admin Checks`, and delete this entry.

## 3. `minimatch` — GHSA-g3hv-75wg-m249 (moderate)

| Field | Value |
|---|---|
| Title | minimatch has potential denial of service via globparts amplification |
| Vulnerable versions | `<11.0.1` |
| Patched versions | `>=11.0.1` |
| Registry latest | `11.0.4` |
| Path | `@umijs/max > @umijs/lint > eslint-plugin-react > minimatch` |

Reachability: this is lint tooling only. `eslint-plugin-react@7.33.2` uses
`minimatch` only in `jsx-pascal-case` to compare rule names, and `max build`
does not ship `minimatch` or the plugin in the served bundle.

Why not an override: `@umijs/lint@4.7.30` pins `eslint-plugin-react@7.33.2`;
that plugin declares `minimatch: ^3.1.2` and calls the required module
directly (`minimatch(name, entry, { noglobstar: true })`). `minimatch@11`
changed the CommonJS export to an object whose callable is
`exports.minimatch`, so forcing the direct consumer to 11 makes the call
`TypeError: minimatch is not a function`. The compatible patched upgrade
must come from `eslint-plugin-react` (or `@umijs/lint`).

Why not a dependency replacement: the vulnerable leaf is imported directly by
the lint rule. Replacing it with a shim would mask the API mismatch rather
than use an upstream-supported compatibility release.

Unblock condition: `@umijs/lint` releases a version using an
`eslint-plugin-react` release compatible with `minimatch@11`. Then update
that chain, run `typecheck` + `lint` + `build`, and delete this entry.

## Why an `@umijs/max` upgrade does not close any of these

`@umijs/max` was upgraded from 4.7.16 to the current 4.7.30. That update plus
the `rimraf@5.0.1 → 6.1.4` override now resolves the mako build chain to
`rimraf@6.1.4 → glob@14.0.3 → minimatch@11.0.4`, closing the vulnerable glob
10 chain. rimraf@6.1.4 declares `glob: ^14.0.3`, so the previous
`glob@>=10.2 <10.5: 14.0.3` override (which injected glob@14 into rimraf@5.0.1
declaring `^10.2.5`, a semver violation) was removed. The remaining three chains
are structural to UMI: webpack node polyfills for `elliptic`; webpack svgr /
TS-checker tooling for `deepmerge`; and the pinned lint plugin chain for
`minimatch`. The blocking facts are the
leaves and the consumer API, not the top of the chain.

## Procedure when an upstream fix lands

1. Re-run `npx -y pnpm@10.22.0 --dir apps/admin audit` and confirm the patched
   version is published.
2. Add the narrowest `pnpm.overrides` entry that follows the pattern already in
   use (`react-router@<7.18.0`, `axios@<1`, `qs@<6.16`, ...).
3. `pnpm install`, then `run typecheck`, `run lint`, `run build`.
4. Wait for CI `Admin Checks` green.
5. Delete the matching entry in this file — the exemption is only valid while
   the patched range is `<0.0.0`.
