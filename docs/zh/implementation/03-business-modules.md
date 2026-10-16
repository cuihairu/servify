# 03 Business Modules

范围：

- conversation
- routing
- agent
- ticket
- customer
- automation
- analytics
- voice

## B1 conversation

- [x] 创建 `internal/modules/conversation/domain`
- [x] 创建 `internal/modules/conversation/application`
- [x] 创建 `internal/modules/conversation/infra`
- [x] 创建 `internal/modules/conversation/delivery`
- [x] 定义 `Conversation`
- [x] 定义 `Participant`
- [x] 定义 `ConversationMessage`
- [x] 定义 `ChannelBinding`
- [x] 定义 `ConversationStatus`
- [x] 实现 CreateConversation
- [x] 实现 ResumeConversation
- [x] 实现 IngestTextMessage
- [x] 实现 IngestSystemEvent
- [x] 发布 `conversation.created`
- [x] 发布 `conversation.message_received`
- [x] 新增 `conversation` GORM repository，并映射现有 `Session` / `Message` 模型
- [x] `WebSocketHub` 文本消息持久化优先接入 `conversation` adapter，旧 DB 直写保留为 fallback
- [x] `WebSocketHub` 的“人工客服已接管”判断优先接入 `conversation` reader，避免 AI 抢答
- [x] `WebSocketHub` 的最近消息历史读取优先接入 `conversation` adapter，减少对 `Message` 表的直查

验收：

- conversation 成为统一交互模型

## B2 routing

- [x] 创建 `internal/modules/routing/domain`
- [x] 创建 `internal/modules/routing/application`
- [x] 创建 `internal/modules/routing/infra`
- [x] 创建 `internal/modules/routing/delivery`
- [x] 定义 `Assignment`
- [x] 定义 `QueueEntry`
- [x] 定义 `TransferRecord`
- [x] 定义 `AgentAvailabilityPolicy`
- [x] 定义 `SkillMatchPolicy`
- [x] 定义 `LoadBalancePolicy`
- [x] 实现 RequestHumanHandoff
- [x] 实现 AssignAgent
- [x] 实现 AddToWaitingQueue
- [x] 实现 CancelWaiting
- [x] 发布 `routing.agent_assigned`
- [x] 发布 `routing.transfer_completed`
- [x] 新增 `routing` GORM repository，并映射现有 `TransferRecord` / `WaitingRecord` 模型
- [x] `SessionTransferService` 的 waiting queue 路径开始委托 `routing` adapter，旧通知/消息逻辑保留

验收：

- 取代现有 session transfer 交叉依赖

## B3 agent

- [x] 创建 `internal/modules/agent/domain`
- [x] 创建 `internal/modules/agent/application`
- [x] 创建 `internal/modules/agent/infra`
- [x] 创建 `internal/modules/agent/delivery`
- [x] 定义 `AgentProfile`
- [x] 定义 `AgentPresence`
- [x] 定义 `AgentLoad`
- [x] 增加 chat concurrency
- [x] 增加 voice concurrency
- [x] 实现 GoOnline
- [x] 实现 GoOffline
- [x] 实现 MarkBusy
- [x] 实现 MarkAway

验收：

- agent 模块支持未来 voice load

## B4 ticket

- [x] 创建 `internal/modules/ticket/domain`
- [x] 创建 `internal/modules/ticket/application`
- [x] 创建 `internal/modules/ticket/infra`
- [x] 创建 `internal/modules/ticket/delivery`
- [x] 新增 `query_service.go`
- [x] 迁移 `ListTickets`
- [x] 迁移 `GetTicketByID`
- [x] 新增 `command_service.go`
- [x] 迁移 `CreateTicket`
- [x] 将 `CreateTicket` 的初始状态历史并入仓储事务
- [x] 迁移 `UpdateTicket`
- [x] 将 `UpdateTicket` 的主工单更新与状态历史并入仓储事务
- [x] 将 `UpdateTicket` 的自定义字段同步并入模块聚合事务
- [x] 迁移 `AssignTicket`
- [x] 将 `AssignTicket` 下沉到模块命令与仓储事务
- [x] 将 `UnassignTicket` 下沉到模块命令与仓储事务
- [x] 迁移 `AddComment`
- [x] 迁移 `CloseTicket`
- [x] 将 `CloseTicket` 的状态历史与 agent load 递减并入仓储事务
- [x] 拆 `StatusTransitionPolicy`
- [x] 拆 `CustomFieldValidator`
- [x] 拆 `BulkUpdateTickets`
- [x] 发布 `ticket.created`
- [x] 发布 `ticket.assigned`
- [x] 发布 `ticket.closed`
- [x] 将 `eventbus` 注入旧 `TicketService` 运行时入口
- [x] 将 `TicketHandler` 对旧 `TicketService` 的强依赖替换为接口依赖
- [x] 新增 ticket handler adapter，并在 `main.go` 中切换到 adapter 注入
- [x] 将 `AddComment` 从 adapter 下沉到模块命令实现
- [x] 将 `AssignTicket` 从 adapter 下沉到模块命令实现，并保留分配后 SLA 副作用
- [x] 将 `CloseTicket` 从 adapter 下沉到模块命令实现，并保留关闭后评论/SLA/CSAT 副作用
- [x] 将 `BulkUpdateTickets` 从 adapter 下沉到模块命令实现
- [x] 将 `CreateTicket` 从 adapter 下沉到模块仓储实现，并保留创建后自动分配/事件/SLA 副作用
- [x] 将 `UpdateTicket` 从 adapter 下沉到模块聚合事务实现，并保留更新后事件/SLA 副作用
- [x] 将 `ListTicketCustomFields` 从 adapter 下沉为直接数据访问
- [x] 将 `GetTicketStats` 从 adapter 下沉到模块 `QueryService`
- [x] 提取 `TicketOrchestrator`，并改为显式注入 `CommandService + Orchestrator`
- [x] 启动 wiring 不再把整个 `TicketService` 传给 ticket handler adapter
- [x] 将 ticket HTTP/adapter 共享 DTO 迁移到模块 `contract` 包，并保留 `services` 别名兼容
- [x] `SLAHandler` 通过 ticket `ReaderServiceAdapter` 最小读取接口获取工单，不再依赖 `TicketService` 具体类型
- [x] handler 层 sqlite 集成测试迁移到 `integration` build tag，默认测试链路只保留稳定单测
- [x] 旧 `TicketService` 收敛为模块兼容壳：核心读写全部委托给 `application/infra/orchestration`，移除 legacy fallback 分支

