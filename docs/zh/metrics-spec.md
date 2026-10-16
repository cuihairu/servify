# 统计口径文档

## 1. 会话统计

| 指标 | 定义 | 数据来源 | 计算口径 |
|------|------|---------|---------|
| 活跃会话数 | 状态为 active 的会话 | `sessions.status = 'active'` | 实时计数 |
| 等待客服数 | 状态为 waiting_human 的会话 | `sessions.status = 'waiting_human'` | 实时计数 |
| 已结束会话数 | 状态为 ended 的会话 | `sessions.status = 'ended'`（落库值；domain 层 closed 经 conversation/infra 映射为 ended） | 数据口径；当前 DashboardStats 契约未透出该指标 |
| 平均响应时间 | 客服首次回复的平均耗时 | `agents.avg_response_time` | 取 agent 表平均值 |

## 2. 工单统计

| 指标 | 定义 | 数据来源 | 计算口径 |
|------|------|---------|---------|
| 工单总数 | 所有工单 | `tickets` 表 | 按时间范围计数 |
| 按状态分布 | open/assigned/in_progress/resolved/closed | `tickets.status` | 分组计数 |
| 按优先级分布 | low/normal/high/urgent | `tickets.priority` | 分组计数 |
| 按分类分布 | 自定义分类 | `tickets.category` | 分组计数 |
| 工单创建趋势 | 按日/周/月聚合 | `tickets.created_at` | 时间序列计数 |

## 3. 满意度统计

| 指标 | 定义 | 数据来源 | 计算口径 |
|------|------|---------|---------|
| 平均评分 | 所有评分的加权平均 | `customer_satisfactions.rating` | `AVG(rating)` |
| 评价总数 | 满意度记录条数 | `customer_satisfactions` | `COUNT(*)` |
| 好评率 | 评分 >= 4 的占比 | `rating >= 4` 的计数 / 总数 | `positive_rate` |
| 差评率 | 评分 <= 2 的占比 | `rating <= 2` 的计数 / 总数 | `negative_rate` |
| 评分分布 | 1~5 星各档计数 | `GROUP BY rating` | 分布直方图 |
| 满意度趋势 | 按日聚合的平均评分和评价数 | `GROUP BY DATE(created_at)` | 时间序列 |

### 评分分类标准
- 好评：rating >= 4（4 星和 5 星）
- 中评：rating = 3（3 星）
- 差评：rating <= 2（1 星和 2 星）

## 4. AI 使用量统计

| 指标 | 定义 | 数据来源 | 计算口径 |
|------|------|---------|---------|
| AI 使用次数 | AI 处理的消息/查询数 | `daily_stats.ai_usage_count` | 按日累加 |
| AI 自动解决率 | AI 独立解决（未转人工）的会话占比 | 转人工率倒数 | `(1 - transfer_rate) * 100` |
| AI 平均响应时间 | AI 回复的平均耗时 | `daily_stats` | 聚合平均 |

## 5. 客服绩效统计

| 指标 | 定义 | 数据来源 | 计算口径 |
|------|------|---------|---------|
| 在线客服数 | 状态为 online 的客服 | `agents.status = 'online'` | 实时计数 |
| 忙碌客服数 | 已达最大并发或状态为 busy | `agents.status = 'busy'` | 实时计数 |
| 客服评分 | 分配给客服的满意度平均分 | `customer_satisfactions` JOIN `tickets` | 按客服分组 AVG |
| 客服负载 | 当前处理会话数 / 最大并发数 | `agents.current_load / max_concurrent` | 实时比率 |

## 6. 时间范围与筛选

statistics 族接口（`/api/statistics/*`，除 dashboard 外）要求以下查询参数：

| 参数 | 类型 | 说明 |
|------|------|------|
| `start_date` | YYYY-MM-DD | 起始日期（必填） |
| `end_date` | YYYY-MM-DD | 结束日期（必填） |
| `tenant_id` | string | 租户筛选（从 auth context 自动注入） |
| `workspace_id` | string | 工作区筛选（从 auth context 自动注入） |

满意度接口（`/api/satisfactions/stats`）使用：

| 参数 | 类型 | 说明 |
|------|------|------|
| `date_from` | ISO 8601 | 起始时间 |
| `date_to` | ISO 8601 | 结束时间 |

## 7. 前后端字段映射

### Dashboard API (`/api/statistics/dashboard`)

后端 `DashboardStats`（`modules/analytics/contract/types.go`）按 json tag 原样透出，前端 `stats.<json_tag>` 一一对应：

```
total_customers / total_agents / total_tickets / total_sessions
today_tickets / today_sessions / today_messages
open_tickets / assigned_tickets / resolved_tickets / closed_tickets
online_agents / busy_agents / active_sessions
avg_response_time / avg_resolution_time / customer_satisfaction
ai_usage_today / knowledge_provider_usage_today / weknora_usage_today
```

### Satisfaction Stats API (`/api/satisfactions/stats`)
```
前端 stats.average_rating      ← 后端 AverageRating
前端 stats.total_ratings       ← 后端 TotalRatings
前端 stats.rating_distribution ← 后端 RatingDistribution (map[rating]count)
前端 stats.trend_data          ← 后端 TrendData ([]SatisfactionTrend)
```
