# Tenant And Workspace Boundaries

本文件记录当前仓库中核心对象的归属关系与隔离语义，作为 `11 / T1 tenant-and-workspace-boundaries` 的基础盘点。

## 当前最小归属模型

- `tenant`
  - 代表部署级业务租户边界
  - 当前已在配置与 provider 参数中出现，例如 knowledge provider 的 `tenant_id`
  - 核心业务表已普遍落 `tenant_id` / `workspace_id`（逐对象归属见下文），
    残余未收口面见文末清单

- `workspace`
  - 代表租户下的操作空间或管理空间
  - 当前已在权限语义中出现 `workspace.read`
  - 当前仓库存在工作台概览能力，但尚未形成独立 `workspace` 持久化模型

补充说明：

- 当前主业务模型与主运营聚合路径，已基本可以回答“属于哪个 tenant/workspace”
- 仍未完全收口的，主要是少量 legacy 聚合尾项，以及 `DailyStats` 这类系统级全局汇总表是否需要 tenant/workspace 维度拆分的问题

## 核心对象归属现状

### 直接或隐式属于 workspace / tenant 的对象

- `Ticket`
  - 当前已补显式 `tenant_id` / `workspace_id`
  - `ticket` 仓储主查询、统计与创建路径已默认按上下文过滤

- `Session`
  - 当前已补显式 `tenant_id` / `workspace_id`
  - `conversation` 仓储与 runtime adapter 已默认按上下文过滤

- `Message`
  - 当前已补显式 `tenant_id` / `workspace_id`
  - `conversation` 仓储、customer activity、analytics 统计已按上下文过滤

- `TransferRecord`
  - 当前已补显式 `tenant_id` / `workspace_id`
  - `routing` 仓储已默认按上下文过滤

- `WaitingRecord`
  - 当前已补显式 `tenant_id` / `workspace_id`
  - `routing` 仓储已默认按上下文过滤

- `Customer`
  - 当前已补显式 `tenant_id` / `workspace_id`
  - `customer` 仓储通过扩展表 join 收口用户读取、列表、统计与活动查询
  - `GetCustomerActivity` 现已补 integration test，确认 recent sessions / tickets / messages 三条聚合链路都会按请求 scope 过滤，不会把同一 customer_id 在其它 workspace 下的历史数据带回当前上下文
  - 兼容层 `CustomerService.GetCustomerStats` 现已补 scoped 回归测试，并修正 legacy 统计查询中 `users/customers` join 下未限定表前缀的 `created_at` 条件，避免 scoped 客户统计在多表聚合时出现歧义列错误

- `Agent`
  - 当前已补显式 `tenant_id` / `workspace_id`
  - `agent` 仓储与 transfer runtime load 同步路径已按上下文过滤
  - 兼容层 `AgentService.GetAgentStats` 现已补 scoped 回归测试，确认 legacy 统计入口不会跨 workspace 混入其它 agent 主数据

- `KnowledgeDoc`
  - 当前 provider 层支持 `tenant_id`
  - 已视为 tenant/workspace scoped 资源，并默认按上下文过滤

- `CustomField`
  - 已补显式 scope 字段
  - service/repository 默认按上下文过滤

- `SLAConfig`
  - 已补显式 scope 字段
  - service 默认按上下文过滤

- `SLAViolation`
  - 已补显式 `tenant_id` / `workspace_id`
  - `sla` service 的违约创建、列表、解决、统计与监控扫描路径已默认按上下文过滤

- `AppIntegration`
  - 已补显式 scope 字段
  - service 默认按上下文过滤

- `ShiftSchedule`
  - 已补显式 `tenant_id` / `workspace_id`
  - `shift` service 的创建、列表、更新、删除与统计路径已默认按上下文过滤

- `Macro`
  - 已补显式 `tenant_id` / `workspace_id`
  - `macro` service 的列表、创建、更新、删除与应用入口已默认按上下文过滤

- `CustomerSatisfaction` / `SatisfactionSurvey`
  - 已补显式 `tenant_id` / `workspace_id`
  - `satisfaction` service 的调查发送、响应、列表、统计、更新与删除路径已默认按上下文过滤
  - `GetSatisfactionStats` 现已补 scoped 回归测试，确认 category stats 与 trend data 两条聚合视图不会混入其它 workspace 的满意度记录

