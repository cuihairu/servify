# M3 验收矩阵（推送 + 工单 + 品牌化收口，双端）

对应 `docs/mobile-sdk-design.md` M3 验收条款 ①—③。双端共同里程碑（M1=Android、M2=iOS 各自收口后，M3 起双端同刀推进，用例名逐一镜像防单侧漂移）。自动化项逐条锚定测试与提交；依赖外部环境的项如实标注执行状态——**未执行的项不勾**。

刀序列：刀 1 Branding offlineText（4fd2ead）→ 刀 2 服务端访客工单端点（48813a6，§10 #4）→ 刀 3a createTicket 门面 + 摘要（7b6ced4）→ 刀 3b UI 入口（11ae3df；iOS 15 兼容 + 测试时序修正 8d928de）→ 刀 4 pushTokenProvider 注册口 + CocoaPods 评估（5a81558）→ 刀 5 推送注册链路（服务端 §10 #5 注册端点 + SDK 双端真实上报）→ 刀 6 推送下发编排 + FCM/APNs HTTP 传输（服务端代码面：dispatcher + 手搓 JWT 双传输 + config push 节，真凭证联调 P1-1 如实留白）→ 刀 7 服务端访客消息增量拉取端点（§10 #1：`GET /api/v1/sessions/:session_id/messages`，消息 ID 单调游标 + has_more 探测，SDK 双端接入待联调） → 刀 8 服务端访客 token 签发 + WS 握手校验接线（§10 #2 / D6 落地：`POST /api/v1/guest/session` service 链签发 HS256 短期 token、hub 按 `security.guest_token.required` 三层校验 401 拒升级、config gate + 装配兜底双层拒启动；SDK 侧 guestToken 握手参数先行就位 2214bb5 向后兼容）。

## ① 推送端到端 ⏳ 真实凭证联调未通，注册链路 + 下发代码面已通（如实标注）

- **注册链路（自动化锚定 ✅，刀 5）**：`registerPushToken()` 真实上报 `POST {apiUrl}/api/v1/push/register`，体 `{session_id, platform, token}`（platform 服务端固定 ios/android）；2xx→`true`；IO 失败→`network` + `false`；非 2xx→`network`(http 状态码) + `false`（注册可重报，network 可重试语义一致）。保留前两分支语义不变：`pushTokenProvider` 未配置→`unsupported`("push not configured")；provider 返 null/nil→静默 `false`（宿主未授权是正常态，非错误）。服务端：迁移 000012_push_tokens（幂等键 session_id+platform，重复注册保活同一行刷新 token+updated_at，换平台独立行；token 落库但 JSON 面不回显），三包测试 orchestration 5 + delivery 7 + handlers 5。测试：`RegisterPushTokenTest`（Kotlin 6）↔ `RegisterPushTokenTests`（Swift 6），用例名逐一对应。
- **下发编排 + HTTP 传输（自动化锚定 ✅，刀 6，服务端代码面）**：`PushOutboundDispatcher` 订阅 `conversation.message_received`（sender=agent 才出站、AI 回复不外推与 email 出站同口径；EventID 环形去重复用 outboundSeen；访客 WS 在线（`WebSocketHub.IsSessionConnected`）抑制不出站——在线面未装配同样跳过，宁可漏推不误推；离线访客按 push_tokens 注册行分平台出站：android→FCM / ios→APNs，未知 platform 行与空白 token 行跳过；payload `{session_id, message_id}` + 截断 120 字符摘要 + 标题「您有新的客服回复」）。传输层零新依赖手搓：FCM HTTP v1（服务账号 RS256 JWT → oauth2 token 交换缓存至到期前 60s → messages:send）、APNs token-based（ES256 逐请求签、apns-topic/apns-push-type 头、sandbox 开关选域），httptest 全测（push/infra 100%）；config push 节默认 disabled，双层 gate（InsecureDefaults：enabled 无凭证/部分配置 production 拒启动；装配层 wirePushRuntime 畸形凭证 return error 兜底）。测试：dispatcher + wire 15 用例（app/server）+ infra 12 用例 + config 3 用例 + hub presence 1 用例。
- **Branding offlineText 断线提示（自动化锚定 ✅，刀 1 4fd2ead）**：配置后在重连耗尽/握手失败落 disconnected 终态时追加系统提示行（进 history 可回放、不计未读）；未配置不出行；用户主动 destroy 不提示。测试：`ConnectionLifecycleTest` / `ConnectionLifecycleTests` 的 offlineHint 四用例 + `appendSystemHintEntersHistoryAndStreamWithoutUnread`（无 replay 流的用 onSubscription 钩订阅点后触发，消 CI release 变体竞态）。
- **端到端链路 ⏳**：后台 → 系统 → 点开 → 增量补拉 → 未读归零。注册链路已通（刀 5）+ 服务端下发编排与 FCM/APNs HTTP 传输已落地（刀 6，代码面）；仍缺外部依赖（不阻塞代码面收口）：真实 FCM 服务账号 / APNs .p8 凭证端到端联调（P1-1）、SDK 双端游标补拉接入联调（服务端端点刀 7 已落地）、真机推送环境。
- **真机手工项 ⏳（外部配套到位后执行）**：后台收推送展示、点开通知进会话页、增量补拉后未读归零、前台期间不重复推送、进程被杀后 cold start 补拉。

