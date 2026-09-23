# 移动端 SDK 策划：Android 与 iOS 嵌入式客服能力

状态：策划文档（不含实现）。本文回答"做什么、为什么这样做、按什么顺序做"，实现细节留给各阶段开工时的技术方案。

关联文档：

- [07 SDK Multi Surface](/implementation/07-sdk-multi-surface) —— 本文档是其 M3 `app-sdk-reservation` 的落地策划；app-core 已预留的四个契约（offline queue / push token registration / reconnect + session restore / mobile storage）是本文的地基，不再重复发明。
- [v1 产品范围](/v1-product-scope) —— AI 首答、转人工、工单闭环的产品契约。
- Web SDK 源码：`sdk/packages/core`（协议事实标准）、`apps/demo-sdk/widget.js`（最小接入形态参照）。

---

## 1. 产品定位与边界

### 决策 D1：做嵌入式 SDK，不做独立客服 App

**结论**：交付物是两个可嵌入宿主 App 的客户端 SDK（Android AAR / iOS XCFramework），宿主在自己的页面里拉起客服会话；不发布独立的"Servify 客服"App。

**为什么**：

- 客服会话的价值锚点是"用户在业务流程里遇到问题的那一刻"。独立 App 意味着用户要离开宿主业务场景，上下文（订单页、报错页）断裂，转化率必然差于原地拉起。行业先例（Intercom、Zendesk、美洽）全部是 SDK 形态，独立客服 App 没有存活样本。
- 独立 App 需要账号体系、应用市场审核、独立发版节奏三件套，每一件都是持续成本；SDK 的发版成本收敛为"接入方升级依赖"，与 servify 现有"严禁发版操作、接入方自取"的交付模式一致。
- V1 产品范围把"品牌基础配置（名称、欢迎语、主题色、离线提示）"列为渠道要求——嵌入式 SDK 天然由宿主传入品牌配置，独立 App 则要自建配置后台，V1 承担不起。

**边界（V1 明确不做）**：

- 不做坐席端移动 App（坐席工作台保持 Web）；
- 不做语音/视频（后端 WebRTC 能力先服务远程协助，移动端 V1 不暴露，`createMobileCapabilitySet` 已将 voice/remote_assist 置 off，保持一致）;
- 不做宿主 App 内的消息转发到系统短信/邮件等系统级集成。

### 决策 D2：与 Web SDK 能力对齐——协议对齐是硬约束，访客可达面是真实边界

Web 端当前真实具备、移动端 V1 必须对齐的能力（以 `sdk/packages/core` 源码与服务端路由注册为准，不以上线宣传为准）：

| 能力 | Web 现状 | 移动端 V1 |
|---|---|---|
| 会话窗口 | 浮钮 + 底部面板（widget 自绘，绕过 SDK） | 浮钮 + 底部抽屉 + 全屏会话页 |
| 消息收发 | WS `text-message`（客户上行）/ `agent-message`（坐席）/ `ai-response` + `ai-response-delta`（AI 首答，流式增量 + 终帧） | 同协议，消息模型逐字段对齐 |
| WebSocket 链路 | `/api/v1/ws?session_id=`（publicV1 组免认证；`access_token` 参数服务端**不消费**——握手仅要求 session_id 非空，Origin 白名单是唯一门槛），心跳 30s，指数退避重连（5 次，1s 起 2 倍，封顶 30s） | 同路径；重连策略叠加前后台切换与网络可达性感知；鉴权缺口见 D6 访客 token 方案 |
| 未读与新消息通知 | **无任何实现**（无计数、无角标） | 客户端内未读计数为基线；系统推送为可选模块（D7） |
| AI 首答与引用 | WS `ai-response` 帧一等携带 `{content, confidence, source}` + 编排附加输出 `sources`/`strategy`/`next_action`/`handoff_reason`（零值省略）；流式经 `ai-response-delta` 增量帧（2026-09 服务端已接出） | 增量拼接 + 完成帧替换渲染；引用来源可展开列表；置信门建议（next_action=handoff）透出"转人工"提示 |
| 转人工状态 | `transfer_notification`（含 message/agent_id）/ `waiting_notification`（入队）两帧；widget 未特判，走"当 bot 文本渲染"的兜底分支。**注意：core SDK 声明并处理的 `session_update`/`agent_status` 帧服务端从不发送（死分支）** | 状态机改由真实帧驱动：ai_answering →（transfer/waiting_notification）→ waiting_human →（agent-message 到达）→ agent_chatting（见 D5 状态机） |
| 工单创建入口 | REST `POST /api/tickets`（api-client 有封装，widget 未露出）——**管理面端点，访客不可达** | 会话页内"升级为工单"入口，带 AI 摘要预填；依赖访客可用端点（后端配套项，见 §10） |

**REST 面的访客可达性（策划时的关键事实核查）**：服务端现存 REST 端点几乎全部挂在管理面中间件链下（`AuthMiddleware` + `RequirePrincipalKinds("agent","admin","service")` + 资源权限）——包括本文曾计划依赖的 `/api/v1/ai/query`、`/api/tickets`、`/api/omni/sessions/:id/messages`。**访客客户端唯一现成通道就是 `/api/v1/ws`**。这直接约束了架构：移动端 V1 的实时收发、AI 首答（含流式）全部走 WS；"增量补拉""工单创建"两类 REST 依赖必须先有访客可用的服务端配套（§10 清单 #1/#4，推送注册同理见 #5），不能按管理面端点的参数形状直接假设可用。

