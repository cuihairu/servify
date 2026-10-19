# Todo

需求清单。已完成项保留一行证据（commit + 测试数）。

## 未完成

- [ ] real 模式知识库验收：Dify / WeKnora / RAGFlow 真实服务与凭证（脚本入口 `*_ACCEPTANCE_MODE=real` 已就绪）——2026-10-16 复核：环境零凭证（`env | grep -icE 'dify|weknora|ragflow'` = 0）；三家 real 模式就绪但设计性拒绝本地私网（`test-dify-integration.sh:229` / `test-ragflow-acceptance.sh:449` / `test-weknora-integration.sh:236`）→ 解锁条件：真实远端端点 + 凭证——2026-10-19 R2 复核：`env | grep -icE 'dify|weknora|ragflow'` 仍 = 0，状态未变
- [x] CODECOV_TOKEN — 2026-10-15 用户配置 secret 后首传成功：CI 38731452145（f00eeaa）Go Checks「Upload coverage to Codecov」200（806,532 字节，coverage.out 2251 文件）；步骤恢复必需（fail_ci_if_error=true，移除缺 token 条件跳过——secret 吊销时 CI 如实红）
- [ ] 移动端语音消费：Android/iOS 语音上行与字幕渲染（按产品节奏）——2026-10-16 复核：V1 范围裁定仍生效（`sdk/packages/core/src/bindings/mobile.ts:13` voice `enabled:false, version:'reserved'`；`sdk/PROTOCOL.md:36` 移动端 V1 不消费不发送），双端仅帧类型定义无 voice 消费代码 → 解锁条件：产品拍板翻转 V1 范围 + 采集/上行/字幕三件套立项——2026-10-19 R2 复核：`mobile.ts:13` voice 仍 `enabled:false, version:'reserved'`、`PROTOCOL.md:36` 仍声明移动端 V1 不消费不发送，状态未变
- [x] Phase 3 语音：WebRTC 音轨下发（**服务端面已于 2026-10-15 落地**：语音通道 `webrtc-request` 信令 + PCMU 音轨 + 对侧路由 + wav 格式协商，帧词表 PROTOCOL §9.3；**客户端消费已于 2026-10-15 落地**——core VoiceChannel `enableAudioTrack()` 发起请求 + offer/answer 协商 + `voice:audio-track` 透出 + `voice:webrtc-error` 非致命回执 + 挂件 Audio 播放接入，94353c7）、端到端语音评估（研究项，2026-10-15 代决维持不排期——非交付项，不随本条收口而启动）、租户级配额限流（**口径已代决待核 2026-10-15，enforcement 同日落地**：docs/design/tenant-quota-policy.md——`limit + unit(requests|tokens) + window(hour|day|month)` 滚动窗口、超配额 429；三缺口补齐=Redis 固定窗计量 platform/quotameter + translation/delivery QuotaGate 检查点（ctx 租户键控，REST/实时/历史/语音统一过门）+ 窗口锚点入键名，默认 enabled:false，meter 故障 fail-open；configscope 租户覆盖值亦已代决落地 2026-10-16——`Resolver.ResolveTranslationQuotaLimit` 三层解析（system 默认 + tenant_configs / workspace_configs.translation_quota_json 覆盖，工作区优先；覆盖值 <=0 或无覆盖行回落上一层，provider 出错同样回落）+ QuotaGate 增 `QuotaLimitSource` 端口按 ctx 租户每请求解析（无主库部署传 nil 只用系统值）+ 迁移 000024 双表加列与 models 逐列对齐；只覆盖 `limit`（enabled/unit/window 保持系统级——window 进 Redis 计量键名）；scoped_config_handler 的 translation_quota 治理分支已于 2026-10-18 落地（已代决，载荷合并进风险/验证模板、history diff 与分段验证检查 `translation_quota_verified`/`translation_quota_override_verified`，medium 风险免审批——只覆盖 limit 故无需高危门）；**admin 前端首次 scoped-config 写面已于 2026-10-19 落地（本 commit，FIRE1019）**：Security 页新增「作用域配置覆盖」卡片——tenant/workspace 作用域选择（workspace_id 经 query 传请求作用域）、translation_quota limit 表单校验（整数≥0，0=回落系统值）+ change_ref/reason 必填/approval_ref 选填、PUT 按分节合并语义、配置变更历史表（风险/审批/治理/核验状态列）与详情抽屉（当前 vs 快照字段级 diff、治理状态与风险原因、核验模板逐项提交——必填检查项+证据每行一条+核验人分离提示、审批记录表单、回滚表单带确认+高危审批引用门），services/scopedConfig.ts + typings 全量契约类型；admin typecheck+build+lint 全绿）——可施工子项全部收口
- [x] 远程协助 RA-7 媒体桥接 + RA-5 信令归属 — 864f897 075055b：服务端 SFU-lite（trackForwarder 一读 N 写转发注册表、fanOut/replicate 投递、桥接加轨显式重协商推 `webrtc-offer`、下线持锁收口）+ 每条 PC 记录信令归属（`connByOwner` 按归属路由 answer/candidate，`SendToClient` 定向投递）+ 管理端应答服务端 offer（glare 排队）与桥接轨组流兜底；webrtc_bridge_test.go 单元分支全覆盖 + `TestWebRTC_LoopbackMediaBridge` 真实双 pion PC 端到端（RTP 转发+重协商收轨），realtime 包覆盖率 100.0%、race 三连过，admin typecheck+build 绿
- [x] 生产侧派单竞态收口遗留：通用 UpdateTicket 改派 agent_id 不调坐席负载（PUT 更新可改派，current_load 不增不减）；transfer 流负载增减以会话预读 fromAgentID 为准（会话行未锁，并发转派同会话可重复减 1）——两处与 ticket 行锁修复会双重计数，需单独设计。UpdateTicketModelWithStatusAndCustomFields 事务内锁工单行按库内 agent_id 算增减（哨兵 0→NULL 同 Unassign 口径，6 项 integration 回归红转绿）；transfer 锁会话行读库内 agent_id 为准（RuntimeAdapter.LockedTransferAgentID，postgres/mysql 加 FOR UPDATE），并发转派已指给目标客服时跳过负载增减（SyncTransferLoad 恒加目标，仅传 nil 来源仍会重复加）。另修 item #10 两处 CI 回归：handlers 直引 conversation/application 改走 delivery 窄契约（DecodeTranslationCache/TranslationCacheKey），swagger 生成物重跑入库（docs.go/swagger.json/swagger.yaml 13 行）
- [ ] admin pnpm audit 五项收口（2026-10-14 实测 1 high + 3 moderate + 1 low）：braces（GHSA-vfj7-8cjw-p6xm high，vulnerable <=3.0.3 全覆盖、patched <0.0.0 无修复，路径 @umijs/lint ESLint 工具链）、sprintf-js（GHSA-hp3w-g68c-fv3c moderate，vulnerable <=1.1.3 全覆盖——升 1.1.3 也不解、patched <0.0.0，路径 umi test 工具链）、elliptic（GHSA-848j-6mx2-7j84 low，<=6.6.1 无修复，路径 bundler-webpack crypto polyfill）三项等上游发版（2026-10-15 与 2026-10-16 两次复核：`Patched versions <0.0.0`；registry 级复核三家最新版仍停在漏洞边界（braces 3.0.3 / sprintf-js 1.1.3 / elliptic 6.6.1，`npm view` 无更新版），维持不做仓内动作）；react-router 两项 moderate（GHSA-wrjc-x8rr-h8h6 <Link> 反斜杠开放重定向、GHSA-337j-9hxr-rhxg 构造器注入）——2026-10-15 用户明令升级，override react-router@<7.18.0 → ^7.18.0 解析 7.18.4（bdd6cd5，admin typecheck+build 绿、CI 38715891005 全 job 绿、audit 5→3 两项 moderate 消单；umi 4 运行时路由无 e2e 兜底，残留风险记此，等 @umijs/max 跟进）——2026-10-19 R2 复核：registry 复查 braces 3.0.3 / sprintf-js 1.1.3 / elliptic 6.6.1 仍停在漏洞边界，上游未发修复版
- [x] 知识库选型决策：RAGFlow 立项口径、选择链优先级、新客户默认口径 — 2026-10-15 已代决待核（用户可推翻）：docs/design/knowledge-provider-selection-policy.md——新客户默认=代码默认 `""` + 交付面 pgvector 起步（维持 2026-09-21 拍板）、选择链=现行 `pgvector 直配 → local → ragflow → dify → weknora`、立项=RAGFlow 唯一新增投入/WeKnora 双路径/Dify 冻结/MaxKB 观察；与现行代码一致零改动，real 环境证据缺口归并条目 1
- [x] MetricsAggregator Snapshot 读取出口 — 2026-10-15 已代决待核：Prometheus `/metrics`（经 `SnapshotSeries` 桥）为唯一对外读取出口，退役零生产调用方的有损 `Snapshot()`（签名串形态），两个测试观察面改走 `SnapshotSeries` 断言语义等价；handlers 包 `-tags integration` 覆盖率 100.0%、app/server 绿（本轮 commit）
- [x] 远程协助 RA-8 SDK 半边 — e2d6397：SDK 记录 offer 载荷与协商状态（`lastRemoteAssistOffer`/`remoteAssistState`），WS connected 且 PC 存活、offered/connecting 态时重发同一 offer（PC 跨重连保留、协商终态清引用防重复，服务端幂等应答+终态旧 PC 回收收口）；sdk.test.ts 新增 RA-8 五例（待答重发/answer 丢失重发/已连不重发/结束后不重发/无会话不重发）全绿，demo-sdk bundle 重跑入库 e31846d
- [x] 会话消息译文持久化：Message 增 metadata 列（成本治理随 Phase 3 配额）— messages.metadata 列（000023，列级守卫钉住）+ 保留键 translations 按语言编解码（先到先得）+ 历史加载命中缓存免 LLM + 现译现写（失败静默）；响应键仍不落库。go test ./internal/... 全绿（含 -tags integration 4→7 项 infra 回归 + 6 子测试 handler 回归），go vet/gofmt 干净
- [x] retriever 会话语义统一：非空 session_id 查询滤掉全局摄入文档（pgvector 同构既有语义）——口径已定：会话 id 不是知识命名空间 id，KnowledgeID 留空由 provider 落回默认命名空间（1f190df 收口，全 provider 同构验证：pgvector/local 按 workspace_id、dify/ragflow 按 datasetID、weknora 按 KnowledgeBaseID，透传会话 id 即恒空）；租户维度检索为正解。会话级检索需会话级摄入（未立项，随会话知识库刀）；残留：ai_runtime_local_test.go 旧注释曾把修复前行为当现状，已随本轮纠正
- [ ] AI agent-loop 三工具接线：CustomerLookup / TicketLookup / Handoff 现传 nil Ports，调用即报 port is not configured（2026-10-11 拍板保持死路，等用户明令）——2026-10-16 复核：死路状态未变（`ai/delivery/enhanced_service.go:178-180` 三工具仍传 nil）→ 解锁条件：用户明令重启拍板——2026-10-19 R2 复核：三工具仍传 nil（文件现位于 `modules/ai/delivery/enhanced_service.go`），状态未变
- [x] modules 非 infra 层 GORM 写归属下沉 infra（约 70 文件，大重构；2026-10-14 收口：全模块 application 层已无 gorm 写——knowledge/ai 等直调均为 Repository 端口方法，agent/groups.go 的 gorm 导入也已随唯一键哨兵下沉：OpenDB 开 TranslateError（此前驱动约束原文恒不匹配 gorm.ErrDuplicatedKey，同名组建组/改名从 409 退化成 500）、infra duplicateGroupError 折 ErrGroupDuplicate、application 纯透传，全模块已无 gorm 导入）
  - [x] satisfaction（50 操作，新建 infra 层）：application 定 Repository 端口 + 7 个哨兵错误（文案与原 inline 串逐字一致，handlers 按字符串映射不变），infra/gorm_repository.go 逐字承接 SQL 并把 ErrRecordNotFound 映射哨兵；service.go 由 843 行 db 直调瘦为端口调用、零 gorm 导入；7 个 db 型测试文件 git mv 至 infra，应用层补 31 方法 stubRepo + 15 个纯端口单测恢复包内覆盖率归属，infra 补丢 customers 表用例收口客户查询原始错误分支；boundary rules 的 assembly 锚点由 pin 构造参数放宽为 pin 构造名。覆盖率回 100.0%，全量绿
  - [x] sla（32 操作，新建 infra 层）：application 定 Repository 端口 + 3 个哨兵错误，infra/gorm_repository.go 逐字承接 SQL（count/find 拆分保住「failed to count/list」双文案断言、PageSize>0 分页闸、byTier/trend 查询失败静默分支原样）；service.go 零 gorm 导入，CreateSLAViolation 的小写「sla config not found」与哨兵大小写差异保留；7 个 db 型测试文件 git mv 至 infra（monitorSLAViolations/getSLATrendData 改走导出 RunMonitorOnce 或移回应用层 stub 测试），应用层补 32 方法 stubRepo 分支单测（含 automation 分支嵌入接口 noop repo）恢复覆盖率归属；runtime_assembly 经 slainfra.NewGormRepository 装配，boundary 锚点同 satisfaction 放宽。sla 单测+integration 绿、handlers SLA integration 绿、worker 绿、全量 100.0%
  - [x] auth（19 操作，新建 infra 层）：application 定 Repository 端口（19 方法：不存在类返 ErrAuthUserNotFound 哨兵、重复检测转 int64 count 端口保住既有吞错、其余原样透传）+ 11 个哨兵错误（文案与原 inline 串逐字一致，handlers 按字符串映射不变）；infra/gorm_repository.go 逐字承接 SQL（LOWER(email)、COALESCE(token_version)+1、used_at IS NULL 原子核销、ReplaceRecoveryCodes 事务）；service.go/totp.go/oidc.go 零 gorm 导入（登录风险档位与 refresh 重放档位、2FA kill-switch、OIDC 自动建号回落逻辑原样）；14 个 db 型测试文件 git mv 至 infra（未导出符号改走导出 HookRandRead/HookCreateHS256JWT 缝线、svc.db 断言改导出 API 或 helper 回传 db、gorm.ErrInvalidDB 换哨兵），应用层新增 auth_helpers_unit_test.go（纯内部逻辑：claim 解析/会话字段归一化/ID 回退/OIDC 角色映射/登录风险与 refresh 重放档位）+ auth_stub_test.go（19 方法 stubRepo 分支单测：注册/登录/风险执行/2FA 全链/挑战 token 指纹/OIDC 自动建号撞唯一键回落）恢复覆盖率归属；router_auth 与 handlers 测试改经 authinfra.NewGormRepository 装配，boundary 锚点同前放宽为 pin 构造名。application/infra 单测 + handlers 绿、全量 100.0%
