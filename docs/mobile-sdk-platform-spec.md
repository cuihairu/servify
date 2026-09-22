# 移动端 SDK 平台规格：Android/iOS 选型工程化与核心 API 表面

状态：开工技术方案（不含实现）。[mobile-sdk-design.md](/mobile-sdk-design) 回答"做什么、为什么、按什么顺序"；本文回答"具体长什么形状"——分层架构、两平台选型的工程化细节、V1 冻结的 API 表面、接入流程的可执行细化。实现代码不在本文范围，但本文是 M0/M1/M2 开工时的对照基线：代码评审按本文逐条验收，偏离需先改本文。

协议契约见 [sdk/PROTOCOL.md](../sdk/PROTOCOL.md)（帧级事实源）；本文不重复帧定义，只引用。

---

## 1. 分层架构（两平台同构）

```
┌─ L2 UI 层 ────────────────────────────┐
│ Compose 会话页/抽屉/浮钮 (Android)      │
│ SwiftUI 会话页/抽屉/浮钮 (iOS)          │
└──────────────┬───────────────────────┘
               │ 只读状态（StateFlow / AsyncStream）
┌─ L1 会话核心（headless）──────────────┐
│ 状态机 · 消息仓库 · 未读计数 · 补拉编排  │
│ 快照持久化 · 推送注册编排 · 事件分发     │
└──────────────┬───────────────────────┘
               │ 发送帧 / 接收帧
┌─ L0 协议层 ───────────────────────────┐
│ WS 传输 · 帧编解码 · 保活 · 重连退避    │
│ （契约唯一事实源：sdk/PROTOCOL.md）      │
└──────────────────────────────────────┘
```

**依赖方向单向：L2 → L1 → L0**。宿主只见 `ServifyChat` 门面（L1+L2 组装），三层都 `internal`，发布面只有门面与配置/事件模型。

**为什么核心必须 headless（无 UI 可独立运行）**：M0 联调探针要在没有 UI 的情况下跑通 WS 收发与状态机回放；契约回放测试要直接断言 L1 状态转移而非 UI 快照；RN 绑定（react-native 包已在仓）要复用会话核心而不要 Compose/SwiftUI。一个 headless 核心同时服务探针、测试、RN、原生 UI 四个消费者——这是"不复制 Web SDK 结构"（07 号 M3）的正面落点：Web 的结构是"协议+UI 一体的 client 类"，移动端拆成三层。

## 2. Android 选型工程化（D3 细化）

| 项 | 决策 | 为什么 |
|---|---|---|
| 语言/UI | Kotlin + Jetpack Compose（D3 已定） | 状态驱动渲染与会话页同构 |
| Kotlin/工具链 | Kotlin 2.x + JDK 17 构建、compileSdk 35 | Compose 2.x 要求；compileSdk 取当前 stable 供 SDK 编译，运行时行为仍由 minSdk 24 兜底（SDK 编译目标高不抬高接入方运行要求） |
| 交付形态 | 单 AAR、内部多包（`protocol`/`core`/`ui`），`api` 面只有门面 | 多 AAR 会把内部分层泄漏成接入方的依赖管理负担；单 AAR + internal 分层让重构自由保留 |
| 并发模型 | 门面持有应用级 `SupervisorJob` + 内部 `CoroutineScope`；状态暴露 `StateFlow`、事件流 `SharedFlow`；公开挂起函数 | Supervisor 隔离单点失败不拖垮作用域；StateFlow 给 UI 收敛重放语义；接入方不接触任何协程作用域创建 |
| 持久化 | 快照（sessionId/lastMessageId/savedAt）走 DataStore Preferences；访客 token 走 EncryptedSharedPreferences | 快照无敏感字段用轻量存储即可；token 必须进系统加密存储（D6 安全基线） |
| 网络 | OkHttp 4.x（`api` 依赖，随宿主去重，D9 白名单） | WSS + 协议层保活 `pingInterval` 一处配置（PROTOCOL.md §2 移动端口径） |
| R8 | 附 consumer proguard rules：保留消息模型（kotlinx-serialization 反射点）与门面入口 | 接入方开混淆即用，不要求手写规则 |
| 宿主互操作 | View 体系宿主经 `ComposeView` 承载会话页；Compose 宿主直接调用 | D3 已论证不要求宿主用 Compose |