- `SuggestionService`
  - 依赖已 scope 化的 `Ticket` / `KnowledgeDoc`
  - 相似工单与知识库候选查询现已按请求上下文默认过滤，避免跨 workspace 建议结果泄漏

- `GamificationService`
  - 排行榜聚合现已按请求上下文过滤 `Agent` / `Ticket` / `CustomerSatisfaction`
  - 避免跨 workspace 汇总客服绩效与满意度数据

- `MessageRouter` 兼容落库路径
  - 现已从 `UnifiedMessage.Metadata` 提取 `tenant_id` / `workspace_id` 写入 `Session` / `Message`
  - 同一 `sessionID` 若尝试跨 workspace 复用会直接拒绝持久化，避免旧兼容链路串会话
  - 对于已存在且已 scope 化的 `session`，后续消息即使未重复携带 scope metadata，也会继承 session 已知 scope，避免兼容链路把消息写成无 scope 记录
- `routing -> agent` transfer runtime 同步
  - `ApplySessionTransfer` 现已显式透传当前请求 `context`，避免兼容层在会话转接后用无 scope 的 `context.Background()` 回写 agent load/runtime 状态

## 当前运行时上下文

JWT claims 已支持透传：

- `tenant_id`
- `workspace_id`

并在认证中间件中投影到请求上下文，供后续：

- 数据访问过滤
- 审计日志补维
- provider 调用透传
- 后台任务隔离

同时，这两个字段现在也会进入 `request context`，因此 service / repository 层可以直接读取，而不必依赖 gin transport context。

## 当前隔离语义

- 管理面与服务面请求已经可以带 `tenant_id` / `workspace_id` 上下文
- 管理面与服务面受保护路由现已统一拦截显式 scope 选择器：
  - `agent` / `end_user` 不允许在请求中额外声明超出 token 的 `tenant_id` / `workspace_id`
  - `admin` / `service` 仅可在 token 未显式 scope 时通过请求窄化作用域
  - header / query 中若出现互相冲突的 scope 值，直接拒绝请求
- 审计日志会记录这两个字段
- 以下对象已开始显式 scope 化并默认按上下文过滤：
  - `KnowledgeDoc`
  - `CustomField`
  - `SLAConfig`
  - `SLAViolation`
  - `AppIntegration`
  - `ShiftSchedule`
  - `Macro`
  - `CustomerSatisfaction` / `SatisfactionSurvey`
  - `SuggestionService` 的候选查询
- 以下会话链路对象现已补显式 scope 字段，并在 `conversation/routing` 仓储默认按上下文过滤：
  - `Session`
  - `Message`
  - `TransferRecord`
  - `WaitingRecord`
- `Ticket` 现已补显式 scope 字段，并在 `ticket` 仓储的主查询、统计与创建路径按上下文过滤
- `Customer` 现已补显式 scope 字段；`customer` 仓储通过 `customers` 扩展表 join 收口用户读取、列表、统计与活动查询
- `Agent` 现已补显式 scope 字段，并在 `agent` 仓储的创建、读取、列表与统计路径按上下文过滤
- `WorkspaceService`、`analytics` 模块聚合仓储、`gamification` 排行榜聚合、agent transfer load 同步路径已开始按上下文过滤已 scope 化主数据
  - `customer` 模块的 activity 聚合链路现已补回归测试，确认 `recent_sessions` / `recent_tickets` / `recent_messages` 三条历史视图不会因 legacy join 把其它 workspace 的数据混入当前客户时间线
  - `WorkspaceService.GetOverview` 的 `recent_sessions` 关联链现已要求 `sessions -> tickets -> customers -> agents` 保持同一 `tenant_id/workspace_id`，避免 legacy join 因脏外键把其他 workspace 的客户或客服姓名带入当前工作台
- `SLAService.ListSLAViolations`
  - 现已对 `Ticket` / `SLAConfig` preload 继续施加当前 scope，避免 scoped 违约列表因脏外键把其他 workspace 的工单或 SLA 配置详情带回管理面
