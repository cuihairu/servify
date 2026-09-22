# Servify Android SDK

客服会话 SDK（M1 Alpha）：WS 长连接、AI 首答流式渲染、转人工状态机、未读计数、
惰性连接与自动重连、浮动按钮 + 抽屉/全屏会话面板。

## 集成（M1 验收②：≤10 行）

```kotlin
class MainActivity : ComponentActivity() {
    override fun onCreate(savedInstanceState: Bundle?) {
        super.onCreate(savedInstanceState)
        setContentView(R.layout.activity_main) // 宿主自己的业务界面

        val chat = ServifyChat.create(
            this,
            ServifyConfig(apiUrl = "https://your-servify-host"),
        )
        chat.show(this) // 挂浮钮 + 首次触发 WS 连接
    }
}
```

- `ServifyChat.create(context, config)`：门面入口，sessionId 由 SDK 生成（`m-<uuid>`）。
- `show(hostActivity)`：挂浮动按钮（默认右下角 56dp，主色取 `branding.primaryColor`）并
  首次触发连接；`hide()` 收起会话面板，**连接保持**。
- 事件订阅：`chat.events`（§4.3 聚合面：`messages` / `unreadCount` / `connectionState` /
  `reconnecting` / `agentAssigned` / `waitingInQueue` / `error`）。
- `destroy()`：摘除全部 UI、断开连接、取消协程作用域。

## 要求与约束

- 宿主 Activity 需为 `ComponentActivity`（面板经 ComposeView 挂载，需要视图树上的
  LifecycleOwner）；未桥接的宿主自动降级：浮钮仍可显示，面板不挂载。
- 依赖白名单（设计文档 D9）：kotlin-stdlib、kotlinx-coroutines、kotlinx-serialization-json、
  okhttp、Compose runtime/ui/foundation。不引入 material/material3，视觉组件全部自绘。
- 体积门禁（M1 验收③）：宿主（R8 开启）集成增量 ≤ 1.5MB，
  由 `scripts/check-android-sdk-size.sh` 用 demo vs demo-baseline 两 APK 差值校验。
- AAR 自带 consumer-rules.pro（kotlinx.serialization 的 R8 规则），宿主无需手工配置。

## 模块

| 模块 | 说明 |
| --- | --- |
| `servify-sdk` | 单 AAR 主体（协议层/会话核心/连接层/UI 层） |
| `demo` | ≤10 行集成样例宿主（体积门禁加数） |
| `demo-baseline` | 同 UI 零 SDK 基线 app（体积门禁减数） |
| `probe` | 纯 JVM 联调探针（CI 全链路验收用） |

## 里程碑状态

- M0 ✅ 建连 → AI 首答流式 → 转人工 → 坐席回复（真机探针验收）
- M1 刀 1-3 ✅ 工程/门面/UI（浮钮 + 抽屉/全屏面板 + 气泡/来源/置信门/未读）
- M1 刀 4 ✅ 会话连续性：内存级累积 + 未读可见性语义 + 流中断收口 + guestToken 握手参数（磁盘持久化按 D7 核查不做——补拉/推送端点均为服务端待建项，预留即死代码）
- M1 刀 5 ✅ 验收矩阵与状态机穷举（连接状态机 5 测试 + 转人工 4 测试；真机手工项待执行，见 [ACCEPTANCE-M1.md](ACCEPTANCE-M1.md)）

设计依据：`docs/mobile-sdk-design.md`（D3/D5/D8/D9）、`docs/mobile-sdk-platform-spec.md`（§4 API 冻结面）。
