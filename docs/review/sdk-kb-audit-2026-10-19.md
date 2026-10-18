# Servify 外部接入面与群聊知识沉淀闭环审计

审计日期：2026-10-19。范围：SDK 当前接入面、知识库模型及群聊知识沉淀方案。代码未修改；本文逐节核实，未能从仓库确认处标为“未核实”。

## 一、SDK 现状

### 清单与契约

审计时 `ls sdk/` 顶层包含：`ACCEPTANCE-M3.md`、`ACCEPTANCE-M4.md`、`PROTOCOL.md`、`README.md`、`SURFACE_GOVERNANCE.md`、`android/`、`ios/`、`examples/`、`packages/`、`protocol-fixtures/`、`scripts/`、`package.json`、`package-lock.json`、`tsconfig.json`、`eslint.config.mjs`。`sdk/packages/` 当前有 core、React、Vue、Vanilla、API client、app-core、React Native，以及预留 transport-http/transport-websocket 包。依据：`sdk/PROTOCOL.md:1-3`；`sdk/SURFACE_GOVERNANCE.md:3-19`；清单为审计时 `ls sdk/` 结果。

| 对外接入面 | 当前事实与成熟度 | 契约定义 |
|---|---|---|
| Web core、React、Vue、Vanilla | Governance 将 core/react/vue/vanilla 列为可生产发布；API client、app-core、React Native 同列。框架包构建入口存在。SDK workspace 顶层标记 private；`@servify/*` 确认**未发布** npm registry（Governance 明文：依赖声明安装即 404，示例只能 vite alias 指向仓内 dist）。 | WebSocket 帧为手写 `PROTOCOL.md` + `protocol-fixtures/`；SDK 导出面由各包 `src/index.ts` 定义。未发现统一 OpenAPI/protobuf 契约；全仓 grep 无 OpenAPI/Swagger 规格文件（REST 端点散见于 `PROTOCOL.md:119` 引注与 `docs/mobile-sdk-integration.md:8`）。 |
| Android | README 标为 M3 代码面完成，提供 `ServifyChat.create/show` 集成门面、会话 UI、工单与推送注册；真机/凭证联调仍有未完成项。 | Kotlin 实现按手写 WS 契约与共享 fixtures 回放对齐；REST 端点形状在 SDK 文档/代码中手写，未核实有 OpenAPI/protobuf 生成。 |
| iOS / SwiftPM | README 标为 M2 Alpha、M3 代码面完成；`ServifyChat` async/await 门面、会话状态/传输协议与 SwiftPM 工程存在。生产网络实现 Darwin 专属，Linux 侧通过注入 mock。 | 与 Android 相同：手写协议说明、共享 fixtures；未核实 REST 生成式 schema。 |
| React Native | 治理文件将其列为可发布的 headless binding；Capabilities 与 hooks/provider 有实现描述。 | 继承 core 契约，具体外部 REST 覆盖范围未核实。 |
| transport-http / transport-websocket | 被列为 future reserved package；治理要求未实现前保持 `0.0.0`、private。 | 设计占位，不是稳定接入面。 |

### 非本项目应用能否完成完整调用

Web 应用可通过公开入口使用 SDK 做会话连接、发消息、消费 AI/坐席回复，并按需使用集成的 UI 框架包；Web core 没有应用层心跳、断线后需补拉、发送成功以回显帧确认。其“完整调用”取决于部署的访客 session/token 创建机制：WS 必填 `session_id`，配置 guest token 强制时还需 `access_token`；本仓库未核实一个面向任意第三方前端的公开 session bootstrap/API client 文档，故不能认定零配置即可直接上线。另：**拿不到包是硬门槛**——`sdk/SURFACE_GOVERNANCE.md:65-68` 明文 `@servify/*` 不发布 npm registry、依赖声明安装 404，连仓内示例也只能 vite alias / 相对路径引仓内 dist（`sdk/examples/react/vite.config.ts:8-13`、`sdk/examples/vanilla/index.html:241`），外部前端现状只能 clone 仓库本地构建。Android/iOS 有门面可集成，但要求宿主平台生命周期/依赖，且移动端语音通道尚未接入。契约依据：`sdk/PROTOCOL.md:7-19,31-37,54-61,145-156`；治理边界：`sdk/SURFACE_GOVERNANCE.md:14-27`。

### 对外可接入的缺口（优先级）

