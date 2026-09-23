# Servify iOS SDK（ServifyKit）

客服会话 SDK（M2 Alpha 进行中）：Swift + SwiftUI（D4）、零三方依赖（D9，由 Package.swift
无 dependencies 声明结构性保证）、XCFramework 分发（≤2MB 门禁）。

## 当前状态（M2 Alpha 完成：刀 1—5 已落地；验收矩阵见 `ACCEPTANCE-M2.md`）

- 协议层：`WireFrame` / `FrameCodec`（Kotlin `shared/protocol` 逐字段镜像，畸形帧降级
  Unknown 语义一致）。
- 会话核心：`SessionCore` / `HandoffStateMachine` / `StreamingAssembler`（Kotlin
  `shared/core` 镜像，转人工转移表与流式三段契约穷举测试同型）。
- 契约回放：`FixtureReplayTests` 消费 `sdk/protocol-fixtures/` 同一样例集（与 core、
  Android 双端同一套断言，M2 验收①）。
- 传输层（刀 2）：`WebSocketTransport` 协议接缝 + `URLSessionWebSocketTransport`
  （Darwin 专属，Linux CI 编译不到、测试注入 mock）；Linux 无 URLSession WS 实现，
  传输必须可注入——生产走 Darwin 实现，测试走 `MockTransport` 族。
- 门面 API（刀 2）：`ServifyChat`（§4 冻结面 async/await 对齐）——惰性 `connect()`
  幂等、`sendMessage` 回显判据（AsyncMutex 串行 + EchoGate 内容匹配，超时发
  sendTimeout）、断线按 `ReconnectPolicy` 退避重连（重连 Task 可取消 + 过期套接字
  身份守卫）、流中断收口（部分内容翻 `isStreaming=false` + 提示行）、未读可见性
  语义、`buildWsUrl` 与 Kotlin 逐字符同构（form 编码：空格→+，&/=→%XX）、`guestToken`
  握手参数。事件流 `EventStream`/`StateStream`（SharedFlow/StateFlow 最小镜像：
  组播无 replay / 订阅先吐当前值，AsyncStream builder 同步注册——订阅后 emit 不丢）。
- 测试：`ReconnectPolicyTests`（4）/ `ServifyChatTests`（13）/ `ConnectionLifecycleTests`
  （6）/ `ServifyErrorTests`（2）与 Kotlin 用例名逐一对应（连接状态机 §4.4 转移表穷举
  同型、错误七码对账 §4.5 同型）。
- 覆盖率口径（本地 `swift test --enable-code-coverage` 行级聚合）：生产代码可覆盖面
  100%（820/820 行）；唯一豁免 `ServifyChat.create` 的 Darwin 分支（`#if canImport(Darwin)`
  在 Linux 编译不到，等价 Go 侧 `[no statements]` 口径）。
- macOS CI（刀 4）：`ios-macos` job 锁 macos-15——Darwin 专属面（`URLSessionWebSocket
  Transport`/`create` 工厂，`CreateFactoryTests` 仅 Darwin 编译）+ XCFramework 打包与
  体积门禁（`scripts/check-ios-sdk-size.sh`，Release + BUILD_LIBRARY_FOR_DISTRIBUTION，
  分发形态 zip 后 ≤2MB，D9/M2 验收②）。
- UI（刀 3，`UI/` 目录）：纯逻辑层无平台条件（`ChatUiState`——toChatListItems 纯映射 /
  `ChatPanelState` reducer / `resolvePanelStyle` 形态选择 / `isRightAligned`，Linux
  单测穷举，Kotlin `ChatUiStateTest`/`ChatPanelStateTest` 镜像）；SwiftUI 层
  `#if canImport(UIKit)`（`ChatPanel` 面板 + `ServifyView` 浮钮入口，`ChatPanelModel`
  事件流 → @Published 薄接线）——§4.2 show/hide 语义（首展 connect、可见清未读、
  收起连接保持未读累计）、D8 浮钮角标/抽屉/全屏、转人工按钮与来源列表同 Kotlin。
  SwiftUI 编译验证由 ios-macos job 的 xcodebuild 承担（Linux/纯 swift test 编译不到）。
- 尚未落地（后续刀）：Keychain/推送注册口（依赖后端配套项，M3 面）。

## 本地开发

```bash
# Swift 6.4 Linux 工具链（swift.org tarball，ubuntu24.04 平台包——26.04 首版 tarball
# 的 Testing.swiftmodule 有打包 bug，勿用）
swift test
```

设计依据：`docs/mobile-sdk-design.md`（D4/D5/D9/D10）、`sdk/PROTOCOL.md`（帧契约唯一事实源）。
