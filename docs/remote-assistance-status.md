# 远程协助能力矩阵与缺口清单

> 盘点时间：2026-09-25。本文是远程协助功能的**当前真实状态源**，取代
> [remote-assistance-current-state.md](./remote-assistance-current-state.md)
> （2026-04 旧盘点）作为事实基准；产品方向与 V1 定位见
> [remote-assistance.md](./remote-assistance.md)、最小链路见
> [remote-assistance-mvp.md](./remote-assistance-mvp.md)。
>
> 判定口径：**已实现** = 端到端链路真实可用且有测试/验收证据；
> **部分实现** = 管道或子链路存在，但端到端有断点或语义缺口；
> **未实现** = 仓库无对应代码。

## 结论先行

- 协助**记录面**（发起/结束/列表/详情/标注/录制元数据）已实现并有真实运行验收；
- 协助**实时面**两端骨架已备（访客 SDK 采集、管理端 video 占位、WS 信令帧、TURN 交付），但**服务端不做媒体桥接**，坐席实际收不到访客画面——屏幕共享端到端未打通；
- **对方同意（consent）、远程控制（输入操控）、WebRTC 断线恢复**未实现；
- assist 是全仓唯一**没有租户/工作区隔离**的业务模块（scope 值由客户端自报、读取无过滤），属真实安全缺口；
- 同一客服会话可叠加多个 active 协助记录（无单活跃约束），且同一 sessionID 的两条服务端 PeerConnection 查找有歧义。

## 能力矩阵

### 1. 发起与接受协助 —— 部分实现

| 子能力 | 判定 | 证据 |
| --- | --- | --- |
| 发起（管理面） | 已实现 | `POST /api/remote-assist/sessions`（`apps/server/internal/handlers/assist_handler.go:43`）创建 active 记录；管理端 `handleStartRemoteAssist`（`apps/admin/src/pages/Conversation/index.tsx:351`）REST 落记录后开独立信令 WS |
| 发起（访客面） | 已实现 | SDK `startRemoteAssist`（`sdk/packages/core/src/sdk.ts:317`）：建 PC + DataChannel + 屏幕采集 + 录制 |
| 结束 | 已实现 | `POST /api/remote-assist/sessions/:id/end`（`assist_handler.go:85`，重复结束 409）；录制元数据可随结束落库 |
| 接受/加入 | 部分实现 | 坐席经同 session WS + `RTCPeerConnection` 加入（`index.tsx:399-501`）；**无"邀请→接受/拒绝"握手**——访客端由宿主页面自行调用 `startRemoteAssist`，不知道协助已发起 |
| 邀请 ID 传递 | 未实现（端到端） | 坐席经 DataChannel 发 `{type:'assist-session', assist_id}`（`index.tsx:405-412`）；服务端 `OnDataChannel` 收到后按 WS `data-channel-message` 帧广播（`apps/server/internal/platform/realtime/webrtc_service.go:173`）；但访客 SDK **只在自己 DataChannel 的 onmessage 上监听**（`sdk.ts:341`），WS 层该帧无人消费——assist_id 实际传不到访客端 |

### 2. 权限与对方同意 —— 部分实现

| 子能力 | 判定 | 证据 |
| --- | --- | --- |
| 管理面 RBAC | 已实现 | `apps/server/internal/app/server/router_management.go:15-31`：AuthMiddleware + EnforceRequestScope + RequirePrincipalKinds(agent/admin/service) + RequireResourcePermission("assist") |
| 访客面归属校验 | 部分实现 | 仅录制回写有会话归属校验（`apps/server/internal/modules/assist/application/service.go:147-161` `AttachRecording` 校验 owner） |
| 对方同意（consent） | 未实现 | `RemoteAssistSession`（`apps/server/internal/modules/assist/domain/models.go:15`）无 consent 字段；无同意/拒绝端点；访客对"被共享屏幕/被录制"无显式授权动作（仅浏览器自身的 getDisplayMedia 授权） |
| 租户/工作区隔离 | 未实现（缺口） | `StartCommand.TenantID/WorkspaceID` 来自**请求体自报**（`assist_handler.go:27-28`）；仓储 Get/List/标注全部**无 scope 过滤**（`apps/server/internal/modules/assist/infra/gorm_repository.go:26-84`）——对照 macro/api_key/satisfaction 等模块的 `applyScopeFilter` + ctx 取值模式（`apps/server/internal/modules/satisfaction/application/scope.go:14-27`），assist 是全仓唯一未隔离的业务模块 |