| 优先级 | 补齐项 | 审计判断/理由 |
|---|---|---|
| P0 | 明确对外支持的 REST + WS 契约、鉴权/session bootstrap 与错误/分页语义；优先提供可验证的 OpenAPI（HTTP）并说明 WS 手写事实源 | 目前 WS 规范较完整，但该文件自称 WS 契约，不覆盖完整 HTTP API；REST 端点散引（`sdk/PROTOCOL.md:119`、`docs/mobile-sdk-integration.md:8`），无独立规格；第三方无法仅凭它确定如何获得可用 session。 |
| P0 | 公开可安装/分发路径与稳定版本策略 | workspace 根包 `private`（`sdk/package.json:8`）；`sdk/SURFACE_GOVERNANCE.md:65-68` 明文不发布 registry、安装 404——分发渠道缺失为已核实事实。移动端已有 CI artifact 发布口径（`docs/mobile-sdk-integration.md`「获取 SDK 产物」节），Web 无对等物。 |
| P1 | 每个包发布的 `.d.ts` / 类型声明、运行时入口、支持平台和 peer dependencies 可见，并有外部消费者 clean-install 示例 | 主包（core/react/vue/vanilla/api-client/app-core/react-native）package.json 均声明 `types: dist/index.d.ts`，core 产物实查含 `dist/index.d.ts`；治理规定公开 API 为 `src/index.ts` 导出（`sdk/SURFACE_GOVERNANCE.md:14-19`）。 |
| P1 | 按包维护 CHANGELOG、语义化版本/弃用/迁移说明 | `sdk/` 全树无 CHANGELOG（find 无命中）；治理已有 breaking checklist（`sdk/SURFACE_GOVERNANCE.md:21-27`）；包版本同号 1.0.0 靠 `sdk/package.json:46-47`（version:sync/version:check）维护，无 git tag 与 per-package semver 记录。 |
| P1 | React/Vue/Vanilla 与纯 REST/WS 的最小完整示例，包含 session 获取、token、错误处理、补拉与重连 | 现有 examples 存在，治理要求构建；第三方后端/任意站点端到端接入步骤不足。 |
| P2 | 增加 API 覆盖矩阵与能力/版本兼容表（Web/Android/iOS/RN；特别是 voice、WebRTC、push） | `PROTOCOL.md` 记录能力差异，但分散于长协议文档；对外接入方不易判断可用性。 |

SDK 文档依据：`sdk/SURFACE_GOVERNANCE.md:3-27,48-61`；Android 状态与门面：`sdk/android/README.md:1-24,56-63`；iOS 状态与平台差异：`sdk/ios/README.md:1-14,20-30`；WS 握手与语义：`sdk/PROTOCOL.md:7-19,31-37,54-61`。

## 二、知识库抽象

### 实体与边界

| 实体 | 当前实现 | 关键边界 |
|---|---|---|
| 库/范围 | 没有独立 KnowledgeBase/KnowledgeLibrary 表可见；文档按 `tenant_id`、`workspace_id` 隔离，搜索请求的 `KnowledgeID` 映射 workspace。 | 可把 workspace 视作知识范围的近似，但其本体是工作区，不能当成独立知识库生命周期。 |
| 文档 | `knowledge_docs` / `KnowledgeDoc`，含标题、正文、分类、provider/external ID、来源、版本、范围、公开位。 | 文档既是业务载体，也是 pgvector 向量/分块数据承载行。 |
| 分块 | `chunk_index`、`doc_chunk_id`、`embedding` 放在 knowledge_docs 行；pgvector indexing provider 切分文本。 | 未见独立 chunk 实体/表；分块由同表多行表达。外部 provider 可能自行管理 chunk，细节依 provider。 |
| 标签 | 文档 `tags` 字符串；领域实体为 `[]string`，持久化模型为字符串字段。 | 未见规范化标签实体、关联表或标签权限。 |
| 权限/可见性 | 文档 `is_public`、tenant/workspace scope 字段；knowledge doc 接口认证/管理权限另由路由/handler 控制。 | 未见知识文档 ACL/角色关联表；`is_public` 不是细粒度授权模型。具体每条查询权限约束仍需核对全部调用路径，未核实。 |
| 来源/索引任务 | `knowledge_sources` 登记来源类型/名称/描述，文档挂 source_id；`knowledge_index_jobs` 记录任务状态与文档版本。 | 来源表当前只登记元数据，没有 ingestion connector 生命周期。 |

依据：`apps/server/internal/modules/knowledge/domain/models.go:9-30,33-58`；`apps/server/internal/modules/knowledge/domain/entities.go:5-20,31-54`；初始表结构 `apps/server/internal/app/bootstrap/migrations/000001_init.up.sql:92-100`；来源迁移 `apps/server/internal/app/bootstrap/migrations/000018_knowledge_sources_version.up.sql:1-22`。

