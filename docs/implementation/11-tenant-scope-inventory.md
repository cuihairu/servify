# 11 Tenant Scope Inventory

本文件记录 `11-tenant-auth-and-audit` 在 `T1 tenant-and-workspace-boundaries` 阶段的当前盘点结果。

## 当前已确认的 scope 来源

- JWT / auth subject
  - `platform/auth/claims.go`
  - 当前可从 token 中归一化出：
    - `tenant_id`
    - `workspace_id`
    - `token_type`
    - `principal_type`
  - subject / scope 归一化在 `platform/auth.AuthMiddleware` 内部完成：`extractClaims` 将标准化后的 claims（`tenant_id` / `workspace_id` / `roles` 等）写入 gin context；请求级 scope 头（`X-Tenant-ID` / `X-Workspace-ID`）经 `platform/auth.EnforceRequestScope()` 校验，阻止调用方放大或抵触 token 自带的 scope（原 `SubjectFromGin` / `ScopeFromGin` 独立读取入口已随 2026-09-20 死代码清理移除）

- AI / knowledge provider 默认配置
  - `config.WeKnora.TenantID`
  - `config.WeKnora.KnowledgeBaseID`
  - 默认值目前来自配置，而不是请求级或 workspace 级覆盖

- knowledge provider namespace 解析
  - `platform/knowledgeprovider.ResolveNamespace(defaultTenantID, defaultKnowledgeID, tenantID, knowledgeID)`
  - 当前 namespace 语义是：
    - 先取请求 tenant / knowledge
    - 没有时回退到默认配置
    - 若 knowledge 为空且 tenant 非空，则用 tenant 作为 knowledge id

- event bus
  - `platform/eventbus.BaseEvent`
  - 事件模型已预留 `EventTenantID`
  - 但尚未形成统一的“所有关键事件都必须带 tenant”约束

## 当前已确认的高风险空白

- 核心业务模型尚未普遍具备显式 tenant / workspace 字段
  - 例如 `Session`、`Ticket`、`Message`、`User`、`Agent` 当前主模型路径里没有统一 tenant 列
  - 这意味着当前隔离更多依赖调用约定，而不是数据库层硬边界

- workspace 工作台仍是全局聚合视图
  - `services/workspace_service.go`
  - handler / service 契约现在已显式接收 `auth.Scope`
  - 当前已固定一条基础边界规则：`workspace` scope 不能脱离 `tenant` scope
  - 当前已固定第二条入口规则：缺省 `global` 视图只允许 internal principal；普通 principal 必须带 tenant scope
  - 当前响应已显式返回 `scope_enforced=false` 与 `scope_warning`，防止调用方误把入口 scope 当成数据库层隔离
  - 目前底层实现仍直接聚合全库 session / agent 数据
  - 尚未接入 tenant / workspace scope 过滤条件

- statistics dashboard 仍是全局聚合视图
  - `handlers/statistics_handler.go`
  - `services/statistics_service.go`
  - handler / service 契约现在已显式接收 `auth.Scope`
  - 当前响应已显式返回 `scope`、`scope_enforced=false` 与 `scope_warning`
  - 这条链路当前只固定了 scope 透传与返回可见性
  - 底层 analytics repository 仍按全库聚合，不带 tenant / workspace 过滤条件

- customer create 已开始显式消费写入 scope
  - `handlers/customer_handler.go`
  - `services/customer_service.go`
  - `modules/customer/delivery/handler_adapter.go`
  - handler / service 契约现在已显式接收 `auth.Scope`
  - 当前已固定两条入口规则：
    - `workspace` scope 不能脱离 `tenant`
    - 缺省 `global` create 只允许 internal principal
  - 底层 customer 主数据仍未带 tenant / workspace 列
  - 当前只能说明“写入入口已开始收口 scope 语义”，还不能说明“customer 数据已形成数据库层租户隔离”

- handler 与 service 主路径尚未普遍消费标准 scope
  - auth subject 已标准化
  - 但多数业务 handler / service 还没有把 tenant / workspace 作为查询、写入、导出的必经条件

- knowledge 与业务主数据的 scope 尚未统一
  - knowledge provider 有 namespace 语义
  - 但 ticket / conversation / customer / routing 等业务主数据尚未跟这套 scope 规则对齐

## 当前可回答的问题

- “tenant / workspace scope 现在从哪里来？”
  - 认证 token claims
  - WeKnora 默认配置
  - knowledge provider request namespace
  - event bus 的可选 tenant 字段

- “哪些核心能力已经开始具备 scope 基础设施？”
  - auth subject / scope 读取
  - knowledge provider namespace 解析
  - event bus tenant 字段预留

- “哪些核心能力还没有真正 tenant 隔离？”
  - workspace overview
  - ticket / session / conversation / message 主数据链路
  - 绝大多数管理端查询与写入接口

## 下一步建议

- 先明确 `workspace overview` 的 scope 语义
  - 是 tenant 级聚合
  - 还是 workspace 级聚合
  - 缺省 scope 时是否允许全局视图
  - 当前入口面已经支持传入 scope，下一步重点应转为定义过滤语义本身

- 为一条核心读链路补最小 scope 入口
  - 优先候选：workspace / statistics / knowledge retrieval

- 为一条核心写链路补最小 scope 入口
  - 优先候选：ticket create / customer create / session transfer action