### 3. 屏幕共享与远程控制 —— 屏幕共享部分实现 / 远程控制未实现

| 子能力 | 判定 | 证据 |
| --- | --- | --- |
| 访客屏幕采集 | 已实现 | `getDisplayMedia` + `addTrack`（`sdk.ts:372-381`）；无采集能力时显式报错 |
| 坐席视频占位 | 已实现 | `remoteAssistVideoRef` + `peer.ontrack` 挂 srcObject（`index.tsx:444-454`），offer 带 `offerToReceiveAudio/Video`（`index.tsx:495-498`） |
| 媒体链路（服务端桥接） | 未实现 | 信令为**服务端终结模式**：`HandleOffer` 每次新建服务端 PC 应答（`webrtc_service.go:209-235`），访客与坐席各连服务端一条 PC，两条 PC 之间**无媒体转发**——`webrtc_service.go` 全文件无 `OnTrack`/`AddTrack`/`NewTrackLocal*`，坐席端 `ontrack` 实际不会触发，**端到端画面不通** |
| DataChannel 透传 | 部分实现 | 服务端 `OnDataChannel` 收文本后按 `data-channel-message` 广播回同 session（`webrtc_service.go:173-197`）、`SendDataChannelMessage` 可下行（`webrtc_service.go:339-355`）；仅纯文本 `SendText`，无二进制，且见上文"访客端不消费 WS 帧"断点 |
| 远程控制（鼠标/键盘） | 未实现 | 全仓（Go + apps/admin + sdk）无 co-browsing / 输入事件转发代码；标注（rect/freehand/arrow）是**录制回放批注**（`domain/models.go:34`），不是实时屏幕标注 |
| WebRTC 连接观测 | 已实现 | `GET /api/v1/webrtc/stats`、`/api/v1/webrtc/connections`、`/api/v1/rtc/ice-servers`（`apps/server/internal/app/server/router_realtime.go:38-42`）；连接状态变化经 `webrtc-state-change` 帧下发 |

### 4. 文件传输 —— 已实现（一次性上传形态）

- `POST /api/v1/upload`（`apps/server/internal/handlers/upload_handler.go:50-89`）：单请求 multipart、默认 32MB 上限（`router_auth.go:127`）、扩展名白名单、本地/S3 双 provider（S3 模式经现签 URL 302）。
- SDK `uploadFile` 走同一端点，URL 附在消息里（`sdk/packages/core/src/sdk.ts:510`）。
- 协助录制文件同链路上传，元数据经 `POST /api/v1/remote-assist/:id/recording` 回写（`assist_recording_handler.go:42`）。
- 边界：**无 WS 流式/分片/断点续传**——大文件受 32MB 上限约束；对客服场景够用，登记为已知边界而非缺口。

### 5. 断线重连与会话恢复 —— 部分实现

| 子能力 | 判定 | 证据 |
| --- | --- | --- |
| WS 自动重连 | 已实现 | SDK 指数退避：默认 5 次、1s 起、2 倍、封顶 30s（`sdk/packages/core/src/contracts/reconnect.ts:5-10`），手动关闭不重连 |
| 消息补拉 | 已实现 | 重连后 `reconcileMissedMessages` 按 lastMessageId 游标分页补拉去重（`sdk.ts:652-690`）；服务端 `ListAfter`（`router_realtime.go:24`） |
| 协助记录恢复 | 已实现 | 协助会话/标注/录制元数据全落库，进程重启不丢（`infra/gorm_repository.go`） |
| WebRTC 恢复 | 未实现 | WS 重连不触发 PC 重建/重协商（`sdk.ts:154` 仅透传事件）；服务端 `HandleOffer` 重连即**新建** PC（`webrtc_service.go:209-210`），旧 PC 不关闭会泄漏；hub unregister 不清理 rtc 连接（`websocket_hub.go:246-253`） |

### 6. 并发会话 —— 部分实现

