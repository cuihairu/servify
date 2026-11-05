# Todo

需求清单。已完成项保留一行证据（commit + 测试数）。

## 未完成

- [ ] real 模式知识库验收：Dify / WeKnora / RAGFlow 真实服务与凭证（脚本入口 `*_ACCEPTANCE_MODE=real` 已就绪）—— BLOCKED：env 无 dify/weknora/ragflow 凭据，缺真实远端端点 + 凭证（外部提供）。2026-11-04 复核（env 仍无凭据，仅模板）；本轮跳过；复核 2026-11-05 仍缺 外部真实远端端点+凭证（env 无 dify/weknora/ragflow 凭据）
- [ ] 聊天平台 Phase 0：Telegram 真实 bot token 联调（隐私模式 / 逐群授权核实与运营授权记录）—— BLOCKED：env 无 telegram/bot_token，token 外部提供；仓内实现已收口。2026-11-04 复核（env 仍无 token）；本轮跳过；复核 2026-11-05 仍缺 外部 telegram bot token（env 无 telegram/bot_token）

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
- [x] 依赖安全批次收口（admin @umijs/max 4.7.16→4.7.30、follow-redirects 1.16.2、glob 10.5.0→14.0.3 使 mako 链 minimatch→11.0.4；docs mermaid 11.16.1→11.17.2 / vitepress 1.6.0→1.6.4；minimatch 经 eslint-plugin-react 链无兼容补丁记豁免）— 32fa296 a82fd59（apps/admin/package.json:19,43；apps/admin/AUDIT-EXEMPTIONS.md:132；docs/package.json:14,15），admin typecheck+lint+build 0 退出、docs npm ci+build 0 退出、admin audit 5→3（1 moderate+2 low）、docs audit 0

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

## 漏洞扫描遗留（2026-11-03 全舰队扫描，源 scratch/vuln-scan-*.md）

- [x] VULN-SERVIFY-001 [非安全·过时依赖] go get -u ./... 因 proxy EOF 部分失败（仅安全项保留）：补跑非安全升级；验收：go list -m -u all 无网络错误 + go build ./... && go test ./... 绿（另：根与 apps/server 的 govulncheck 已于 2026-11-03 晚实测 No vulnerabilities found，EOF 需重跑一项已闭合）— 2026-11-04 证据：go list -m -u all exit 0（无网络错误）；go build ./... exit 0；go test -timeout 60m ./... exit 0（含 scripts 2260.387s / perfbench 6.252s / weknora-mock 0.096s，其余包 ok 或 no test files）；make test（run-tests.sh 集成 tag）exit 0、覆盖率 100.0%；go.mod:5 + apps/server/go.mod:5 toolchain go1.27.3，gorm.io/gorm v1.25.7→v1.31.2（apps/server/go.mod:43）等全量升级；schema.ParseIndexes() map→[]*Index API 变更已适配（migrations_tag_indexes_parity_test.go:53），bootstrap 包 go test -tags integration 32.943s ok。

## 审核发现（e3a0fabd..HEAD，2026-11-03 只审不改）

覆盖范围：6 提交（aed3ba0 todo 验证 1f59ae44..HEAD / 32fa296 fix(deps) 升级 admin+docs 依赖并收口 glob 审计链 / a82fd59 chore(docs) 移除过时 pnpm lockfile / 62ea1e6 todo 记录依赖安全批次证据 / 63dab1d todo 复核 blocked 项 / 09c45c6 docs 记录漏洞扫描遗留），仅改 apps/admin/{package.json,pnpm-lock.yaml,AUDIT-EXEMPTIONS.md}、docs/{package.json,package-lock.json,pnpm-lock.yaml(删)}、todo.md 七文件；`git diff -- '*.go' '*.sql' '*.sh' 'Dockerfile*' 'Makefile'` 为空（pnpm-lock.yaml 命中 *.yaml 除外），服务端/配置面零改动。

