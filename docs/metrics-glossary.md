# Servify 统计口径定义（Metrics Glossary）

> 最后更新：2026-03-31

本文档定义 Dashboard 和 Analytics 模块中所有统计指标的计算口径，确保前后端理解一致。

---

## 1. Dashboard 核心指标

| 指标名 | 英文标识 | 计算口径 | 数据源 |
|--------|----------|----------|--------|
| 总客户数 | `total_customers` | `COUNT(*) FROM users WHERE role='customer'`，受租户/工作空间隔离 | `users` 表 |
| 总客服数 | `total_agents` | `COUNT(*) FROM agents`，受租户/工作空间隔离 | `agents` 表 |
| 总工单数 | `total_tickets` | `COUNT(*) FROM tickets` | `tickets` 表 |
| 总会话数 | `total_sessions` | `COUNT(*) FROM sessions` | `sessions` 表 |
| 今日工单 | `today_tickets` | `COUNT(*) FROM tickets WHERE created_at >= 今日0时` | `tickets` 表 |
| 今日会话 | `today_sessions` | `COUNT(*) FROM sessions WHERE created_at >= 今日0时` | `sessions` 表 |
| 今日消息 | `today_messages` | `COUNT(*) FROM messages WHERE created_at >= 今日0时` | `messages` 表 |
| 开放工单 | `open_tickets` | `COUNT(*) FROM tickets WHERE status='open'` | `tickets` 表 |
| 已分配工单 | `assigned_tickets` | `COUNT(*) FROM tickets WHERE status='assigned'` | `tickets` 表 |
| 已解决工单 | `resolved_tickets` | `COUNT(*) FROM tickets WHERE status='resolved'` | `tickets` 表 |
| 已关闭工单 | `closed_tickets` | `COUNT(*) FROM tickets WHERE status='closed'` | `tickets` 表 |
| 在线客服 | `online_agents` | `COUNT(*) FROM agents WHERE status='online'` | `agents` 表 |
| 忙碌客服 | `busy_agents` | `COUNT(*) FROM agents WHERE status='busy'` | `agents` 表 |
| 活跃会话 | `active_sessions` | `COUNT(*) FROM sessions WHERE status='active'` | `sessions` 表 |
| 平均响应时间 | `avg_response_time` | `AVG(avg_response_time) FROM agents`（秒） | `agents` 表 |
| 平均解决时间 | `avg_resolution_time` | `AVG(EXTRACT(epoch FROM (resolved_at - created_at))) FROM tickets WHERE resolved_at IS NOT NULL`（秒） | `tickets` 表 |
| 客户满意度 | `customer_satisfaction` | `COALESCE(AVG(rating), 0) FROM customer_satisfactions`（1~5 分） | `customer_satisfactions` 表 |
| AI 使用次数(今日) | `ai_usage_today` | `daily_stats.ai_usage_count WHERE date=今日` | `daily_stats` 表 |

## 2. 时间范围统计

**接口**: `GET /api/statistics/time-range?start_date=YYYY-MM-DD&end_date=YYYY-MM-DD`

每日聚合以下指标：

| 指标名 | 英文标识 | 计算口径 |
|--------|----------|----------|
| 工单数 | `tickets` | `COUNT(*) FROM tickets WHERE created_at >= ? AND created_at < next_day` |
| 会话数 | `sessions` | `COUNT(*) FROM sessions WHERE created_at >= ? AND created_at < next_day` |
| 消息数 | `messages` | `COUNT(*) FROM messages WHERE created_at >= ? AND created_at < next_day` |
| 已解决工单 | `resolved_tickets` | `COUNT(*) FROM tickets WHERE resolved_at >= ? AND resolved_at < next_day` |
| 平均响应时间 | `avg_response_time` | `AVG(avg_response_time) FROM agents`（全局平均，非按日） |
| 客户满意度 | `customer_satisfaction` | `COALESCE(AVG(rating), 0) FROM customer_satisfactions WHERE created_at >= ? AND created_at < next_day` |

> **注意**：`avg_response_time` 在时间范围统计中使用全局平均值（agents 表），因为目前没有按消息级别的响应时间记录。

## 3. 客服绩效统计

**接口**: `GET /api/statistics/agent-performance?start_date=...&end_date=...&limit=N`

| 指标名 | 英文标识 | 计算口径 |
|--------|----------|----------|
| 客服 ID | `agent_id` | `agents.user_id` |
| 客服姓名 | `agent_name` | `users.name` |
| 部门 | `department` | `agents.department` |
| 总工单数 | `total_tickets` | `COUNT(tickets.id) WHERE tickets.agent_id = agents.user_id AND tickets.created_at 在范围内` |
| 已解决工单数 | `resolved_tickets` | `COUNT(CASE WHEN status IN ('resolved','closed'))` |
| 平均响应时间 | `avg_response_time` | `agents.avg_response_time`（全局值，秒） |
| 平均解决时间 | `avg_resolution_time` | `AVG(EXTRACT(epoch FROM (resolved_at - created_at))) WHERE resolved_at IS NOT NULL`（秒） |
| 评分 | `rating` | `agents.rating` |

## 4. 分类统计

### 工单分类统计
**接口**: `GET /api/statistics/ticket-category?start_date=...&end_date=...`

按 `tickets.category` 分组 `COUNT(*)`，降序排列。

### 工单优先级统计
**接口**: `GET /api/statistics/ticket-priority?start_date=...&end_date=...`

按 `tickets.priority` 分组 `COUNT(*)`，降序排列。

### 客户来源分布
**接口**: `GET /api/statistics/customer-source`

按 `customers.source` 分组 `COUNT(*)`，降序排列。

## 5. 每日统计（Daily Stats）

通过 `daily_stats` 表存储，由 Worker 定时更新或事件驱动递增。

> 注：下列「递增事件」为 `IncrementKind` 常量（`modules/analytics/application/types.go`），统一经 `IncrementDailyStat` 单一仓库方法写入（按 Kind 分派到对应列），并非八个独立方法。

| 字段 | 说明 | 递增事件 |
|------|------|----------|
| `total_sessions` | 当日会话数 | `IncrementSessions` |
| `total_messages` | 当日消息数 | `IncrementMessages` |
| `total_tickets` | 当日工单数 | `IncrementTickets` |
| `resolved_tickets` | 当日解决工单数 | `IncrementResolved` |
| `ai_usage_count` | AI 使用次数 | `IncrementAIUsage` |
| `knowledge_provider_usage_count` | 外部知识库 provider 使用次数 | `IncrementKnowledgeProvider` |
| `we_knora_usage_count` | WeKnora 使用次数 | `IncrementWeKnora` |
| `sla_violations` | SLA 违规次数 | `IncrementSLA` |

## 6. 多租户隔离规则

所有统计查询受 `tenant_id` 和 `workspace_id` 约束：
- **实体表**（tickets, sessions, messages, agents, customer_satisfactions）：直接 `WHERE tenant_id = ? AND workspace_id = ?`
- **用户表**（users + customers）：通过 JOIN `customers` 表获取租户信息
- 空值表示不限制（管理员视角）

## 7. 已知限制

1. **响应时间粒度**：当前 `avg_response_time` 存储在 `agents` 表，是全局累计值，无法精确到特定时间范围
2. **满意度评分**：基于 `customer_satisfactions.rating`（1~5 分），无数据时返回 0
3. **Daily Stats 更新**：依赖 Worker 或事件总线触发，非实时精确