### 来源可插拔性、检索抽象与耦合

| 评估项 | 结论 |
|---|---|
| 文件/网页/数据库/对话记录来源 | 当前 source 类型只允许 markdown、website、pdf、faq、api，并且文档注明“V1 只做登记与归属，不按 source 定制 provider 行为”。这证明来源可分类登记，不证明有可插拔采集器。数据库同步与对话记录 ingestion 未见实现/登记类型；均未核实为可用能力。 |
| 检索/索引替换 | `KnowledgeProvider` 接口提供 Search/Upsert/Delete/HealthCheck；已有 pgvector、local、Dify、Ragflow、WeKnora 等实现，应用层通过接口注入，故存储/检索后端有可替换接缝。Embedding 另有 Embed/Dimension/HealthCheck 接口，可独立换 embedding provider。 |
| 关键词/向量/混合 | 公共 SearchRequest 有 strategy 字符串；pgvector 实际搜索先 embedding，再做向量距离排序，且只有 cosine/euclidean 两个距离度量（`apps/server/internal/platform/knowledgeprovider/pgvector/search.go:107-120`）。`semantic` 与 `hybrid` 在查询期一律归一为 cosine（`apps/server/internal/platform/knowledgeprovider/pgvector/provider.go:350-356`，local 同口径 `.../local/provider.go:294`）。**当前没有真正 hybrid 检索**：关键词通路只存在于管理列表过滤（`apps/server/internal/modules/knowledge/infra/gorm_repository.go:112`，`title/content/tags LIKE`），不进检索链路。接口形状允许未来扩展（strategy 字符串 + Metadata map）。 |
| 业务耦合与剥离成本 | knowledge 模块已有 domain/application/infra/delivery 分层，抽出基本文档 CRUD 与 provider 选择为中等成本；但表把向量、块字段与业务文档混存，作用域借用 workspace，AI 首答还将 provider 结果转成旧 WeKnora SearchResult 形状，增加适配成本。客服话术、商品专属实体/规则在本次定位到的核心 knowledge schema 中未发现；若它们藏在具体内容/调用者处，未核实。 |

依据：来源限制 `apps/server/internal/modules/knowledge/domain/models.go:47-58`；Provider 接口 `apps/server/internal/platform/knowledgeprovider/provider.go:10-28`；Search 输入输出 `apps/server/internal/platform/knowledgeprovider/types.go:10-42`；pgvector 向量路径与 strategy 归一 `apps/server/internal/platform/knowledgeprovider/pgvector/provider.go:67-115,350-356`；Embedding 接口 `apps/server/internal/platform/embedding/provider.go:5-15`；知识服务 provider 注入 `apps/server/internal/modules/knowledge/application/service.go:14-27`；文档创建同步 `.../service.go:30-64`；AI 响应来源适配 `apps/server/internal/modules/ai/delivery/enhanced_service.go:475-490`。

## 三、群聊知识沉淀闭环设计评估（方案，不改代码）

### 现有可复用锚点

当前访客 WS 收到 `text-message` 后进入 `handleTextMessage`，消息通过 conversation writer 持久化，随后判断转人工/调用 AI；此处可判定提问、关联 session 和收集后续群内回复。消息表已存 tenant/workspace/session、content、sender、type、时间；AI 首答记录另存 `ai_answers` 与来源快照，但不含审核状态。AI WS 帧已有 `sources`、`strategy`、`answer_id` 等来源信息出口，适合作为引用标记扩展点。依据：`apps/server/internal/platform/realtime/websocket_hub.go:370-391,429-467`；writer 注入与持久化契约 `apps/server/internal/platform/realtime/websocket_hub.go:176-181`、`apps/server/internal/modules/conversation/delivery/websocket_adapter.go:14-18,28-45`；消息字段 `apps/server/internal/models/models.go:255-270`；答案记录 `apps/server/internal/app/bootstrap/migrations/000019_ai_answers_feedback.up.sql:1-22`；AI 引用帧字段 `apps/server/internal/platform/realtime/websocket_hub.go:855-880`。

### 设计决策

