# 11 Tenant Scope Inventory

本文件记录 `11-tenant-auth-and-audit` 在 `T1 tenant-and-workspace-boundaries` 阶段的当前盘点结果。

> 注：本盘点最初成文时核心业务表尚未普遍落 scope 列；**2026-10-08 文档一致性审计已按现码整体重写**——Session/Ticket/Message/Customer/Agent 等核心表均已带 `tenant_id`/`workspace_id` 列与复合索引，workspace/statistics 仓库层均已按请求 context 过滤。当前进展权威口径见 `11-tenant-auth-and-audit.md` T1。

## 当前已确认的 scope 来源

- JWT / auth subject
  - `platform/auth/claims.go`
  - 当前可从 token 中归一化出：
    - `tenant_id`
    - `workspace_id`
    - `token_type`
    - `principal_type`
  - subject / scope 归一化在 `platform/auth.AuthMiddleware` 内部完成：`extractClaims` 将标准化后的 claims（`tenant_id` / `workspace_id` / `roles` 等）写入 gin context；请求级 scope 头（`X-Tenant-ID` / `X-Workspace-ID`）经 `platform/auth.EnforceRequestScope()` 校验，阻止调用方放大或抵触 token 自带的 scope（原 `SubjectFromGin` / `ScopeFromGin` 独立读取入口已随 2026-09-20 死代码清理移除）
  - scope 经 `platform/auth/context.go` 投影进 request context，供仓库层取用（`gin_middleware.go` 注入）

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
  - 但尚未形成统一的"所有关键事件都必须带 tenant"约束

## 当前落库与过滤现状（2026-10-08 按现码核验）

- 核心业务表已普遍带显式 tenant / workspace 列
  - `Session`（`models/models.go:238-239`）、`Ticket`（:132-133）、`Message`（:263-264）、`Customer`（:89-90）、`Agent`（:108-109）等 16+ 模型均带 `TenantID` + `WorkspaceID` 复合索引（`idx_sessions_scope` 等）
  - 数据库层边界已建立，不再只依赖调用约定
  - **残余空白：`User` 表无租户列**（用户主体按设计全局唯一，跨租户登录）

- workspace 工作台已按 scope 过滤
  - `modules/workspace/infra/gorm_repository.go:24-28,87-100`：查询按 request context 的 tenant / workspace scope 过滤
  - `workspace` scope 不能脱离 `tenant` scope、缺省 `global` 视图只允许 internal principal 的两条入口规则仍在入口面固定
  - 注：早期版本的 `scope_enforced` / `scope_warning` 响应字段已不存在于任何契约——隔离已由仓库层过滤实际承接，不再靠「入口透传可见性」声明

- statistics / analytics 已按 scope 过滤
  - `modules/analytics/infra/gorm_repository.go:188-198,316-347`：统计查询按 context scope 过滤
  - 该链路有 `analytics/infra/gorm_repository_scope_integration_test.go` 集成测试锚定（`integration && sqlite_integration` 标签）
  - `DailyStats` 仍是 system 级 read model（`models.go` 内无 scope 字段，与 `current-architecture.md` 口径一致）

- customer 主数据已带租户列
  - `models/models.go:89-90`，create 链路的 scope 入口规则（workspace 不脱离 tenant、global 仅 internal）继续有效

## 当前可回答的问题

- "tenant / workspace scope 现在从哪里来？"
  - 认证 token claims（经 `platform/auth` 标准化 + request context 投影）
  - WeKnora 默认配置
  - knowledge provider request namespace
  - event bus 的可选 tenant 字段

- "哪些核心能力已经开始具备 scope 基础设施？"
  - auth subject / scope 读取
  - knowledge provider namespace 解析
  - event bus tenant 字段预留
  - 核心业务表落库列 + workspace/statistics 仓库层过滤（含集成测试锚）

- "哪些核心能力还没有真正 tenant 隔离？"
  - `User` 主数据（无租户列，按设计全局）
  - `DailyStats` 聚合表（system 级 read model，按口径不落 scope）
  - event bus 事件的 tenant 字段仍是预留，未形成全链强制约束

## 下一步建议

- event bus 全链 tenant 强制：把「所有关键事件必须带 tenant」从预留变成门禁
- knowledge 检索与业务主数据的 scope 语义继续对齐（knowledge 已有 namespace，ticket/conversation/customer 链路已落库列，残余是对账类读面的统一）
