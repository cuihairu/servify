# 坐席工作台与人工接管

面向坐席与管理员的接待面：查看会话、接管、回复、转派、关闭、回看事件流水。
本文覆盖 HTTP API 跟跑链路；管理端（admin SPA）工作台走同一批接口。
访客侧嵌入见 [Web 嵌入集成指南](/embedding-guide)；会话沉淀为工单见
[工单全流程](/ticket-workflow)。

文中接口以仓库当前代码为准（路由注册：
`apps/server/internal/app/server/router_management.go`、
`apps/server/internal/handlers/conversation_workspace_handler.go`、
`apps/server/internal/handlers/session_transfer_handler.go`），示例可直接跟跑。
实时消息推送（WS 通道）不在本文范围，口径见 `apps/demo-sdk/README.md` 的
「当前协议口径」。

## 1. 准备凭据与坐席实体

```sh
cd apps/server && go run ./cmd/server
# 服务地址 http://localhost:8080
```

**首个管理员**：全新实例（用户表为空）时，注册请求可直接带 `role:"admin"`；
实例已有用户后，注册请求里的 `admin` 会落为 `customer`，管理员只能由既有
管理员创建或提权。

```sh
# 1) 全新实例：注册管理员，响应含 token 与 user.id
curl -sS -X POST http://localhost:8080/api/v1/auth/register \
  -H 'Content-Type: application/json' \
  -d '{"username":"admin","email":"admin@example.com","password":"Admin!12345","name":"Admin","role":"admin"}'
```

**坐席账号**两步：注册 `role:"agent"` 的用户，再为该用户建坐席实体
（部门/技能/并发上限，供接管、转派与负载统计使用）：

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

后续示例统一用 `TOKEN`（登录所得）与 `AGENT_ID`（坐席实体 id）：

```sh
# 4) 登录（注册响应已含 token 时可跳过）
curl -sS -X POST http://localhost:8080/api/v1/auth/login \
  -H 'Content-Type: application/json' \
  -d '{"username":"admin","password":"Admin!12345"}'
```

## 2. 工作台总览

```sh
curl -sS -H "Authorization: Bearer $TOKEN" http://localhost:8080/api/omni/workspace
```

响应字段：

| 字段 | 含义 |
| --- | --- |
| `total_active_sessions` | 进行中会话总数 |
| `waiting_queue` | 转人工等待队列长度 |
| `online_agents` / `busy_agents` | 在线/忙碌坐席数 |
| `channels` | 按渠道的会话摘要 |
| `recent_sessions` | 最近会话列表（id/platform/status/agent_id/customer_id 等） |
| `agent_stats` | 坐席负载统计（可选） |

## 3. 转人工等待队列与会话进入方式

访客在聊天里请求转人工后，会话进入等待队列（访客侧收到队列确认帧）。
坐席侧从两个面看到它：

```sh
# 等待队列（可按 status 过滤，如 waiting）
curl -sS -H "Authorization: Bearer $TOKEN" \
  'http://localhost:8080/api/session-transfer/waiting?status=waiting'

# 或看 §2 总览里的 waiting_queue 计数与 recent_sessions
```

坐席侧也可主动把会话转给人工队列（`session_id` 换成实际会话 id）：

```sh
curl -sS -X POST http://localhost:8080/api/session-transfer/to-human \
  -H "Authorization: Bearer $TOKEN" -H 'Content-Type: application/json' \
  -d '{"session_id":"ws_1699000000000","reason":"需要人工核对订单"}'
```

## 4. 接管、回复与历史

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

## 5. 转派

两种口径：会话维度的直接转派，或走转人工队列：

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

## 6. 关闭与事件流水

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

## 7. 权限与审计

`/api` 管理面的统一中间件链（`router_management.go`）：JWT 认证 →
请求级 scope 校验（`X-Tenant-ID`/`X-Workspace-ID` 不得放大 token 自带
scope）→ principal 类型限制（agent/admin/service）→ 审计落库。本文接口
分属两个资源权限：`/api/omni/*` 需要 `workspace`，`/api/session-transfer/*`
需要 `session_transfer`。策略口径见
[接入面与权限策略](/auth-surface-policy)、[审计日志](/audit-log-policy)、
[租户与工作区边界](/tenant-workspace-boundaries)。

## 8. 会话沉淀为工单

会话里解决不了或需要跨天跟进的问题，建工单继续走：
`POST /api/tickets` 可带 `session_id` 关联原会话，坐席在工单里回看
`GET /api/tickets/:id/conversations`。跟跑链路见
[工单全流程](/ticket-workflow)。
