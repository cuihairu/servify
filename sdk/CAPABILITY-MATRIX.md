# SDK 能力与版本兼容矩阵

跨端能力覆盖与协议兼容对账视图。本表不新增契约：WS 帧语义权威是
[`PROTOCOL.md`](PROTOCOL.md)，公开 API 导出面权威是
[`SURFACE_GOVERNANCE.md`](SURFACE_GOVERNANCE.md)；本表只做对账，两份权威文档更新时须同步复核本表。

## 契约事实源与版本口径

| 维度 | 事实源 | 说明 |
|---|---|---|
| WS 帧契约 | 手写 [`PROTOCOL.md`](PROTOCOL.md) + [`protocol-fixtures/`](protocol-fixtures/) 共享样例 | Web core、Android（`shared/protocol` Kotlin 镜像）、iOS（Swift 镜像）三端消费同一样例集回放断言；无生成式 schema |
| REST HTTP 面 | OpenAPI/Swagger 规格 [`../docs/generated/api/swagger.json`](../docs/generated/api/swagger.json)（`make docs` 重生成，pinned swag） | SDK 内的 REST 调用（session 补拉、工单、推送注册、guest token 等）为手写端点，以该规格为服务端参照 |
| 包版本 | 全部包同号 `1.0.0`（`package.json` `version:sync` / `version:check` 维护） | 无 per-package semver 与 git tag；变更记录见 [`CHANGELOG.md`](CHANGELOG.md)，破坏面流程见 [`SURFACE_GOVERNANCE.md`](SURFACE_GOVERNANCE.md) |
| 能力协商 | Web 缺省 `createWebCapabilitySet()`，移动端 `createMobileCapabilitySet()`（`packages/core/src/bindings/`） | 协商决定 `remote_assist` / `voice` 等 WS 面；移动端 V1 显式 `reserved`/off |

## 能力覆盖矩阵

`✓` = 已实现且随样例/单测覆盖；`✗` = 未实现（V1 冻结或未排期）；括注为关键锚点。

| 能力 / 帧面 | Web core | React / Vue / Vanilla | Android | iOS | React Native |
|---|---|---|---|---|---|
| WS 会话聊天（`text-message` / `agent-message` / 回显确认） | ✓ | ✓（包装 core） | ✓（`FrameCodec.kt`） | ✓（`WireFrame`/`FrameCodec` 镜像） | ✓（经 core） |
| AI 首答流式（`ai-response` / `ai-response-delta`） | ✓（`streaming.ts`） | ✓ | ✓（`StreamingAssembler`） | ✓ | ✓（经 core） |
| 转人工状态机（`transfer_notification` / `waiting_notification`） | ✓ | ✓ | ✓（`HandoffStateMachine`） | ✓ | ✓（经 core） |
| 断线补拉对账（`GET /api/v1/sessions/:id/messages?after_id=`，首连即拉 + 指纹去重） | ✓（`sdk.ts` reconcile） | ✓（随 core） | ✓（history snapshot 合并） | ✓ | ✓（经 core） |
| 客户端未读计数 | ✓（`unread.ts`） | ✓ | ✓（`unreadCount` 事件） | ✓ | ✓（经 core） |
| 服务端已读游标提交（`POST /api/v1/sessions/:id/read`） | ✗ | ✗ | ✓（`ServifyChat.syncReadState()`） | ✓（Swift 镜像） | ✗ |
| 实时翻译消费（`message-translated` 帧 + metadata 译文键读取） | ✓（`websocket.ts`） | ✓ | ✗（未知帧静默忽略） | ✗ | ✗ |
| 语音通道 §9（上行 + 字幕 + WebRTC 音轨 §9.3 `voice:audio-track`） | ✓（`voice.ts` / `voice-capture.ts`） | ✓ | ✗（V1 `reserved`） | ✗ | ✗ |
| 远程协助（`remote_assist` 能力：webrtc 信令 + 桥接轨 + 录制上传） | ✓（缺省能力启用） | ✓ | ✗（mobile 能力集 off） | ✗ | ✗ |
| 推送注册（`POST /api/v1/push/register`，宿主注入 FCM/APNs token） | ✗ | ✗ | ✓（`pushTokenProvider`） | ✓（`registerPushToken()`） | ✗ |
| 访客工单创建（REST `createTicket`） | ✓（`sdk.ts`） | ✓（随 core） | ✓（AI 摘要预填） | ✓（`TicketHTTP`） | ✓（经 core） |
| 文件上传（`POST /api/v1/upload`） | ✓（`api.ts`） | ✓（随 core） | ✗ | ✗ | ✗ |
| ICE 配置获取（`GET /api/v1/rtc/ice-servers` + WS `webrtc-ice-config` 推送） | ✓ | ✓ | ✗（无 WebRTC 面） | ✗ | ✗ |

## 平台形态与已知边界

| 平台 | 形态 | 已知边界 |
|---|---|---|
| Web core | 无 UI 的 WS/REST SDK（浏览器绑定） | 无应用层心跳；发送成功以回显帧确认；补拉对账收口断线窗口 |
| React / Vue / Vanilla | core 的框架包装（组件 / hooks / 挂件脚本） | 能力面与 core 完全一致 |
| Android | Kotlin 门面 `ServifyChat` + Compose 会话面板（M3 代码面完成） | 语音/远程协助帧按未知类型静默忽略；推送下发编排服务端已落地、FCM 真实凭证联调待配套 |
| iOS | Swift/SwiftUI `ServifyChat`（M2 Alpha、M3 代码面完成），SwiftPM/XCFramework 分发 | 同上；Linux CI 无 URLSession WS 实现，传输必须可注入 |
| React Native | headless 绑定（mobile 能力集 + storage/会话快照 + Provider/hooks） | 无 UI 组件；不 import react-native，宿主注入；无屏幕面故 `remote_assist`/`voice` 关闭 |
| transport-http / transport-websocket | future reserved package（`0.0.0`、private） | 未实现前不得对外（治理要求） |

## 分发状态（与本矩阵相关的硬边界）

`@servify/*` 当前不发布 npm registry，workspace 根包 `private: true`；外部消费走源码
checkout / workspace 依赖 / 本地 tarball，详见
[`EXTERNAL-INTEGRATION.md`](EXTERNAL-INTEGRATION.md)。移动端产物走 CI artifact
口径（见 `docs/mobile-sdk-integration.md`「获取 SDK 产物」节）。公开 registry 发布属
独立发版决策，不在日常交付面内。