| 子能力 | 判定 | 证据 |
| --- | --- | --- |
| 多客服会话并行 | 已实现 | 会话模型与 WS hub 按 session_id 广播天然支持多会话并行；协助记录按 conversation_session_id 关联 |
| 单会话单活跃协助 | 未实现（缺口） | `StartSession`（`application/service.go:81-100`）不查重——同一客服会话可叠加任意多个 active 协助记录，录制归属与审计口径会分裂 |
| 同 session 双端 PC 歧义 | 未实现（缺口） | 服务端 connections 以 connectionID 为键（`webrtc_service.go:24,200`），`getConnectionBySessionID` 遍历 map 取**首个命中**（`webrtc_service.go:294-305`）——访客与坐席对同一 sessionID 各建一条 PC 后，ICE candidate / answer 路由到哪条不确定（map 遍历序随机） |

### 7. 审计日志 —— 部分实现

| 子能力 | 判定 | 证据 |
| --- | --- | --- |
| 管理面审计 | 已实现 | 协助管理路由挂在 `AuditMiddleware` 下（`router_management.go:18`），写操作按路由推断 action/resource 落库（`apps/server/internal/platform/audit/gin_middleware.go:31-67`） |
| 访客面审计 | 未实现（缺口） | `POST /api/v1/remote-assist/:id/recording` 注册时不带审计（`router_auth.go:144-148`）——录制回写这类访客侧敏感写操作无留痕 |
| 聚合报表 | 已实现 | `GetRemoteAssistTicketStats`（`apps/server/internal/modules/analytics/infra/gorm_repository.go`）已接入统计面（空集 COALESCE 归零，无工单环境不 500） |

### 8. 安全与鉴权 —— 部分实现

| 子能力 | 判定 | 证据 |
| --- | --- | --- |
| 管理面鉴权链 | 已实现 | 见第 2 行管理面 RBAC；audit 组有独立 audit 权限查看留痕 |
| TURN/STUN | 已实现 | coturn 独立部署 + 时间限 HMAC 短时凭据（`apps/server/internal/platform/realtime/iceturn.go:74-89`），配置双层 gate（`config.go:750-758`）；ICE 配置经 WS `webrtc-ice-config`（`websocket_hub.go:230-244`）与 REST `GET /api/v1/rtc/ice-servers` 双通道下发；交付资产 `infra/compose/docker-compose.coturn.yml` |
| 坐席端 ICE | 部分实现（缺口） | 管理端硬编码 `stun:stun.l.google.com:19302`（`index.tsx:400`），未消费服务端 `webrtc-ice-config` 下发（访客 SDK 已接，`sdk.ts:327`）——严格网络（无公网 STUN 出口）下坐席建连会失败 |
| 数据面租户隔离 | 未实现 | 见第 2 行"租户/工作区隔离" |

## 缺口清单与优先级

按"安全 > 正确性 > 审计 > 产品语义 > 实时面大件"排序：

| 编号 | 缺口 | 优先级 | 类型 | 状态 |
| --- | --- | --- | --- | --- |
| RA-1 | assist 租户/工作区隔离（ctx 取值 + 仓储过滤 + 跨 scope 404） | P0 | 安全 | 本轮修复 |
| RA-2 | 同一客服会话单活跃协助约束（重复发起 409） | P0 | 正确性 | 本轮修复 |
| RA-3 | 访客面（recording/consent）写操作审计 | P1 | 审计 | 本轮修复 |
| RA-4 | 对方同意（consent）状态机：pending→granted/declined + 访客端点 + declined 拒绝录制回写 | P1 | 产品语义 | 本轮修复（最小闭环） |
| RA-5 | 同 session 双端 PC 查找歧义（信令帧无对端标识，需协议扩展） | P2 | 正确性/协议 | 设计登记，随 RA-7 一并定协议 |
| RA-6 | 坐席端 ICE 硬编码（消费 webrtc-ice-config 下发） | P2 | 可用性 | 待做（管理端小改） |
| RA-7 | 屏幕共享媒体桥接（服务端 track 转发 SFU-lite，或信令改 peer 中继） | P2 | 实时面大件 | 设计登记，见下节 |
| RA-8 | WebRTC 断线恢复（重连后重协商 + 服务端旧 PC 清理） | P3 | 实时面大件 | 设计登记 |
| RA-9 | 远程控制（输入事件经 DataChannel 中继 + 访客端授权开关） | P3 | 实时面大件 | 设计登记 |
| RA-10 | 邀请握手端到端（WS assist-invite 帧或修复 data-channel-message 消费） | P2 | 协议 | 随 RA-7 一并定 |

