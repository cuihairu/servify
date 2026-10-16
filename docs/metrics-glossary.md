# Servify Statistical Metric Definitions (Metrics Glossary)

> Last updated: 2026-03-31

This document defines the calculation conventions for all statistical metrics in the Dashboard and Analytics modules, ensuring frontend and backend share the same understanding.

---

## 1. Dashboard Core Metrics

| Metric name | English identifier | Calculation convention | Data source |
|--------|----------|----------|--------|
| Total customers | `total_customers` | `COUNT(*) FROM users WHERE role='customer'`, subject to tenant/workspace isolation | `users` table |
| Total agents | `total_agents` | `COUNT(*) FROM agents`, subject to tenant/workspace isolation | `agents` table |
| Total tickets | `total_tickets` | `COUNT(*) FROM tickets` | `tickets` table |
| Total sessions | `total_sessions` | `COUNT(*) FROM sessions` | `sessions` table |
| Today's tickets | `today_tickets` | `COUNT(*) FROM tickets WHERE created_at >= 今日0时` | `tickets` table |
| Today's sessions | `today_sessions` | `COUNT(*) FROM sessions WHERE created_at >= 今日0时` | `sessions` table |
| Today's messages | `today_messages` | `COUNT(*) FROM messages WHERE created_at >= 今日0时` | `messages` table |
| Open tickets | `open_tickets` | `COUNT(*) FROM tickets WHERE status='open'` | `tickets` table |
| Assigned tickets | `assigned_tickets` | `COUNT(*) FROM tickets WHERE status='assigned'` | `tickets` table |
| Resolved tickets | `resolved_tickets` | `COUNT(*) FROM tickets WHERE status='resolved'` | `tickets` table |
| Closed tickets | `closed_tickets` | `COUNT(*) FROM tickets WHERE status='closed'` | `tickets` table |
| Online agents | `online_agents` | `COUNT(*) FROM agents WHERE status='online'` | `agents` table |
| Busy agents | `busy_agents` | `COUNT(*) FROM agents WHERE status='busy'` | `agents` table |
| Active sessions | `active_sessions` | `COUNT(*) FROM sessions WHERE status='active'` | `sessions` table |
| Average response time | `avg_response_time` | `AVG(avg_response_time) FROM agents` (seconds) | `agents` table |
| Average resolution time | `avg_resolution_time` | `AVG(EXTRACT(epoch FROM (resolved_at - created_at))) FROM tickets WHERE resolved_at IS NOT NULL` (seconds) | `tickets` table |
| Customer satisfaction | `customer_satisfaction` | `COALESCE(AVG(rating), 0) FROM customer_satisfactions` (1–5 scale) | `customer_satisfactions` table |
| AI usage (today) | `ai_usage_today` | `daily_stats.ai_usage_count WHERE date=今日` | `daily_stats` table |

## 2. Time-Range Statistics

**Endpoint**: `GET /api/statistics/time-range?start_date=YYYY-MM-DD&end_date=YYYY-MM-DD`

The following metrics are aggregated daily:

| Metric name | English identifier | Calculation convention |
|--------|----------|----------|
| Ticket count | `tickets` | `COUNT(*) FROM tickets WHERE created_at >= ? AND created_at < next_day` |
| Session count | `sessions` | `COUNT(*) FROM sessions WHERE created_at >= ? AND created_at < next_day` |
| Message count | `messages` | `COUNT(*) FROM messages WHERE created_at >= ? AND created_at < next_day` |
| Resolved tickets | `resolved_tickets` | `COUNT(*) FROM tickets WHERE resolved_at >= ? AND resolved_at < next_day` |
| Average response time | `avg_response_time` | `AVG(avg_response_time) FROM agents` (global average, not per day) |
| Customer satisfaction | `customer_satisfaction` | `COALESCE(AVG(rating), 0) FROM customer_satisfactions WHERE created_at >= ? AND created_at < next_day` |

> **Note**: `avg_response_time` uses the global average (the agents table) in time-range statistics, because there is currently no per-message response time record.

## 3. Agent Performance Statistics

**Endpoint**: `GET /api/statistics/agent-performance?start_date=...&end_date=...&limit=N`

| Metric name | English identifier | Calculation convention |
|--------|----------|----------|
| Agent ID | `agent_id` | `agents.user_id` |
| Agent name | `agent_name` | `users.name` |
| Department | `department` | `agents.department` |
| Total tickets | `total_tickets` | `COUNT(tickets.id) WHERE tickets.agent_id = agents.user_id AND tickets.created_at 在范围内` |
| Resolved tickets | `resolved_tickets` | `COUNT(CASE WHEN status IN ('resolved','closed'))` |
| Average response time | `avg_response_time` | `agents.avg_response_time` (global value, seconds) |
| Average resolution time | `avg_resolution_time` | `AVG(EXTRACT(epoch FROM (resolved_at - created_at))) WHERE resolved_at IS NOT NULL` (seconds) |
| Rating | `rating` | `agents.rating` |

## 4. Category Statistics

### Ticket category statistics
**Endpoint**: `GET /api/statistics/ticket-category?start_date=...&end_date=...`

Grouped by `tickets.category` with `COUNT(*)`, sorted in descending order.

### Ticket priority statistics
**Endpoint**: `GET /api/statistics/ticket-priority?start_date=...&end_date=...`

Grouped by `tickets.priority` with `COUNT(*)`, sorted in descending order.

### Customer source distribution
**Endpoint**: `GET /api/statistics/customer-source`

Grouped by `customers.source` with `COUNT(*)`, sorted in descending order.

## 5. Daily Stats

Stored via the `daily_stats` table, updated on a schedule by a Worker or incremented in an event-driven fashion.

> Note: the "increment events" below are `IncrementKind` constants (`modules/analytics/application/types.go`), all written through the single repository method `IncrementDailyStat` (dispatched to the corresponding column by Kind); they are not eight separate methods.

| Field | Description | Increment event |
|------|------|----------|
| `total_sessions` | Sessions that day | `IncrementSessions` |
| `total_messages` | Messages that day | `IncrementMessages` |
| `total_tickets` | Tickets that day | `IncrementTickets` |
| `resolved_tickets` | Tickets resolved that day | `IncrementResolved` |
| `ai_usage_count` | AI usage count | `IncrementAIUsage` |
| (no separate column) | External knowledge provider usage count: there is no separate `daily_stats` column; `IncrementKnowledgeProvider` and `IncrementWeKnora` are merged into `we_knora_usage_count`; `knowledge_provider_usage_count` appears only in the `AIMetrics` response JSON (`modules/ai/delivery/contract_types.go:39`) and is not a field of this table | `IncrementKnowledgeProvider` |
| `we_knora_usage_count` | WeKnora usage count | `IncrementWeKnora` |
| `sla_violations` | SLA violation count | `IncrementSLA` |

## 6. Multi-Tenant Isolation Rules

All statistics queries are constrained by `tenant_id` and `workspace_id`:
- **Entity tables** (tickets, sessions, messages, agents, customer_satisfactions): direct `WHERE tenant_id = ? AND workspace_id = ?`
- **User tables** (users + customers): tenant information obtained by JOINing the `customers` table
- Null values mean no restriction (the admin view)

## 7. Known Limitations

1. **Response time granularity**: `avg_response_time` is currently stored in the `agents` table as a globally accumulated value and cannot be pinned to a specific time range
2. **Satisfaction rating**: based on `customer_satisfactions.rating` (1–5 scale); returns 0 when there is no data
3. **Daily Stats updates**: rely on the Worker or the event bus being triggered; not real-time exact
