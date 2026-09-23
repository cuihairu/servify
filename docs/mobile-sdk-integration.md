# 移动端 SDK 接入指南（Android / iOS）

双端 SDK 同一套协议与服务端（WS `wss://<host>/api/v1/ws` + 访客工单 `POST /api/v1/tickets`），
API 语义逐一镜像。本页面向接入方；协议细节见 `sdk/PROTOCOL.md`（帧契约）与
[平台规格 §4](/mobile-sdk-platform-spec)（API 冻结面），架构与决策见[设计文档](/mobile-sdk-design)。

## 1. 前置要求

| | Android | iOS |
|---|---|---|
| 系统版本 | minSdk 24+ | iOS 15+ |
| 权限/配置 | 宿主 manifest 已有 `INTERNET`（绝大多数 App 已有） | 无 Info.plist 新增项（不启用推送时）；强制 ATS/WSS |
| 依赖注入 | 零三方（白名单：kotlin-stdlib、kotlinx-coroutines、kotlinx-serialization-json、okhttp、Compose） | 零三方（SwiftUI/Combine 全系统库，Package.swift 无 dependencies 结构性保证） |
| 体积增量 | ≤ 1.5MB（R8 口径） | ≤ 2MB（XCFramework zip 口径） |

服务端侧：`wss://<host>/api/v1/ws` 握手与 `POST /api/v1/tickets` 免认证可达；移动端原生 WS
不发送 Origin 头，若服务端开了 Origin 白名单需放行空 Origin。

## 2. 获取 SDK 产物

当前为仓库内分发（无正式发布渠道前，不内置自动发版）：

- **Android**：CI `android` job 的 AAR 产物，或本地构建：
  `cd sdk/android && ./gradlew :servify-sdk:assembleRelease`（产物 `servify-sdk/build/outputs/aar/`）。
- **iOS**：XCFramework（静态，`BUILD_LIBRARY_FOR_DISTRIBUTION`），CI `ios-macos` job 产物，
  或本地构建：`sdk/scripts/check-ios-sdk-size.sh`（产物 `build/ServifyKit.xcframework`）。
- CocoaPods 不提供（M3 评估结论：SPM/XCFramework 单轨，按需重建触发条件见
  [评估文档](/mobile-sdk-cocoapods-evaluation)）。

引入方式：

```kotlin
// Android：libs/ 放入 AAR
implementation(files("libs/servify-sdk-release.aar"))
```

```swift
// iOS：XCFramework 拖入工程（General → Frameworks, Embed 静态库选 Do Not Embed），
// 或本地路径 SwiftPM 引用（远程 URL 引用需仓库根的 Package.swift，暂不适用——见评估文档）
```

## 3. 最小接入（≤10 行）

### Android

```kotlin
class MainActivity : ComponentActivity() {
    override fun onCreate(savedInstanceState: Bundle?) {
        super.onCreate(savedInstanceState)
        setContentView(R.layout.activity_main) // 宿主自己的业务界面

        val chat = ServifyChat.create(
            this,
            ServifyConfig(apiUrl = "https://your-servify-host"),
        )
        chat.show(this) // 挂浮动按钮 + 首次触发 WS 连接
    }
}
```

- `show(hostActivity)`：浮钮（右下角 56dp，主色取 `branding.primaryColor`）+ 面板；
  `hide()` 收起面板但**连接保持**，期间到达的坐席/AI 消息计入未读（浮钮角标）。
- 宿主 Activity 需为 `ComponentActivity`（面板经 ComposeView 挂载）；未桥接的宿主自动降级：
  浮钮仍显示，面板不挂载。
- AAR 自带 consumer-rules.pro（kotlinx.serialization 的 R8 规则），宿主无需手工配置。

### iOS（SwiftUI）

```swift
struct ContentView: View {
    var body: some View {
        // 浮钮 + 抽屉/全屏面板一行拉起
        ServifyView(config: try! ServifyConfig(apiUrl: "https://your-servify-host"))
    }
}
```

- 需要门面级控制（事件订阅/工单/推送注册）时：`let chat = ServifyChat.create(config:)`
  后用 `ServifyView(chat: chat)` 挂载。
- 收起（点 ✕ / scrim）连接保持；展开即清未读并回放 hide 期间累积的消息。

## 4. 配置参考（`ServifyConfig`）

