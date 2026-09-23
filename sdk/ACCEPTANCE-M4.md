# M4 验收矩阵（稳定化与接入就绪）

对应 `docs/mobile-sdk-design.md` M4 验收条款 ①—③。M4 的特殊性：三条产线里两条
（长稳、性能基线）是**环境依赖项**而非代码项——本矩阵如实标注执行状态与到位后的
执行方案，**未执行的项不勾、不伪实现**。

## ① 双端 demo 连续 72h 长稳 ⏳ 环境阻塞，执行方案已冻结

- **阻塞面**：GitHub Actions job 上限 6h 无法容纳 72h；本机无 Android 模拟器/iOS 真机；
  双端 demo 工程（Android APK 可装真机，iOS 需 Xcode 项目文件）待 macOS 环境落地（M2 验收③同源约束）。
- **执行方案（环境到位后照单执行，不临时设计）**：
  1. 双端 demo 宿主装真机，接真实 servify 服务端（或长稳 mock 服务）；
  2. 压力脚本：重连循环（服务端周期性断开）+ 前后台往返（30s 周期）+ 低频收发消息；
  3. 判据：72h 无崩溃、无泄漏（内存增量曲线平台稳态、连接句柄数不增长）、
     重连循环恢复率 100%、前后台往返未读计数一致；
  4. 留档：脚本 + 曲线截图 + manifest（同 `mobile-probe` 留档惯例）。
- **CI 内可自动化的部分已前置锚定**：重连循环/耗尽/恢复、前后台可见性语义均已在
  M1-M3 单测矩阵穷举（见下"弱网矩阵"节）——72h 长稳验证的是这些路径在长时间与
  真实网络下的组合稳定性，不是新代码路径。

## ② 文档站构建绿并挂载发布位 ✅

- **接入指南（6ba98c7）**：`docs/mobile-sdk-integration.md`——面向接入方的双端指南
  （前置要求/产物获取/最小接入 ≤10 行/配置表/M3 API/事件流/错误码/自验清单/V1 边界），
  以平台规格 §5 为蓝本，全部 API 签名经源码核对。
- **站点挂载（6ba98c7）**：nav 增"SDK 接入"，`productPages` 增 `/mobile-sdk-integration`；
  本地 `vitepress build` 绿。
- **发布位**：`docs-pages.yml` 既有流程——push docs/** 自动构建 + 发布 GitHub Pages，
  无需新接线。

## ③ 对外交付物（AAR/XCFramework 产物流程）就绪 ✅（发布动作仍由维护者手动执行）

- **CI 产物归档（f4a129d，路径修正）**：android job 上传 `servify-sdk-aar`（servify-sdk-release.aar），
  ios-macos job 上传 `servify-kit-xcframework`（`sdk/ios/build/ServifyKit.xcframework.zip`——体积门禁
  脚本 cd 进 sdk/ios，产物留在该目录；即分发形态）；均 `if-no-files-found: error`（路径错明确红）+
  retention 30 天（产物可重建）。
- **获取路径文档化**：接入指南 §2（CI artifact / 本地构建命令双路径）。
- **SwiftPM 远程引用的边界**（如实记录）：`sdk/ios/Package.swift` 不在仓库根，远程
  `.package(url:)` 引用不可用；V1 接入路径为 XCFramework 手动嵌入，远程分发需独立
  distribution 仓库——发布动作由维护者手动执行（仓库不内置自动发版），非 M4 交付面。
- **体积门禁**：两门禁（AAR ≤1.5MB R8 口径 / XCFramework ≤2MB zip 口径）随每次 push
  持续绿（M1-M3 验收③口径延续）。

## 断线/弱网自动化测试矩阵 ✅（已由 M1-M3 锚定面穷举，无新增用例——缺口分析记录）

M4 产线定义里的"断线/弱网自动化测试矩阵"经缺口分析确认已被 M1-M3 矩阵穷举
（100% 行级覆盖门禁的副产品），**不为测而测添加时间敏感用例**（CI flaky 纪律）：

| 弱网场景 | 代码路径 | 锚定（Android ↔ iOS 镜像） |
|---|---|---|
| 高延迟回显（> echoTimeout） | sendTimeout 错误路径 | `sendMessageTimesOutWithoutEchoAndEmitsSendTimeout` |
| 连接中断 | 退避重连 | `reconnectsAfterServerDrop` ↔ Swift 同名 |
| 重连耗尽 | → disconnected 终态 + offlineText | `reconnectExhaustionMarksDisconnectedAndConnectRecovers` 等 |
| 握手失败（404） | → disconnected | `handshakeFailureBeforeEverConnectedMarksDisconnected` |
| 流中断（连接断于流式中途） | 部分内容保留 + 提示行 | `streamInterruptionOnDisconnectFinalizesPartialWithHint` |
| 退避序列/耗尽判定 | ReconnectPolicy | `ReconnectPolicyTests` 4 用例 |

延迟注入（MockWebServer `setBodyDelay` 类）不触达上表之外的新代码路径，只会引入
时间敏感断言——不添加。

## 性能基线（会话页首帧 < 300ms、内存增量 < 30MB）⏳ 环境阻塞

- 需真机 + 宏基准（Android Macrobenchmark / iOS XCTest Metrics），与 ① 同源环境约束。
- 口径已冻结（设计文档 M4 产出清单），环境到位后随 ① 的长稳执行一并跑。
- 现有间接证据：体积增量双端在 D9 门禁内（AAR ≈1.19MB / XCFramework ≈521KB），
  UI 面为自绘/纯 SwiftUI 零三方——但不以此替代实测，如实标注待执行。
