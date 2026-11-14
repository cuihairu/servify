# 开源解析 / 队列 / LLM SDK 选型实验（2026-11-10）

对应 todo：「对候选开源解析、队列及 LLM SDK 做选型实验：比较许可证、维护、可靠性和自部署依赖；
验收：每类至少两种方案或保留现状的对照、最小实验和明确采纳决定。」

实验环境：本机（Linux，go 1.26.10/toolchain go1.27.3，doggo-redis redis:7-alpine @127.0.0.1:6379）。
实验代码为一次性 throwaway（`/tmp/sdkexp` 独立 go module，不入仓，避免触碰覆盖率/边界门禁），
关键代码与命令内联于本文，可复现。维护性数据来源：GitHub API（gh api，2026-11-10 取样）+
Go module proxy `go list -m -json <mod>@latest`（发布时间以 proxy 为准）。

## 结论速览（明确采纳决定）

| 类别 | 决定 | 核心依据 |
|------|------|---------|
| 文档解析 | **保留现状**（不新增依赖）；若未来落地本地 PDF→文本管线，首选 `ledongthuc/pdf` | 仓内现状 PDF 仅存储不抽取（摄取依赖外部 provider）；两候选最小实验均通过，ledongthuc 更活跃（pushed 2026-10-20） |
| 队列 | **保留现状 outbox**（DB 轮询） | outbox 与领域写同事务、支持 sqlite+postgres 双轨；asynq 实验通过（roundtrip 3ms）但入队与 DB 写不共事务，会割裂可靠投递路径 |
| LLM SDK | **保留现状自写 provider** | 实验实证 `sashabaranov/go-openai` 无法区分「EOF 无 [DONE]」与「正常完成」（两者都是 `Recv()=EOF`），而产品契约（流式取消持久化验收场景 3）依赖该区分 |

## 一、文档解析（PDF 文本抽取）

**现状**：upload 允许 `.pdf/.doc/.docx`（`upload_handler.go:49`）但只做校验+存储，无文本抽取
（go.mod 无 PDF 库；excelize 仅用于 analytics 导出写入）。knowledge `SourceTypes` 含 "pdf"
（`entities.go:63`），本地 pgvector/local 链路的 PDF→文本摄取实际依赖外部 provider（dify/weknora
自行解析文档）。

**候选对照**（同一最小 PDF 上实验，`genpdf.py` 手工生成 604 字节单页 PDF，文本
`Servify acceptance text 12345`，xref 偏移正确）：

| 候选 | 许可证 | 维护 | 最小实验 |
|------|--------|------|---------|
| `github.com/ledongthuc/pdf` | BSD-3-Clause | 631★，pushed 2026-10-20，36 open issues，未归档 | ✅ `pdf.Open` + `Reader.GetPlainText()` → 全文精确命中 `match=true` |
| `github.com/dslipak/pdf` | BSD-3-Clause | 92★，pushed 2024-04-09（约 19 个月未动），4 open issues | ✅ `pdf.Open` + `Page(1).GetPlainText(nil)` → `match=true` |

实验代码（throwaway `/tmp/sdkexp/pdfexp/main.go` 核心）：

```go
f, r, err := ledong.Open(path)          // ledongthuc：返回 *os.File + Reader
rd, err := r.GetPlainText()             // io.Reader，全文
raw, _ := io.ReadAll(rd)

r, err := dslipak.Open(path)            // dslipak：返回 *Reader
got, err := r.Page(1).GetPlainText(nil) // 按页抽取
```

运行输出：

```text
ledongthuc: got="Servify acceptance text 12345" match=true
dslipak: got="Servify acceptance text 12345" match=true
```

**决定**：保留现状——当前没有本地解析需求在排期（PDF 摄取走外部 provider），按依赖卫生不预引依赖。
留档：若落地本地 PDF→文本管线，选 `ledongthuc/pdf`（实验通过 + 活跃维护；dslipak 维护停滞）。

## 二、队列

**现状**：工单可靠事件走 DB outbox（`internal/modules/ticket/outbox` +
`app/worker/ticket_outbox_worker.go`，5s 轮询，`ProcessDueEvents` 退避重试，event_id 全局唯一
去重）。优势：与领域写**同事务**落库（崩溃不丢事件）、sqlite+postgres 双轨都支持。

**候选对照**：

