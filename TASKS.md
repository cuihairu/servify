# Servify V1.0 发布判断

> 最后更新: 2026-10-04
> 本文件只记录当前真实发布判断，不维护"全部完成"式总表；切片级状态源是
> [todo.md](./todo.md)，逐项证据链是
> [docs/acceptance-checklist.md](./docs/acceptance-checklist.md)。

## 当前结论

`v1.0.0` 按 [V1.0 收敛改造计划书](./docs/v1-convergence-plan.md) 的口径收口：

- 实施批次 B1（Domain 边界/迁移双路径）✅、B2（Agent Workspace/Routing
  事件统一/PII 与保留策略）✅、B3（Knowledge 产品化/AI 首答持久化与反馈
  闭环）✅ 均已过闸，提交号与过闸证据见 todo.md 各批次登记；
- B4（口径统一与发布）进行中：TASKS.md/验收矩阵/文档站口径统一（本文件
  即 B4-1 的一部分），随后 `v1.0.0` 发布；
- 27 个一级模块不增（架构门禁），薄壳模块已按「核心 7 模块的子能力」
  叙事收口（README 业务模块边界表）。

## V1.0 产品面（收敛后口径）

产品中心是 7 个核心模块：`conversation`（唯一中心聚合）、`routing`、
`ticket`、`ai`、`knowledge`、`agent`、`customer`。`sla`/`satisfaction`/
`custom_field`/`shift`/`macro`/`gamification`/`suggestion`/`quality` 为
薄壳子能力，不再单列产品叙事。冻结面（不新增能力、不进产品文案）：
`api_key`、`email`、`push`、`webhook`、`translation`、`app_integration`、
`auth`（薄壳）、`voice` 及其扩展。

V1.0 收敛新增/收口的能力面（全部带自动化证据，运行证据见验收矩阵）：

- AI 反馈闭环：`POST /api/v1/ai/feedback`（认证即可，访客 guest token
  会话绑定）、ai_answers / answer_feedback 持久化（REST+WS 旁路记录）、
  检索分析读口 `GET /api/v1/ai/retrieval-analytics`；
- Knowledge 产品化：knowledge_sources 来源登记（枚举/引用守卫）、文档
  版本（内容变更自增）、索引任务 HTTP 面（排队/执行/重试/版本关联）、
  admin 管理页（来源/版本/任务抽屉/检索分析）；
- citation 可视化：坐席侧知识建议面板（relevance 渲染）、访客 widget
  sources 引用行 + "Was this helpful?" 反馈条；
- Routing 事件统一：直派接管发 `routing.agent_assigned`、改派发
  `routing.transfer_completed`（统一分配事件路径）；
- PII 与保留策略：导出/擦除数据边界、保留策略过期擦除（B2 gate）。

## 已知限制（如实声明，不阻塞 V1.0 发布口径）

- 外部 Knowledge Provider（RAGFlow/WeKnora/Dify）的 real 模式端到端
  证据依赖真实外部环境凭证；mock/兼容模式已有全链自动化与留痕（验收
  矩阵 §3）。
- 语音（SIP/PSTN/转写）冻结为扩展边界，V1 不投资源。
- 多渠道接入（Telegram/WeCom/WhatsApp 等）是 P2+ 扩展，不在 V1。

## 维护规则（沿用）

1. 以 `todo.md` 和 `docs/acceptance-checklist.md` 作为真实状态源。
2. 只有同时具备代码、自动化、运行、数据四类证据的功能，才允许标记为
   `通过`。
3. `TASKS.md` 不再维护"全部完成"式总表，只记录当前真实发布判断。

## 参考验证命令

```bash
go -C apps/server test -count=1 ./...
go -C apps/server test -count=1 -tags=integration ./internal/...
make build
make release-check CONFIG=./config.yml
make security-check
make observability-check
make local-check
```