| 问题 | 推荐方案 | 取舍/必须约束 |
|---|---|---|
| 状态位放哪张表 | 新增 `knowledge_candidates`（或 `knowledge_entries`）沉淀候选问答，字段 `status=pending/approved/rejected`、tenant/workspace、question、answer、source_session_id、source_message_id(s)、reviewer、reviewed_at、approved_document_id、timestamps；索引 status+scope。审核通过时生成/升级为正式 `knowledge_docs` 文档。 | 不把状态加到 `knowledge_sources`（那是 connector 元数据），也不建议直接加 `knowledge_docs` 状态：候选问答在审核前不应进入正式检索，更要保留原始聊天引用及审核过程。若最小化表数，可把 pending 文档放 knowledge_docs 且加 status，但所有 provider 查询必须强制过滤 approved，漏过滤会造成风险，故不推荐。 |
| 群消息采集器接哪层 | 接在 conversation message persistence 后的应用层事件出口（WebSocket handler 通过已注入 writer/事件发布口），而非直接写入知识库或 provider。采集器以消息保存成功事件为准，异步消费；按 session/thread 滑动窗口维护“问题待回答”上下文，识别后续 agent/user 的有效回复并生成候选。 | 当前 WS 确认是单会话私聊形态：hub 按 `session_id` 定向广播、仅空 SessionID 才全员广播（`apps/server/internal/platform/realtime/websocket_hub.go:278-280`），消息模型亦为 session 域（`apps/server/internal/models/models.go:255-271`）；真正群聊 channel/thread、成员权限与消息 edit/delete 事件未核实。采集器可复用 `handleTextMessage` 已有的旁路挂载先例（转化归因同步挂 `websocket_hub.go:443-447`、翻译异步 goroutine 挂 `:456-463`，均“失败仅日志不阻主链路”）。先抽象 `ConversationKnowledgeSource`/`MessageObserved` 接口，之后接群平台 connector；不要把群平台 API 耦合到 knowledge domain。 |
| 回答溯源标注 | 正式命中沿用 `sources[]`，来源对象加 `status=approved`/`source_type`；候选命中另设显式字段或统一 `knowledge_status=pending`，UI/消息文本上均显示“待审核”，附来源群/消息链接或时间、候选 ID。通过状态必须来自检索记录的文档状态，不由模型自由生成；输出做服务端强制后处理。具体透传链已核实且全程零改动即可加字段：`KnowledgeHit`（`knowledgeprovider/types.go:22-29`，Metadata map 已存在）→ sources 适配（`ai/delivery/enhanced_service.go:475-490` 原样透传 Metadata）→ `ai-response` 帧载荷（`websocket_hub.go:855-880`，契约 `sdk/PROTOCOL.md:47`）→ widget 引用行渲染（`README_KNOWLEDGE.md:224`）；落库快照同步加状态（`ai/domain/models.go:30-35` `SourceSnapshot`）。 | 未审核内容默认不进入正常知识检索。若产品允许内部人员引用候选，候选检索必须单独 opt-in、权限隔离，并将“【待审核】”作为不可省略标签，UI 不应只依赖模型写文案。答案/引用快照保留当时状态，审核后不能回写成 approved 伪造历史。 |
| 审核与升级 | 后台 pending 列表呈现问题、建议答案、逐条原消息引用、重复候选聚类；审核通过后创建正式文档（source 类型建议 conversation，需扩充枚举）并排入现有 index job；驳回保留拒绝原因和审计记录但不入索引。 | `knowledge_sources` 当前 source type 不含 conversation，且 V1 是登记不采集，需迁移枚举/校验和 UI。 |

### 最小改动与分期

| 阶段 | 内容 | 依赖/验收 |
|---|---|---|
| 0：候选落库与人工审核 | 新表候选、审核状态与来源 message/session IDs；消息持久化后发布观察事件；仅 agent 明确回答的紧邻问答成候选，后台列表 approve/reject；approve 转 knowledge_docs + index job。 | 保留 tenant/workspace 边界；候选唯一/去重键；权限只允许有知识管理权限的坐席审核。 |
| 1：可追踪检索与标注 | 正式知识命中返回 document/source ID、approved；`ai_answers.sources_json` 快照加状态及可追溯引用；前端渲染来源；候选默认不可检索。 | 确保 metadata 来自服务端实体，记录答案时 snapshot 状态；拒绝后历史引用仍显示当时的 pending/rejected 状态。 |
| 2：待审核候选有限引用 | 对内部坐席提供独立候选搜索入口，可选择引用；响应 `status=pending` 且服务端/UI 强制“待审核”水印；访客端禁用。 | 单独权限和显著标识；不可把候选加入默认 RAG top-k。 |
| 3：群平台 connector 与自动归纳 | 在来源端口接入目标群聊平台，处理 thread/reply/edit/delete、成员权限、速率限制、重放幂等；候选生成增加归纳/去重建议但审核仍为人工。 | 先确定群平台与数据保留策略；本仓库现有单会话 WS 不等于外部群聊接入器。 |