| 候选 | 许可证 | 维护 | 自部署依赖 | 最小实验 |
|------|--------|------|-----------|---------|
| 现状 outbox | 仓内 | 仓内测试覆盖 | 无额外（复用主 DB） | 既有验收（streaming-cancel/timeline 等均依赖） |
| `github.com/hibiken/asynq` | MIT | 13822★，pushed 2026-10-19，293 open issues，活跃 | Redis（生产已强制 redis：`config.Validate` 拒 production+inmemory） | ✅ enqueue→process roundtrip，payload 完整往返，延迟 3ms |
| `riverqueue/river`（旁证，未实验） | MPL-2.0 | 5875★，pushed 2026-11-09，活跃 | **仅 postgres**（sqlite 轨不兼容） | 未实验：许可证 MPL 与双轨不兼容两点已足以出局 |

asynq 实验代码（throwaway `/tmp/sdkexp/asynqexp/main.go`）：`asynq.NewClient(RedisClientOpt{Addr:
"127.0.0.1:6379"})` 入队 `sdkexp:probe`（payload `{"event_id":"evt_1","kind":"ticket.created"}`），
server `HandleFunc` 收到后解包回显。输出：

```text
asynq: enqueued id=4a6035df-... queue=sdkexp
asynq: processed payload=map[event_id:evt_1 kind:ticket.created] latency=3ms
asynq: OK roundtrip
```

**决定**：保留现状 outbox。asynq 技术上可用且延迟优势明显（3ms vs 5s 轮询），但其入队与 DB 领域写
不共事务，恰恰丢掉 outbox 存在的理由（2026-10 的 eventbus replay/reconcile 工作
`52fbb53` 正是在补 at-least-once 缺口）；且 outbox 随 sqlite/postgres 双轨工作，river 仅
postgres。当前没有需要亚秒级任务延迟的需求场景，不引入。

## 三、LLM SDK

**现状**：自写 provider（`internal/platform/llm/{openai,anthropic,local,mock}`，原生 HTTP+SSE）。
产品契约：连接 EOF 而无 `[DONE]`（模型中断）时，orchestrator 把已累积的部分内容 finalize 为
最终回答（流式取消持久化验收场景 3 断言持久化==部分内容）；provider 错误只落日志不进内容 delta。

**候选对照**：

| 候选 | 许可证 | 维护 | 最小实验 |
|------|--------|------|---------|
| `github.com/sashabaranov/go-openai` v1.43.0 | Apache-2.0 | 10811★，pushed 2026-09-29，229 open issues，活跃 | ⚠️ 流式可用，但**无法区分中止与完成**（见下） |
| `openai/openai-go`（官方，旁证） | Apache-2.0 | 官方维护 | 未实验：sashabaranov 的区分度缺陷属 SSE 语义层，官方 SDK 同样不暴露「EOF 无 [DONE]」这一连接层判据 |
| 现状自写 | 仓内 | 仓内测试+验收覆盖（streaming-cancel 11 项证据） | ✅ 场景 3 实证：partial finalize、无错误细节泄漏 |

实验代码（throwaway `/tmp/sdkexp/openaiexp/main.go`）：本地 SSE mock 两模式——`done`（2 delta +
`data: [DONE]`）与 `abort`（2 delta 后裸关连接、无 `[DONE]`），用 sashabaranov 客户端
（`cfg.BaseURL` 指向 mock）`CreateChatCompletionStream` 循环 `Recv()` 累积 delta。输出：

```text
[done] accumulated="你好，世界" recvErr=EOF
[abort] accumulated="部分回答" recvErr=EOF
```

两种情形调用面完全一致（都是 `Recv()` 返回 EOF），调用方无从得知模型是正常收尾还是中途断连——
若迁移到该 SDK，中断的部分回答会被当完整回答直接落库/下发，正是流式取消验收专门要防的错误。

**决定**：保留现状自写 provider。唯一候选的调用面语义与产品契约不兼容（EOF 判据丢失），收益
（少维护一个 provider 包）不抵契约损失。若未来官方/社区 SDK 在 API 面区分「stream ended without
[DONE]」，可重开选型。

## 附：实验可复现命令

```bash
mkdir -p /tmp/sdkexp && cd /tmp/sdkexp && go mod init sdkexp
go get github.com/ledongthuc/pdf@latest github.com/dslipak/pdf@latest \
       github.com/hibiken/asynq@latest github.com/sashabaranov/go-openai@latest
python3 genpdf.py sample.pdf && go run ./pdfexp sample.pdf
go run ./asynqexp          # 需本机 redis 127.0.0.1:6379
go run ./openaiexp         # 自带 SSE mock，无外部依赖
```
