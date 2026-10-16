# 远程协助能力矩阵与缺口清单

> 盘点时间：2026-09-25（2026-09-26 更新：RA-1..RA-4 修复与 RA-8 服务端半边已落地，判定同步刷新；2026-10-14 更新：RA-8 SDK 半边（WS 重连后重发 offer）已落地，WebRTC 恢复条目改为已实现；2026-10-15 更新：RA-7 媒体桥接（路线 A SFU-lite）与 RA-5 信令归属定向已落地，屏幕共享端到端在服务端回环链路打通）。本文是远程协助功能的**当前真实状态源**，取代
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
- 协助**实时面**两端骨架已备（访客 SDK 采集、管理端 video 占位、WS 信令帧、TURN 交付），**服务端媒体桥接（RA-7 路线 A SFU-lite）已落地**：访客发布轨经服务端重建转发到坐席 PC，坐席端经服务端发起的重协商收轨；端到端画面在服务端回环链路（真实 pion 双 PC + RTP）打通（`TestWebRTC_LoopbackMediaBridge`），**浏览器级双端验收脚本仍缺**；
- **对方同意（consent）**最小可跑链路已实现（访客端点 + declined 终态 + 录制回写阻断）；**远程控制**未实现；**WebRTC 断线恢复**已落地（服务端重发 offer 前回收终态旧 PC + SDK 在 WS 重连后重发未完成的 offer）；
- 租户/工作区隔离与单活跃约束已修复（RA-1/2，见"本轮修复记录"）；
- 同一 sessionID 双端 PC 的信令歧义已修复（RA-5）：服务端按 WS 连接身份定向下发 answer/candidate/状态帧，无归属的旧调用方回退按会话首个命中。

## 能力矩阵

### 1. 发起与接受协助：部分实现

| 子能力 | 判定 | 证据 |
| --- | --- | --- |
| 发起（管理面） | 已实现 | `POST /api/remote-assist/sessions`（`apps/server/internal/handlers/assist_handler.go:44`）创建 active 记录；管理端 `handleStartRemoteAssist`（`apps/admin/src/pages/Conversation/index.tsx:645`）REST 落记录后开独立信令 WS |
| 发起（访客面） | 已实现 | SDK `startRemoteAssist`（`sdk/packages/core/src/sdk.ts:318`）：建 PC + DataChannel + 屏幕采集 + 录制 |
| 结束 | 已实现 | `POST /api/remote-assist/sessions/:id/end`（`assist_handler.go:84`，重复结束 409）；录制元数据可随结束落库 |
| 接受/加入 | 部分实现 | 坐席经同 session WS + `RTCPeerConnection` 加入（`index.tsx:690-797`）；**无"邀请→接受/拒绝"握手**，访客端由宿主页面自行调用 `startRemoteAssist`，不知道协助已发起 |
| 邀请 ID 传递 | 未实现（端到端） | 坐席经 DataChannel 发 `{type:'assist-session', assist_id}`（`index.tsx:704`）；服务端 `OnDataChannel` 收到后按 WS `data-channel-message` 帧广播（`apps/server/internal/platform/realtime/webrtc_service.go:188`）；但访客 SDK **只在自己 DataChannel 的 onmessage 上监听**（`sdk.ts:342`），WS 层该帧无人消费，assist_id 实际传不到访客端 |

### 2. 权限与对方同意：已实现（最小可跑链路，UI 通知面依赖 RA-10）

| 子能力 | 判定 | 证据 |
| --- | --- | --- |
| 管理面 RBAC | 已实现 | `apps/server/internal/app/server/router_management.go:15-31`：AuthMiddleware + EnforceRequestScope + RequirePrincipalKinds(agent/admin/service) + RequireResourcePermission("assist") |
| 访客面归属校验 | 已实现 | 录制回写与同意表态均校验会话归属（`apps/server/internal/modules/assist/application/service.go` `AttachRecording`/`RespondConsent` 校验 owner） |
| 对方同意（consent） | 已实现（最小可跑链路） | `ConsentStatus/ConsentAt` 字段（`domain/models.go`）+ `POST /api/v1/remote-assist/:id/consent`（`assist_recording_handler.go`）：同意保持 active、拒绝即结束且阻断录制回写；同表态幂等、相反表态 409。缺口：坐席发起后通知访客弹同意 UI 依赖 RA-10 邀请帧 |
| 租户/工作区隔离 | 已实现（本轮修复） | `StartCommand` 不再收请求体自报 scope（`assist_handler.go`）；ctx 取值 + 仓储 `applyScopeFilter`/sessions 子查询守卫（`infra/gorm_repository.go`），与 macro/satisfaction 模块同口径；跨 scope 404 语义 |

