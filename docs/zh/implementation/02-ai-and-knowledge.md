# 02 AI And Knowledge

> 状态（2026-10-04 文档对账）：A1–A8 任务清单已全部完成（`[x]`）。V1.0 收敛
> B3 新增的 AI 首答持久化（`ai_answers`）、反馈回传（`POST /api/v1/ai/feedback`
> 会话绑定）、检索分析（`GET /api/v1/ai/retrieval-analytics`）与 Knowledge
> 来源登记/文档版本/索引任务 HTTP 面不在本任务包内，状态与证据以
> [验收矩阵 §14](../acceptance-checklist.md) 为准。工具调用遗留项（Port/
> PermissionChecker 生产装配为 `nil`）见 [13-ai-agent-loop.md](./13-ai-agent-loop.md)。

范围：

- llm provider
- knowledge provider
- ai orchestrator
- prompt builder
- retriever
- tools
- metrics
- knowledge indexing

## A1 provider contracts

- [x] 创建 `internal/platform/llm`
- [x] 定义 `ChatRequest`
- [x] 定义 `ChatResponse`
- [x] 定义 `TokenUsage`
- [x] 定义 `LLMProvider`
- [x] 创建 `internal/platform/knowledgeprovider`
- [x] 定义 `SearchRequest`
- [x] 定义 `KnowledgeHit`
- [x] 定义 `KnowledgeDocument`
- [x] 定义 `KnowledgeProvider`

验收：

- provider 接口不暴露 OpenAI、Dify、WeKnora compatibility 私有 DTO

## A2 provider adapters

- [x] 新增 `llm/openai/provider.go`
- [x] 迁移模型 HTTP 调用逻辑
- [x] 新增 `llm/mock/provider.go`
- [x] 新增 `knowledgeprovider/dify/provider.go`
- [x] 包装 `pkg/dify`
- [x] 新增 `knowledgeprovider/weknora/provider.go`
- [x] 包装 `pkg/weknora`
- [x] 新增 `knowledgeprovider/mock/provider.go`

验收：

- AI 模块可在 mock provider 下运行，且 runtime 可按 `Dify -> WeKnora compatibility -> fallback` 装配

## A3 ai module skeleton

- [x] 创建 `internal/modules/ai/domain`
- [x] 创建 `internal/modules/ai/application`
- [x] 创建 `internal/modules/ai/infra`
- [x] 创建 `internal/modules/ai/delivery`
- [x] 定义 `AIRequest`
- [x] 定义 `AIResponse`
- [x] 定义 `TaskType`
- [x] 定义 `RetrievalPolicy`
- [x] 定义 `ToolPolicy`

验收：

- AI 模块独立存在

## A4 query orchestrator

- [x] 新增 `query_orchestrator.go`
- [x] 注入 `LLMProvider`
- [x] 注入 `KnowledgeProvider`
- [x] 实现 happy path
- [x] 实现无知识检索 fallback
- [x] 增加 orchestrator 单测

验收：

- 可以替代旧增强 AI 主流程

## A4.1 runtime bridge

- [x] 新增 runtime AI bridge，允许旧运行态逐步切到 `QueryOrchestrator`
- [x] 让旧运行态入口可调用新 `QueryOrchestrator`
- [x] 在标准模式入口 wiring 中逐步替换旧 AI service
- [x] 在增强模式入口 wiring 中逐步替换旧 AI service

验收：

- 不改现有 handler / runtime 对外接口的情况下，可以复用新 AI 模块

## A5 prompt and retrieval

- [x] 新增 `prompt_builder.go`
- [x] 拆 system prompt
- [x] 拆 context prompt
- [x] 拆 knowledge prompt
- [x] 新增 `retriever.go`
- [x] 支持 topK
- [x] 支持 threshold
- [x] 支持 strategy

验收：

- prompt 和 retrieval 从旧 service 中独立出来

## A6 tools

- [x] 定义 `Tool`
- [x] 定义 `ToolRegistry`
- [x] 定义 `ToolExecutor`
- [x] 第一批：
  - [x] ticket lookup tool
  - [x] customer lookup tool
  - [x] handoff tool

验收：

- 工具调用有 schema 和权限检查

## A7 ai guardrails and metrics

- [x] 输入长度限制
- [x] 敏感内容拦截占位
- [x] 输出截断
- [x] query_count
- [x] success_count
- [x] provider_usage_count
- [x] token_usage
- [x] latency
- [x] fallback_count

验收：

- 指标由 orchestrator 统一上报

## A8 knowledge module

- [x] 创建 `internal/modules/knowledge/domain`
- [x] 创建 `internal/modules/knowledge/application`
- [x] 创建 `internal/modules/knowledge/infra`
- [x] 创建 `internal/modules/knowledge/delivery`
- [x] 定义 `Document`
- [x] 定义 `DocumentRepository`
- [x] 定义 `IndexJobRepository`
- [x] 实现 CreateDocument
- [x] 实现 UpdateDocument
- [x] 实现 DeleteDocument
- [x] 实现 QueueIndexJob
- [x] 实现 RunIndexJob

验收：

- 知识库文档和索引成为正式模块