- [x] models 侧过渡别名最终删除（3481171，26 文件；models.go 9 个别名块 + 5 个 domain 导入摘除，automation/webhook/embedding 三文件删除，bootstrap/migrate、cmd/migrate、platform knowledgeprovider、knowledge_doc 测试随 domain 类型迁移；handlers 无别名引用、swag 重生成零漂移、boundary rules 无别名残留；别名载体禁令由 TestModelsDoesNotImportModulePackages 接管（379e8c9，变异验证：塞回 import 模块包的别名文件必红点名文件+包）。同轮补齐 CI 灰区 14 条未覆盖分支（cc43cb0/7d02e9b：丢表、fail-nth 查询回调、RowsAffected=0、postgres-named 方言包装四手法 + json.Marshal(map[string]string) 不可达死分支摘除），CI 38547016404 全 job 绿、覆盖率 100.0%）

## 已定口径

- tickets.session_id 保持可空不收紧：管理员独立建单与邮件建单两条路径不传 session_id，关闭前未结单拦截不依赖必填
- dependabot：有修复版本即升级，无修复版本保留并记录；excelize 两轮 bump 后消单（2026-10-14 实测 open=0，braces/sprintf-js 已不在告警面）；apps/admin 侧残留以 pnpm audit 为准，另记于上方五项条目
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
- [x] P1-7 Modules 与 legacy services/models 边界收口（modules→services 清零、六刀模块自有模型迁移、两类 parser 级守卫）— 8a3a624，TestModulesDoNotImportLegacyServices 绿；别名最终态 2026-10-14 删除后 TestModulesDoNotUseMigratedTypeAliases 退役、换代为 TestModelsDoesNotImportModulePackages（models→modules 反向 import 禁令，变异验证可拦别名回流）
- [x] P1-8 文本编码与仓库可读性（编码门禁、乱码存量清零）— check-text-encoding.sh 覆盖 1999 文件，0 非 UTF-8 / 0 BOM / 0 U+FFFD
- [x] P1-9 demo-sdk 生成链路一致性（sync-sdk-to-demo.sh、一键同步、漂移校验、CI 门）— check-generated-drift.sh 绿
- [x] P2-0 客户侧推荐问题与上下文联想（含 RQ-5 曝光/转化归因、ExposureSummary 出口、路径级限流）— 38566cf 3b8370e，CI 35688179671 绿
- [x] P2-1 多实例与高可用边界明确化（multi-instance-boundary 文档与实测口径）——2026-10-15 补回第 1 节测试锚点（P3-2 删 services 层时随 agent_service_redis_integration_test.go 丢失）：双 RedisRegistry 共享同一 Redis+DB 互见 presence、transfer load 与 TTL 过期收敛，退回进程内存储必红；锚点首上 CI 即抓到真竞态（041ffb6 Module Checks 红）：online 回灌经 GoOnline 整条重建本地条目，落在 AssignSession 本地变更与 Redis 同步之间时把未同步负载重置回过期快照——MergeRemote 改合并语义（元数据远端为准、负载/会话/时间本地保留）+ 变更与同步整体持 r.mu，redis_registry_status_echo_test.go 确定性复现窗口
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
- [x] docs 根目录双语配对（2026-10-16 用户令，59 篇实质文档全部配对：57 篇中文源配 .en.md、2 篇英文源配 .zh.md，源与译文首屏均挂语言切换器，代码块/命令/路径/专有名词原样保留，最长 980 行 <1000 故暂无译版 0 条）— 本轮 commit，docs build 绿 + check-text-encoding 绿。**注：其后缀布局已被下一条 i18n 目录布局取代**
- [x] docs 站 VitePress 官方 i18n 接路由（2026-10-16 用户令，只接路由不翻译）：`docs/.vitepress/config.ts:21` 加 `locales`（`root` = English/en，`zh` = 简体中文/zh-CN），两 locale 的 `themeConfig.nav` / `sidebar` 分别由 `site-structure.ts` 独立维护（`docsNavEn`/`docsSidebarEn` 与 `docsNavZh`/`docsSidebarZh`，中文侧链接带 `/zh` 前缀）；文件布局按官方惯例改为英文在 `docs/`、中文在 `docs/zh/`，原 `*.zh.md` / `*.en.md` 后缀全部并入目录（`.en`→根、`.zh`→`docs/zh/` 同名路径，87 篇中文 / 59 篇英文，后缀清零），手写语言切换行删除（改由主题 `VPNavBarTranslations` 自动渲染）— 校验：`pnpm -C docs build` 绿（44.49s），`dist/zh/` 产出 87 个 HTML、`dist/` 根 59 个，`<html lang>` 分别为 `en` / `zh-CN`，EN 页切换器实测链到 `/zh/testing-pyramid.html`；`make text-encoding-check` 绿（2193 文件）、`scripts/check-repo-hygiene.sh` 绿；41 条 nav/sidebar 链接全命中真实文件（0 断链，`ignoreDeadLinks: true` 会静默吞断链故手工核）。迁移中修复一处副本缺陷：`testing-pyramid` / `release-versioning`（原英文源 + `.zh.md` 译本）英文副本一度同时落入 `docs/` 与 `docs/zh/`，中文译文丢失，已自 HEAD 取回中文正文覆写 zh 侧；全量比对 87 对页面现已 0 对雷同。缺口（28 篇中文独有页暂无英文正文）见「未完成」首条登记，本轮不做