### 3. 屏幕共享与远程控制：屏幕共享已实现（服务端回环链路证据）/ 远程控制未实现

| 子能力 | 判定 | 证据 |
| --- | --- | --- |
| 访客屏幕采集 | 已实现 | `getDisplayMedia`（`sdk.ts:987-996`）+ `addTrack`（`sdk.ts:374-384`）；无采集能力时显式报错 |
| 坐席视频占位 | 已实现 | `remoteAssistVideoRef` + `peer.ontrack` 挂 srcObject（`index.tsx:738-746`），offer 带 `offerToReceiveAudio/Video`（`index.tsx:796-797`） |
| 媒体链路（服务端桥接） | 已实现（RA-7 路线 A，2026-10-15） | 信令仍为**服务端终结模式**：访客与坐席各连服务端一条 PC；服务端 PC `OnTrack` 登记转发轨（一条读 + N 条写，`webrtc_service.go` `trackForwarder`），经 `NewTrackLocalStaticRTP` 重建挂到同会话其它存活 PC 并**服务端主动发起重协商**（`negotiate` 推 `webrtc-offer` 帧，坐席端应答回传）；晚加入者经 `replicateTracksTo` 补投。管理端 `ontrack` 对无 MediaStream 的桥接轨有组流兜底。回环链路证据：`TestWebRTC_LoopbackMediaBridge`（真实双 pion PC，访客 RTP → 服务端转发 → 坐席收 VP8 轨）；浏览器级验收待补 |
| DataChannel 透传 | 部分实现 | 服务端 `OnDataChannel` 收文本后按 `data-channel-message` 广播回同 session（`webrtc_service.go:188-207`）、`SendDataChannelMessage` 可下行（`webrtc_service.go:387`）；仅纯文本 `SendText`，无二进制，且见上文"访客端不消费 WS 帧"断点 |
| 远程控制（鼠标/键盘） | 未实现 | 全仓（Go + apps/admin + sdk）无 co-browsing / 输入事件转发代码；标注（rect/freehand/arrow）是**录制回放批注**（`domain/models.go:38`），不是实时屏幕标注 |
| WebRTC 连接观测 | 已实现 | `GET /api/v1/webrtc/stats`、`/api/v1/webrtc/connections`、`/api/v1/rtc/ice-servers`（`apps/server/internal/app/server/router_realtime.go:51-54`）；连接状态变化经 `webrtc-state-change` 帧下发 |

### 4. 文件传输：已实现（一次性上传形态）

- `POST /api/v1/upload`（`apps/server/internal/handlers/upload_handler.go:55-90`）：单请求 multipart、默认 32MB 上限（`router_auth.go:127`）、扩展名白名单、本地/S3 双 provider（S3 模式经现签 URL 302）。
- SDK `uploadFile` 走同一端点，URL 附在消息里（`sdk/packages/core/src/sdk.ts:544`）。
- 协助录制文件同链路上传，元数据经 `POST /api/v1/remote-assist/:id/recording` 回写（`assist_recording_handler.go:44`）。
- 边界：**无 WS 流式/分片/断点续传**，大文件受 32MB 上限约束；对客服场景够用，登记为已知边界而非缺口。

### 5. 断线重连与会话恢复：部分实现