- [x] 审核发现：override `glob@>=10.2 <10.5: 14.0.3` 强制跨 4 个主版本把 glob@14.0.3 注入 rimraf@5.0.1（其声明 `glob: ^10.2.5`，锁文件 rimraf@5.0.1→glob:14.0.3），构成 semver 范围违规（apps/admin/package.json:43）。实测当前可用：glob@14 导出 glob/globSync/sync 均为可调用函数，rimraf@5.0.1 async+sync glob 路径均正常删除、无 runtime 错误；但为潜在脆性——未来 glob 主版本或 rimraf 变更可能在 admin build/lint 静默破坏，建议以升级 rimraf 至声明含 glob@14 的版本替代该强制 override，或把此约束固化为豁免/注释。 — 2026-11-04 已修复：删除 glob override，新增 `rimraf@5.0.1: 6.1.4` override（apps/admin/package.json:58）；rimraf@6.1.4 声明 `glob: ^14.0.3`，锁文件 rimraf@6.1.4→glob:14.0.3（apps/admin/pnpm-lock.yaml:14411），semver 满足（14.0.3 ∈ ^14.0.3=true）；锁文件 glob 仅 14.0.3/7.2.3，无 glob@10.x；admin typecheck+lint+build 全 0 退出；pnpm audit 3（1 moderate minimatch + 2 low elliptic/deepmerge）与基线一致，无新增漏洞。
- [x] 审核发现：测试缺口 — rimraf@5.0.1 ↔ glob@14.0.3 override 兼容性无自动化回归测试（唯一证据为一次 admin typecheck+lint+build 0 退出，todo.md:103），而 admin build 路径多半不触发 rimraf 的 glob:true 分支（rimraf 默认 glob:false，仅 -g/--glob 或 glob 选项触发），该 override 破坏面未被构建覆盖（apps/admin/pnpm-lock.yaml:14408）；建议补一个 rimraf+glob14 冒烟用例或把此约束写入验收。 — 2026-11-04 已闭合：override 改为 rimraf@5.0.1→6.1.4（声明 glob ^14.0.3），构建面全绿（typecheck 0 / lint 0 / build 0 退出），glob@14.0.3 为 rimraf@6.1.4 声明范围，不再依赖跨主版本强制注入，破坏面消除。
- [x] 审核发现：apps/admin/package.json 末行 `}` 丢失结尾换行（\ No newline at end of file，文件 61 行无末尾 \n），纯卫生、无功能影响（apps/admin/package.json:61）；系 32fa296 编辑引入，建议补回换行以免后续 diff 噪声。 — 2026-11-04 已修复：package.json 现以 `}\n` 结尾（tail -c 3 为 0a 7d 0a），git diff --check 通过。
- [x] 审核发现：apps/admin/AUDIT-EXEMPTIONS.md 新增「## 3. minimatch」（:132）置于「## Why an @umijs/max upgrade does not close any of these」（:122）之后、Procedure（:162）之前，而条目 1/2（elliptic :54 / deepmerge :88）在该 Why 节之前，节序不一致（内容本身无误，无安全/正确性影响，纯文档结构小瑕疵）（apps/admin/AUDIT-EXEMPTIONS.md:132）。 — 2026-11-04 已修复：章节顺序现为 Snapshot→1 elliptic→2 deepmerge→3 minimatch→Why→Procedure（AUDIT-EXEMPTIONS.md:13/54/88/122/152/164），并在 Why 节同步记录 rimraf@6.1.4→glob@14 的 semver 兼容链；git diff --check 通过。
- [x] 审核发现：本轮 diff 无新增漏洞依赖——旧锁文件 e3a0fab 已含 minimatch@3.1.5（eslint-plugin-react 链）/elliptic@6.6.1/deepmerge@1.5.2+4.3.1，本次仅消除 glob@10.5.0 链、修补 follow-redirects 1.16.1→1.16.2（锁内无 <1.16.1 残留）、升 @umijs/max 4.7.16→4.7.30 与 docs mermaid/vitepress，安全面净改善（5→3）；pnpm audit 因 proxy EOF 无法在本环境独立复跑，5→3（1 moderate+2 low）与 docs audit 0 依 todo.md:103 台账记载，锁文件状态与所述 3 项残留（elliptic/deepmerge/minimatch@3.1.5）一致，且「无 glob@10.x 残留」已由 grep 证实。 — 2026-11-04 复核：pnpm audit 复跑 3 vulnerabilities（1 moderate minimatch + 2 low elliptic/deepmerge），与所述 3 项残留一致；锁文件 glob 仅 14.0.3/7.2.3、无 glob@10.x（grep 证实），rimraf@6.1.4→glob:14.0.3，无新增漏洞。

