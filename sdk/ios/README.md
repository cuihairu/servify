# Servify iOS SDK（ServifyKit）

客服会话 SDK（M2 Alpha 进行中）：Swift + SwiftUI（D4）、零三方依赖（D9，由 Package.swift
无 dependencies 声明结构性保证）、XCFramework 分发（≤2MB 门禁）。

## 当前状态（M2 刀 2）

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
  在 Linux 编译不到，等价 Go 侧 `[no statements]` 口径，macOS CI 刀落地后自然覆盖）。
- 尚未落地（后续刀）：SwiftUI 会话面板与浮钮（show/hide 接线）、XCFramework 打包与
  体积门禁（macOS CI）、Keychain/推送注册口（依赖后端配套项）。

## 本地开发

```bash
# Swift 6.4 Linux 工具链（swift.org tarball，ubuntu24.04 平台包——26.04 首版 tarball
# 的 Testing.swiftmodule 有打包 bug，勿用）
swift test
```

设计依据：`docs/mobile-sdk-design.md`（D4/D5/D9/D10）、`sdk/PROTOCOL.md`（帧契约唯一事实源）。
