# Todo

需求清单。已完成项保留一行证据（commit + 测试数）。

## 未完成

- [ ] real 模式知识库验收：Dify / WeKnora / RAGFlow 真实服务与凭证（脚本入口 `*_ACCEPTANCE_MODE=real` 已就绪）
- [ ] CODECOV_TOKEN：用户侧 `gh secret set CODECOV_TOKEN -R cuihairu/servify` 后对 CI 手动 re-run 首传
- [ ] 移动端语音消费：Android/iOS 语音上行与字幕渲染（按产品节奏）
- [ ] Phase 3 语音：WebRTC 音轨下发、端到端语音评估、租户级配额限流（依赖 RA-7 SFU-lite）
- [x] 生产侧派单竞态收口遗留：通用 UpdateTicket 改派 agent_id 不调坐席负载（PUT 更新可改派，current_load 不增不减）；transfer 流负载增减以会话预读 fromAgentID 为准（会话行未锁，并发转派同会话可重复减 1）——两处与 ticket 行锁修复会双重计数，需单独设计。UpdateTicketModelWithStatusAndCustomFields 事务内锁工单行按库内 agent_id 算增减（哨兵 0→NULL 同 Unassign 口径，6 项 integration 回归红转绿）；transfer 锁会话行读库内 agent_id 为准（RuntimeAdapter.LockedTransferAgentID，postgres/mysql 加 FOR UPDATE），并发转派已指给目标客服时跳过负载增减（SyncTransferLoad 恒加目标，仅传 nil 来源仍会重复加）。另修 item #10 两处 CI 回归：handlers 直引 conversation/application 改走 delivery 窄契约（DecodeTranslationCache/TranslationCacheKey），swagger 生成物重跑入库（docs.go/swagger.json/swagger.yaml 13 行）
- [ ] braces@3.0.3 与 sprintf-js@1.0.3 升级：npm 上游无修复版，等上游发版
- [ ] 知识库选型决策：RAGFlow 立项口径、选择链优先级、新客户默认口径（待产品确认）
- [ ] MetricsAggregator Snapshot 读取出口（待产品/运维口径）
- [ ] 远程协助 RA-8 SDK 半边
- [x] 会话消息译文持久化：Message 增 metadata 列（成本治理随 Phase 3 配额）— messages.metadata 列（000023，列级守卫钉住）+ 保留键 translations 按语言编解码（先到先得）+ 历史加载命中缓存免 LLM + 现译现写（失败静默）；响应键仍不落库。go test ./internal/... 全绿（含 -tags integration 4→7 项 infra 回归 + 6 子测试 handler 回归），go vet/gofmt 干净
- [x] retriever 会话语义统一：非空 session_id 查询滤掉全局摄入文档（pgvector 同构既有语义）——口径已定：会话 id 不是知识命名空间 id，KnowledgeID 留空由 provider 落回默认命名空间（1f190df 收口，全 provider 同构验证：pgvector/local 按 workspace_id、dify/ragflow 按 datasetID、weknora 按 KnowledgeBaseID，透传会话 id 即恒空）；租户维度检索为正解。会话级检索需会话级摄入（未立项，随会话知识库刀）；残留：ai_runtime_local_test.go 旧注释曾把修复前行为当现状，已随本轮纠正
- [ ] AI agent-loop 三工具接线：CustomerLookup / TicketLookup / Handoff 现传 nil Ports，调用即报 port is not configured（2026-10-11 拍板保持死路，等用户明令）
- [ ] modules 非 infra 层 GORM 写归属下沉 infra（约 70 文件，大重构）
- [ ] models 侧过渡别名最终删除（15 类型别名 + 4 投递状态常量 + NewEmbedding 转发，随 bootstrap/migrate、handlers swag、platform provider 各自迁移）

## 已定口径

- tickets.session_id 保持可空不收紧：管理员独立建单与邮件建单两条路径不传 session_id，关闭前未结单拦截不依赖必填
- dependabot：有修复版本即升级，无修复版本保留并记录；excelize 两轮 bump 后消单，open 仅剩 braces/sprintf-js
- AI agent-loop 三工具保持死路不接线：Handoff 属副作用动作与 PROTOCOL「转接由用户显式发起」相抵，CustomerLookup/TicketLookup 模型侧无 customer_id 上下文

## 已完成