## ② 工单创建携带 AI 摘要且坐席侧可见 ✅（SDK + 服务端闭环）

- **门面（7b6ced4）**：`createTicket(title, description?)` → `POST {apiUrl}/api/v1/tickets`（§10 #4 刀 2 落地的免认证端点），体 `{session_id, title, description?, ai_summary?}`；201→`TicketReceipt(ticketId)`；失败返 null + 错误流（IO→`network`、非 2xx/畸形→`ticket_failed`，与 sendMessage 同风格，ServifyError 非 Throwable）。
- **摘要（7b6ced4）**：`TicketSummary` 最近 10 条，标注 `[访客]`/`[AI]`/`[客服]`（门面 AI 回复 = System sender + isAiResponse 标志归 `[AI]`）；System 提示行跳过；空会话返 null（请求不带 `ai_summary` 键）。
- **UI 入口（11ae3df + 8d928de）**：标题栏"工单"按钮 → 表单（标题必填/描述选填/提交中禁用/scrim 防穿透）→ 成功 appendSystemHint("工单 #N 已创建…") 进 history 不计未读、失败显示重试提示行。多行输入 iOS 15 兼容（`TextField(axis:)` 是 iOS 16+ 禁用，TextEditor + 手动 placeholder 替代）。
- **服务端（48813a6）**：`POST /api/v1/tickets` 免认证（与 WS 同 `/api/v1` 前缀），`session_id`/`title` 必填 + `description`/`ai_summary`；分类/优先级/来源服务端固定默认值（general/normal/chat），租户 scope 从 session 行继承；迁移 000011_ticket_ai_summary（水位断言三处同步 337f613/9a36216）；三包测试 orchestration 5 + delivery 3 + handlers 5。
- **坐席侧可见 ✅**：同表同响应链，`ai_summary` 进 ticketResponse，管理面 tickets 列表/详情自然可见（状态史/自动派单/`ticket.created` 事件与管理面创建同构），无新开发面。

## ③ 体积门禁持续绿 ✅

- Android：R8 口径 delta ≤ 1.5MB（M1 验收③口径不变）；iOS：分发形态 zip 后字节 ≤ 2MB（M2 验收②口径不变）。两门禁均为 CI 独立 step 每次 push 复核，M3 期间无漂移。

## 状态机与流语义补充锚定 ✅（M3 期间新增的行为面）

- `appendSystemHint`：SDK 自造系统提示行（工单成功/离线提示/流中断提示同构收敛）进 history（面板 hide 后随快照回放）、不计未读——`appendSystemHintEntersHistoryAndStreamWithoutUnread` 双端镜像。
- 流中断尾部提示收敛复用同一实现（`finalizeInterruptedStream` → appendSystemHint），行为不变。

## CocoaPods 兼容层评估 ✅（产出清单收尾项）

- `docs/mobile-sdk-cocoapods-evaluation.md`（5a81558）：零已知 CocoaPods-only 接入方，SPM-only 单轨维持；按需重建触发条件（首个 CocoaPods-only 接入方 / 调研中 ≥2 家明确前提）与建法（`vendored_frameworks` 指同一份 XCFramework、spec lint 进 CI、tag 同源单点发版）已记录；复核时点 M4 文档站定稿。

## 真机手工项（同 M1/M2 矩阵口径）⏳

前后台往返 30s+ / 飞行模式 ≥ 重连耗尽 / 蜂窝↔WiFi 切换 / 工单表单键盘遮挡与滚动——需真机与真实网络环境，CI 无法模拟；推送真机项见验收①清单。

## 后端配套依赖（不阻塞 M3 代码面收口）

§10 #5 下发侧真实凭证联调（FCM 服务账号 / APNs .p8，P1-1；注册端点刀 5 + 下发编排与 HTTP 传输刀 6 均已落地）、#2（guest token 签发——已随刀 8 落地，见上）。#1 已随刀 7 落地（`GET /api/v1/sessions/:session_id/messages`，消息 ID 单调游标，SDK 双端接入待联调）、#4 已随刀 2 落地（见验收②）。