**为什么逐字段对齐而不是"移动端重新设计消息模型"**：07 号计划 M3 验收语明确"后续移动端 SDK 不会直接复制 Web SDK 结构"——这句话约束的是**代码结构**（不把 JS 的 EventEmitter/SDK 类层级照搬到原生），而不是**协议契约**。跨端协议不一致的代价是服务端要维护两套广播分支，消息历史的可移植性（换端续聊）也会断裂。因此：协议与消息模型对齐是硬约束，运行时结构按各平台惯例重构。对齐基线里的死分支（`session_update`/`agent_status`）不进移动端契约——对着不存在的服务端行为做兼容，只会把幻影帧固化成三端负担；它们作为 core SDK 的历史遗留另行清理。

---

## 2. Android 技术选型

### 决策 D3：Kotlin + Jetpack Compose，minSdk 24

**语言与 UI**：Kotlin（官方主推语言，协程天然适配 WS 长连接与异步消息流）+ Jetpack Compose（声明式 UI 与客服这种"状态驱动渲染"的消息流高度契合：`messages: List<Message>` 一个状态源驱动整个会话页，无需命令式 diff）。

**为什么不用 XML View 体系**：客服会话页的核心交互是"无限滚动消息列表 + 底部输入栏 + 键盘协同"，View 体系下这三件的组合（RecyclerView + WindowInsets + 输入法高度动画）是出名的碎片化重灾区；Compose 的 `LazyColumn` + `imePadding()` 把这三件事收敛为声明式布局，且与宿主 App 是否使用 Compose 无关（SDK 内部自洽，宿主互操作经 `ComposeView` 即可）。

**minSdk 论证（为什么是 24 而不是 21 或 26）**：

- **21（Android 5.0）被否**：Compose 最低支持 21，但 21-23 设备占比已跌到统计噪声级（Android Studio 新建项目模板默认 minSdk 24）；更关键的是 TLS 与网络安全：API 20 以下不支持 TLS 1.2 默认开启，21-23 上部分厂商 ROM 的 TLS/证书链实现有已知缺陷，客服链路携带访客 token，不值得为千分位设备背安全调试成本。
- **26（Android 8.0）被否**：虽然 26+ 有通知渠道（NotificationChannel）等便利，但会砍掉约 5-8% 在网设备（存量低端机在新兴市场仍多），而通知渠道的兼容写法只是一层 `if (Build.VERSION.SDK_INT >= 26)` 的薄封装，不足以换取覆盖率损失。
- **24（Android 7.0）胜出**：覆盖约 97%+ 在网设备；`Java 8` API 全可用（okHttp 4.x 要求 21+，kotlinx.serialization 无版本坎）；Jetpack 全家桶（Lifecycle、Security、DataStore）最低 21-24 区间全覆盖。这是覆盖率与维护成本的最优交点，也与行业 SDK 惯例（Intercom minSdk 24 级别）一致。

**为什么不用跨端框架作为主路径**：

- **Flutter**：引入 Flutter 引擎基座（约 4-6MB 起步）只为一个客服会话页，包体积惩罚不可接受（见 D9 预算）；且 Flutter 与宿主原生页面的混合栈（手势、键盘、深链）是长期摩擦源。
- **Kotlin Multiplatform**：技术上有吸引力，但 KMP 的 UI 仍需 Compose Multiplatform（iOS 侧尚在演进期）或双端各自写 UI，"共享"收益主要在协议层——而协议层用"共享契约 + 各端代码生成/单测互验"（D5）就能拿到 80% 收益，不必承担 KMP 工具链对接入方构建环境（Gradle 插件版本、Xcode 配置）的传染。
- **React Native（已有 `@servify/react-native` 绑定）**：定位为 RN 宿主的可选快捷路径（headless 绑定 + app-core 契约已就绪），但 RN 宿主在接入方里是少数派，不作为主推形态。本文档规划的原生 SDK 是默认路径，RN 绑定继续独立演进。

**依赖白名单（Android）**：kotlin-stdlib、kotlinx-coroutines、kotlinx-serialization-json、okHttp（`api` 依赖，宿主大概率已有，gradle 会去重）、Compose Runtime/UI/Foundation（`compileOnly` 语义按宿主版本对齐的方案在 M1 定稿）。禁入：任何 DI 框架（Hilt/Koin——SDK 规模不需要，手工注入即可）、任何图片加载库（V1 消息类型只有 text，附件为 P2）。

---

## 3. iOS 技术选型

### 决策 D4：Swift + SwiftUI，最低 iOS 15

**为什么 SwiftUI 而不是 UIKit**：与 Android 侧同理，消息流是状态驱动渲染，SwiftUI 的 `List`/`ScrollViewReader` + `ObservableObject` 恰好是一一映射；UIKit 的 `UICollectionView` 组合键盘协同、自动高度 cell 的胶水代码量大且难测。风险控制：SDK 的 UI 面很窄（一个会话页 + 浮钮），SwiftUI 在这个复杂度区间已足够稳。

