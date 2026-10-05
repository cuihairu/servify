# Servify v1.0.0 Release Notes

`Servify v1.0.0` 是首个正式版本，按
[V1.0 产品与架构收敛改造计划书](./v1-convergence-plan.md) 的口径收口。

## 版本定位

从 v0.1.0（公开预览）到 v1.0.0（收敛发布）：

- 产品中心收敛为 7 个核心模块：`conversation`（唯一中心聚合）、`routing`、
  `ticket`、`ai`、`knowledge`、`agent`、`customer`；薄壳模块按「核心模块
  子能力」叙事，不再单列产品概念；一级模块总数 27 封顶（架构门禁）。
- 每批次过闸制实施：B1（Domain 边界/迁移双路径）→ B2（Routing 事件统一/
  PII 保留策略）→ B3（Knowledge 产品化/AI 反馈回传）→ B4（收口发布），
  切片提交号与过闸证据见 [todo.md](../todo.md)。

## 本版本新增能力（V1.0 收敛面）

- **AI 反馈回传（§5.3）**：AI 首答旁路持久化（`ai_answers`，REST+WS 同源
  hook，失败静默不阻塞作答，来源快照不落内容全文）；`POST /api/v1/ai/feedback`
  坐席/访客双面评价（访客 guest token 强制会话绑定）；`GET /api/v1/ai/
  retrieval-analytics` 检索分析读口（top 问答/无命中率/低置信率/反馈计数）。
- **Knowledge 产品化（§8）**：来源登记（`knowledge_sources`，类型枚举校验
  与删除引用守卫）、文档版本（内容变更自增，索引关联执行时版本）、索引
  任务 HTTP 面（排队/执行/重试/按文档列表）；admin Knowledge 管理页
  （来源/版本/任务抽屉/检索分析展示）。
- **Citation 可视化（§8.4）**：坐席侧知识建议面板 relevance 渲染；访客
  widget 引用行（`📄 标题 · relevance 0.91`）与「Was this helpful?」反馈条。
- **Routing 统一分配事件路径（B2 gate）**：直派接管 `routing.agent_assigned`、
  改派 `routing.transfer_completed`，聚合 `routing:<id>` 与消息投影口径一致。
- **PII 数据边界（B2 gate）**：凭证导出全覆盖、擦除全 PII（文本
  `[已擦除]`）、保留策略过期擦除（幂等）。
- **迁移双路径**：postgres versioned SQL（embed FS 自动发现）+ sqlite
  AutoMigrate，开发/测试无 postgres 可回退。

## 核心能力基线（延续自 v0.1.0 并收口验收）

- 客服主链路：会话工作台、消息收发、人工接管（直派/排队）/转接/关闭
- 工单全流程：建单、状态机、指派、评论、关闭、（SLA/评价挂靠 ticket 子能力）
- 认证与会话：登录/注册、refresh 轮转、会话自服务、TOTP 2FA、OIDC、
  session risk 档位；scope/RBAC/audit/token policy 接入管理面
- AI：QueryOrchestrator + LLMProvider + KnowledgeProvider；知识检索服务
  选择链 ragflow → dify → weknora → pgvector → local（外部 provider 的
  real 模式端到端证据依赖环境凭证，mock/兼容模式已有全链留痕）
- 实时：WebSocket 消息/流式、语音通道（扩展边界）

## 发布门禁（B4 gate，2026-10-04 全绿）

- `make local-check` ✅
- `make release-check CONFIG=./config.yml` ✅（security baseline + 回归
  测试 + build + 迁移 + HTTP 烟测；SQLite 回退形态）
- `make security-check CONFIG=./config.yml`（注入 dev 凭证）✅
- `make observability-check CONFIG=./config.yml` ✅
- 全量单测 + integration 测试全绿：`go -C apps/server test ./...`、
  `go -C apps/server test -tags integration ./internal/...`
- admin `npm run typecheck` + `npm run build` ✅；27 模块不增（架构门禁）✅

## 已知限制（如实声明）

- 外部 Knowledge Provider（RAGFlow/WeKnora/Dify）real 模式端到端证据依赖
  真实外部环境凭证；mock/兼容模式已有全链自动化与留痕。
- 语音（SIP/PSTN/转写）冻结为扩展边界，V1 不投资源。
- 多渠道接入（Telegram/WeCom/WhatsApp 等）为 P2+ 扩展。
- Dependabot 报告 10 个依赖告警（5 high / 5 moderate），见
  GitHub Security 页，不构成 V1.0 发布阻塞。

## 演进方向（V1.0 之后）

- Routing 打分引擎（多因子：skill/language/availability/workload/
  priority/tier/channel/SLA，§6）
- Ticket 生命周期锚定会话的关闭前拦截规则（§7）
- 薄壳模块文档面与叙事完全收口（§1.2 剩余项）