- [x] docs 子目录英文正文补齐（i18n 目录布局后口径：中文在 `docs/zh/`、英文落根 `docs/` 同名路径，切换器由 VitePress `locales` 自动渲染故不手写）——28 篇全部落地：25 篇真译（design 3 + implementation 19 + research 1 + review verification 1 + contributing 1 + superpowers specs 1；contributing 中文源本身已是英文故逐字复制；competitive-survey 的 `title:` 全角冒号译成半角冒号致 frontmatter YAML 解析 build 红、加引号修复；16 张竞品截图复制进 `docs/research/assets/` 后打包正常）+ 2 篇 >1000 行占位页（review/chatgpt-review-2026-10-04 1349 行、superpowers/plans/2025-05-01-pgvector-knowledge-base 3112 行，标「暂无英文译版」+链中文原文）；verify 28/28 clean、docs build 绿、check-text-encoding 绿——残留英文侧栏收录已于同日随 i18n 布局收口补齐
- [x] docs 站 i18n 目录布局收口（并发会话遗留的在途改动经 R1025PATROL 代决授权合成提交 9efb65a：中文全部迁入 `docs/zh/`（59 根目录 + 28 子目录 + contributing），根 `docs/` 换英文正文并删除 `.en.md`/`.zh.md` 双文件形态，VitePress `locales` 自动渲染切换器；540 个 dist-verify 构建产物剔出 staged 并补 .gitignore；英文侧栏补齐 28 篇收录 + implementation 独立分组 + nav 入口，与 zh 侧栏逐组对齐；28 篇中文原文不再只活在 index）— 9efb65a，docs build 绿 + check-text-encoding 绿 + Docs Pages 38924039803 绿
- [x] 审核整改（2026-10-18 独立审核，见 docs/review/luna-review-2026-10-18.md）：按风险修表一、以代码为准修表二、表三只评估分期；未 bump 版本。`205cff0` 已在 `origin/main`；全量 Go 4209 测试/158 包通过、覆盖率 100%，race、golden、release-check（端口 18081）、SDK 199 测试/build/typecheck/lint、docs build、encoding/hygiene 均通过；golangci diff-only 0 issue（完整 lint 仍报 11 个未修改文件基线项）。
  - [x] 表一 P1 限流 key/bucket：核实旧实现以未验证请求头取 key 且 map 无界；改为 socket 对端 IP、每 limiter 上限 4096、闲置 10 分钟后按分钟扫除，满载走共享 overflow bucket，移除 header key 与 key 白名单配置。证据：`apps/server/internal/middleware/ratelimit.go:25-71,115-127,168-184`；回归 `apps/server/internal/middleware/ratelimit_test.go:192-295`；配置/schema `apps/server/internal/config/config.go:502-510`、`config.yml:218-225`。
  - [x] 表一 P2 token 配额语义：核实 token 为事后记账且 batch 无 usage；明确为软阈值（非硬预算），非空 tokens batch 返回不可计量错误、不调用 provider。证据：`apps/server/internal/modules/translation/delivery/quota_gate.go:35-40,95-102,118-134,138-159`；回归 `apps/server/internal/modules/translation/delivery/quota_gate_test.go:128-184`；规范 `docs/design/tenant-quota-policy.md:61-74,101-119` / `docs/zh/design/tenant-quota-policy.md:50-58,76-87`。
  - [x] 表二 WS 访客 token：改协议与中英文移动端设计，说明 `guest_token.required` 开启时校验、默认关闭；未改业务代码。证据：`sdk/PROTOCOL.md:11-17`、`docs/mobile-sdk-design.md:198-202`、`docs/zh/mobile-sdk-design.md:198-202`；实现 `websocket_hub.go:302-313` / `runtime_assembly.go:226-232`。
  - [x] 表二 Portal 作用域：文档改为 public 读取已合并 system/tenant/workspace、管理端治理 UI 尚缺；未改业务代码。证据：`docs/configuration-scopes.md:297-303`、`docs/zh/configuration-scopes.md:297-303`；实现 `router_public.go:16-21`、`resolver.go:154-174`。
  - [x] 表二 AI/knowledge/routing 作用域：文档改为部分落地，列出 provider/quota 覆盖与 routing policy 缺口；未改业务代码。证据：`docs/configuration-scopes.md:299-303`、`docs/zh/configuration-scopes.md:299-303`；实现 `gorm_provider.go:26-86,118-178`、`resolver.go:221-302,331-359`。
  - [x] 表三能力评估（仅建议，未实现）：真实 Dify/WeKnora/RAGFlow 验收列 Phase 0，等真实端点/凭证后用现有 real 脚本补证；依据 `todo.md:7`、报告表三。
  - [x] 表三能力评估（仅建议，未实现）：浏览器双端媒体验收列 Phase 1，先以 Playwright 假媒体覆盖采集/信令/重协商，再把真实设备列手工发布门；依据 `docs/remote-assistance-status.md:16,151`、`webrtc_bridge_test.go:562-565`。
  - [x] 表三能力评估（仅建议，未实现）：Scoped config 管理端写面列 Phase 1，依赖服务端治理 API 稳定；先做 scope 选择、字段校验、effective value 与审计/历史视图，再逐配置族扩展；依据 `docs/design/tenant-quota-policy.md:166-178`、`scoped_config_handler.go:58-68,167-178`。
  - [x] 表三能力评估（仅建议，未实现）：移动端语音上行/字幕列 Phase 2，先由产品明确翻转 V1 范围，再拆采集权限、上行、字幕消费与双端验收；依据 `mobile.ts:7-14`、`sdk/PROTOCOL.md:35-37`、`todo.md:9`。
  - [x] 表三能力评估（仅建议，未实现）：AI agent-loop 工具接线列 Phase 2，先拍板客户上下文授权与 Handoff 副作用确认，再分 Customer/Ticket 只读上下文与显式转接；依据 `enhanced_service.go:176-181`、`docs/implementation/13-ai-agent-loop.md:9-10`、`todo.md:19`。
  - [x] 表三能力评估（仅建议，未实现）：严格 token 预算列 Phase 0 决策、Phase 1 设计；先扩 usage 合约覆盖 batch，再做原子预留/结算与 meter 故障策略，未具备前不得宣称硬成本封顶；依据 `apps/server/internal/modules/translation/delivery/quota_gate.go:95-100,118-134,141-159`、`docs/design/tenant-quota-policy.md:65-69,111-119`。