**最低系统版本论证（为什么是 15 而不是 16 或 17）**：

- **iOS 17 被否**：砍掉过多存量设备（iOS 16 发布两年后 15 系仍有可观保有量，客服 SDK 的接入方——电商、本地服务——恰是旧设备占比高的品类）。
- **iOS 16 被否的理由与保留的诱惑**：16 带来 `NavigationStack` 和 `presentationDetents`（半屏抽屉），对会话页很诱人；但两者都有 15 上的替代写法（`NavigationView` + 编程式导航；自定义 `sheet` 高度），放弃的只是 API 优雅度。客服是"低频但关键"功能，接入方对覆盖率极敏感——为导航 API 的优雅度放弃 2-3% 设备，ROI 不成立。
- **iOS 15 胜出**：覆盖率约 98%；SwiftUI 自 iOS 14 起 `List`/`sheet`/状态管理已可用，15 补齐了 `AsyncImage`、`task` 修饰符、`refreshable` 等稳定化改进；`URLSessionWebSocketTask`（iOS 13+）满足 WS 需求。未来若需 16 独占 API（如富媒体预览），提升 min 版本是一行部署目标修改，成本可逆。

**为什么不用第三方网络/WS 库（Starscream 等）**：`URLSessionWebSocketTask` 原生覆盖连接、收发、ping（`sendPing`）能力，且随系统自动获得 TLS 与代理配置的正确行为；第三方库引入 1-2 个依赖传递与 App Store 隐私清单负担，无对应收益。

**分发形态**：XCFramework + Swift Package Manager 二进制分发为主（接入方零 CocoaPods 配置），CocoaPods spec 作为兼容层维护到 M3 评估去留（✅ 已评估，2026-09-23：SPM-only 单轨维持，零已知 CocoaPods-only 接入方不建 spec，按需重建触发条件见 `docs/mobile-sdk-cocoapods-evaluation.md`）。禁入依赖（iOS）：任何三方网络/序列化/DI 库，序列化用 `Codable`。

---

## 4. 共享设计：协议与消息模型跨端一致性

### 决策 D5：单一协议事实标准（后端 WS 契约）+ 各端原生实现 + 契约互验测试

**结构（不共享代码，共享契约）**：

1. **协议事实标准**：后端 `apps/server` 的 WS 契约（`/api/v1/ws`，访客唯一现成通道）+ 访客可达 REST 契约（当前为零——`/api/omni/*`、`/api/v1/ai/query`、`/api/tickets` 均为管理面端点，访客配套端点见 §10 清单）是唯一事实源。
2. **契约文档化**：`sdk/PROTOCOL.md` 首版已入库（2026-09）——逐帧列明消息类型、载荷字段、方向与边界语义，全部条目经服务端源码核实并带文件行号锚点。事实核查的关键产出已固化进契约：core 类型声明的六个死分支帧（`session_update`/`agent_status`/`typing`/`message`/`error`/`system`）单独立表声明"移动端契约不含"；心跳双向机制澄清（服务端协议层 Ping 54s，Web core 的 JSON `system/ping` 帧服务端不处理——移动端走平台原生协议层保活）；慢客户端 256 帧缓冲踢线、发送成功判据=收到自己回显等边界语义成文。命名混用（`transfer_notification`/`waiting_notification` 用 snake_case、`text-message`/`ai-response`/`ai-response-delta`/`agent-message` 用 kebab-case）**在契约里冻结现状、不借机改名**——改名是服务端 breaking change，V1 不做。
3. **互验测试**：Android/iOS 各建一组"契约回放测试"——用同一组 JSON 样例（放 `sdk/protocol-fixtures/`，与 core 测试共用）驱动反序列化与状态机断言。服务端 WS 契约变更时，改 fixtures 会让三端测试同时红，这比"人记得三处都改"可靠。

**消息模型（跨端一致的规范形）**，逐字段对齐 core `Message`（`types.ts:72-83`）：

```
ConversationMessage {
  id: String                 // 服务端消息 id；WS 事件缺省时用 "ws-" + 本地序号回退（对齐 core normalizeMessage 行为）
  session_id: String
  sender_type: Enum { customer, agent, system }
  content: String            // 取值顺序 data.content ?? data.message ?? 原始字符串（对齐 normalizeMessage）
  message_type: Enum { text, image, file, system }   // V1 仅产生/消费 text 与 system
  is_ai_response: Bool       // ai-response 事件置 true
  metadata: Map<String, Any> // WS 事件的原始 data 整体入此（sources/confidence/strategy 由此透出）
  created_at: Instant
}
```

**会话状态机（转人工链路的核心跨端契约）**——转移事件全部使用服务端真实发送的帧（D2 事实核查结论）：

```
ai_answering ──(transfer_notification：已分配坐席)──> agent_chatting
ai_answering ──(waiting_notification：入等待队列)──> waiting_human
waiting_human ──(transfer_notification：队列派发)──> agent_chatting
agent_chatting ──(system 帧或增量补拉发现会话 closed)──> closed
任意状态 ──(WS 断连)──> reconnecting ──(恢复)──> 原状态 + 增量补拉（D7）
```