### RA-7/8/9/10 的实现方向（不在本轮）

- **媒体桥接（RA-7）**：两条路线。A）服务端 SFU-lite——访客 PC 上 `OnTrack` 收 RTP，经 `webrtc.NewTrackLocalStaticRTP` 重建 track 挂到坐席 PC（pion 支持纯转发，无需编解码）；B）信令改 peer 中继——hub 把 offer/answer/candidate 在同 session 的客户端间转发，服务端退出媒体路径（与现有 voice 服务端终结模式冲突，需按用途分流）。路线 A 不动客户端、与 voice 共存，倾向 A。
- **断线恢复（RA-8）**：SDK 在 WS reconnecting 时保留 PC、connected 后重发 offer；服务端 `HandleOffer` 前先关同 session 旧 PC（顺带修 RA-5 的泄漏）。
- **远程控制（RA-9）**：访客端开关（默认关）+ DataChannel JSON 事件（pointer/scroll/key）+ 访客端合成派发，consent 必须为 granted 才允许开启（与 RA-4 衔接）。
- **邀请握手（RA-10）**：新增 WS 下行帧 `assist-invite {assist_id}`（复用 session 广播），访客 SDK 据此弹同意 UI——与 RA-4 的 consent 端点闭环；帧契约需同步 `sdk/PROTOCOL.md` 与 fixtures。

## 本轮修复记录（2026-09-25）

RA-1..RA-4 已按"服务端可测先行"落地（媒体/协议面 RA-5..RA-10 仍按上节方向登记）：

- **RA-1 租户/工作区隔离**：`StartCommand` 不再接收请求体自报的 `tenant_id/workspace_id`，改由认证中间件注入的 ctx 取值（`platformauth.TenantIDFromContext/WorkspaceIDFromContext`，与 macro/satisfaction 等模块同模式）；仓储 `GetSession/ListSessions` 按 scope 过滤、`ListAnnotations/DeleteAnnotation` 经 sessions 子查询守卫、新增 `FindActiveSessionIDByConversation` 也走 scope（`infra/gorm_repository.go`）。跨 scope 读写统一 `ErrAssistNotFound`（404 语义，不泄露存在性）。无 scope ctx（本地 dev/存量链路）不过滤，行为兼容。
- **RA-2 单活跃约束**：`StartSession` 先查同会话 active 记录，命中返回 `ErrAssistSessionActive`（HTTP 409）；结束后可重新发起。
- **RA-3 访客面审计**：`router_auth.go` 的 recording/consent 两条访客路由挂 `AuthMiddleware + AuditMiddleware`，访客侧敏感写操作留痕。
- **RA-4 同意状态机（最小闭环）**：`RemoteAssistSession` 增 `ConsentStatus`（`pending/granted/declined`）+ `ConsentAt`；新端点 `POST /api/v1/remote-assist/:id/consent`（访客面，校验会话归属）：同意保持 active、拒绝即结束协助；同表态幂等、相反表态 409（`ErrAssistConsentDecided`）；`declined` 后录制回写被拒（409 `ErrAssistConsentDeclined`）。存量空 consent 值兼容为"未走同意流程"，不阻断既有回写链路（验收脚本口径不变）。迁移 `000014_remote_assist_consent.up.sql`（ALTER ADD COLUMN，水位 13→14）。
- 测试：application 层 `RespondConsent` 全分支（归属/幂等/冲突/落库）、`StartSession` 单活跃与 scope 注入；infra 层跨租户隔离、标注守卫、consent 生命周期；handlers 层 consent 端点与 409 映射。
- 尚未接线（登记）：坐席发起后通知访客弹同意 UI 依赖 RA-10 邀请帧；declined 会话的 WS/RTC 侧清理依赖 RA-7/8 协议工作。

## 与验收证据的关系

- REST 记录面 20 项 checks 有真实运行证据：`scripts/test-remote-assist-acceptance.sh`（`make remote-assist-acceptance`），manifest 入库 `scripts/test-results/remote-assist/`。
- 实时面（WS 信令、WebRTC 建连）在 `websocket_hub`/`webrtc_service` 有单测，但**无访客↔坐席双端真实媒体验收**——RA-7 落地时应补对应验收脚本。
