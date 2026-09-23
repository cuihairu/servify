# M2 验收矩阵（iOS SDK Alpha）

对应 `docs/mobile-sdk-design.md` M2 验收条款 ①—④。自动化项逐条锚定测试与提交；真机手工项与依赖外部环境的项如实标注执行状态——**未执行的项不勾**。用例与 Android（Kotlin）逐一镜像，防单侧漂移。

## ① 契约回放测试全绿 ✅

- `FixtureReplayTests`（XCTest 11 测试）：`sdk/protocol-fixtures/` 同一样例集喂 core、Android、Swift 三端，断言一致（M0 样例集锚定；CI `ios-swift` job 持续执行）。
- 当前 SDK 全量测试（2026-09-23）：Linux **61 全绿**（Swift Testing 40 + XCTest 21）；macOS `ios-macos` job 另跑 Darwin 面含 `CreateFactoryTests` 2 测试（合计 63）。
- 门面接线面（`ServifyChatTests` 13 + `ConnectionLifecycleTests` 6）与 Kotlin `ServifyChatTest`/`ConnectionLifecycleTest` 用例名逐一对应。

## ② XCFramework ≤ 2MB ✅

- 口径：Release 配置 + `BUILD_LIBRARY_FOR_DISTRIBUTION` 的 iOS XCFramework，**分发形态 zip 后字节数** ≤ 2097152（SwiftUI/Combine 是系统库零增量，预算=自身代码+协议模型，D9）。
- 门禁：`scripts/check-ios-sdk-size.sh`（xcodebuild build 取对象目录 → `libtool -static` 归档全部 .o + 手工组装 framework bundle（swiftmodule 文件组 + FMWK Info.plist）→ create-xcframework → zip）；CI `ios-macos` job 独立 step，超时 15min。
- 首测基线：**533,905 bytes（≈521KB）≤ 2,097,152**（80100bb，CI `ios-macos` job 2026-09-23 首绿）。
- 核查记录：SwiftPM 包无 framework target，`xcodebuild build`/`archive` 产物均非 framework（build 出逐源文件 .o + swiftmodule 文件组，archive 只出 `Products/Users/runner/Objects/*.o`）——必须 libtool 手工组装（c83199b/ec1c422/80100bb 三轮 dump 实锤）。

## ③ demo 宿主 ≤10 行集成 ⏳ 接入面已证，demo 工程待落地

- 接入面本身已达标：`ServifyView(config: try! ServifyConfig(apiUrl: "..."))` 一行完成初始化 + 浮钮拉起（`sdk/ios/README.md` 接入示例）。
- 真实 demo 工程需要 Xcode 项目文件，本仓库 CI/开发环境（Linux）无法生成——**待 macOS 环境落地**（M4 接入文档站阶段随接入样例一起交付）。如实标注：此项未完成，不假装通过。

## ④ Keychain 存储、后台切换、推送注册口 ⏳ 分层如实标注

- **后台切换的可见性语义（自动化锚定 ✅）**：`ServifyChatTests.unreadCountsOnlyWhileSessionHidden`——面板收起期间到达的坐席/AI 消息计入未读、可见即清零；`ServifyView` 展开即 `onSessionVisible` + `historySnapshot` 回放（D7 hide 期间消息不丢）。
- **流中断渲染降级（自动化锚定 ✅）**：`streamInterruptionOnDisconnectFinalizesPartialWithHint`——已渲染部分保留 + 提示行"回答中断，请重试"，提示行不计未读。
- **Keychain 存储 / 推送注册口 ⏳**：M3 面（设计文档 M3 产出），且依赖后端配套项（§10 #2 guest token 端点、#3 未读游标）——V1 冻结面不含，`README.md` 已注明。如实标注：未实现，进 M3 刀计划。
- **真机手工项（同 M1 矩阵）⏳**：前后台往返 30s+ / 飞行模式 ≥ 重连耗尽 / 蜂窝↔WiFi 切换——需真机与真实网络环境，CI 无法模拟。

## 状态机穷举 ✅（与 M1⑤ 同型口径）

- **转人工状态机**：`HandoffStateMachineTests`（XCTest 4，与 Android `StateMachineTest` 同型）。
- **流式三段契约**：`StreamingAssemblerTests`（XCTest 6，含 interruptedStreamIsFlagged）。
- **连接状态机**（§4.4）：`ConnectionLifecycleTests` 6 测试——idle 初始、idle→connecting→connected、重复 connect 幂等、握手失败→disconnected、重连耗尽→disconnected→connect 恢复、服务端主动关闭→reconnecting→connected（1000 闲置踢线边）。
- **支撑件**：`ReconnectPolicyTests` 4（退避序列 + 越界 null + 非法参数拒绝）；`ServifyErrorTests` 2（七码对账 §4.5 + suggestsHandoff 语义锚定）。
- **覆盖率口径**：生产代码行级可覆盖面 100%（820/820，a646e2a）；Darwin 分支（`create` 工厂/`URLSessionWebSocketTransport`）Linux 编译不到，由 `ios-macos` job 的 `CreateFactoryTests` 覆盖（等价 Go 侧 `[no statements]` 豁免口径）。

## 后端配套依赖（不阻塞 M2 收口，进服务端排期）

与 M1 同清单：§10 #1（访客消息增量端点）、#2（guest token 签发端点）、#3（未读游标增强）。SDK 侧 `access_token` 握手参数已就位（`buildWsUrlCarriesAccessTokenOnlyWhenConfigured` 与 Kotlin 同断言，PROTOCOL §1 服务端当前不消费、向后兼容）。