**为什么状态机显式成契约**：Web 端转人工状态散落在 widget 的 if/else 里（无状态机），导致"转人工后 AI 抢答"需要服务端 `HasActiveHumanAgent` 兜底。移动端把状态机做成一等契约后，同一份状态转移表可在 Android/iOS 互验测试中回放，服务端兜底逻辑退化为保险丝而非正确性来源。

**SSE 的角色**：服务端已接出 WS 流式首答——AI 编排层 `HandleStream` 与非流式共用同一条流水线，WS 侧在 `text-message` 链路上以 `ai-response-delta` 增量帧（`{content_delta, done}`，`message_id` 预留）推送增量，完成时补终末 delta（`done=true`）再发完整 `ai-response` 终帧（增量拼接 + 完成帧替换），帧契约与本文档预留位一致；流式能力缺失或流启动即失败时服务端自动回退单发 `ai-response`，移动端 V1 可直接按该契约渲染。这样协议先行、实现跟进，三端不会在服务端接出流式时各自发明帧格式。

---

## 5. 核心 API 与初始化接入流程

### 决策 D6：API 形态对齐 Web SDK 的心智模型（config → connect → chat → events），鉴权默认访客 token

两平台 API 表面刻意保持同名同义，降低文档与接入心智的跨端成本；差异仅限语言习惯（Kotlin 协程挂起函数 / Swift async-await）。

**Android（Kotlin，伪代码）**：

```kotlin
// 1. 最简接入（访客模式）
val servify = ServifyChat.create(
    context = context,
    config = ServifyConfig(
        apiUrl = "https://support.example.com",
        guestToken = hostProvidedGuestToken,      // 见 D6 鉴权；为空则走匿名会话（兼容现状）
        branding = Branding(title = "帮助中心", primaryColor = 0xFF667EEA.toInt()),
    ),
)

// 2. 事件订阅（StateFlow 暴露，UI 层 collect）
scope.launch {
    servify.events.unreadCount.collect { badgeView.count = it }
}
servify.events.messages.onEach { render(it) }.launchIn(scope)

// 3. 拉起会话（底部抽屉；activity 为宿主任意前台 Activity）
servify.show(activity)

// 4. 发送 / 工单
servify.sendMessage("我的订单 #123 退款到账了吗")
servify.createTicket(subject = "退款咨询", aiSummaryIncluded = true)
```

**iOS（Swift，伪代码）**：

```swift
// 1. 最简接入
let servify = ServifyChat.create(config: ServifyConfig(
    apiUrl: "https://support.example.com",
    guestToken: hostProvidedGuestToken,
    branding: Branding(title: "帮助中心", primaryColor: 0x667EEA)
))

// 2. 事件（AsyncStream）
for await unread in servify.events.unreadCount { badge.text = "\(unread)" }
for await message in servify.events.messages { append(message) }

// 3. 拉起
servify.show(from: presentingViewController)

// 4. 发送 / 工单
try await servify.sendMessage("我的订单 #123 退款到账了吗")
try await servify.createTicket(subject: "退款咨询", aiSummaryIncluded: true)
```

**初始化时序（两平台一致）**：

1. `create`：载入持久化快照（app-core `SessionSnapshot` 对齐形：sessionId / lastMessageId / savedAt）；
2. 惰性连接：首次 `show` 才建 WS（对齐 widget 的惰性策略——多数用户不点客服，不为其建连接）；
3. 连接成功 → 用 `lastMessageId` 做增量补拉，合并本地消息表 → 未读计数按"agent/system 来源且本地未渲染过"累加。**补拉通道 V1 按"访客可用会话消息端点"设计（§10 清单 #1，服务端待建）**——现存的 `/api/omni/sessions/:id/messages` 是管理面端点，访客不可达（D2 事实核查），M0 联调探针先用 WS 重放（重连后服务端无历史重发机制，探针阶段以"断连期间静默丢消息"为已知边界记录，不假装有补拉）；
4. 前后台切换：后台不保活 WS（iOS 后台长连接本就不可靠），回前台时先探测再重连 + 增量补拉。

**为什么 API 不提供"自定义消息页面视图控制器/Fragment"级别的 UI 注入点**：V1 的定制面收敛为品牌配置（标题、主色、欢迎语），这是行业 SDK 的成熟边界。暴露整页替换钩子等于承诺 UI 契约稳定，会显著拖慢后续迭代；真有深度定制需求的接入方应该走协议层自建 UI（PROTOCOL.md 支持这种用法）。

### 鉴权与安全（决策 D6 续）

**现状缺口（必须正视，经源码核实）**：WS 路由挂在 publicV1 组（无 AuthMiddleware），握手仅要求 `session_id` 非空即升级连接；URL 里的 `access_token` 参数服务端**不读取**——任何知道 session_id 的客户端都能收听该会话流，Origin 白名单（可配置放行所有来源）是唯一门槛。Web 侧靠"session_id 不出浏览器"侥幸安全，移动端不能复制这个假设（移动端常有多设备/换机/日志脱敏场景，id 泄露面更大）。