**CI 与发布边界**：CI 构建矩阵做 compile/lint/契约回放/AAR 体积门禁（≤1.5MB，D9）；**产物发布（Maven Central/直发 AAR）由维护者手动执行，仓库与 CI 不内置任何自动发版步骤**——与 M4 验收口径一致，也是全程发版禁区的延伸。

## 3. iOS 选型工程化（D4 细化）

| 项 | 决策 | 为什么 |
|---|---|---|
| 语言/UI | Swift + SwiftUI（D4 已定），Swift tools 5.9 | 与 Android 同构的状态驱动；5.9 是 Xcode 15 起的 stable tools 线 |
| 交付形态 | XCFramework + SPM binary target（D4 已定）；源码工程用 SPM 多 target（`ServifyProtocol`/`ServifyCore`/`ServifyUI`），打包时合并 | 同 Android：发布面收敛为一个产物，源码期分层保重构自由 |
| 并发模型 | structured concurrency；状态类 `@MainActor`；事件暴露 `AsyncStream`；公开方法 `async` | 与 Kotlin 侧 StateFlow/挂起函数一一映射（D6 跨端同名同义）；`@MainActor` 收敛 UI 状态写竞争 |
| 持久化 | token 走 Keychain（kSecClassGenericPassword）；快照走 Application Support 下 JSON 文件（无敏感字段） | 对齐 D6 安全基线；避免为快照引入 Keychain 复杂度 |
| 网络 | `URLSessionWebSocketTask` + `sendPing` 保活（PROTOCOL.md §2） | 零三方依赖（D4/D9） |
| 隐私 | `PrivacyInfo.xcprivacy` 声明"不采集数据、无追踪"；所需系统权限只有网络（ATS 强制 WSS，不提供明文例外） | App Store 审核一次通过；ATS 例外口子不开是安全姿态也是接入方少填一项 |
| 宿主互操作 | UIKit 宿主经 `UIHostingController` 承载；sheet 展示会话页（iOS 15 无 `presentationDetents`，自定义高度，D4 已论证） | 双宿主体系都能接 |

## 4. 核心 API 表面规格（V1 冻结面）

两平台同名同义（D6）；差异仅语言习惯。**V1 冻结的含义**：本节字段与方法在 M1-M3 只增不改语义；删除或改语义视为 breaking，须过 surface 评审（对齐 sdk/SURFACE_GOVERNANCE 的精神延伸到原生面）。

### 4.1 配置对象 `ServifyConfig`

| 字段 | 类型 | 必填 | 默认 | 行为 |
|---|---|---|---|---|
| `apiUrl` | String | 是 | — | 服务端根地址；强制 https/wss，http 值构造期直接报错（错误模型 `config_invalid`） |
| `guestToken` | String? | 否 | null | 访客 token（D6）；null = 匿名 session 模式（过渡形态，PROTOCOL.md §1）；仅在内存 + 安全存储 |
| `branding.title` | String | 否 | "在线客服" | 会话页标题 |
| `branding.primaryColor` | Int/Color | 否 | 平台默认主色 | 主题色（按钮、气泡强调） |
| `branding.welcomeText` | String | 否 | 平台默认欢迎语 | 会话页首条 system 提示 |
| `presentationStyle` | Enum `{drawer, fullscreen}` | 否 | `drawer` | 抽屉/全屏；小屏自动升级全屏（D8） |
| `pushTokenProvider` | `(async) -> String?` | 否 | null | M3 推送：宿主提供 FCM/APNs token 获取闭包则启用推送模块（D7 可选性） |
| `loggingEnabled` | Bool | 否 | false | debug 日志开关；**关闭态不打印消息正文与 token**（D6 脱敏基线） |

### 4.2 门面方法（`ServifyChat`）