- [x] 文档时效性清理（2026-10-18 用户令：很多文档已过时）——18 篇文档（EN 9 + zh 镜像 9）13 处过期修正、零业务代码改动：v1-convergence-plan/delivery-priorities 加执行状态注记（B0–B4 已过闸，演进 backlog 只留薄壳收口）；implementation/04+07 「仅实现 web sdk」改为 web/API/app 三面落地（sdk/packages/* + sdk/android + sdk/ios，ACCEPTANCE-M3/M4）；implementation/01 入口未接线改为 RunStandalone→BuildApp 已接线（standalone.go:84-89）；backup-and-recovery 迁移链头 23|f→24|f（000024_translation_quota_scoped_config）；web-sdk.md 语音端点 /ws/voice→/api/v1/ws/voice、WebServifyConfig 锚点改别名（bindings/web-sdk.ts:4）、6 个配置字段类型按 types.ts:6-37 重取（AuthProvider/ReconnectPolicy/RemoteAssistConfig/WebSocketFactory/CapabilitySet/onTokenRefreshRequired→void）、ServifySDK 方法表 27 行行号锚点全量重锚（sdk.ts 129→134 等）、vanilla init 98→97；security-baseline-operations 限流路径清单补 /public/suggestions/ 与 /api/v1/remote-assist/（对齐 config.yml 与 security.go:19-29 必需集）；testing-pyramid integration 38→41 个测试文件。方法：README+docs 全量扫描（README 全锚点核验干净）+ 12 项发现逐条 self-verify 后再改；命令示例 make -n 干跑 + 脚本 bash -n 通过；docs build 绿 + check-text-encoding 绿（2310 文件）。