- [x] B0 文档与架构声明（README 漂移、current-architecture 快照、ARCHITECTURE §6.4-6.6、domain Owns 声明）— b419cef 081e819 62b78a7 baabeb5，make local-check 绿
- [x] B1 客服核心产品闭环（conversation_events 投影、Timeline API 与 admin 组件、Workspace 三栏工作台、薄壳模块叙事降级）— 5b557ce 30a5146 26862b4 bcac4bf ddd71ed 80c0e6f e5ae355 b5a35eb，make release-check 绿 + e2e conversation-lifecycle 12 步通过
- [x] B2 Routing 打分引擎与安全数据边界（八因子评分落库、PII 清单导出与擦除、retention 配置化、statistics 收口进 analytics、scope 回填迁移）— fff2116 97f947a f9a14b1 4749eea 7e7357e a2fa76d，全量 go test 绿
- [x] B3 Knowledge 产品化与 AI 反馈闭环（来源与版本与索引、AI 答案与反馈持久化、检索分析、citation 与反馈 UI）— a66da78 0a7a152 f7b99e5 78361f1 3a570ce，unit + integration 绿，admin typecheck + build 绿
- [x] B4 收口与 V1.0 发布（TASKS 与验收矩阵口径统一、release notes、四门禁）— b9c66e0 f0487ac 58b7c4e 16693c3，tag v1.0.0，release/security/observability/local 四检查绿
- [x] C1 web 嵌入集成指南与组件外观（icon 三档、主题 token 23 项、亮暗两套、远程主题、两风格测试页、embedding-guide）— widget 12/12 用例（node --test），ferry 真嵌验收全链通过，b8f881e webhook 验收 16 文件入库
- [x] C2 admin-legacy 清理（apps/admin-legacy 整目录删除、5 处文档提法清理、补测至 100% 复核无凑数假测试）— 19 文件 384K 删除，全量测试绿
- [x] C3 文档全面重整（按用户任务八组重排、30 个导航孤儿收口、清 AI 味、六处实盘核对修漂移）— 93f6d13，docs build 绿，57 页路径双向校验无 404
- [x] P0-1 事件总线生产默认切 Redis，inmemory 生产拒启
- [x] P0-2 Voice 运行时去 mock provider，disabled provider 形态显式
- [x] P0-3 Agent 运行态去内存注册表与 legacy runtime 适配层
- [x] P0-4 配置加载去 panic，默认模型配置更新
- [x] P0-5 Fallback 收口 legacy_kb_enabled 兼容语义
- [x] P0-6 SDK 与后端协议漂移收口（漂移面收敛到 1 处、生成资产同步、漂移守卫三测试 + 广域扫描守卫 101 处端点对账）— sdk_route_drift_test.go 3 测试 + sdk_route_contract_test.go，sdk 186 用例全绿
- [x] P0-7 SDK 工程门禁失效修复（typecheck 绑定 workspace TypeScript 编译器、聚合 9 包）— npm -C sdk run typecheck 0 错误
- [x] P0-8 网站与部署脚本路径失配（Makefile 三目标、wrangler 配置、make docs 正规链、CI -run 白名单元守卫）— makefile_generated_assets_test.go + ci_script_checks_coverage_test.go
- [x] P1-1 AI / Knowledge 主链路验收（本机可完成部分全部闭环：ragflow mock CRUD、weknora 全栈 mock、容器化 weknora 栈、ai-fallback 三类证据、管理面聚合对账）— manifest 全入库，validate-acceptance-manifest.sh 绿
- [x] P1-2 Auth 自助 session 链路真实验收 — baeea93，make auth-session-acceptance manifest 入库
- [x] P1-3 会话工作台主操作补齐 — make workspace-acceptance 通过并入库 manifest
- [x] P1-4 运行基线最小事实补齐（/ready、/metrics、/api/v1/messages/platforms）— security-acceptance + runtime-baseline-acceptance manifest 入库
- [x] P1-5 Ticket 主闭环高频操作补齐 — make ticket-acceptance 通过并入库 manifest
- [x] P1-6 Bootstrap 落地与入口 wiring 收口（RunStandalone 唯一编排根、双入口薄壳化、seam 归一）— 9d801a7，CI 35465422927 绿，bootstrap 覆盖率 98.1% → 100%
- [x] P1-7 Modules 与 legacy services/models 边界收口（modules→services 清零、六刀模块自有模型迁移、两类 parser 级守卫）— 8a3a624，TestModulesDoNotImportLegacyServices + TestModulesDoNotUseMigratedTypeAliases 绿
- [x] P1-8 文本编码与仓库可读性（编码门禁、乱码存量清零）— check-text-encoding.sh 覆盖 1999 文件，0 非 UTF-8 / 0 BOM / 0 U+FFFD
- [x] P1-9 demo-sdk 生成链路一致性（sync-sdk-to-demo.sh、一键同步、漂移校验、CI 门）— check-generated-drift.sh 绿
- [x] P2-0 客户侧推荐问题与上下文联想（含 RQ-5 曝光/转化归因、ExposureSummary 出口、路径级限流）— 38566cf 3b8370e，CI 35688179671 绿
- [x] P2-1 多实例与高可用边界明确化（multi-instance-boundary 文档与实测口径）
- [x] P2-2 配置治理与环境分层强化（配置加载、校验、模板、文档四收口）
- [x] P2-3 数据恢复、备份与迁移演练（recovery 包 + dbrecovery 工具 + sqlite/pg 双轨演练证据）
- [x] P2-4 可观测性从有指标升级到可运维（五刀：错误分类与埋点、业务指标、SLO 与 burn rate、worker 轮次、known-gaps 清零）— 871119c 2cb1dd8 99a42d2 b0e4bc5 ac9ef2c 88d633a 822d788 a512989 3090a27，净 -4000 行
- [x] P2-5 安全治理收口到首批企业交付标准（响应头、body 上限、CORS、WS Origin 白名单、auth 审计、登录风险、refresh 吊销、scoped config 审批回滚）— 四份真实验收入库 manifest
- [x] P2-6 管理端产品化收尾（八刀验收：转接、满意度、客户与客服、统计与排班、宏与集成、远程协助、自动化与排行、pgvector 知识库）— 40c1f0e，八条 make *-acceptance 全通过并入库
- [x] P2-7 SDK 与多端 contract 稳定性治理（示例真实可构建、surface governance 入 CI）— efa24a2，sdk 186 用例 + governance + surfaces 绿
- [x] P2-8 性能容量与压测基线（perfbench 四场景、smoke/full 两档、容量基线、CI 驱动测试、perf-baseline 文档）— perfbench 覆盖率 0% → 100.0%，go test -race 绿
- [x] P3-1 清理公开语义中的 legacy/compat 混名 — e6928a6
- [x] P3-2 收拢 services 与 modules 的最终边界（18 刀，services 层整体移除）— 9599c4e 9e1984c f1998f4 440123c 818b792 5c9061f 9b49ac3 1126b60，CI 35457474387 绿
- [x] P3-3 清理 demo/mock/in-memory 资产默认暴露面（eventbus inmemory 生产拒启、demo-sdk 生产不暴露）— 34b66a9，CI 35462743342 绿
- [x] P3-4 统一运行时装配方式 — af58235，CI 35461039568 绿
- [x] 全仓覆盖率逐包收口至 100.0%（含 scripts/perfbench 与 infra/compose/weknora-mock 两处 0% 补齐、子进程覆盖率合并口径）— e4e7d77 5991d63 6de0377 516f94a c863828，CI 36316434375 绿，未覆盖语句 0
- [x] 迁移 schema 守卫家族六件（表级、列级、运行时索引、tag 索引、NOT NULL、DEFAULT）+ schema-parity 真库对账工具 + 三条真漂移迁移 — 3201805 0852423 81f195b 26ba743 60ac4f0，000020/000021/000022，591 列 × 52 表 0 diff
- [x] RAGFlow knowledge provider driver 立项落地（配置面、client、driver、租户作用域、选择链、验收脚本、集成文档）— df86736 d124dd5 043a92f，CI 35566568357 绿
- [x] 知识库选型调研文档（LANDSCAPE：RAGFlow 首选、Dify 定位降级、双路线建议）+ 六处 Dify 优先口径修正 — 47026ca，CI 35544351300 绿
- [x] 本地知识链路三刀（自含向量引擎、抽取式问答引擎、全自含验收）— 0eac6cb 72d5797 fa744ac，local-knowledge 验收 14 项 checks 全 true
- [x] AI 主链路智能化四批（首答质量三件套、坐席 Copilot、首答流式、模型配置现代化）+ 边界修复 — fafac8d 7a454e8 18a0a25 72cea90 1a7fe17，CI 35767697076 绿，五个触包覆盖率 100%
- [x] 移动端 SDK M0 里程碑（协议契约与 fixtures 10 例、Android 探针工程 21 测试、联调探针验收）— 39f2a67 2b96be6 ff8c49b，CI 35565315819 系绿
- [x] 移动端 SDK 消费半边（core message-translated 事件、admin 语言偏好与历史译文、双端 syncReadState 游标提交、服务端已读面）— core 125 + iOS 85 tests / 14 suites + Android gradle build 绿
- [x] 实时翻译 Phase 1 全链（偏好存储与 REST 双面、viewer 角色维度、访客→坐席帧、坐席→访客异步、批量子段翻译、消费半边）— 000015/000016 迁移，translation 四包与 handlers 覆盖率 100%
- [x] 实时翻译 Phase 2 语音全链（ASR/TTS 契约与 OpenAI provider、分句管线、独立语音 WS 通道、PROTOCOL §9 与 fixtures、Web 上行与字幕、成本控制与运维面）— 0359f2e e3eaf52 e4eb487 538a849 d4f38ea，core 125 用例，run-tests 100.0%
- [x] 管理端坐席侧语音实时翻译入口（voice.ts 采集与渲染、PROTOCOL §9 契约守卫两条）— f269f05，CI 36758621877 绿
- [x] go 1.26 升级与连带修复（toolchain 钉版、Dockerfile builder 镜像、GOTOOLCHAIN 口径、admin eslint 配置补全 lint 74→0）— 420ae5c e1ae50e 296060c d9bad86 b15f6a6 8c65fd2 970c6a4 7f61e5c 5b6729b，open 15→2
- [x] WebRTC DataChannel 数据竞争修复（dcMu 串行化字段访问）— f336ca4，go test -race 绿，realtime 覆盖率 100.0%
- [x] Android SDK 抖动面确定性化（awaitUntil 段标与 fail-fast、MockWebServer 串行化门控、纯握手计数双证据）— 5d6457e 8aef789 d7c33a2 3edeff4 3615031，CI 36848776775 全绿
- [x] 验收脚本 CI 重放收口（五脚本真跑门禁、webhook 端口硬编码修复、mobile-probe 挂 android-probe、元守卫扩多调用点）— 9956c7a 1dce8b0，五个证据测试本地全绿
- [x] strict 基线与 release-readiness 挂 CI（secure 示例配置口径、两脚本首次真实重跑）— 9956c7a，6.46s + 145.17s PASS
- [x] 事件租户盖章收口（四模块事件构造器盖章、每模块守卫测试、解 models→voice/infra 循环边）— 904c399，apps/server 全量绿
- [x] O1 透传字段落地（FieldsFromContext 三源、X-Trace-ID 回退、AccessLogger 走 logrus 标准出口）— 3306feb，telemetry 8 测试绿
- [x] golangci-lint 基线归零与门落 CI（存量 214 处清零、.golangci.yml 钉 standard、config verify 口径守卫）— 存量 214 违例 → 0，make lint 0 issues，门坑清单入记
- [x] 覆盖率门口径对齐（run-tests 默认目标 20% → 100%、聚合灰区基线补齐、本地口径与 CI 双向守卫）— TestCoverageGateTargetMatchesCIPin 绿，未覆盖语句归零
- [x] ticket handler 测试与异步自动派单竞态修复（种子 agent 改 busy）— ca54704，20 连跑绿
- [x] 生产侧派单竞态收口（assign/unassign/close 三路径事务内锁工单行，负载增减以库内 agent_id 为准；恢复被 lint 误删的 memdsn helper、双 tag 测试文件进标准门禁）— 92f867b，4 竞态回归测试红转绿，全量 4033 测试 0 失败，覆盖率 100.0%，lint 0 issues
- [x] 流式错误文本泄露收口（provider 失败分支不再进 ContentDelta、转人工失败帧固定文案）— c222ee7，llm 四包 + ai + realtime 绿
- [x] Web SDK 独立文档（8 节 API 与配置全表、data-* 19 项权威单一化、导航登记）— 0c4a1ee，docs build 绿
- [x] 文档-代码一致性审计（三类分桶、逐条 file:line 取证、审计表落库）— 见 docs/审计-文档一致性.md