| 方法 | 语义 | 备注 |
|---|---|---|
| `create(context?, config)` | 构造门面：载入 SessionSnapshot 快照，**不建连接** | 惰性连接（策划 §5）；重复 create 由接入方负责避免（单例语义，文档明示） |
| `show(hostActivity / presentingVC)` | 拉起会话 UI（抽屉或全屏）；首次调用触发 WS 连接 | UI 参数是宿主当前前台上下文，SDK 不自行推断 |
| `hide()` | 收起会话 UI，连接保持 | 前后台切换由 SDK 内部处理（策划 §5 时序 4），接入方无需挂钩 |
| `sendMessage(text)` | 发送 `text-message`；成功判据=收到自己回显帧（PROTOCOL.md §6.3），超时本地标记失败 | V1 仅 text；挂起函数 / async |
| `createTicket(subject, aiSummaryIncluded)` | 升级为工单（M3）；依赖访客工单端点（策划 §10 #4） | 端点未上线的过渡期行为：返回 `unsupported` 错误码而非静默成功 |
| `registerPushToken()`（M3） | 调 `pushTokenProvider` 取 token → 按 app-core `PushTokenRegistration` 形状上报 | `pushTokenProvider` 未配置时调用返回 `unsupported` |
| `destroy()` | 断连接、取消作用域、清订阅；快照保留（下次 create 恢复） | 宿主真正退出时调用；普通退后台**不要**调用 |

### 4.3 事件流（与 core `ServifyEventMap` 对齐命名）

| 事件 | 载荷 | 来源 |
|---|---|---|
| `messages` | `ConversationMessage`（策划 D5 消息模型） | `text-message` 回显（去重后）/ `agent-message` / `ai-response` / 流式增量（带 `isStreaming` 标记，UI 据此渲染拼接中的气泡） |
| `unreadCount` | Int | L1 推导：agent/system 来源且本地未渲染过累加；会话页可见时清零 |
| `connectionState` | Enum | 见 4.4 |
| `reconnecting` | attempt: Int | 对齐 core 同名事件 |
| `agentAssigned` | `{agentId: Long, message: String}` | `transfer_notification`（PROTOCOL.md §4.2） |
| `waitingInQueue` | `{message: String}` | `waiting_notification`；与 `agentAssigned` 共同驱动状态机 |
| `error` | `ServifyError`（4.5） | 对齐 core 同名事件 |
| `ticketCreated` / `ticketUpdated`（M3） | Ticket | 对齐 core 同名事件 |

**显式排除**：`agent_typing`（core 有、服务端零发射——PROTOCOL.md §5 死分支，移动端不设此事件）；webrtc 系全部。

### 4.4 连接状态机

```
idle ──(首次 show)──> connecting ──> connected ──(断)──> reconnecting(n/5)──> connected
                                       │                        └─(5 次耗尽)──> disconnected
                                       └─(握手失败)──────────────> disconnected
disconnected ──(用户再次打开会话页)──> connecting
```

参数沿用 core 默认：指数退避 1s 起 2 倍封顶 30s、5 次（源码核实，`contracts/reconnect.ts`）。后台切换不产生 disconnected——内部断开 + 回前台自动恢复，对外表现"连接保持"（避免宿主 UI 无谓闪烁）。

### 4.5 错误模型

两平台呈现：Kotlin `sealed class ServifyError` / Swift `enum ServifyError: Error`，统一携带 `{code, message, retryable}`：

| code | 场景 | retryable |
|---|---|---|
| `config_invalid` | apiUrl 非 https、必填缺失 | false |
| `network` | WS 建连/发送失败、DNS | true |
| `server_unavailable` | 握手 5xx | true |
| `handshake_rejected` | 400 session_id 缺失 / Origin 白名单拒绝 | false（配置问题） |
| `send_timeout` | 发送后超时未见回显 | true |
| `unsupported` | 调用 M3 能力但配套未启用/端点未上线 | false |
| `ticket_failed` | M3 工单创建失败 | true |

### 4.6 app-core 契约映射（已预留四契约，落地对应关系）