## 审核发现（09c45c63..HEAD，2026-11-04 只审不改）

覆盖范围：3 提交（c18321b chore 升级 Go 依赖 / 21a25e6 fix(deps) rimraf→glob14 兼容并删 glob override / d3b6f6f todo 复核 blocked 项），改 apps/server/go.{mod,sum}、go.{mod,sum,work,work.sum}、apps/server/internal/app/bootstrap/migrations_tag_indexes_parity_test.go、apps/admin/{package.json,pnpm-lock.yaml,AUDIT-EXEMPTIONS.md}、todo.md；实测 go build ./...（根+server）exit 0、go mod verify 全绿、bootstrap 包单测 ok、pnpm audit 复跑 3（1 moderate+2 low）与台账一致；无 replace 指令，CI 用 go-version-file=apps/server/go.mod+setup-go@v7.0.0 正确消费 toolchain go1.27.3（apps/server/go.mod:5），21a25e6 对上一轮 5 项发现的闭合描述与锁文件/audit 复核一致（仅 d3b6f6f 复核日期改 11-04，无实质改动）。

- [x] 审核发现：gorm.io/plugin/opentelemetry 0.1.6→0.1.16 的 tracing 包从仅 import gorm.io/gorm 变为无条件 import gorm.io/driver/clickhouse + gorm.io/driver/mysql（tracing.go:18-19），把 ClickHouse(clickhouse-go/v2 v2.50.0) 与 MySQL(go-sql-driver/mysql v1.10.1) 驱动整链编进服务端二进制（go list -deps ./internal/app/bootstrap 证实 33 个相关包入闭包），而应用仅用 postgres+sqlite（database.go:10,12），纯依赖体积/攻击面膨胀、无功能收益（apps/server/go.mod:44）。 — 2026-11-05 属实已修复：otel 回滚至 0.1.12（apps/server/go.mod:44），`go list -deps ./internal/app/bootstrap` 无 clickhouse/mysql 残留，全测试 100.0% 绿。
- [x] 审核发现：gin 1.10.0→1.12.0 在 gin.go:20 无条件 import github.com/quic-go/quic-go/http3（v1.10.0 无此 import），把 quic-go v0.65.0（大体积、CVE 历史密集）编进服务端二进制，而 internal/ 全量无 http3/RunQUIC/ListenAndServeQUIC 使用，属未用依赖面扩张（apps/server/go.mod:15）。 — 2026-11-05 属实已修复：gin 回滚至 1.10.1（apps/server/go.mod:15），`go list -deps ./internal/app/bootstrap` 无 quic-go 残留，全测试 100.0% 绿。
- [x] 审核发现：golang.org/x/crypto 直依赖钉在未发布 commit 的伪版本 v0.58.1-0.20261009181718-2f1f834330b2，而 go list -m -versions 最新 tag 为 v0.58.0；auth 口令 bcrypt 依赖其子包，伪版本可审计性/可复现性弱于 tag，建议改钉 v0.58.0 或注明 commit 缘由（apps/server/go.mod:37）。 — 2026-11-05 属实已修复：改钉 v0.58.0 tag（apps/server/go.mod:37），`go mod verify` 全绿，全测试 100.0% 绿。

## 审核发现（09c45c63..HEAD，2026-11-05 只审不改·复核）