- `SLAService.CheckSLAViolation` / `CreateSLAViolation`
  - 现已在 service 内重新按当前 scope 校验 `Ticket` 与 `SLAConfig`，不再信任外部传入的 `Ticket` / `SLAViolation` 结构体
  - 避免调用方拿其他 workspace 的工单或配置对象触发违约检测/落库，导致跨 workspace 违约记录写入
- `SLAService.GetSLAStats`
  - 按优先级 / 客户等级统计违约时，现已要求 `sla_violations -> sla_configs` 保持同一 `tenant_id/workspace_id`
  - 避免 scoped 统计因脏 `sla_config_id` 关联把其他 workspace 的优先级或客户等级维度带入当前报表
  - `TrendData` 现已补 scoped 回归测试，确认最近 7 天 SLA 合规趋势不会把其它 workspace 的当天工单或违约数混入当前视图
- `SatisfactionService`
  - `CreateSatisfaction` / `GetSatisfaction` / `ListSatisfactions` / `GetSatisfactionByTicket` / `UpdateSatisfaction` 现已统一使用 scope-aware preload
  - `Ticket` / `Customer` 继续按当前 scope 过滤，`Agent` 通过 `agents.user_id` 关联后再校验 `tenant_id/workspace_id`，避免满意度列表与详情回显跨 workspace 的工单、客户或客服身份
  - `ScheduleSurvey` 现已在 service 内重新按当前 scope 校验 `ticket.ID`，避免调用方传入其他 workspace 的 `Ticket` 结构体后越权创建调查
  - `GetSurveyPreviewByToken` 的工单预览也已对 `Agent` preload 继续施加 scope 校验，避免 survey token 预览回显跨 workspace 客服姓名
- `ShiftService.ListShifts`
  - 现已对 `Agent` preload 增加 `agents.user_id` + 当前 scope 校验，避免班次列表把其他 workspace 的客服用户资料回填到当前排班结果
  - `ShiftService.CreateShift` 现已要求目标 `Agent` 本身属于当前 scope，避免跨 workspace 排班写入
- `MacroService.ApplyToTicket`
  - 现已在当前 scope 下确认目标 `Ticket` 存在后才创建评论，避免旧宏服务把宏内容写入其他 workspace 或不存在的工单
- `analytics` 模块现已进一步避免在 scoped dashboard / time-range 请求中直接读取全局 `DailyStats`
  - `GetDashboardStats` 的 `AIUsageToday` / `KnowledgeProviderUsageToday` / `WeKnoraUsageToday` 仅在 system 级请求下消费 `DailyStats`
  - `GetTimeRangeStats` 在 scoped 请求下不再回填全局 `DailyStats.AvgResponseTime` / `CustomerSatisfaction`，其中满意度改为按 scoped `CustomerSatisfaction` 日维度重算，避免跨 tenant/workspace 泄漏
  - `GetAgentPerformanceStats` 现已补 scoped integration test，并把平均解决时长聚合改为按 SQL 方言选择表达式；兼容层 `StatisticsService.GetAgentPerformanceStats` 也已补回归测试，确认 legacy 绩效榜单不会把其它 workspace 的工单汇总进当前报表
  - `GetTicketCategoryStats` / `GetTicketPriorityStats` / `GetCustomerSourceStats` 及其兼容层 `StatisticsService` 入口现已补 scoped 回归测试，确认 legacy 分类/优先级/来源聚合不会混入其它 workspace 的工单或客户维度
- 尚未统一 scope 化的主要剩余面：
  - 仍保留在旧 `services/*` 中、未完全迁移到 modules 的少量兼容层入口，但当前已覆盖到的 legacy 聚合查询都已有 scoped 回归测试，不再是本轮阻塞
  - `DailyStats` 这类跨租户全局聚合表，当前明确按 system 级统计处理；`GetDashboardStats` / `GetTimeRangeStats` 在 scoped 读取时不会消费它，而 `UpdateDailyStats` 也已固定为重算全局主数据，不再受请求 scope 影响

## 下一步建议

1. 将 `T4 configuration-scopes` 作为下一阶段重点，明确系统级与租户级配置边界
2. 若后续需要租户级报表，新增 tenant/workspace 维度的日报或汇总表，而不是直接复用当前 system 级 `DailyStats`
3. 若要继续降低兼容层风险，逐步把旧 `MessageRouter` / 旧 service 聚合路径迁到 modules 边界