**V1 方案：访客 token（guest token）**：

1. 后端新增轻量端点（建议 `POST /api/v1/guest/session`）：宿主后端用 server-to-server 凭证（复用 api-client 的 auth contract）换取**短期签名访客 token**（HMAC/JWT，payload 含 session_id 绑定 + 过期时间），下发给客户端；
2. SDK 连接 WS 时以 `access_token` 参数携带（与 Web 的 `AuthProvider` 形态一致），服务端校验签名与 session 绑定；
3. 兼容路径：`guestToken` 为空时允许匿名 session_id 模式（与 Web 现状对齐），但在 PROTOCOL.md 标注为"过渡形态，服务端未来可配置关闭"。

**为什么不在 V1 做完整 OAuth/设备绑定**：客服会话的数据敏感度是"对话内容"，风险敞口是单会话，短期签名 token 已把泄露影响收敛到单会话时效窗口内；全量身份体系是宿主的责任（宿主可经 guest 换取端点把自己的用户身份编译进 token payload），SDK 不越界。

**其他安全基线**：token 仅存内存 + 系统安全存储（Android EncryptedSharedPreferences / iOS Keychain），不落普通文件；WS 与 REST 强制 HTTPS/WSS；日志默认脱敏（不打印消息正文与 token，debug 开关显式开启才输出）。

---

## 6. UI 形态

### 决策 D8：三形态 = 浮动按钮 + 底部抽屉（默认）/ 全屏会话页（可选 + 小屏自动）

- **浮动按钮**：56dp/56pt 圆形悬浮，位置可配（默认右下角），宿主可完全自绘入口而只调 `servify.show()`——浮钮只是默认入口，不是强绑组件。
- **底部抽屉（默认会话形态）**：Android 用 ModalBottomSheet（max 高度约 85% 屏高），iOS 用 sheet（`presentationDetents` 的 15 级替代实现）。**为什么默认抽屉**：客服会话是"业务中插曲"，半屏保留宿主上下文，用户可边看订单边问——这是嵌入式 SDK 区别于独立 App 的核心体验（呼应 D1）。
- **全屏会话页**：宿主配置 `presentationStyle = .fullscreen` 时使用；小屏设备（高度 < 600dp）抽屉自动升级为全屏，避免半屏里键盘挤压出不可用布局。
- **品牌注入**：主色、标题、欢迎语经 `Branding` 配置进入主题层；不提供 CSS 级/布局级定制（理由见第 5 节）。
- **消息渲染**：AI 消息与坐席消息视觉区分（AI 带标识与"转人工"按钮）；AI 首答流式渲染——`ai-response-delta` 增量即到即拼，收到 `done=true` 的终末增量后再以 `ai-response` 终帧整体替换（增量拼接结果与终帧内容一致，替换是幂等收口）；流中断语义（终末增量已到但无 ai-response 终帧）= 保留已渲染部分 + 追加"回答中断，请重试"提示行，不静默清空也不自动重发；引用来源（`sources`）以可展开的来源列表渲染（文档标题 + 得分排序），点击在应用内浏览器打开链接——这是对齐 v1 产品范围"回答应带可追溯来源"的移动端承载；置信门建议（`next_action=handoff`）渲染为"转人工"按钮的强调态。
- **未读呈现**：抽屉收起时浮钮角标计数；宿主自绘入口时经 `events.unreadCount` 自行呈现。

---

## 7. 推送与离线消息

### 决策 D7：推送是可选模块（FCM/APNs），离线消息 = 推送唤醒 + `lastMessageId` 增量补拉，不引入消息持久化同步协议

**为什么推送必须是可选模块而不是内置依赖**：会话进行中用户切走 App，坐席回复到达——没有推送，用户永远看不到，这是移动端相对 Web 的**真实缺口**（Web 没做是因为浏览器推送权限率低且 widget 场景多在当前页面）。但推送依赖宿主集成 FCM/APNs（需要宿主的推送凭证与后台配置），SDK 强绑推送会把无推送诉求的接入方也拖进集成成本。因此：SDK 暴露 `pushTokenProvider` 注册口（对齐 app-core 的 push token registration contract），宿主提供 token 则启用，否则仅应用内通知。

**流程**：

1. 宿主向 SDK 注册 FCM/APNs token → SDK 经 push registration contract 上报后端（绑定 session/customer）；
2. 会话非前台时坐席/系统消息 → 服务端经 FCM/APNs 发静默+提醒推送；
3. 用户点推送或 App 回前台 → SDK 重连 + `lastMessageId` 游标增量补拉 → 合并渲染 + 未读计数。

**为什么不用"离线队列反向同步"（客户端消息也可离线发）**：客服对话的发送方是用户，用户在客服页时必然在线；离线发送的典型场景（弱网抖动）用"发送超时 + 本地标记失败 + 手动重发"覆盖即可，与 Web 端行为一致。完整的离线优先同步协议（cursor、冲突消解、幂等去重）是给"服务端→客户端数据面"用的，即上面的增量补拉；客户端→服务端方向 V1 不做队列重放，避免引入"用户以为发了、实际三天后才发出去"的客服场景事故。app-core 的 offline queue contract 保留为 P2 演进位。