验收：

- 旧 TicketService 不再是超大单体

## B5 customer

- [x] 创建 `internal/modules/customer/domain`
- [x] 创建 `internal/modules/customer/application`
- [x] 创建 `internal/modules/customer/infra`
- [x] 创建 `internal/modules/customer/delivery`
- [x] 定义 `CustomerProfile`
- [x] 定义 `CustomerTag`
- [x] 定义 `CustomerNote`
- [x] 定义 `CustomerActivity`
- [x] 实现 CreateCustomer
- [x] 实现 UpdateCustomer
- [x] 实现 AddNote
- [x] 实现 UpdateTags
- [x] 实现 GetCustomerActivity

验收：

- customer 模块独立成型

## B6 automation

- [x] 创建 `internal/modules/automation/domain`
- [x] 创建 `internal/modules/automation/application`
- [x] 创建 `internal/modules/automation/infra`
- [x] 创建 `internal/modules/automation/delivery`
- [x] 定义 `Trigger`
- [x] 定义 `Condition`
- [x] 定义 `Action`
- [x] 定义 `Execution`
- [x] 订阅 ticket events
- [x] 订阅 conversation events
- [x] 订阅 routing events
- [x] 迁移 dry-run

验收：

- automation 基于 event bus 执行

## B7 analytics

- [x] 创建 `internal/modules/analytics/domain`
- [x] 创建 `internal/modules/analytics/application`
- [x] 创建 `internal/modules/analytics/infra`
- [x] 创建 `internal/modules/analytics/delivery`
- [x] DashboardReadModel
- [x] TicketTrendReadModel
- [x] AgentPerformanceReadModel
- [x] SatisfactionTrendReadModel
- [x] SLATrendReadModel
- [x] 先做定时聚合
- [x] 再做事件驱动增量聚合

验收：

- 统计从业务 service 中分离

## B8 voice

- [x] 创建 `internal/modules/voice/domain`
- [x] 创建 `internal/modules/voice/application`
- [x] 创建 `internal/modules/voice/infra`
- [x] 创建 `internal/modules/voice/delivery`
- [x] 定义 `CallSession`
- [x] 定义 `MediaSession`
- [x] 定义 `VoiceParticipant`
- [x] 定义 `Recording`
- [x] 定义 `Transcript`
- [x] 抽象 `RecordingProvider`
- [x] 抽象 `TranscriptProvider`
- [x] 实现 StartCall
- [x] 实现 AnswerCall
- [x] 实现 HoldCall
- [x] 实现 ResumeCall
- [x] 实现 EndCall
- [x] 实现 TransferCall
- [x] 发布 `call.started`
- [x] 发布 `call.held`
- [x] 发布 `call.resumed`
- [x] 发布 `call.transferred`
- [x] 发布 `call.ended`
- [x] 实现 StartRecording
- [x] 实现 StopRecording
- [x] 实现 AppendTranscript
- [x] 发布 `recording.started`
- [x] 发布 `recording.stopped`
- [x] 发布 `transcript.appended`
- [x] 组装 `VoiceCoordinator`
- [x] 接入 runtime / WebRTC lifecycle

验收：

- voice 模块作为 SIP 的业务落点
- `voice` 不直接绑定单一协议；signaling 走 `voiceprotocol.CallSignalingAdapter`，media 走 `voiceprotocol.MediaSessionAdapter`
- 录音与转写通过 provider 接口预留，不与单一厂商绑定
- 录音与转写已经形成正式 application use case，而不只是接口声明
- runtime 已通过 `VoiceCoordinator` 把 WebRTC 生命周期接入 voice 模块