- [x] 审核发现：Dockerfile 构建基座 golang:1.26-alpine 未随新增 toolchain go1.27.3 指令更新——官方 golang 镜像设 GOTOOLCHAIN=local（Dockerfile:13-14 注释亦确认），GOTOOLCHAIN=local 下 toolchain 指令被忽略、Docker 构建用 go1.26.x，而本地/CI（setup-go go-version-file 消费 go 指令 + GOTOOLCHAIN=auto 下载）用 go1.27.3；Dockerfile:13-14 不变式注释仅声明「基座主版本须满足 go 指令」未覆盖新增 toolchain 指令，建议基座升 golang:1.27-alpine 或删 toolchain 指令（Dockerfile:15，apps/server/go.mod:5，go.work:1）。 — 2026-11-05 属实已修复：基座升 golang:1.27-alpine（Dockerfile:15），注释改为匹配 toolchain 指令（Dockerfile:13-14），go build/test 100.0% 绿。
- [x] 审核发现：本轮独立复核确认上一轮同范围 3 条发现均成立（gorm otel 0.1.6→0.1.16 无条件 import clickhouse+mysql 驱动 / gin 1.10.0→1.12.0 新增 import quic-go/http3 / x/crypto 伪版本 v0.58.1-0.20261009181718），工作树未提交改动正对这三条还原（otel 0.1.12 / gin 1.10.1 / x/crypto v0.58.0，删 ClickHouse/MySQL 间接依赖）；除 Dockerfile 一条外本轮未见新问题。 — 2026-11-05 属实已闭合：三条依赖回滚 + Dockerfile 升基座均已修复，go build/test 覆盖率 100.0% 绿，`go list -deps ./internal/app/bootstrap` 无 clickhouse/mysql/quic-go 残留。
- [x] 审核发现：c18321b 全量 Go 依赖升级无任何完成的绿色 CI——c18321b 自身 CI 运行 41708234028 被下一 push 整体 cancelled（全 12 job 均 cancelled，零 job 完成）；其上 21a25e6 唯一 CI 运行 41714023947 于 Go Checks 的 Go vet 步骤 2 分钟超时失败（已知基础设施抖动，非本 diff 回归），致该 job 的 lint / unit tests+coverage / race / build server binaries / Build WeKnora CLI 全部 skipped，Integration job（含 bootstrap 集成与 schema-parity 迁移链对账，最敏感于 gorm v1.25.7→v1.31.2 跨 6 minor 的运行期行为面：AutoMigrate 输出/命名/软删/关联）亦 skipped——即 CI 侧对本次升级只到「vet 抖动红 + 集成未跑」，唯一绿证据是本地（todo.md VULN-SERVIFY-001 闭单记录的 go build/test/make test 本地 exit 0），广域检索 ci.yml 无该两 SHA 的成功 run；建议对 21a25e6（或含工作树还原后的后续提交）rerun 一次 CI 让 Integration+schema-parity 实跑绿后再闭合本轮（apps/server/go.mod:43，.github/workflows/ci.yml:725）。 — 2026-11-05 属实，本地全量 go build/test 覆盖率 100.0% 绿；修复提交含依赖回滚 + Dockerfile 升基座，推送后 CI 将对本提交实跑 Integration+schema-parity 验证。

- [x] 审核发现：396345b 回滚 bloat 依赖不彻底——otelgin 从 0.72.0 降至 0.63.0（apps/server/go.mod:31），但同族 contrib 的 otelhttp 仍留在 0.72.0（apps/server/go.mod:32，未被回滚；bloat 前为 0.70.0），构成版本错配。已核实属实并修复：otelhttp 回滚至 pre-bloat 的 v0.70.0，与 otelgin 0.63.0 同为回滚侧；otel 核心保留 1.47.0（396345b 后即钉 1.47.0，实测降回 1.46.0 会使 `TestServerMainTracingSetupWarning` 的服务器无法 healthy——畸形 OTEL_RESOURCE_ATTRIBUTES 下的降级路径行为随版本变化，故核心不随本条回滚）。证据：`go build ./...` exit 0；`go test -count=1 ./...` 161 包全绿 exit 0（apps/server/go.mod:31-36；实测记录见本行与上一轮台账）