---

## 8. 包体积与依赖约束

### 决策 D9：Android AAR 增量 ≤ 1.5MB，iOS XCFramework ≤ 2MB，依赖白名单封闭

**为什么体积是硬约束**：客服 SDK 是宿主 App 里性价比最敏感的依赖——它不产生日活，接入方却要为它付出下载转化成本。行业基准：Intercom Android SDK 增量约 2-4MB（含图片库），Servify V1 功能面更窄（无附件、无富媒体、无离线队列实现），应该显著小于这个数，且这是对接入方宣传"轻量"的资格线。

**预算分解（Android，rar/jar 内字节）**：

| 项 | 预算 |
|---|---|
| SDK 自身类 + 资源 | ≤ 600KB |
| Compose UI（仅会话页用到的 runtime/ui/foundation 子集，R8 后增量） | ≤ 600KB |
| kotlinx-serialization 增量 | ≤ 150KB |
| 协程增量 | ≤ 150KB |
| okHttp | 0（`api` 依赖，随宿主去重；独立计不计入 SDK 增量） |

**R8 规则**：SDK 附带 consumer proguard 规则（保留消息模型与反射点）；kotlinx-serialization 的规则随库自带。

**iOS 预算**：XCFramework ≤ 2MB（SwiftUI/Combine 是系统库零增量；主要体积是自身代码 + 协议模型）。App Store 隐私清单：SDK 不采集数据（对话经宿主网络栈到服务端），声明"无追踪"。

**依赖冻结机制**：Android 依赖白名单写进构建脚本断言（`dependencies` 锁定列表，CI 校验新增依赖即红）；iOS 零三方依赖由"Package.swift 无 dependencies 声明"结构性保证。任何白名单变更须过一次策划评审——这是 07 号 M4 surface governance 精神在依赖维度的延伸。

---

## 9. 分阶段里程碑与验收标准

### 决策 D10：四阶段，协议先行，Android 先于 iOS

**为什么 Android 先行**：CI 与构建环境自举成本低（本仓库已有 JVM 工具链），先在 Android 上把协议联调跑通，iOS 侧照抄契约回放测试的样例集，减少双端并行时的协议摇摆成本。

**M0 — 协议契约与联调探针（无 UI）**

- 产出：`sdk/PROTOCOL.md` 首版（✅ 已入库）；`sdk/protocol-fixtures/` 样例集；Android 探针 CLI/单测：WS 连接、text-message 收发、ai-response + ai-response-delta 解析（含增量拼接与流中断样例）、transfer/waiting_notification 状态机转移全部经 fixtures 回放通过。
- 验收：① fixtures 被 core、Android 双端同一套样例喂过且断言一致；② 与后端真实环境完成一次全链路联调（建连 → AI 首答流式 → 转人工 → 坐席回复）；③ 契约文档覆盖当前服务端全部广播消息类型（含 webrtc 类型的"移动端 V1 不消费"显式标注，以及 core SDK 死分支 `session_update`/`agent_status` 的"服务端不发送、移动端契约不含"显式标注）。
- 状态（2026-09-22）：**已完成**。③ 由 PROTOCOL.md 达成（3bbee3e2）；① 样例集 10 例 + core 9 测试 / Android 10 测试同一套样例回放断言一致（39f2a67、2b96be6，含 CI `android-probe` job）；② `make mobile-probe-acceptance` 对真实服务跑通全链路并留档 manifest（ff8c49b，内嵌 OpenAI 兼容流式 mock LLM，`scripts/test-results/mobile-probe/manifest.json` overall=passed）。

**M1 — Android SDK Alpha**

- 产出：三形态 UI、消息收发、AI 首答（流式渲染 + 引用展示 + 置信门提示）、转人工状态机、未读计数、惰性连接与重连、访客 token 接入（含后端 guest token 端点——**后端配套项，需进后端排期**）。
- 验收：① 契约回放测试全绿；② demo 宿主 App 集成 ≤ 10 行代码完成初始化 + 拉起；③ AAR 增量 ≤ 1.5MB（CI 体积门禁）；④ 弱网/断网/后台切换手工测试矩阵通过（含流式中断的渲染降级）；⑤ 单元测试覆盖协议层与状态机（覆盖率不设 100% 目标，但状态机转移表必须穷举）。
- 状态（2026-09-22）：**已完成（Android Alpha；真机手工项待执行，逐条锚定见 `sdk/android/ACCEPTANCE-M1.md`）**。① 同一样例集 core/Android 双端回放断言一致 + SDK 全量 65 测试全绿（39f2a67、2b96be6、42ba7f6）；② demo 集成 3 行（02bb8c1）；③ 体积门禁取 R8 口径（未混淆 Compose 全家桶增量 5MB+ 非验收口径，D9 核查记录），delta ≈1.19MB ≤ 1.5MB，CI 独立 step（02bb8c1）；④ 自动化项（断线重连/发送超时/流中断渲染降级/重连耗尽恢复/握手失败分类）CI 锚定全绿，真机手工项（前后台/飞行模式/蜂窝 WiFi 切换/转人工状态跨断线）⏳ 待执行——断连期间静默丢消息为 V1 已知边界（服务端无历史重发，不假装有补拉）；⑤ 转人工状态机 3 条合法转移 + 非法拒绝、连接状态机 §4.4 转移表（含 idle→connecting→connected、幂等 connect、重连耗尽→disconnected→connect 恢复）穷举（StateMachineTest 4 测试、ConnectionLifecycleTest 5 测试，42ba7f6）。访客 token：SDK 握手参数已就位（2214bb5，PROTOCOL §1 服务端当前不消费、向后兼容），guest token 端点为 §10 #2 后端配套项，进后端排期。

