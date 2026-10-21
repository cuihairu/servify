# Ticket End-to-End Workflow

Tickets are the persistence surface after a conversation for "actually solving the problem": creation, assignment, follow-up comments, status transitions, closing, statistics, and export. This document covers the agent/admin HTTP API walkthrough path; for the visitor-side no-login ticket-creation entry point, see the ticket context section of the Web Embedding Integration Guide.

The interfaces below follow the current repository code (route registration: `apps/server/internal/app/server/router_management.go`, contract definitions: `apps/server/internal/modules/ticket/contract/types.go`); the examples can be run as-is.

## 1. Prepare Credentials and Prerequisite Entities

See the Agent Workspace for the rules on starting the service and registering the first admin (on a fresh instance the first registered user can use `role:"admin"` directly; agent entities are registered in two steps). The examples below reuse the `TOKEN` and `AGENT_ID` from there.

A ticket's `customer_id` takes the id of the customer user:

```sh
# 注册客户用户（已有客户账号可跳过，用其 user.id）
curl -sS -X POST http://localhost:8080/api/v1/auth/register \
  -H 'Content-Type: application/json' \
  -d '{"username":"cust1","email":"cust1@example.com","password":"Cust!12345","name":"客户一","role":"customer"}'
# 记下响应里的 user.id，下文写作 CUSTOMER_ID
```

## 2. Create a Ticket

```sh
curl -sS -X POST http://localhost:8080/api/tickets \
  -H "Authorization: Bearer $TOKEN" -H 'Content-Type: application/json' \
  -d '{
    "title": "发票抬头开错",
    "description": "10 月账单发票抬头写错，需要重开。",
    "customer_id": '"$CUSTOMER_ID"',
    "category": "billing",
    "priority": "normal",
    "source": "web",
    "tags": "invoice",
    "session_id": "ws_1699000000000",
    "custom_fields": {"order_no": "SO-1001"}
  }'
# 201，记下响应里的 id，下文写作 TICKET_ID
```

Field notes:

| Field | Required | Description |
| --- | --- | --- |
| `title` | Yes | Ticket title |
| `customer_id` | Yes | Customer user id |
| `description` / `category` / `priority` / `source` / `tags` | No | Description, category, priority, source channel, tags |
| `session_id` | No | Links the original conversation so the agent side can look back (see §4) |
| `custom_fields` | No | Custom fields (field definitions live in the admin-side custom field configuration) |

The visitor-side no-login ticket-creation path uses `POST /api/v1/tickets` (only `session_id` + `title` are needed; category / priority / source take fixed server-side defaults, and custom fields and tags are not exposed to visitors).

## 3. Assignment and Follow-up

```sh
# 指派给坐席并调整优先级/标签（字段均可选，传哪个改哪个）
curl -sS -X PUT http://localhost:8080/api/tickets/$TICKET_ID \
  -H "Authorization: Bearer $TOKEN" -H 'Content-Type: application/json' \
  -d '{"priority":"high","tags":"invoice,oct","agent_id":'"$AGENT_ID"'}'

# 跟进评论
curl -sS -X POST http://localhost:8080/api/tickets/$TICKET_ID/comments \
  -H "Authorization: Bearer $TOKEN" -H 'Content-Type: application/json' \
  -d '{"content":"已联系财务，预计两个工作日重开。"}'

# 查看工单当前状态
curl -sS -H "Authorization: Bearer $TOKEN" \
  http://localhost:8080/api/tickets/$TICKET_ID
```

## 4. Linked Conversations and Status Transitions

Tickets created with a `session_id` can look back at the linked conversation, letting the agent reconstruct the context:

```sh
curl -sS -H "Authorization: Bearer $TOKEN" \
  http://localhost:8080/api/tickets/$TICKET_ID/conversations
```

Status surface: five states — `open` / `assigned` / `in_progress` / `resolved` / `closed` — with the full transition rules in (`ticket/application/status_policy.go`):

- `open` → `assigned` / `resolved` / `closed`
- `assigned` → `open` (can be sent back) / `in_progress` / `resolved` / `closed`
- `in_progress` → `open` (can be sent back) / `resolved` / `closed`
- `resolved` / `closed` → `closed` (same-state submission is idempotently allowed)

`PUT` changing `status` follows the transitions; use the close endpoint to close a ticket (it records the close reason):

```sh
curl -sS -X PUT http://localhost:8080/api/tickets/$TICKET_ID \
  -H "Authorization: Bearer $TOKEN" -H 'Content-Type: application/json' \
  -d '{"status":"resolved"}'

curl -sS -X POST http://localhost:8080/api/tickets/$TICKET_ID/close \
  -H "Authorization: Bearer $TOKEN" -H 'Content-Type: application/json' \
  -d '{"reason":"发票已重开并寄出"}'
```

Batch operations (status, tags, assignment):

```sh
curl -sS -X POST http://localhost:8080/api/tickets/bulk \
  -H "Authorization: Bearer $TOKEN" -H 'Content-Type: application/json' \
  -d '{"ticket_ids":[1,2],"status":"resolved","add_tags":"batch-a"}'
# 响应含 updated（成功 id 列表）与 failed（逐条失败原因）
```

## 5. List Search, Statistics, and Export

```sh
# 列表：分页 + 多条件过滤 + 排序 + 自定义字段过滤（cf.前缀）
curl -sS -H "Authorization: Bearer $TOKEN" \
  'http://localhost:8080/api/tickets?page=1&page_size=20&status=open&priority=high&agent_id=2&search=发票&sort_by=created_at&sort_order=desc&cf.order_no=SO-1001'

# 统计：总量/今日新建/待处理/已解决/按状态与优先级分布
curl -sS -H "Authorization: Bearer $TOKEN" \
  http://localhost:8080/api/tickets/stats

# 导出 CSV
curl -sS -H "Authorization: Bearer $TOKEN" \
  'http://localhost:8080/api/tickets/export?limit=1000' -o tickets.csv
```

List filter parameters: `status`/`priority`/`category`/`source` (multi-value allowed), `tag`, `agent_id`, `customer_id`, `search`, `page`/`page_size`, `sort_by`/`sort_order`, plus `cf.<字段名>=<值>` custom field filtering.

## 6. Permissions and Audit

The ticket surface sits inside the unified middleware chain of the `/api` management surface (JWT → scope check → principal kind → audit persistence), with `tickets` as the resource permission. For the policy convention see Auth Surface Policy and Audit Log Policy; for conversation operations during an engagement see the Agent Workspace.