| 字段 | 类型 | 默认 | 说明 |
|---|---|---|---|
| `apiUrl` | String | 必填 | 服务端地址，须 `https://`/`wss://` 前缀（否则构造期报 `config_invalid`）；无尾斜杠、无路径后缀，SDK 自行拼 WS 与 REST 路径 |
| `guestToken` | String? | null | 访客 token（服务端签发端点为配套待建项，当前握手不消费、向后兼容） |
| `branding` | Branding | 全默认 | 品牌化四件套，见下表 |
| `presentationStyle` | Drawer / FullScreen | Drawer | 面板形态 |
| `loggingEnabled` | Bool | false | 调试日志 |
| `pushTokenProvider` | 闭包 | null/nil | 推送 token 获取闭包（宿主 FCM/APNs 集成面）；null = 推送不启用 |

**Branding 四件套**：`primaryColor`（Int/UInt32，ARGB，默认 `0xFF2563EB` 品牌蓝）、
`welcomeText`（欢迎行）、`agentTitle`（坐席显示名）、`offlineText`（断线系统提示行，null = 不出提示行）。

## 5. 工单创建 / 推送注册 / 离线提示（M3 API）

```kotlin
// Android
val config = ServifyConfig(
    apiUrl = "https://your-servify-host",
    branding = Branding(offlineText = "客服当前不在线，请稍后再来"),
    pushTokenProvider = { fetchFcmToken() },
)

val receipt: TicketReceipt? = chat.createTicket(title = "无法登录", description = "选填补充")
chat.registerPushToken() // 挂起，返回 Boolean
```

```swift
// iOS
let config = try ServifyConfig(
    apiUrl: "https://your-servify-host",
    branding: Branding(offlineText: "客服当前不在线，请稍后再来"),
    pushTokenProvider: { await fetchAPNsToken() }
)

let receipt: TicketReceipt? = await chat.createTicket(title: "无法登录", description: "选填补充")
_ = await chat.registerPushToken()
```

语义：

- **createTicket**：服务端自动组装会话摘要（最近 10 条，`[访客]`/`[AI]`/`[客服]` 标注）随单
  提交，成功返回 `TicketReceipt(ticketId)`，会话内出现系统提示行"工单 #N 已创建…"；
  失败返回 null 且 `error` 流给出错误（IO→`network`、非 2xx/畸形→`ticket_failed`）。也可从
  会话面板标题栏"工单"按钮发起（表单：标题必填 + 描述选填）。
- **registerPushToken**（服务端推送端点上线前的过渡语义）：未配置 provider→`error` 流
  `unsupported`；provider 返回 null（宿主未授权，正常态）→静默 false，**非错误**；取到 token
  →过渡期 `unsupported`（端点落地后仅补上报实现，接入代码不变）。
- **offlineText**：断线（重连耗尽/握手失败）后自动追加系统提示行；用户主动关闭面板不提示。

## 6. 事件流（`chat.events`）

| 流 | 内容 |
|---|---|
| `messages` | 消息（含流式增量标记 `isStreaming`；System 提示行不来自服务端帧） |
| `unreadCount` | 未读数（面板可见期间为 0） |
| `connectionState` | idle / connecting / connected / reconnecting(n) / disconnected |
| `agentAssigned` | 坐席接入 |
| `waitingInQueue` | 排队提示 |
| `error` | 错误事件（码表见下） |

Android 为 Kotlin Flow（`collect`），iOS 为 `EventStream`/`StateStream`（`for await`）。
订阅即时生效：订阅前发生的历史经 `historySnapshot()` 获取（内存级会话连续性，面板
hide 期间消息不丢）。

## 7. 错误码

`network` / `send_timeout` / `echo_timeout` / `ws_closed` / `protocol_invalid` /
`ticket_failed` / `unsupported` / `config_invalid`（构造期抛出）。详细语义见
[平台规格 §4.5](/mobile-sdk-platform-spec)。

## 8. 接入自验清单

1. `wss://<host>/api/v1/ws?session_id=test` 握手 101；
2. `apiUrl` 无尾斜杠、无路径后缀；
3. 混淆构建（Android R8）/ 裁剪构建（iOS）通过——无需接入方额外配置；
4. 退后台 → 回前台：会话页状态与未读计数无丢失；
5. 工单创建后管理面 tickets 列表可见（含 AI 摘要列）；
6. 体积对账：AAR ≤ 1.5MB / XCFramework ≤ 2MB。

常见问题（握手 400/403、反复断线、无回显）对照表见
[平台规格 §5.4](/mobile-sdk-platform-spec)。

## 9. V1 边界（明确不做的，防止接入误期许）

离线发送队列、消息撤回/编辑、富媒体消息（图片/文件）、坐席移动端、多语言内置
（V1 中文，Branding 预留 i18n 钩子）。断连期间的客户消息不自动补发（服务端无历史
重发端点，§10 #1 配套待建项）；Keychain 凭证存储不在 V1（匿名 session 无凭证）。
