# Todo

需求清单。已完成项保留一行证据（commit + 测试数）。

## 未完成

- [ ] real 模式知识库验收：Dify / WeKnora / RAGFlow 真实服务与凭证（脚本入口 `*_ACCEPTANCE_MODE=real` 已就绪）—— BLOCKED：env 无 dify/weknora/ragflow 凭据，缺真实远端端点 + 凭证（外部提供）。2026-11-03 复核（env 仍无凭据，仅 .env.example 模板）
- [ ] 聊天平台 Phase 0：Telegram 真实 bot token 联调（隐私模式 / 逐群授权核实与运营授权记录）—— BLOCKED：env 无 telegram/bot_token，token 外部提供；仓内实现已收口。2026-11-03 复核（env 仍无 token）

## 已定口径

- tickets.session_id 保持可空不收紧（管理员独立建单与邮件建单两条路径不传）
- dependabot：有修复版本即升级，无修复版本保留并记录；apps/admin 残留以 pnpm audit 为准
- AI agent-loop 三工具会话作用域接线：Lookup 从会话注入租户/工作区；Handoff 只进人工等待队列，不自动指派坐席（2026-10-22）

## 已完成

- [x] B0 文档与架构声明（README 漂移、current-architecture、ARCHITECTURE §6.4-6.6、domain Owns）— b419cef 081e819 62b78a7 baabeb5，make local-check 绿
- [x] B1 客服核心产品闭环（conversation_events 投影、Timeline API、Workspace 三栏工作台、薄壳模块叙事降级）— 5b557ce 30a5146 26862b4 bcac4bf ddd71ed 80c0e6f e5ae355 b5a35eb，e2e conversation-lifecycle 12 步通过
- [x] B2 Routing 打分引擎与安全数据边界（八因子评分、PII 清单导出与擦除、retention 配置化、statistics 收口 analytics）— fff2116 97f947a f9a14b1 4749eea 7e7357e a2fa76d，全量 go test 绿
- [x] B3 Knowledge 产品化与 AI 反馈闭环（来源/版本/索引、AI 答案与反馈持久化、检索分析、citation 与反馈 UI）— a66da78 0a7a152 f7b99e5 78361f1 3a570ce，admin typecheck+build 绿
- [x] B4 收口与 V1.0 发布（TASKS 与验收矩阵统一、release notes、四门禁）— b9c66e0 f0487ac 58b7c4e 16693c3，四检查绿
- [x] C1 web 嵌入集成指南与组件外观（icon 三档、主题 token 23 项、亮暗两套、embedding-guide）— widget 12/12 node --test，b8f881e
- [x] C2 admin-legacy 清理（apps/admin-legacy 整目录删除）— 19 文件 384K 删除，全量测试绿
- [x] C3 文档全面重整（八组重排、30 导航孤儿收口、清 AI 味、六处修漂移）— 93f6d13，docs build 绿，57 页 0 404
- [x] P0-1 事件总线生产默认切 Redis，inmemory 生产拒启
- [x] P0-2 Voice 运行时去 mock provider，disabled provider 显式
- [x] P0-3 Agent 运行态去内存注册表与 legacy runtime 适配层
- [x] P0-4 配置加载去 panic，默认模型配置更新
- [x] P0-5 Fallback 收口 legacy_kb_enabled 兼容语义
- [x] P0-6 SDK 与后端协议漂移收口（漂移收敛 1 处、生成资产同步、漂移守卫、101 处端点对账）— sdk 186 用例全绿
- [x] P0-7 SDK 工程门禁修复（typecheck 绑 workspace 编译器、聚合 9 包）— npm -C sdk run typecheck 0 错误
- [x] P0-8 网站与部署脚本路径失配（Makefile 三目标、wrangler、make docs、CI -run 白名单守卫）
- [x] P1-1 AI / Knowledge 主链路验收（ragflow/weknora mock CRUD、容器化 weknora、ai-fallback 三类证据）— validate-acceptance-manifest.sh 绿
- [x] P1-2 Auth 自助 session 链路验收 — baeea93，manifest 入库
- [x] P1-3 会话工作台主操作补齐 — make workspace-acceptance 通过并入库
- [x] P1-4 运行基线最小事实补齐（/ready、/metrics、/api/v1/messages/platforms）— manifest 入库
- [x] P1-5 Ticket 主闭环高频操作补齐 — make ticket-acceptance 通过并入库
- [x] P1-6 Bootstrap 落地与入口 wiring 收口（RunStandalone 唯一编排根、双入口薄壳化）— 9d801a7，覆盖率 98.1%→100%
- [x] P1-7 Modules 与 legacy services/models 边界收口（modules→services 清零、六刀模型迁移、两类 parser 守卫）— 8a3a624，TestModulesDoNotImportLegacyServices 绿
- [x] P1-8 文本编码与仓库可读性 — check-text-encoding.sh 1999 文件，0 非 UTF-8 / 0 BOM / 0 U+FFFD
- [x] P1-9 demo-sdk 生成链路一致性（sync-sdk-to-demo.sh、漂移校验、CI 门）— check-generated-drift.sh 绿
- [x] P2-0 客户侧推荐问题与上下文联想（RQ-5 曝光/转化归因、路径级限流）— 38566cf 3b8370e，CI 35688179671 绿
- [x] P2-1 多实例与高可用边界明确化（多 RedisRegistry 互见 presence、transfer load 收敛）— 041ffb6 MergeRemote 合并语义，确定性复现竞态
- [x] P2-2 配置治理与环境分层强化（加载、校验、模板、文档四收口）
- [x] P2-3 数据恢复、备份与迁移演练（recovery 包 + dbrecovery + sqlite/pg 双轨证据）
- [x] P2-4 可观测性从有指标到可运维（错误分类、业务指标、SLO/burn rate、worker 轮次）— 871119c 2cb1dd8 99a42d2 b0e4bc5 ac9ef2c 88d633a 822d788 a512989 3090a27
- [x] P2-5 安全治理收口（响应头、body 上限、CORS、WS Origin 白名单、auth 审计、登录风险、refresh 吊销、scoped config 审批回滚）— 四份真实验收入库
- [x] P2-6 管理端产品化收尾（八刀验收）— 40c1f0e，八条 make *-acceptance 全通过
- [x] P2-7 SDK 与多端 contract 稳定性治理（示例真实可构建、surface governance 入 CI）— efa24a2，sdk 186 用例全绿
- [x] P2-8 性能容量与压测基线（perfbench 四场景、smoke/full 两档、容量基线、CI 驱动）— perfbench 覆盖率 100.0%，-race 绿
- [x] P3-1 清理公开语义中的 legacy/compat 混名 — e6928a6
- [x] P3-2 收拢 services 与 modules 最终边界（18 刀，services 层整体移除）— 9599c4e 9e1984c f1998f4 440123c 818b792 5c9061f 9b49ac3 1126b60，CI 35457474387 绿
- [x] P3-3 清理 demo/mock/in-memory 资产默认暴露面 — 34b66a9，CI 35462743342 绿
- [x] P3-4 统一运行时装配方式 — af58235，CI 35461039568 绿
- [x] 全仓覆盖率逐包收口至 100.0%（含 scripts/perfbench、weknora-mock 补齐、子进程合并口径）— e4e7d77 5991d63 6de0377 516f94a c863828，未覆盖语句 0
- [x] 迁移 schema 守卫家族六件（表/列/运行时索引/tag 索引/NOT NULL/DEFAULT）+ schema-parity 真库对账 — 3201805 0852423 81f195b 26ba743 60ac4f0，591 列 × 52 表 0 diff
- [x] RAGFlow knowledge provider driver 立项落地（配置面、client、driver、租户作用域、选择链、验收脚本）— df86736 d124dd5 043a92f，CI 35566568357 绿
- [x] 知识库选型调研文档（LANDSCAPE：RAGFlow 首选、Dify 定位降级）+ 六处 Dify 优先口径修正 — 47026ca，CI 35544351300 绿
- [x] 本地知识链路三刀（自含向量引擎、抽取式问答引擎、全自含验收）— 0eac6cb 72d5797 fa744ac，14 项 checks 全 true
- [x] AI 主链路智能化四批（首答质量三件套、坐席 Copilot、首答流式、模型配置现代化）— fafac8d 7a454e8 18a0a25 72cea90 1a7fe17，五触包覆盖率 100%
- [x] 移动端 SDK M0 里程碑（协议契约与 fixtures 10 例、Android 探针 21 测试）— 39f2a67 2b96be6 ff8c49b，CI 35565315819 绿
- [x] 移动端 SDK 消费半边（core message-translated、admin 语言偏好与历史译文、双端 syncReadState）— core 125 + iOS 85 tests
- [x] 实时翻译 Phase 1 全链（偏好存储与 REST、viewer 维度、访客/坐席帧、批量子段翻译）— 000015/000016 迁移，translation 四包与 handlers 100%
- [x] 实时翻译 Phase 2 语音全链（ASR/TTS 契约与 OpenAI provider、分句管线、语音 WS、PROTOCOL §9、Web 上行与字幕）— 0359f2e e3eaf52 e4eb487 538a849 d4f38ea，core 125 用例
- [x] 管理端坐席侧语音实时翻译入口（voice.ts、PROTOCOL §9 守卫两条）— f269f05，CI 36758621877 绿
- [x] go 1.26 升级与连带修复（toolchain 钉版、Dockerfile builder、GOTOOLCHAIN、admin lint 74→0）— 420ae5c e1ae50e 296060c d9bad86 b15f6a6 8c65fd2 970c6a4 7f61e5c 5b6729b
- [x] WebRTC DataChannel 数据竞争修复（dcMu 串行化）— f336ca4，-race 绿，realtime 100.0%
- [x] Android SDK 抖动面确定性化（awaitUntil 段标与 fail-fast、MockWebServer 串行化、握手计数双证据）— 5d6457e 8aef789 d7c33a2 3edeff4 3615031，CI 36848776775 全绿
- [x] 验收脚本 CI 重放收口（五脚本真跑门禁、webhook 端口修复、mobile-probe 挂 android-probe）— 9956c7a 1dce8b0，五证据测试全绿
- [x] strict 基线与 release-readiness 挂 CI — 9956c7a，6.46s + 145.17s PASS
- [x] 事件租户盖章收口（四模块事件构造器盖章、守卫测试、解 models→voice/infra 循环边）— 904c399，apps/server 全量绿
- [x] O1 透传字段落地（FieldsFromContext 三源、X-Trace-ID 回退、AccessLogger 走 logrus）— 3306feb，telemetry 8 测试绿
- [x] golangci-lint 基线归零与门落 CI（存量 214→0、.golangci.yml 钉 standard）— make lint 0 issues
- [x] 覆盖率门口径对齐（run-tests 默认 20%→100%、聚合灰区补齐、本地与 CI 双向守卫）— 未覆盖语句归零
- [x] ticket handler 测试与异步自动派单竞态修复（种子 agent 改 busy）— ca54704，20 连跑绿
- [x] 生产侧派单竞态收口（assign/unassign/close 三路径事务内锁工单行，负载以库内 agent_id 为准）— 92f867b，4033 测试 0 失败、100.0%、lint 0
- [x] 流式错误文本泄露收口（provider 失败不进 ContentDelta、转人工失败帧固定文案）— c222ee7，llm 四包 + ai + realtime 绿
- [x] Web SDK 独立文档（8 节 API 与配置全表、data-* 19 项、导航登记）— 0c4a1ee，docs build 绿
- [x] docs 根目录双语配对（59 篇实质文档全部配对）— docs build 绿 + check-text-encoding 绿
- [x] docs 站 VitePress 官方 i18n 接路由（root=en、zh=zh-CN、site-structure 独立维护、87 中文 / 59 英文）— docs build 绿，41 条 nav/sidebar 0 断链
- [x] docs 子目录英文正文补齐（28 篇：25 真译 + 2 >1000 行占位）— verify 28/28 clean、docs build 绿
- [x] docs 站 i18n 目录布局收口（中文迁入 docs/zh/、根换英文、删 .en/.zh 双文件、切换器自动渲染）— 9efb65a，Docs Pages 38924039803 绿
- [x] 审核整改（2026-10-18 luna-review：表一修风险、表二以码为准、表三分期）— 205cff0，Go 4209 测试/158 包、覆盖率 100%
- [x] 文档时效性清理（2026-10-18 用户令：18 篇文档 13 处过期修正、零业务代码改动）— docs build 绿 + check-text-encoding 绿
- [x] 聊天平台接入契约（Phase 1 类型/事件/收件箱/能力快照/游标/安装映射、Phase 2 Memex、Phase 2b Telegram、replay、操作者公告面、出站策略单源化）— 1c774c6 ae18f1f d8a8d99 836bf5c e08a169 2160de1 a6d47e0 579f1c0 373e526，run-tests 100.0%
- [x] CODECOV_TOKEN — 2026-10-15 secret 配置后首传成功（CI 38731452145，f00eeaa）
- [x] 移动端语音消费 — V1 维持不含语音（2026-10-30 用户拍板）；mobile.ts:13 voice enabled:false、PROTOCOL.md:36 不消费不发送
- [x] Phase 3 语音 WebRTC 音轨下发 — 服务端 2026-10-15 落地 + 客户端 94353c7；租户配额限流 2026-10-15 落地（三缺口、000024、scoped-config 2026-10-18、admin 2026-10-19）
- [x] 远程协助 RA-7 媒体桥接 + RA-5 信令归属 — 864f897 075055b，realtime 100.0%、-race 三连过
- [x] admin pnpm audit 五项收口（braces high / elliptic low 无 patched release 保留可见豁免，sprintf-js / react-router 消单）— AUDIT-EXEMPTIONS.md，typecheck+build 绿
- [x] 知识库选型决策（RAGFlow 唯一新增投入、选择链、新客户默认口径）— docs/design/knowledge-provider-selection-policy.md，零代码改动
- [x] MetricsAggregator Snapshot 读取出口 — Prometheus /metrics 为唯一出口，退役有损 Snapshot()，handlers integration 100.0%
- [x] 远程协助 RA-8 SDK 半边（offer 载荷与协商状态、跨重连重发）— e2d6397 + e31846d，sdk.test 五例绿
- [x] 会话消息译文持久化（messages.metadata 000023、translations 先到先得、历史免 LLM）— go test ./internal/... 全绿
- [x] retriever 会话语义统一（非空 session_id 滤掉全局摄入文档）— 1f190df，全 provider 同构验证
- [x] AI agent-loop 三工具接线（CustomerLookup / TicketLookup / Handoff）— 82002cf，服务包 272 测试绿
- [x] modules 非 infra 层 GORM 写归属下沉 infra（satisfaction 50 / sla 32 / auth 19 操作，reopen TranslateError 哨兵）— 覆盖率回 100.0%，全量绿
- [x] models 侧过渡别名最终删除（models.go 9 别名块 + 5 domain 导入 + 3 文件删除）— 3481171（26 文件）+ 379e8c9 守卫 + cc43cb0/7d02e9b 灰区 14 条，CI 38547016404 绿
- [x] 文档-源码一致性审计（2026-11-01 复扫，38 条：超前 3 / 滞后 13 / 不符 22，P0 0、P1 6）— 8073a14…e82f1f6，docs build 绿
- [x] 依赖安全批次收口（admin @umijs/max 4.7.16→4.7.30、follow-redirects 1.16.2、glob 10.5.0→14.0.3 使 mako 链 minimatch→11.0.4；docs mermaid 11.16.1→11.17.2 / vitepress 1.6.0→1.6.4；minimatch 经 eslint-plugin-react 链无兼容补丁记豁免）— apps/admin/AUDIT-EXEMPTIONS.md:132-162，admin typecheck+lint+build 绿（0 退出）、docs npm ci+build 绿、admin audit 5→3（1 moderate+2 low）、docs audit 0

