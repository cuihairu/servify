# 工单全流程

工单是会话之后「把问题解决掉」的沉淀面：创建、指派、跟进评论、状态流转、
关闭、统计与导出。本文覆盖坐席/管理员侧 HTTP API 跟跑链路；访客侧免登录
建单入口见 [Web 嵌入集成指南](/embedding-guide)的工单上下文一节。

文中接口以仓库当前代码为准（路由注册：
`apps/server/internal/app/server/router_management.go`、契约定义
`apps/server/internal/modules/ticket/contract/types.go`），示例可直接跟跑。

## 1. 准备凭据与前置实体

起服务与首个管理员的注册规则见
[坐席工作台](/agent-workspace)（全新实例首个注册用户
可直接 `role:"admin"`；坐席实体两步注册）。本文示例沿用那里的 `TOKEN` 与
`AGENT_ID`。

工单的 `customer_id` 取客户用户的 id：

```sh
# 注册客户用户（已有客户账号可跳过，用其 user.id）
curl -sS -X POST http://localhost:8080/api/v1/auth/register \
  -H 'Content-Type: application/json' \
  -d '{"username":"cust1","email":"cust1@example.com","password":"Cust!12345","name":"客户一","role":"customer"}'
# 记下响应里的 user.id，下文写作 CUSTOMER_ID
```

## 2. 创建工单

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

字段说明：

| 字段 | 必填 | 说明 |
| --- | --- | --- |
| `title` | 是 | 工单标题 |
| `customer_id` | 是 | 客户用户 id |
| `description` / `category` / `priority` / `source` / `tags` | 否 | 描述、分类、优先级、来源渠道、标签 |
| `session_id` | 否 | 关联原会话，坐席侧可回看（见 §4） |
| `custom_fields` | 否 | 自定义字段（字段定义见管理端自定义字段配置） |

访客侧免登录建单走 `POST /api/v1/tickets`（仅需 `session_id` + `title`，
分类/优先级/来源由服务端固定默认值，自定义字段与标签不开放给访客）。

## 3. 指派与跟进

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

## 4. 关联会话与状态流转

带 `session_id` 创建的工单可回看关联会话，坐席据此还原上下文：

```sh
curl -sS -H "Authorization: Bearer $TOKEN" \
  http://localhost:8080/api/tickets/$TICKET_ID/conversations
```

状态面：`open` / `assigned` / `in_progress` / `resolved` / `closed` 五态，
完整流转规则（`ticket/application/status_policy.go`）：

- `open` → `assigned` / `resolved` / `closed`
- `assigned` → `open`（可退回）/ `in_progress` / `resolved` / `closed`
- `in_progress` → `open`（可退回）/ `resolved` / `closed`
- `resolved` / `closed` → `closed`（同态提交幂等放行）

`PUT` 改 `status` 走流转；收单用关闭端点（记录关闭原因）：

```sh
curl -sS -X PUT http://localhost:8080/api/tickets/$TICKET_ID \
  -H "Authorization: Bearer $TOKEN" -H 'Content-Type: application/json' \
  -d '{"status":"resolved"}'

curl -sS -X POST http://localhost:8080/api/tickets/$TICKET_ID/close \
  -H "Authorization: Bearer $TOKEN" -H 'Content-Type: application/json' \
  -d '{"reason":"发票已重开并寄出"}'
```

批量操作（状态、标签、指派）：

```sh
curl -sS -X POST http://localhost:8080/api/tickets/bulk \
  -H "Authorization: Bearer $TOKEN" -H 'Content-Type: application/json' \
  -d '{"ticket_ids":[1,2],"status":"resolved","add_tags":"batch-a"}'
# 响应含 updated（成功 id 列表）与 failed（逐条失败原因）
```

## 5. 列表检索、统计与导出

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

列表过滤参数：`status`/`priority`/`category`/`source`（可多值）、`tag`、
`agent_id`、`customer_id`、`search`、`page`/`page_size`、`sort_by`/
`sort_order`，以及 `cf.<字段名>=<值>` 自定义字段过滤。

## 6. 权限与审计

工单面在 `/api` 管理面统一中间件链内（JWT → scope 校验 → principal 类型
→ 审计落库），资源权限为 `tickets`。策略口径见
[接入面与权限策略](/auth-surface-policy)、[审计日志](/audit-log-policy)。
接待过程中的会话操作见 [坐席工作台](/agent-workspace)。