“待审核”标注是硬业务不变量：任何返回 pending 候选的 API/WS 都必须携带结构化状态，服务端渲染端在显示正文前强制加标签；不允许由 LLM 自主决定是否披露。拒绝内容不参与向量索引；审核通过原子化地产生正式文档并排 index job，避免“状态已 approved 但搜索不到”或“pending 被默认命中”的窗口。

## 修复记录

### 一、知识库混合检索

- pgvector provider 新增向量与关键词双路召回，并按 RRF（`k=60`）融合；`strategy: hybrid` 为默认，`strategy: vector` 可选纯向量，`SearchRequest` 签名不变。
- 关键词路径使用 PostgreSQL `pg_trgm` 字符三元组相似度及 GIN 索引；没有使用 `tsvector/ts_rank`，因为 PostgreSQL 默认不提供中文分词器。中文按字符片段匹配，不宣称有中文分词能力。
- 新增迁移 `000026_pg_trgm.up.sql`，创建扩展与标题/正文检索索引；配置和检索说明见 `README_KNOWLEDGE.md`。
- 测试覆盖 RRF 排名与分值、错误码精确词对比纯向量、中文查询回归；`go test ./...`（`apps/server`）通过。

### 二、知识库独立实体

- 新增 `KnowledgeBase`（租户/工作区隔离、库级公开标志）与有序 `KnowledgeChunk`，文档可绑定知识库；见 `apps/server/internal/modules/knowledge/domain/models.go:9-55`。
- 新增知识库/分块 service 与 repository，文档创建时写默认或指定知识库及第 0 块；见 `apps/server/internal/modules/knowledge/application/catalog_service.go:12-43`、`apps/server/internal/modules/knowledge/infra/gorm_repository.go:24-80,83-135`。
- 迁移 `000025` 为存量 scope 建默认库、回填文档外键，并逐行复制现有文档内容/向量为分块；见 `apps/server/internal/app/bootstrap/migrations/000025_knowledge_bases_chunks.up.sql:1-49`。迁移保留原文档行与内容。
- 测试：`go test ./internal/modules/knowledge/...` 与 `go test ./internal/app/bootstrap -run 'TestMigrationChain' -count=1` 通过。
- Commit: `41852ed9d8bec3e9a24bbdbc4ec230d491b1329a`。

### 三、SDK 对外接入门槛

- 增加 session/token 步骤与 Web 分发事实说明；见 `sdk/EXTERNAL-INTEGRATION.md:1-54`。SDK workspace 当前 `private: true`，没有公开 npm/CDN 地址；见 `sdk/package.json:1-18`。
- 增加从当前 `1.0.0` 开始的变更记录并从 SDK README 链接；见 `sdk/CHANGELOG.md:1-12`、`sdk/README.md:1-5`。
- 测试：`npm ci` 后 `npm test` 通过（188 项测试）。
- Commit: `961a5569effbf164f2ae76dca74a75e8e7e78bd3`。

### 四、对外契约可验证性与能力矩阵

- 更正本审计「全仓无 OpenAPI/Swagger 规格文件」的否定性结论：规格在 `docs/generated/api/swagger.json`（`make docs` pinned swag 生成，CI 漂移守卫），审计检索面漏看了 `docs/generated/`。EXTERNAL-INTEGRATION 增「Contract sources」节，REST 面指向该规格、WS 面声明 PROTOCOL.md 手写事实源；见 `sdk/EXTERNAL-INTEGRATION.md`。
- 新增跨端能力/版本兼容矩阵 `sdk/CAPABILITY-MATRIX.md`（P2 补齐项）：契约事实源与版本口径（WS 手写+共享 fixtures、REST OpenAPI、同号 1.0.0 无 per-package semver、能力协商缺省）、13 行能力矩阵（聊天/流式/转人工/补拉/未读/已读游标/翻译消费/语音 §9·§9.3/远程协助/推送注册/工单/上传/ICE——逐格带代码锚点）、平台形态与已知边界、分发硬边界；README 挂链接。逐格锚点核验：Web core `websocket.ts` switch/`voice.ts`/`unread.ts`、Android `FrameCodec.kt`/`ServifyChat.syncReadState()`、iOS Swift 镜像、RN `createRNServifySDK`（mobile 能力集）、移动端 `message-translated` 仅 fixtures 回放无消费实现（静默忽略）。
- 测试：`npm -C sdk test` 通过（199 项）；docs 门未受影响（本项只动 `sdk/` 树与本文）。