| 子能力 | 判定 | 证据 |
| --- | --- | --- |
| WS 自动重连 | 已实现 | SDK 指数退避：默认 5 次、1s 起、2 倍、封顶 30s（`sdk/packages/core/src/contracts/reconnect.ts:8-13`），手动关闭不重连 |
| 消息补拉 | 已实现 | 重连后 `reconcileMissedMessages` 按 lastMessageId 游标分页补拉去重（`sdk.ts:686 起`）；服务端 `ListAfter`（`router_realtime.go:35`） |
| 协助记录恢复 | 已实现 | 协助会话/标注/录制元数据全落库，进程重启不丢（`infra/gorm_repository.go`） |
| WebRTC 恢复 | 已实现（RA-8 两端齐备，单测覆盖） | 服务端 `HandleOffer` 前回收同 session 终态旧 PC（`webrtc_service.go` `closeTerminalConnections`，仅 disconnected/failed/closed，存活 PC 不动）；SDK 侧 WS `connected` 时若 PC 存活且 offer 尚未完成协商（offered/connecting 态）则重发同一 offer（`sdk.ts` `resendRemoteAssistOfferIfNeeded`，协商进入终态清引用防重复发送；PC 跨 WS 重连保留，媒体不依赖 WS）。hub unregister 刻意不清理 rtc 连接：PC 跨 WS 重连存活是 RA-8 的既有语义，回收发生在下次 offer 的终态回收与显式 `CloseConnection` |

### 6. 并发会话：已实现

| 子能力 | 判定 | 证据 |
| --- | --- | --- |
| 多客服会话并行 | 已实现 | 会话模型与 WS hub 按 session_id 广播天然支持多会话并行；协助记录按 conversation_session_id 关联 |
| 单会话单活跃协助 | 已实现（本轮修复） | `StartSession` 先查同会话 active 记录，命中返回 `ErrAssistSessionActive`（HTTP 409，`application/service.go`）；结束后可重新发起 |
| 同 session 双端 PC 歧义 | 已实现（RA-5，2026-10-15） | 每条 PC 记录信令归属 WS 连接 ID（`WebRTCConnection.ownerClientID`，由创建它的 offer 的 WS 连接推导，客户端无需感知）；answer/candidate 经 `connByOwner` 按归属查找，服务端下发的 candidate/state/重协商 offer 经 `wsHub.SendToClient` 定向投递（`sendToOwner`）；无归属的旧调用方（REST 面、adapter）回退按会话首个命中，既有行为不变 |

### 7. 审计日志：已实现

| 子能力 | 判定 | 证据 |
| --- | --- | --- |
| 管理面审计 | 已实现 | 协助管理路由挂在 `AuditMiddleware` 下（`router_management.go:19`），写操作按路由推断 action/resource 落库（`apps/server/internal/platform/audit/gin_middleware.go:31-67`） |
| 访客面审计 | 已实现（本轮修复） | recording/consent 两条访客路由挂 `AuthMiddleware + AuditMiddleware`（`router_auth.go`），录制回写与同意表态均留痕 |
| 聚合报表 | 已实现 | `GetRemoteAssistTicketStats`（`apps/server/internal/modules/analytics/infra/gorm_repository.go`）已接入统计面（空集 COALESCE 归零，无工单环境不 500） |

### 8. 安全与鉴权：部分实现

| 子能力 | 判定 | 证据 |
| --- | --- | --- |
| 管理面鉴权链 | 已实现 | 见第 2 行管理面 RBAC；audit 组有独立 audit 权限查看留痕 |
| TURN/STUN | 已实现 | coturn 独立部署 + 时间限 HMAC 短时凭据（`apps/server/internal/platform/iceturn/iceturn.go:65-77`），配置双层 gate（`config.go:828-834` 告警 + `iceturn.go:37-43` Validate 拒绝）；ICE 配置经 WS `webrtc-ice-config`（`websocket_hub.go:250-254`）与 REST `GET /api/v1/rtc/ice-servers` 双通道下发；交付资产 `infra/compose/docker-compose.coturn.yml` |
| 坐席端 ICE | 已实现（本轮修复） | 管理端消费服务端下发：WS `webrtc-ice-config` 推送缓存 → REST `GET /api/v1/rtc/ice-servers` 回退（`services/remoteAssist.ts` `getIceServers`），两者皆空才退公网 STUN，与访客 SDK（`sdk.ts` `resolveServerIceServers`）同口径，严格网络下由服务端 TURN 兜住 |
| 数据面租户隔离 | 已实现（本轮修复） | 见第 2 行"租户/工作区隔离" |

