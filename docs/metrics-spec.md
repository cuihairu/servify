# Metrics Spec

## 1. Session Statistics

| Metric | Definition | Data Source | Calculation |
|------|------|---------|---------|
| Active sessions | Sessions whose status is active | `sessions.status = 'active'` | Real-time count |
| Waiting-for-agent sessions | Sessions whose status is waiting_human (not exposed in the dashboard stats payload — `DashboardStats` has no waiting field) | `sessions.status = 'waiting_human'` | Real-time count |
| Ended sessions | Sessions whose status is ended | `sessions.status = 'ended'` (stored value; the domain-level closed is mapped to ended by conversation/infra) | Data spec; the current DashboardStats contract does not expose this metric |
| Average response time | Average time to the agent's first reply | `agents.avg_response_time` | Average over the agent table |

## 2. Ticket Statistics

| Metric | Definition | Data Source | Calculation |
|------|------|---------|---------|
| Total tickets | All tickets | `tickets` table | Counted over the time range |
| Distribution by status | open/assigned/in_progress/resolved/closed | `tickets.status` | Grouped count |
| Distribution by priority | low/normal/high/urgent | `tickets.priority` | Grouped count |
| Distribution by category | Custom categories | `tickets.category` | Grouped count |
| Ticket creation trend | Aggregated by day/week/month | `tickets.created_at` | Time-series count |

## 3. Satisfaction Statistics

| Metric | Definition | Data Source | Calculation |
|------|------|---------|---------|
| Average rating | Weighted average of all ratings | `customer_satisfactions.rating` | `AVG(rating)` |
| Total ratings | Number of satisfaction records | `customer_satisfactions` | `COUNT(*)` |
| Positive rate | Share of ratings >= 4 (not emitted — removed) | Count of `rating >= 4` / total | — |
| Negative rate | Share of ratings <= 2 (not emitted — removed) | Count of `rating <= 2` / total | — |
| Rating distribution | Count per 1~5 star bucket | `GROUP BY rating` | Distribution histogram |
| Satisfaction trend | Average rating and rating count aggregated by day | `GROUP BY DATE(created_at)` | Time series |

### Rating classification criteria
- Positive: rating >= 4 (4 and 5 stars)
- Neutral: rating = 3 (3 stars)
- Negative: rating <= 2 (1 and 2 stars)

## 4. AI Usage Statistics

| Metric | Definition | Data Source | Calculation |
|------|------|---------|---------|
| AI usage count | Messages/queries handled by AI | `daily_stats.ai_usage_count` | Accumulated by day |
| AI auto-resolution rate | Share of sessions resolved by AI alone (no human handoff) | Inverse of the transfer rate | `(1 - transfer_rate) * 100` |
| AI average response time | Average time for AI replies | `daily_stats` | Aggregated average |

## 5. Agent Performance Statistics

| Metric | Definition | Data Source | Calculation |
|------|------|---------|---------|
| Online agents | Agents whose status is online | `agents.status = 'online'` | Real-time count |
| Busy agents | Agents at max concurrency or with status busy | `agents.status = 'busy'` | Real-time count |
| Agent rating | Average satisfaction score assigned to the agent | `customer_satisfactions` JOIN `tickets` | Grouped by agent, AVG |
| Agent load | Current sessions / max concurrency | `agents.current_load / max_concurrent` | Real-time ratio |

## 6. Time Ranges and Filters

The statistics API family (`/api/statistics/*`, except dashboard) uses the following query parameters. `start_date`/`end_date` are required by the time-range and agent-performance endpoints; the ticket-category and ticket-priority endpoints treat them as optional and default to the last 30 days when omitted; the customer-source endpoint takes no date parameters.

| Parameter | Type | Description |
|------|------|------|
| `start_date` | YYYY-MM-DD | Start date (required by time-range & agent-performance; optional with a default 30-day window for ticket-category & ticket-priority) |
| `end_date` | YYYY-MM-DD | End date (same requirement as `start_date`) |
| `tenant_id` | string | Tenant filter (injected automatically from the auth context) |
| `workspace_id` | string | Workspace filter (injected automatically from the auth context) |

The satisfaction API (`/api/satisfactions/stats`) uses:

| Parameter | Type | Description |
|------|------|------|
| `date_from` | YYYY-MM-DD | Start date |
| `date_to` | YYYY-MM-DD | End date |

## 7. Frontend/Backend Field Mapping

### Dashboard API (`/api/statistics/dashboard`)

The backend `DashboardStats` (`modules/analytics/contract/types.go`) is exposed as-is per its json tags; the frontend `stats.<json_tag>` maps one to one:

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
前端 stats.category_stats      ← 后端 CategoryStats (map[category]SatisfactionStat)
前端 stats.trend_data          ← 后端 TrendData ([]SatisfactionTrend)
```