**M2 — iOS SDK Alpha**

- 产出：与 M1 能力面一致的 iOS 版本 + Swift Package 分发。
- 验收：① 同一套 fixtures 回放全绿；② XCFramework ≤ 2MB；③ demo 宿主集成 ≤ 10 行代码；④ Keychain 存储、后台切换、推送注册口联调通过。
- 状态（2026-09-23）：**已完成（iOS Alpha；③ demo 工程与 ④ Keychain/推送注册口按分层如实标注，逐条锚定见 `sdk/ios/ACCEPTANCE-M2.md`）**。① protocol-fixtures 同一样例集 core/Android/Swift 三端回放断言一致（`FixtureReplayTests` 11 测试），SDK 全量 61 测试 Linux 全绿 + macOS `ios-macos` job 跑 Darwin 面 63（0f99bee 起，dda9e16/a646e2a/51d1bf0）；② 体积门禁取分发形态 zip 后字节 ≤2MB（`scripts/check-ios-sdk-size.sh`，Release + BUILD_LIBRARY_FOR_DISTRIBUTION，SwiftPM 包打包须走 archive——c83199b 核查记录），CI 独立 step；③ 接入面已证（`ServifyView(config:)` 一行拉起），真实 demo 工程需 Xcode 项目文件，待 macOS 环境（M4 接入文档站阶段）落地；④ 后台切换的可见性语义已自动化锚定（unreadCountsOnlyWhileSessionHidden + historySnapshot 回放），流中断渲染降级同 Kotlin 锚定，Keychain 存储/推送注册口为 M3 面且依赖后端配套项（§10 #2/#3）——V1 匿名 session 模式无凭证存储需求，如实标注未实现；真机手工项（前后台/飞行模式/蜂窝 WiFi）同 M1 矩阵待执行。状态机穷举同 M1⑤ 口径（转人工 4 + 连接 §4.4 转移表 6 + 退避策略 4）；覆盖率取行级可覆盖面 100%（Darwin 分支由 macos job 覆盖，a646e2a）。

**M3 — 推送 + 工单 + 品牌化收口（双端）**

- 产出：FCM/APNs 可选推送模块、会话页工单创建入口（AI 摘要预填）、Branding 全量配置、CocoaPods 兼容层评估。
- 验收：① 推送端到端（后台 → 系统 → 点开 → 增量补拉 → 未读归零）双端通过；② 工单创建携带 AI 摘要且坐席侧可见；③ 体积门禁持续绿。
- 状态（2026-09-23）：**代码面完成（双端；验收① 推送端到端待服务端/凭证配套，如实标注，不假装可验收；逐条锚定见 `sdk/ACCEPTANCE-M3.md`）**。刀序列：刀 1 Branding offlineText（4fd2ead）→ 刀 2 服务端访客工单端点（48813a6，§10 #4；迁移水位断言随 000011 递增 337f613/9a36216）→ 刀 3a createTicket 门面 + TicketSummary 摘要（7b6ced4）→ 刀 3b 双端 UI 入口（11ae3df，iOS 15 兼容 + 测试时序修正 8d928de）→ 刀 4 pushTokenProvider 注册口 + CocoaPods 评估（5a81558）。① SDK 侧注册口就位：`pushTokenProvider` 未配置→unsupported / provider 返 nil→静默（宿主未授权是正常态）/ 有 token→过渡期 unsupported（§10 #5 端点落地后仅补上报实现，冻结面不变，5a81558 双端 6 用例镜像）；端到端链路（后台→系统→点开→增量补拉→未读归零）依赖 §10 #5 推送下发 + §10 #1 游标端点 + FCM/APNs 凭证（P1-1），未通；offlineText 断线提示双端锚定（握手失败/重连耗尽/销毁不提示/不计未读，ConnectionLifecycleTest ↔ ConnectionLifecycleTests 用例名逐一对应）。② 双端闭环：门面 createTicket 自动组 ai_summary（TicketSummary 最近 10 条，[访客]/[AI]/[客服] 标注，System 提示行跳过，空则不带键）+ UI 入口（标题必填/描述选填/提交中禁用/失败重试提示/成功 appendSystemHint 进 history 不计未读）+ 服务端三包测试（orchestration 5 + delivery 3 + handlers 5）；坐席侧可见走管理面既有 tickets 列表/详情（同表同响应链，ai_summary 进 ticketResponse），无新开发面。③ 体积门禁持续绿：Android R8 口径 + iOS 分发 zip 口径 CI 独立 step 每次 push 复核，M3 期间无漂移。CocoaPods 兼容层评估：`docs/mobile-sdk-cocoapods-evaluation.md`——零已知 CocoaPods-only 接入方，SPM-only 单轨维持，按需重建触发条件已记录，复核时点 M4 文档站定稿。真机手工项（推送/前后台/弱网）随验收①外部配套一并待执行。