## 缺口清单与优先级

按"安全 > 正确性 > 审计 > 产品语义 > 实时面大件"排序：

| 编号 | 缺口 | 优先级 | 类型 | 状态 |
| --- | --- | --- | --- | --- |
| RA-1 | assist 租户/工作区隔离（ctx 取值 + 仓储过滤 + 跨 scope 404） | P0 | 安全 | 本轮修复 |
| RA-2 | 同一客服会话单活跃协助约束（重复发起 409） | P0 | 正确性 | 本轮修复 |
| RA-3 | 访客面（recording/consent）写操作审计 | P1 | 审计 | 本轮修复 |
| RA-4 | 对方同意（consent）状态机：pending→granted/declined + 访客端点 + declined 拒绝录制回写 | P1 | 产品语义 | 本轮修复（最小可跑链路） |
| RA-5 | 同 session 双端 PC 查找歧义（信令帧无对端标识，需协议扩展） | P2 | 正确性/协议 | 已落地（信令归属定向，无需客户端协议变更，见 2026-10-15 一节） |
| RA-6 | 坐席端 ICE 硬编码（消费 webrtc-ice-config 下发） | P2 | 可用性 | 本轮修复（管理端） |
| RA-7 | 屏幕共享媒体桥接（服务端 track 转发 SFU-lite，或信令改 peer 中继） | P2 | 实时面大件 | 已落地（路线 A，见 2026-10-15 一节） |
| RA-8 | WebRTC 断线恢复（重连后重协商 + 服务端旧 PC 清理） | P3 | 实时面大件 | 已落地（服务端终态旧 PC 回收 + SDK 断线重发 offer） |
| RA-9 | 远程控制（输入事件经 DataChannel 中继 + 访客端授权开关） | P3 | 实时面大件 | 设计登记 |
| RA-10 | 邀请握手端到端（WS assist-invite 帧或修复 data-channel-message 消费） | P2 | 协议 | 设计登记 |

### RA-9/10 的实现方向（不在本轮）

- **媒体桥接（RA-7）**：已按路线 A 落地（2026-10-15，见下节）；路线 B（信令 peer 中继）不再需要。
- **断线恢复（RA-8）**：已完成（两端齐备）。服务端 `HandleOffer` 前先关同 session 终态旧 PC（只回收终态 PC，RA-5 歧义下的存活 PC 不动）；SDK 在 WS `connected` 后重发未完成的同一 offer——PC 跨 WS 重连保留（媒体不依赖 WS，丢的只是 `webrtc-offer` 信令帧），协商中途新造 offer 违反信令态机，故重发原 offer，服务端幂等应答。
- **远程控制（RA-9）**：访客端开关（默认关）+ DataChannel JSON 事件（pointer/scroll/key）+ 访客端合成派发，consent 必须为 granted 才允许开启（与 RA-4 衔接）。
- **邀请握手（RA-10）**：新增 WS 下行帧 `assist-invite {assist_id}`（复用 session 广播），访客 SDK 据此弹同意 UI，与 RA-4 的 consent 端点收口；帧契约需同步 `sdk/PROTOCOL.md` 与 fixtures。

## 本轮修复记录（2026-09-25）

RA-1..RA-4、RA-6 与 RA-8 服务端半边已按"服务端可测先行"落地（媒体面 RA-5/RA-7 已于 2026-10-15 收口，见对应章节；RA-9/RA-10 仍按上节方向登记）：