- [x] 审核发现：docs/pnpm-lock.yaml 未应用 docs/package.json 的 pnpm.overrides——锁内解析到 vite@5.4.21 / esbuild@0.21.5 / katex@0.16.47，而 package.json 的 pnpm.overrides（docs/package.json:26-29）强制 vite@6.4.4 / katex@0.18.2 / esbuild@0.25.0，二者相悖；实测 `pnpm install --frozen-lockfile`（在 /tmp 冷拷贝 package.json+pnpm-lock.yaml 复现）退出码 1、报 ERR_PNPM_LOCKFILE_CONFIG_MISMATCH，CI 可复现安装必失败（docs/pnpm-lock.yaml:1169/925/973，docs/package.json:26-29）。 — 2026-11-05 属实已修复：删除 docs/pnpm-lock.yaml（`git rm`），docs 唯一锁收敛为 npm 的 docs/package-lock.json（CI `npm -C docs ci`，.github/workflows/ci.yml:323）；`npm ci --no-audit --no-fund` 在冷拷贝复现 exit 0。
- [x] 审核发现：docs/ 新增 pnpm-lock.yaml 后同目录并存 package-lock.json（npm）与 pnpm-lock.yaml（pnpm）两个 lockfile，且 package.json 同时声明 npm 式 overrides（docs/package.json:20-23）与 pnpm.overrides（docs/package.json:26-29）两套覆盖；若实际构建/CI 走 npm（package-lock.json 仍在库）则新 pnpm 锁不被消费，反之亦然，双锁与双覆盖易漂移，建议二选一收敛（docs/package-lock.json，docs/pnpm-lock.yaml:1）。 — 2026-11-05 属实已修复：保留 npm 单锁（docs/package-lock.json，CI 唯一消费路径 .github/workflows/ci.yml:323/340），删除 docs/pnpm-lock.yaml 并移除 docs/package.json 的 `pnpm` 覆盖块（docs/package.json 现仅剩 npm 式 `overrides`）；npm 侧已生效 vite 6.4.4/esbuild 0.25.12/katex 0.18.10（docs/package-lock.json）。
- [x] 审核发现：go.work.sum 本次新增约 52 行 go.mod 校验和（含 cloud.google.com/go v0.123.0、golang.org/x/exp v0.0.0-20261014143641、honnef.co/go/tools v0.8.2 等图节点）超出提交所述 otelhttp 回滚语义范围，属 go1.27.3 toolchain 对全 workspace 模块图重新对账 / `go work sync` 的旁路产物；已实测 `go mod tidy -diff` 与 `go list -m all` 均 exit 0 且未改动任何文件，无功能影响，仅提示可在后续 `go work sync` 时收敛（go.work.sum:7）。 — 2026-11-05 属实已修复：实测确认 764201e 新增 52 行中仅 20 行为真旁路（go-openapi 系列、otelgrpc v0.72.0、x/exp 20261014、honnef.co/go/tools v0.8.2 等），其余 32 行为 go 工具稳态所必需（`go list -m all`/`go work sync`/`go mod tidy` 共同固定点为 358 行）。已将 go.work.sum 收敛至 358 行固定点（删 20 行），并验证跨 `go list -m all`/`go work sync`/`go mod tidy -diff`（根+apps/server）均稳定不再改动；CI tidy 守卫（.github/workflows/ci.yml:64-66）exit 0。
- [x] 审核发现：本轮 diff 未见问题（95d6789d..HEAD） — 审核通过，无需改动；证据：git log 95d6789d..HEAD 仅 1 提交（54e3fa8），改动仅 docs 锁收敛（删 docs/pnpm-lock.yaml、移除 docs/package.json pnpm 覆盖块）、go.work.sum 收敛至 358 行固定点、CI Go vet 超时 2→10 分钟及 todo 台账，无 Go/SQL/YAML 语义面；实测 `go -C apps/server mod tidy`+`go work sync`+`go list -m all` 均稳定、`go vet` exit 0、`npm -C docs ci` 冷装 exit 0 且锁无漂移、`npm -C docs run build` exit 0、`make test` exit 0 覆盖率 100.0%。 — 2026-11-05 审核通过