| app-core 契约 | 移动端落点 |
|---|---|
| `SessionSnapshot {sessionId, lastMessageId?, savedAt}` | create 时载入 / 断连与退后台时落盘（Kotlin DataStore / iOS JSON 文件） |
| `PushTokenRegistration {token, platform, deviceId, environment?}` | `registerPushToken` 的上报载荷形状（M3） |
| `MobileStorageAdapter {getItem/setItem/removeItem}` | 存储注入点：默认实现（DataStore/文件），测试与特殊宿主可替换——保持接口同形，替换注入 |
| `AppReconnectPolicy` | 4.4 重连参数的类型化载体，默认值=core 默认，允许宿主覆盖（覆盖不进 V1 冻结面，标注 experimental） |

## 5. 接入流程细化（M1 验收"≤10 行"的可执行版）

### 5.1 前置条件

- Android：宿主 manifest 已有 `INTERNET` 权限（绝大多数 App 已有）；minSdk ≥ 24 的宿主直接依赖，低于 24 拒绝依赖解析（这是覆盖率决策的代价面，接入文档首行声明）。
- iOS：宿主 iOS 15+；无 Info.plist 新增项（无推送时）；强制 ATS/WSS。

### 5.2 最小接入（8 行级，对应 4.2/4.3 API）

Android：

```kotlin
val servify = ServifyChat.create(context, ServifyConfig(apiUrl = "https://support.example.com"))
lifecycleScope.launch {
    servify.events.messages.collect { render(it) }        // 消息（含流式增量标记）
}
servify.events.unreadCount.observe { badge.count = it }   // 未读（自绘入口时）
servify.show(this)                                         // 首次调用触发连接
```

iOS：

```swift
let servify = ServifyChat.create(config: ServifyConfig(apiUrl: "https://support.example.com"))
Task { for await msg in servify.events.messages { render(msg) } }
Task { for await n in servify.events.unreadCount { badge.count = n } }
servify.show(from: self)
```

### 5.3 集成检查清单（接入方自验）

1. WSS 可达：`wss://<host>/api/v1/ws?session_id=test` 握手 101；
2. `apiUrl` 无尾斜杠、无路径后缀（SDK 自行拼 WS 与未来 REST 路径）；
3. 混淆构建（Android R8）/ 裁剪构建（iOS）通过——SDK 自带 consumer rules，接入方无需额外配置；
4. 退后台 → 回前台：会话页状态与未读计数无丢失（SDK 内部处理）；
5. 体积对账：AAR ≤ 1.5MB / XCFramework ≤ 2MB（D9 预算，接入文档给出生成方法）。

### 5.4 常见接入错误对照

| 现象 | 原因 | 处置 |
|---|---|---|
| 握手 400 `BadRequest` | 未带 session_id / apiUrl 拼错 | 检查 apiUrl 与 guest token 流程 |
| 握手 403 | Origin 白名单拒绝（移动端原生 WS 无 Origin 头，服务端白名单需放行空 Origin——**服务端配置项，接入文档明示**） | 白名单加条目或空配置放行 |
| 连接反复断开（1006 类） | 代理/防火墙掐 WSS 长连 | 检查企业网络；SDK 重连退避自动恢复 |
| 消息发出无回显 | PROTOCOL.md §6.3 无 ACK——按 send_timeout 标记 | UI 提示重发；勿自动重发（策划 D7 事故边界） |

## 6. 与里程碑的衔接

| 本文段落 | 服务里程碑 |
|---|---|
| §1 分层 / §2 Android 工程 / §3 iOS 工程 | M0 探针按 L1 headless 核心口径实现；M1/M2 工程脚手架按表落地 |
| §4 API 表面 | M1 冻结 Android 面；M2 iOS 面逐条对照（同名同义）；M3 增量只动 M3 标注项 |
| §5 接入流程 | M1 验收第②条（≤10 行集成）的直接对照物；接入文档站（M4）以此为本 |

本文与 PROTOCOL.md 同属"契约先行"资产：实现阶段的偏离一律先改文档再改代码，评审以文档为基线。