- **RA-1 租户/工作区隔离**：`StartCommand` 不再接收请求体自报的 `tenant_id/workspace_id`，改由认证中间件注入的 ctx 取值（`platformauth.TenantIDFromContext/WorkspaceIDFromContext`，与 macro/satisfaction 等模块同模式）；仓储 `GetSession/ListSessions` 按 scope 过滤、`ListAnnotations/DeleteAnnotation` 经 sessions 子查询守卫、新增 `FindActiveSessionIDByConversation` 也走 scope（`infra/gorm_repository.go`）。跨 scope 读写统一 `ErrAssistNotFound`（404 语义，不泄露存在性）。无 scope ctx（本地 dev/存量链路）不过滤，行为兼容。
- **RA-2 单活跃约束**：`StartSession` 先查同会话 active 记录，命中返回 `ErrAssistSessionActive`（HTTP 409）；结束后可重新发起。
- **RA-3 访客面审计**：`router_auth.go` 的 recording/consent 两条访客路由挂 `AuthMiddleware + AuditMiddleware`，访客侧敏感写操作留痕。
- **RA-4 同意状态机（最小可跑链路）**：`RemoteAssistSession` 增 `ConsentStatus`（`pending/granted/declined`）+ `ConsentAt`；新端点 `POST /api/v1/remote-assist/:id/consent`（访客面，校验会话归属）：同意保持 active、拒绝即结束协助；同表态幂等、相反表态 409（`ErrAssistConsentDecided`）；`declined` 后录制回写被拒（409 `ErrAssistConsentDeclined`）。存量空 consent 值兼容为"未走同意流程"，不阻断既有回写链路（验收脚本口径不变）。迁移 `000014_remote_assist_consent.up.sql`（ALTER ADD COLUMN，水位 13→14）。
- **RA-8 服务端半边（终态旧 PC 回收）**：`WebRTCService.HandleOffer` 前先 `closeTerminalConnections(sessionID)`，回收同 session 处于 disconnected/failed/closed 的旧 PC（含 voice EndCall），再新建应答（`platform/realtime/webrtc_service.go`）。**刻意只回收终态**：访客与坐席双端各持一条存活 PC 的现状拓扑（RA-5 歧义，2026-10-15 已收口）下，按 session 全量回收会误伤对端连接。
- **RA-6 坐席端 ICE 消费下发**：管理端建 PC 前按"服务端下发优先"解析 ICE（`apps/admin/src/pages/Conversation/index.tsx` `resolveRemoteAssistIceServers`）：WS `webrtc-ice-config` 推送缓存 → REST `GET /api/v1/rtc/ice-servers` 回退 → 公网 STUN 最后兜底，与访客 SDK 同口径。TURN 凭据短时有效，缓存跨尝试复用、过期由下次 REST 回退自然刷新。
- 测试：application 层 `RespondConsent` 全分支（归属/幂等/冲突/落库）、`StartSession` 单活跃与 scope 注入；infra 层跨租户隔离、标注守卫、consent 生命周期；handlers 层 consent 端点与 409 映射；realtime 层 re-offer 回收（存活存活/终态回收/voice EndCall、nil voice、close 报错仍移除、他 session 不波及），包覆盖率维持 100%。
- 尚未接线（登记）：坐席发起后通知访客弹同意 UI 依赖 RA-10 邀请帧；declined 会话的 WS/RTC 侧清理如今可借 RA-5 归属定向对对端单独投递/回收，留待接线时实施。

## RA-8 SDK 半边收口（2026-10-14）

- **WS 重连后重发未完成的 offer**：SDK 在 `startRemoteAssist` 记录本次 offer 载荷与协商状态（`sdk/packages/core/src/sdk.ts` `lastRemoteAssistOffer`/`remoteAssistState`）；WS `connected` 事件上若 PC 存活、offer 尚未完成协商（`offered`/`connecting` 态）且连接可用，重发**同一** offer（`resendRemoteAssistOfferIfNeeded`）。理由：RTCPeerConnection 跨 WS 重连存活（媒体不依赖 WS，只有 `webrtc-offer` 帧会丢），协商中途新造 offer 违反信令态机；服务端 `HandleOffer` 幂等应答且先回收终态旧 PC，重复 offer 无副作用。协商进入 `connected`/`ended`/`failed` 即清 offer 引用，连接成功后不会重复发送。
- 测试：`sdk/packages/core/src/sdk.test.ts` 新增 RA-8 describe 五例（等待应答期间重连重发 / answer 中途丢失重发 / 已连接不重发 / `endRemoteAssist` 后不重发 / 无活跃会话不重发）；`apps/demo-sdk` 两个 bundle 随 SDK 构建重跑入库。

