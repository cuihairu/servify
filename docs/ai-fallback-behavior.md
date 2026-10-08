# AI Fallback Behavior

当前 AI 编排层的降级规则如下：

1. 输入命中 guardrail 时，直接拒绝并计入 fallback 指标。
2. knowledge retrieval 失败时，`QueryOrchestrator` 返回错误；上层 `OrchestratedEnhancedAIService` 切到 legacy fallback（恒开启，无配置开关；`fallback.enabled` 配置作用于知识源级降级，见 `internal/app/server/knowledge_source.go`）。
3. LLM provider 失败时，`OrchestratedEnhancedAIService` 也会走 fallback 路径。
4. retrieval 成功但没有 sources 时，仍然允许 LLM 正常回答，响应策略标记为 `llm`（prom 口径记 `primary`）；`fallback` 专指编排失败走 legacy 兜底。

这份行为文档说明当前 provider 切换和故障降级边界，不冻结实现细节；后续如引入 retrieval soft-degrade，应同步更新这里与测试矩阵。
