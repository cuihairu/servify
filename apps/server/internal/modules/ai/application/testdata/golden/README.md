# AI Golden Set

本目录存放 AI 编排链路的 golden 回归用例，由 `golden_test.go` 表驱动执行，
随常规 `go test ./...` 必跑（mock 模式，零网络零 secret）。

## 文件约定

- 一个用例一个 JSON 文件，按 `NN-<name>.json` 命名，`name` 字段必须非空
- `request`：构造 `AIRequest`（task_type/query/model/temperature/system_prompt/retrieval/messages）
- `knowledge_hits`：注入 mock 知识提供方的固定检索结果
- `mock_reply`：mock LLM 的预设回答（real 模式忽略）
- `expect`：断言矩阵
  - `content_contains` / `content_not_contains`：响应内容关键词（片段级，避免脆断言）
  - `source_titles`：来源标题按顺序断言（防检索透传劣化）
  - `prompt_contains` / `prompt_not_contains`：发送给 LLM 的消息内容（仅 mock 模式断言，防 prompt 组装劣化）
  - `handoff`：同请求下 `ShouldTransferToHuman` 的期望值
  - `error_contains`：非空时期望 Handle 返回错误并包含这些子串

## 断言语义

- `request.model` / `request.temperature` 会与 mock provider 收到的 `ChatRequest`
  逐字段比对（防透传劣化）；缺省即断言零值不写入请求体（provider 默认兜底）
- real 模式跳过 `prompt_contains` 断言（真实 provider 不回放请求），
  其余断言照常执行

## 运行方式

```bash
# mock 模式（默认，CI 必跑）
make test-golden

# real 模式（需要 OPENAI_API_KEY，LLM 真实调用，知识检索仍用 fixture）
SERVIFY_GOLDEN_MODE=real OPENAI_API_KEY=sk-... ./scripts/run-golden-tests.sh
```

## 新增用例

1. 新建 `NN-<name>.json`，name 全目录唯一
2. 关键词断言只锚定知识片段或 mock 回答里的稳定子串，不断言整句
3. real 模式下会被真实模型回答的用例，`content_contains` 需选取回答必含的词
