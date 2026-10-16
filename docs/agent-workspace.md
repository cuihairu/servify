# Agent Workbench And Manual Takeover

The reception surface for agents and admins: viewing sessions, takeover, replying, transfer, close, and reviewing the event timeline.
This document covers the HTTP API walk-through path; the admin console (admin SPA) workbench uses the same set of interfaces.
Guest-side embedding see the [Web embedding integration guide](/embedding-guide); sessions distilled into tickets see the
[full ticket workflow](/ticket-workflow).

The interfaces in this document are based on the repo's current code (route registration:
`apps/server/internal/app/server/router_management.go`,
`apps/server/internal/handlers/conversation_workspace_handler.go`,
`apps/server/internal/handlers/session_transfer_handler.go`), and the examples can be walked through directly.
Real-time message push (WS channel) is out of scope for this document; the spec see the
"current protocol spec" in `apps/demo-sdk/README.md`.

## 1. Prepare credentials and the agent entity

```sh
cd apps/server && go run ./cmd/server
# 服务地址 http://localhost:8080
```

**First admin**: on a fresh instance (user table empty), a register request can carry `role:"admin"` directly;
once the instance already has users, an `admin` in a register request lands as `customer`; admins can only be
created or promoted by existing admins.

```sh
# 1) 全新实例：注册管理员，响应含 token 与 user.id
curl -sS -X POST http://localhost:8080/api/v1/auth/register \
  -H 'Content-Type: application/json' \
  -d '{"username":"admin","email":"admin@example.com","password":"Admin!12345","name":"Admin","role":"admin"}'
```

**Agent account** two steps: register a user with `role:"agent"`, then create an agent entity for that user
(department/skills/concurrency cap, used for takeover, transfer, and load stats):

```sh
# 2) 坐席用户
curl -sS -X POST http://localhost:8080/api/v1/auth/register \
  -H 'Content-Type: application/json' \
  -d '{"username":"agent1","email":"agent1@example.com","password":"Agent!12345","name":"坐席一","role":"agent"}'
# 记下响应里的 user.id（下一步的 user_id）

# 3) 坐席实体（管理员 token）
curl -sS -X POST http://localhost:8080/api/agents \
  -H "Authorization: Bearer $ADMIN_TOKEN" -H 'Content-Type: application/json' \
  -d '{"user_id":2,"department":"support","skills":"billing","max_concurrent":5}'
# 记下响应里的 id（坐席实体 id，接管/转派都用它）
```

Subsequent examples uniformly use `TOKEN` (from login) and `AGENT_ID` (the agent entity id):

```sh
# 4) 登录（注册响应已含 token 时可跳过）
curl -sS -X POST http://localhost:8080/api/v1/auth/login \
  -H 'Content-Type: application/json' \
  -d '{"username":"admin","password":"Admin!12345"}'
```

## 2. Workbench overview

```sh
curl -sS -H "Authorization: Bearer $TOKEN" http://localhost:8080/api/omni/workspace
```

Response fields:

| Field | Meaning |
| --- | --- |
| `total_active_sessions` | Total in-progress sessions |
| `waiting_queue` | Queue-for-human length |
| `online_agents` / `busy_agents` | Online/busy agent counts |
| `channels` | Per-channel session summaries |
| `recent_sessions` | Recent session list (id/platform/status/agent_id/customer_id etc.) |
| `agent_stats` | Agent load stats (optional) |

## 3. Queue for human and session entry

After a guest requests human transfer in chat, the session enters the waiting queue (the guest side receives a queue confirmation frame).
The agent side sees it from two surfaces:

```sh
# 等待队列（可按 status 过滤，如 waiting）
curl -sS -H "Authorization: Bearer $TOKEN" \
  'http://localhost:8080/api/session-transfer/waiting?status=waiting'

# 或看 §2 总览里的 waiting_queue 计数与 recent_sessions
```

The agent side can also actively transfer a session to the human queue (`session_id` replaced with the actual session id):

```sh
curl -sS -X POST http://localhost:8080/api/session-transfer/to-human \
  -H "Authorization: Bearer $TOKEN" -H 'Content-Type: application/json' \
  -d '{"session_id":"ws_1699000000000","reason":"需要人工核对订单"}'
```

## 4. Takeover, reply, and history

```sh
# 1) 接管：把会话指派给自己（或某坐席）
curl -sS -X POST http://localhost:8080/api/omni/sessions/ws_1699000000000/assign \
  -H "Authorization: Bearer $TOKEN" -H 'Content-Type: application/json' \
  -d '{"agent_id":'"$AGENT_ID"'}'

# 2) 坐席回复（访客侧实时收到）
curl -sS -X POST http://localhost:8080/api/omni/sessions/ws_1699000000000/messages \
  -H "Authorization: Bearer $TOKEN" -H 'Content-Type: application/json' \
  -d '{"content":"您好，退货政策是 7 天无理由，请提供订单号。"}'

# 3) 会话详情与历史消息（limit 上限 200，before 游标翻页）
curl -sS -H "Authorization: Bearer $TOKEN" \
  'http://localhost:8080/api/omni/sessions/ws_1699000000000/messages?limit=50'
```

## 5. Transfer

Two specs: direct transfer at the session dimension, or going through the queue-for-human:

```sh
# 直接转派给另一坐席（to_agent_id 为坐席实体 id）
curl -sS -X POST http://localhost:8080/api/omni/sessions/ws_1699000000000/transfer \
  -H "Authorization: Bearer $TOKEN" -H 'Content-Type: application/json' \
  -d '{"to_agent_id":3}'

# 或指定坐席转接（离线坐席会被拒绝）
curl -sS -X POST http://localhost:8080/api/session-transfer/to-agent \
  -H "Authorization: Bearer $TOKEN" -H 'Content-Type: application/json' \
  -d '{"session_id":"ws_1699000000000","target_agent_id":3,"reason":"账务问题转专席"}'
```

## 6. Close and event timeline

```sh
# 关闭会话
curl -sS -X POST http://localhost:8080/api/omni/sessions/ws_1699000000000/close \
  -H "Authorization: Bearer $TOKEN"

# 事件流水（接线/转人工/接管/消息/关闭等投影记录，只读）
curl -sS -H "Authorization: Bearer $TOKEN" \
  'http://localhost:8080/api/omni/sessions/ws_1699000000000/timeline?limit=100'

# 转人工处理历史
curl -sS -H "Authorization: Bearer $TOKEN" \
  http://localhost:8080/api/session-transfer/history/ws_1699000000000
```

## 7. Permissions and audit

The unified middleware chain for the `/api` management plane (`router_management.go`): JWT auth →
request-level scope validation (`X-Tenant-ID`/`X-Workspace-ID` must not amplify the token's own
scope) → principal type restriction (agent/admin/service) → audit persistence. The interfaces in this
document belong to two resource permissions: `/api/omni/*` requires `workspace`, `/api/session-transfer/*`
requires `session_transfer`. The policy spec see
[reception surface and permission policy](/auth-surface-policy), [audit log](/audit-log-policy),
[tenant and workspace boundaries](/tenant-workspace-boundaries).

## 8. Sessions distilled into tickets

Issues that cannot be resolved in the session or need cross-day follow-up, create a ticket to continue:
`POST /api/tickets` can carry `session_id` to associate the original session, the agent reviews it in the ticket
`GET /api/tickets/:id/conversations`. The walk-through path see the
[full ticket workflow](/ticket-workflow).
