# Servify iOS SDK（ServifyKit）

客服会话 SDK（M2 Alpha 进行中）：Swift + SwiftUI（D4）、零三方依赖（D9，由 Package.swift
无 dependencies 声明结构性保证）、XCFramework 分发（≤2MB 门禁）。

## 当前状态（M2 刀 1）

- 协议层：`WireFrame` / `FrameCodec`（Kotlin `shared/protocol` 逐字段镜像，畸形帧降级
  Unknown 语义一致）。
- 会话核心：`SessionCore` / `HandoffStateMachine` / `StreamingAssembler`（Kotlin
  `shared/core` 镜像，转人工转移表与流式三段契约穷举测试同型）。
- 契约回放：`FixtureReplayTests` 消费 `sdk/protocol-fixtures/` 同一样例集（与 core、
  Android 双端同一套断言，M2 验收①）。
- 尚未落地（后续刀）：WS 传输与门面 API（async/await，§4 冻结面）、SwiftUI 会话面板、
  XCFramework 打包与体积门禁（macOS CI）、Keychain/推送注册口（依赖后端配套项）。

## 本地开发

```bash
# Swift 6.4 Linux 工具链（swift.org tarball，ubuntu24.04 平台包——26.04 首版 tarball
# 的 Testing.swiftmodule 有打包 bug，勿用）
swift test
```

设计依据：`docs/mobile-sdk-design.md`（D4/D5/D9/D10）、`sdk/PROTOCOL.md`（帧契约唯一事实源）。