## RA-7 媒体桥接与 RA-5 信令归属收口（2026-10-15）

按 2026-09-26 登记的路线 A（服务端 SFU-lite）落地，客户端协议面零变更（访客 SDK 不动；管理端仅新增对服务端重协商 offer 的应答处理）：

- **转发核心（`webrtc_service.go` `trackForwarder`）**：服务端 PC `OnTrack` 后登记一条发布轨（键 = 发布连接 ID + 轨道 ID，重复发布幂等跳过），**一条读 + N 条写**——单个 goroutine 循环读 `TrackRemote`，逐包写进所有订阅端的 `TrackLocalStaticRTP` 本地轨（pion 纯转发，无编解码）。订阅端写面报错（PC 已关）即撤下；发布端读面报错（下线）转发 goroutine 自然退出。
- **投递路径**：`fanOutTrack` 把新发布轨挂到同会话其它存活 PC（终态连接跳过）；`replicateTracksTo` 在新 PC 应答 offer 后补投同会话既有发布轨（晚加入者立即可见）。
- **服务端发起重协商**：桥接加轨后 `negotiate()` 在该 PC 上 CreateOffer/SetLocalDescription 并把 offer 以 `webrtc-offer` 帧定向下发（按连接 `negotiationMu` 串行化）。刻意**不挂 `OnNegotiationNeeded`**——数据通道等本端变更同样会触发它，向未实现服务端 offer 处理的客户端自发推 offer 会打断既有信令流（`TestWebRTC_FullSignalingFlow` 实证）。
- **RA-5 信令归属**：每条服务端 PC 记录创建它的 offer 所属 WS 连接（`ownerClientID`）；`HandleAnswer`/`HandleICECandidate` 经 `connByOwner` 按归属查找；服务端下发的 candidate/state-change/renegotiation offer 经 `wsHub.SendToClient`（互斥锁内投递，避开 unregister 关闭通道的竞态）定向投递，无归属（REST 面）回退会话广播/首个命中。
- **下线收口**：`CloseConnection`/终态回收在持锁下 `dropForwarderStateLocked`——删除该连接作为发布方的转发条目、撤掉它对其它发布轨的订阅写面。
- **管理端配套（`apps/admin/src/pages/Conversation/index.tsx`）**：`onmessage` 处理 `webrtc-offer`（稳态直接应答；answer 在途的 glare 场景排队、`onsignalingstatechange` 稳态后补应用，失败仅告警不判协助失败）；`ontrack` 对无 MediaStream 的桥接轨用 `new MediaStream([event.track])` 组流兜底。
- 测试：`webrtc_bridge_test.go`——转发器登记/替换/撤下/循环分支、fanOut/replicate 过滤、attach 错误分支（构造失败/PC 已关/幂等）、negotiate 三态（成功定向/广播回退/seam 错误不外发）、SendToClient 三分支、connByOwner 归属命中/未命中/回退；**`TestWebRTC_LoopbackMediaBridge`** 用真实双 pion PC 走完整链路（访客 offer 带发布轨 → ICE/DTLS 建连 → 访客写 RTP → 服务端 OnTrack 转发 → 坐席收 `webrtc-offer` 重协商应答 → 坐席 ontrack 收到 VP8 桥接轨），候选/应答全程走归属定向路由（路由错则链路不成立，兼作 RA-5 回归）。realtime 包覆盖率 100%、race 三连过。

## 与验收证据的关系

- REST 记录面 20 项 checks 有真实运行证据：`scripts/test-remote-assist-acceptance.sh`（`make remote-assist-acceptance`），manifest 入库 `scripts/test-results/remote-assist/`。
- 实时面（WS 信令、WebRTC 建连、媒体桥接）在 `websocket_hub`/`webrtc_service` 有单测与回环链路测试（`TestWebRTC_LoopbackMediaBridge`，真实双 pion PC + RTP 转发 + 重协商）；**浏览器级双端真实媒体验收脚本仍缺**，登记为后续项（真实浏览器 getDisplayMedia + 管理端 video 帧断言）。
