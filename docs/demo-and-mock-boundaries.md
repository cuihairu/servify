# Demo / Mock / In-Memory Boundaries

这份清单用于回答一个上线前必须说清楚的问题：仓库里哪些 demo、mock、stub、in-memory 能力仍然保留，它们为什么还在，以及什么时候才能退出。

原则：

- demo 资产可以保留，但不能被误判成正式产品面。
- mock / stub 可以用于本地回归、CI 验收或适配器边界测试，但不能拿来宣称外部平台已正式支持。
- in-memory 能力如果承担运行时职责，必须明确它的单进程边界、数据丢失边界和替换条件。

## 仍保留的运行时与交付资产

| 能力/资产 | 位置 | 当前保留原因 | 是否可用于正式部署 | 退出条件 |
| --- | --- | --- | --- | --- |
| Demo 页面与 Demo SDK 静态资源 | `apps/demo/` `apps/demo-sdk/` `apps/server/internal/app/server/static.go` `scripts/sync-sdk-to-demo.sh` | 用于销售演示、SDK 嵌入示例、以及生成物漂移校验；当前还承担“最小可见集成样例”职责 | 否。它们是演示与集成示例，不是正式客户门户，也不是多租户发布面；P3-3 起 production 环境不再服务 `/demo-sdk/*`（请求走 SPA 兜底），仅非 production 环境暴露 | 当正式嵌入式 SDK 文档站、独立示例仓库或正式客户门户替代当前 demo 面时，可把 demo 资源迁出主运行时或改为单独发布 |
| 一键演示与 seed 脚本 | `scripts/demo-setup.sh` `scripts/seed-data.sh` | 用于本地演示、验收准备和快速生成最小业务数据 | 否。默认账号、演示密钥、示例数据都不应进入生产环境 | 当 staging/preview 环境有独立 fixture 流程，且演示数据改为一次性环境初始化任务后，可降级为仅内部工具或迁出仓库主脚本入口 |
| WeKnora mock 服务 | `infra/compose/weknora-mock/` `infra/compose/docker-compose.weknora.yml` | 为 `G1-2` 和本地协议回归提供可重复的上游依赖，避免每次验收都依赖真实 WeKnora 环境 | 否。它只验证协议连通性与 fallback/上传/同步链路，不代表真实检索质量、容量或稳定性 | 当团队具备可重复创建的真实 WeKnora staging 环境，并把验收脚本默认切到 `WEKNORA_ACCEPTANCE_MODE=real` 后，可把 mock 降为兼容测试专用 |
| WeKnora 验收脚本的 `mock` 模式 | `scripts/test-weknora-integration.sh` | 用于在 CI 或本地先验证 Servify 对上游协议的调用约定、证据输出和降级策略 | 条件可用。它适合回归脚本，不适合作为“真实知识库可上线”的唯一证据 | 当 `real` 模式已成为 release gate 的强制前置时，`mock` 模式只保留给开发回归 |
| 进程内事件总线 | `apps/server/internal/platform/eventbus/inmemory_bus.go` | 当前单体运行时内的事件分发默认实现，已经补了 dead letter 与基础指标，足够支撑单进程部署 | 条件可用。仅适用于开发/演示与单实例、允许事件不持久化的部署；P3-3 起 production/staging 的启动校验（`config.InsecureDefaults` 零容忍）与装配层（`bootstrap.BuildEventBus`）都直接拒绝 `inmemory`，必须显式配置 `redis` | 当部署拓扑需要跨实例消费、事件持久化、重放或严格恢复保证时，替换为外部消息队列/总线 |
| 知识库 memory provider | `apps/server/internal/platform/knowledgeprovider/memory/provider.go` | 作为 provider 切换测试与无外部依赖时的本地 fallback，保证 AI/知识链路在无网络环境下仍可回归 | 条件可用。只能视为降级知识源，不能当作正式企业知识库能力 | 当所有目标环境都具备真实知识库 provider，且 release 验收不再接受 memory fallback 作为主证据时，可把它收窄为测试/开发专用 |
| 语音录音/转写 mock provider | `apps/server/internal/modules/voice/provider/mock/` `apps/server/internal/app/server/voice_runtime.go` | Voice call/recording/transcript 已持久化，mock 作为可选 provider 保持链路可回归（真实 Twilio/Deepgram provider 已接入，默认 `disabled`） | 否。mock 只保证命令链路与落库，不提供真实媒体采集或语音识别；production 直接拒绝 mock（P0-2 守卫） | 当录音与转写的真实 provider 验收闭环（配置切换、错误观测与供应商 SLA）后，mock 收窄为测试专用 |
| Telegram / WeChat 渠道路由 stub | `apps/server/internal/platform/realtime/message_router.go` | 保留统一平台适配器接口，便于后续接入真实渠道；当前实现只占位，不阻塞 Web 主链路 | 否。不能据此宣称 Telegram 或 WeChat 已正式打通 | 当真实 webhook / send API / 鉴权 / delivery receipt 接入完成，并补最小验收与故障处理后，删除 stub 实现或仅保留测试桩 |

## 仅用于测试的 mock，不属于默认运行时

下面这些 mock 仍应保留，但它们不应被计入“运行时仍依赖 mock”的问题单中：

| 能力 | 位置 | 保留原因 | 退出条件 |
| --- | --- | --- | --- |
| LLM mock provider | `apps/server/internal/platform/llm/mock/provider.go` | 用于稳定覆盖 LLM 接口契约、错误分支和 streaming 行为 | 当测试策略不再需要 provider 级隔离时才考虑删除；目前没有删除价值 |
| Knowledge provider mock | `apps/server/internal/platform/knowledgeprovider/mock/provider.go` | 用于覆盖知识检索接口契约与错误路径 | 同上，保留作为测试夹具即可 |

## 上线判断口径

当前可以接受的边界：

- demo 页面、demo SDK、seed 脚本继续保留，但必须只服务于演示、文档和本地回归。
- WeKnora mock 继续保留，但 release readiness 不能只靠 mock 证据。
- in-memory event bus 可以支撑当前单体/单实例基线，但不是多实例生产消息系统。

当前仍明确不能宣称“已正式产品化”的部分：

- 语音录音与转写的外部 provider 能力。
- Telegram / WeChat 等外部渠道接入能力。
- 任何基于 demo 数据、mock 上游或 memory provider 得出的“生产可用”结论。

## 建议执行规则

- 以后凡是新增 demo/mock/stub/in-memory 运行时能力，必须同步补三件事：保留原因、prod-safe 判断、退出条件。
- 验收文档若引用 mock 或 seed 结果，必须显式标注是“协议/回归证据”还是“真实环境证据”。
- 正式对外承诺某项渠道或 AI 能力前，先确认它不再依赖本清单中的占位实现。
