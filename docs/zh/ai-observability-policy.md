# AI Observability And Policy Hooks

当前 AI 编排层已预留以下控制点：

- tracing span 标签：`ai.task_type`、`ai.retrieval.enabled`、`ai.provider`、`ai.tokens.total`、`ai.latency.ms`
- module metrics：provider usage、provider error、policy rejection、last error category
- `PolicyHook`：可接入内容安全、租户策略、拒答规则
- `PromptAuditRecorder`：可记录 prompt version、任务类型、消息数量、检索命中数

当前这些接口是 contract-first 预留，默认不绑定外部审计或策略系统。