**M4 — 稳定化与接入就绪**

- 产出：接入文档站（VitePress 移动端章节）、断线/弱网自动化测试矩阵、崩溃与性能基线（会话页首帧 < 300ms、内存增量 < 30MB）。
- 验收：① 双端 demo 连续 72h 长稳（重连循环、前后台往返）无泄漏；② 文档站构建绿并挂载发布位；③对外交付物（AAR/XCFramework 产物流程）就绪——**发布动作本身仍由维护者手动执行，SDK 仓库不内置任何自动发版步骤**。
- 状态（2026-09-23）：**可交付面完成（② ③ ✅；① 与性能基线为环境依赖项如实标注待执行；逐条锚定与 72h 执行方案见 `sdk/ACCEPTANCE-M4.md`）**。② 接入指南 `docs/mobile-sdk-integration.md`（双端，API 签名经源码核对）+ nav/sidebar 挂载 + 本地 vitepress build 绿 + docs-pages.yml 既有自动发布（6ba98c7）；③ CI 产物归档 servify-sdk-aar / servify-kit-xcframework（`if-no-files-found: error`，retention 30 天，f4a129d）+ 获取路径文档化 + SwiftPM 远程引用边界如实记录（`sdk/ios/Package.swift` 不在仓库根，V1 为 XCFramework 手动嵌入，远程分发待维护者手动建独立 distribution 仓库）；弱网矩阵经缺口分析确认已由 M1-M3 锚定面穷举（高延迟回显/断线重连/耗尽/握手失败/流中断/退避序列，镜像用例逐一对应），延迟注入不触达新路径、不添加时间敏感用例；① 72h 长稳与性能基线（首帧 < 300ms、内存 < 30MB）阻塞于 GitHub Actions 6h job 上限 + 本机无真机/模拟器 + demo 工程待 macOS（M2 验收③同源约束）——执行方案（压力脚本/判据/留档口径）已在验收矩阵冻结，环境到位后照单执行。

**明确排除项（记录不做的决定，防止范围回弹）**：坐席移动端、语音/视频、富媒体消息（图片/文件）、离线发送队列、消息撤回/编辑、多语言内置（V1 中文，Branding 预留 i18n 钩子）。

---

## 10. 后端配套需求清单（SDK 依赖项，需进服务端排期）

> 共同背景（D2 事实核查）：现存 REST 端点几乎全部挂在管理面中间件链下（认证 + agent/admin/service 主体 + 资源权限），访客唯一现成通道是 `/api/v1/ws`。下列 #1/#2/#5 本质是同一件事的三个面——**为访客开设最小可达面**，服务端排期时建议按"访客配套端点"一个设计评审收敛。

| # | 需求 | 服务 SDK 阶段 | 现状 |
|---|---|---|---|
| 1 | 访客可用的会话消息增量拉取端点（`lastMessageId` 游标语义） | M1 | 无——`/api/omni/sessions/:id/messages` 是管理面端点，访客 401 |
| 2 | 访客 token 签发端点（server-to-server 换取）+ WS 握手校验接线 | M1 | 无——WS 握手免认证，`access_token` 参数不消费（见 D6） |
| 3 | 未读计数（服务端会话级未读数或客户端可推导的已读游标） | M1 | 无（客户端推导即可起步，服务端游标为增强；与 #1 游标语义一并设计） |
| 4 | 访客可用的工单创建端点，接受 `ai_summary` 字段 | M3 | ✅ 已落地（M3 刀 2，48813a6）：`POST /api/v1/tickets` 免认证（与 WS 同 `/api/v1` 前缀），`session_id`/`title` 必填 + `description`/`ai_summary`；分类/优先级/来源服务端固定默认值，租户 scope 从 session 行继承；迁移 000011_ticket_ai_summary；坐席侧管理面 tickets 列表/详情同表同响应链自然可见（状态史/自动派单/`ticket.created` 事件同构） |
| 5 | 推送 token 注册 + 会话消息推送下发 | M3 | 服务端未实现（P1-1 凭证阻塞一并排队）；SDK 侧注册口过渡语义已就位（M3 刀 4，5a81558）：`pushTokenProvider` 未配置→unsupported、provider 返回 nil→静默（宿主未授权是正常态）、有 token→过渡期 unsupported（端点落地后仅补上报实现，冻结面不变） |
| 6 | （已完成，记录在案）WS `ai-response` 携带 `sources`/`strategy`/`next_action`/`handoff_reason`（零值省略，向后兼容）+ `ai-response-delta` 流式帧（增量/终末增量/完整终帧三段契约，流中断语义明确） | M1 | ✅ 已落地（2026-09 AI 主链路智能化批次），M0 fixtures 可直接从服务端实现提炼 |

> 其中 #6 随 AI 主链路智能化批次完成；#1/#2/#3 是移动端独立配套（游标与鉴权可合并评审）；#4/#5 服务 M3。
