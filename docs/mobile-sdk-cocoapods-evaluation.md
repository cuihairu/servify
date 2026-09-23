# CocoaPods 兼容层评估（M3 刀 4，设计文档 §3 分发形态决策的收尾）

结论先行：**暂不建 CocoaPods spec，继续 XCFramework + SwiftPM 二进制分发单轨**；
把兼容层转为"有接入方明确要求时再建"的按需项，触发条件与建法见 §4。

## 1. 现状

- 分发形态：XCFramework（静态，`BUILD_LIBRARY_FOR_DISTRIBUTION`）+ Swift Package Manager
  二进制分发。CI `ios-swift` job 在每次 push 产出并过体积门禁（≤ 2MB，M2 验收②）。
- 仓库内无 `.podspec`；接入文档（M4 产出）只需覆盖 SwiftPM 接入路径。

## 2. 评估维度

| 维度 | SwiftPM（现状） | 增设 CocoaPods 兼容层 |
| --- | --- | --- |
| 接入面 | Xcode 13+ 全覆盖（项目要求 iOS 15） | 仅老工程（CocoaPods-only）需要 |
| 维护成本 | 0（已随 CI 落地） | spec 版本对齐、podspec lint、`pod trunk`/私有 repo 流程各一套 |
| 二进制引用 | `binaryTarget` 直接指 XCFramework | `vendored_frameworks` 同一份产物，无技术差异 |
| CI 增量 | — | 每次 SDK 变更需要 podspec 校验步骤；发布节奏（版本号/tag）与"不自动发版"纪律叠加人工步骤 |
| 风险 | — | spec 与 SwiftPM 版本漂移是长期税；早期接入方为零时纯负担 |

## 3. 判定

M3 里程碑内**无已知接入方被 CocoaPods-only 工程阻塞**；兼容层在零消费者的现状下
只有成本没有收益——这与设计文档"避免无消费面的预留字段"同一纪律。SwiftPM 主路径
已覆盖 iOS 15（2023+ Xcode 默认全部支持），纯 CocoaPods 工程在现代基线里占比极低。

## 4. 按需重建的触发条件与建法

触发条件（任一）：出现第一个 CocoaPods-only 接入方；或接入调研中 ≥ 2 家明确以
CocoaPods 为前提。

到时建法（一次到位，不做半兼容）：

1. `ServifyKit.podspec`：`vendored_frameworks` 指向 CI 产出的同一份 XCFramework
  （不双打包）；`deployment_target = 'ios15.0'`；`swift_version = '5.10'`。
2. spec 校验进 CI ios job（`pod lib lint --allow-warnings` 或至少 `pod spec lint`），
   挂在体积门禁之后。
3. 版本号与 SwiftPM tag 同源（单一事实：git tag），发布仍由维护者手动执行
  （仓库不内置自动发版，M4 验收③口径不变）。

## 5. 记录

- 评估人：SDK 里程碑推进（M3 刀 4）；日期：2026-09-23。
- 复核时点：M4 接入文档站定稿时复核一次"是否出现 CocoaPods 诉求"。
