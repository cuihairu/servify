> **English** | [中文](mobile-sdk-cocoapods-evaluation.md)

# CocoaPods Compatibility Layer Evaluation (M3 cut 4, wrap-up of the design doc §3 distribution-form decision)

Conclusion first: **no CocoaPods spec for now; stay on the single track of XCFramework + SwiftPM binary distribution**;
turn the compatibility layer into an on-demand item of "build it when an integrator explicitly asks"; trigger conditions and build recipe are in §4.

## 1. Current State

- Distribution form: XCFramework (static, `BUILD_LIBRARY_FOR_DISTRIBUTION`) + Swift Package Manager
  binary distribution. The CI `ios-macos` job produces the XCFramework on every push (artifact `servify-kit-xcframework`)
  and passes the size gate (≤ 2MB, M2 acceptance ②; `ios-swift` is a source test job and produces no artifacts).
- There is no `.podspec` in the repository; the integration documentation (M4 deliverable) only needs to cover the SwiftPM integration path.

## 2. Evaluation Dimensions

| Dimension | SwiftPM (current) | Adding a CocoaPods compatibility layer |
| --- | --- | --- |
| Integration surface | Fully covered by Xcode 13+ (the project requires iOS 15) | Only needed by legacy projects (CocoaPods-only) |
| Maintenance cost | 0 (already landed with CI) | A spec version alignment, a podspec lint, and a `pod trunk`/private repo flow, one of each |
| Binary reference | `binaryTarget` points directly at the XCFramework | `vendored_frameworks` with the same artifact, no technical difference |
| CI increment | — | Every SDK change needs a podspec validation step; the release cadence (version number/tag) stacks manual steps on top of the "no automatic release" discipline |
| Risk | — | Spec vs SwiftPM version drift is a long-term tax; with zero early integrators it is pure burden |

## 3. Verdict

Within the M3 milestone, **no known integrator is blocked by a CocoaPods-only project**; with zero consumers the
compatibility layer carries only cost and no benefit — the same discipline as the design doc's "avoid reserved fields with no consumer surface". The SwiftPM main path
already covers iOS 15 (all 2023+ Xcode defaults support it), and pure CocoaPods projects make up a very small share of the modern baseline.

## 4. Trigger Conditions and Build Recipe for On-Demand Rebuild

Trigger conditions (either one): the first CocoaPods-only integrator appears; or ≥ 2 prospects in
integration research explicitly require CocoaPods as a premise.

Build recipe at that time (do it right in one pass, no half-compatible version):

1. `ServifyKit.podspec`: `vendored_frameworks` points at the same XCFramework produced by CI
  (no double packaging); `deployment_target = 'ios15.0'`; `swift_version = '5.10'`.
2. Spec validation joins the CI ios job (`pod lib lint --allow-warnings` or at least `pod spec lint`),
  hung after the size gate.
3. Version numbers share the same source as the SwiftPM tag (single source of truth: git tag), and releases are still executed manually by maintainers
  (the repository does not ship automatic releases; the M4 acceptance ③ convention is unchanged).

## 5. Record

- Evaluator: SDK milestone advancement (M3 cut 4); date: 2026-09-23.
- Recheck point: recheck once at M4 integration documentation site finalization on "whether a CocoaPods demand has appeared".