## 审核发现

- [x] Telegram 离线 fixture 崩溃重投路径实证 — 接受后清游标以 offset=0 重拉，inbox 仍 1 行、只 forward 一次（adapter_test.go:446）
- [x] Telegram EventID 跨会话唯一性 — 改为 `chatID:messageID`，两个 chat 同 message_id=7 均提交/转发（adapter.go:76）
- [x] 旧格式 inbox 行迁移 — 000031 up/down + BackfillTelegramEventIDs（幂等、空库 no-op），postgres 与 sqlite 双路径测试
- [x] 回滚教义中英镜像同步 — backup-and-recovery 可逆数据迁移可配 .down.sql（zh:20 与 en 对齐）
- [x] recovery 包注释与回滚教义一致 — db_backup.go:6 更新，go vet 绿
- [x] 000031 postgres 真库执行测试 — 临时 schema 执行 up.sql，六类分支断言，CI Integration job 注入 TEST_POSTGRES_DSN
- [x] 排除分支覆盖 — 参数化 platform/event_type，补 message.deleted 行，sqlite 与 postgres 各断言不动
- [x] 临时 schema 名安全 — 短 hash + %q 引用标识符，杜绝未来子测试名拼非法 DDL
- [x] 复用 000027 真实索引 — 删手抄副本，断言 uq_chat_connector_inboxes_scope_key + pg_indexes
- [x] 迁移执行器口径 — 知悉项：migrate_runner 逐版本执行，CI 已验迁移链与 schema_migrations 水位
- [x] postgres 断言改业务键 — eventIDByKey/keyByKey，不依赖 serial id 顺序
- [x] 000031.down.sql 保守处理 — 知悉项：仅完整性/审计用途、单冒号 + 数字后缀，回滚走恢复备份
- [x] BackfillTelegramEventIDs 事务化 — 整段包 db.Transaction，失败整体回滚，补失败回滚测试
- [x] CI Skip 静默空跑 — 改 -json 输出 + 断言 skip/fail 计数 0
- [x] sqlite 种子真实 platform — seedTelegramInbox 参数化，真落 memex 行覆盖 platform 过滤
- [x] touched 计数按 RowsAffected — Delete/Update 两分支均累加实际影响行
- [x] docs/zh 迁移水位硬编码 — zh:26,142 改为 CI 动态对账口径
- [x] postgres 隔离依赖单连接 — SetMaxOpenConns(1) + current_schema() 对账断言
- [x] ci.yml 回滚教义注释同步 — 可逆数据迁移可配 .down.sql（可逆数据迁移可配，schema/基线 up-only）
- [x] AsQuarantined 保留规范化 Message — message_text 快照列（000032），4096 rune 截断
- [x] 逐会话授权门顺序 — 原始 chat id 先授权，未授权翻译后立即 AsQuarantined，不进正常流
- [x] 决策文档自述与实际落档一致 — 写入面改为本报告 + todo.md（2026-10-30-decisions.md:17,217）
- [x] 验收脚本行号偏差 — ragflow:448 / weknora:235 / dify:228 校正（kb-real / decisions 文档）
- [x] architecture-zh.svg 核心模块描边 — 6 模块补 class="core"，7 核心 rect 均 2.5px，Playwright 实测
- [x] todo.md 行号区间 — :38-42 改 :38-43（与 SVG 实测一致）
- [x] 历轮 diff 复核无发现（1b68c905 / 6da8ffc / b82a2868 / 5bd3d9c7 / 40aa8c63..HEAD）— 仅 todo/SVG 文档改动，git diff -- '*.go' '*.sql' 为空，run-tests 100.0%
- [x] chat_connectors_test.go 行号 — :459 改 :458（函数声明行）
- [x] 审核发现：本轮 diff 未见问题（865f0b60..HEAD）。证据：git log --oneline 865f0b60..HEAD 仅含 14 个 docs/todo 提交（60674e2/f4aaece/f6f5530/c9c337f/8073a14/a8257e5/b2f5d07/0952310/818a7d6/f8afd4b/e82f1f6/64b2484/c14d22d/c70c538），git diff 865f0b60..HEAD -- '*.go' '*.sql' '*.yml' '*.yaml' '*.sh' 'Dockerfile*' 为空；仅文档/审计台账改动，无语义/契约/安全发现。 — verify audit finding for 865f0b60..HEAD
- [x] 审核发现：本轮 diff 未见问题（c70c538c..HEAD）。覆盖范围：3 提交（9b26e0e todo 验证 865f0b60..HEAD / f83ee1b docs README 徽章行与标题布局 / 0ca9311 todo 复核 blocked 项），仅改 README.md、README.zh.md、todo.md 三文件，git diff c70c538c..HEAD -- '*.go' '*.sql' '*.yml' '*.yaml' '*.sh' 'Dockerfile*' 为空。核对项：README.zh.md 文档徽章指向 docs/zh 路由（/zh/）与 vitepress config zh locale 一致；<h1 align="center"> 在 GitHub 正常渲染；todo.md 新增行「env 仍无凭据，仅 .env.example 模板」与 .env.example 无 dify/weknora/ragflow/telegram 凭据、.env.weknora.example 仅模板一致；9b26e0e 证据 14 提交哈希逐一比对 git log 865f0b60..c70c538 相符。无注入/反序列化/越界/泄漏/竞态/错误处理面（无代码改动），无测试缺口。附：f83ee1b 提交信息「badge row layout and locale links」与本提交实际 diff（仅标题/布局，无链接改动）略不符，纯提交信息卫生，非缺陷。；复核属实（2026-11-03）：git log c70c538c..HEAD 仅 3 提交、git diff -- '*.go' '*.sql' '*.yml' '*.yaml' '*.sh' 'Dockerfile*' 为空，README.zh 文档徽章 href=/zh/ 与 config.ts zh locale 一致，.env.example 无 dify/weknora/ragflow/telegram 凭据、.env.weknora.example 仅模板，14 提交哈希与 git log 865f0b60..c70c538 逐一相符；f83ee1b 提交信息略述链接但 diff 仅标题/布局，系提交信息卫生、非缺陷。 — 复核属实 c70c538c..HEAD
- [x] 审核发现：本轮 diff 未见问题（0ca93119..HEAD）。证据：git log 0ca93119..HEAD 仅含 1 个提交（1f59ae4 todo: verify audit finding for c70c538c..HEAD），git diff 0ca93119..HEAD 仅改 todo.md（+1 行），-- '*.go' '*.sql' '*.yml' '*.yaml' '*.sh' 'Dockerfile*' 为空；仅审计台账追加，无语义/契约/安全发现。；复核属实（2026-11-03）：git log 0ca93119..HEAD 仅 1 提交、git diff -- '*.go' '*.sql' '*.yml' '*.yaml' '*.sh' 'Dockerfile*' 为空；1f59ae4 仅向 todo.md 追加该验证行，未触及代码/文档/测试面，无测试缺口。 — 复核属实 0ca93119..HEAD
- [x] 审核发现：本轮 diff 未见问题（1f59ae44..HEAD） — e3a0fab（todo.md:134）复核属实：git log 1f59ae44..HEAD 仅 1 提交 e3a0fab；git diff 仅 todo.md +1 行，Go/SQL/YAML/Shell/Docker 范围为空；纯审计台账追加，无代码面，git diff --check todo.md 通过。
