# 实时翻译设计（语音 + 聊天文本）

> 状态：预研设计 + Phase 0 已落地（聊天文本翻译）+ Phase 1 全部落地
> （刀一偏好存储与 REST 面、刀二 hub message-translated 帧、刀三 viewer
> 角色双面 + 坐席 → 访客方向翻译、收尾历史消息批量标注、消费半边 core
> 事件面 + 管理端工作台渲染）+ Phase 2 刀一已落地（ASR/TTS provider
> 抽象与配置面，`platform/asr`/`platform/tts`）+ 刀二 provider 面已落地
> （OpenAI 兼容口进 factory switch：`platform/asr/openai` 实时转写 WS +
> `platform/tts/openai` 逐句合成）+ 刀二b-1 管线半边已落地（分句器 +
> 每说话方语音管线 + 逐句翻译上下文尾窗）。WS 面（音频上行 + 字幕帧族
> 过 PROTOCOL §5 流程）是刀二b-2；移动端消费后续刀。
> 本文是"大模型实时翻译"能力的设计基准：整体链路、延迟预算与分句策略、
> 模型选型与成本、隐私与安全、备选方案与取舍、分阶段落地计划。
>
> 与现状的接口关系：LLM 出站统一走 `platform/llm`（`LLMProvider`，
> openai 兼容 / anthropic / mock 三 provider，`factory.New` 唯一构造入口）；
> 实时通道复用 WS hub（`platform/realtime/websocket_hub.go`，按 session 广播）
> 与 WebRTC/TURN 设施（远程协助语音/屏幕同链路）。

## 0. 目标与非目标

**目标**：跨语言客服会话中，双方用各自语言实时沟通——
1. 聊天文本：对方消息即时以我的语言显示（译文可折叠/对照原文）；
2. 语音通话：我说中文、对方听英语（字幕 + 合成语音双形态）。

**非目标（本期）**：文档级长文本翻译（走批处理）；离线端侧翻译模型；
自动多语言知识库索引（已有 RAG 链路独立演进）。

## 1. 整体链路

### 1.1 聊天文本翻译（Phase 0，已落地）

```
客户端（坐席 admin / 访客 SDK）
  └─ 对到达的消息调用 POST /api/v1/translation/translate {text, target_lang, source_lang?}
       └─ modules/translation（application.Service）
            ├─ 参数校验（必填/4000 字符上限/BCP-47 语言标签）
            ├─ 提示词组装（system 定纪律 + user 携语言指令与待译文本）
            └─ llm.LLMProvider.Chat（与首答/Copilot 同 provider 同参数）
                 └─ 译文回显（text/source_lang/target_lang）
```

- 服务端无状态、单条消息粒度；"实时性"来自客户端逐消息即时调用（目标
  P50 < 800ms，见延迟预算），不需要为文本链路引入流式。
- 落点：`apps/server/internal/modules/translation/`（application/delivery，
  domain/infra 为后续阶段占位）；路由挂管理面
  （agent/admin/service + EnforceRequestScope），`docs` API 注解齐全。
- 复用面：provider 配置（`ai.*`）、超时/参数（`llmfactory.RuntimeParams`）、
  错误口径（参数 400 / 未装配 503 / 上游 502 / 超时 504）。

### 1.2 语音实时翻译（Phase 2+，目标链路）

```
采集端（浏览器/SDK）
  ├─ getUserMedia(Audio) → AudioWorklet 20ms 帧 → Opus 编码
  │    （可走 WebRTC 与远程协助同 PC，也可独立 DataChannel/WS 二进制帧）
  ▼
服务端 translation pipeline（每说话方一条）
  1. 流式识别 ASR        ：WebSocket 流式协议（Deepgram/火山/阿里/AssemblyAI，
                          或 self-host Whisper/faster-whisper + 分块）
                          → partial hypotheses（增量）+ final（带 segment 边界）
  2. 分句与稳定化        ：标点/语义双触发切句（见 §2），partial→final 去抖
  3. 大模型翻译          ：llm.LLMProvider（与 Phase 0 同 provider 体系），
                          逐句翻译 + 上一句尾作为上下文防割裂；
                          可选 ChatStream 首 token 提前下发
  4. 分发
     ├─ 字幕：WS 下行帧 translation-delta / translation-final（按 session 广播，
     │        payload 含 speaker/seq/text/lang，客户端做增量渲染）
     └─ 播报：TTS（OpenAI TTS / Azure / ElevenLabs，OpenAI 兼容口优先）
              → 服务端混音下发（WebRTC track）或客户端本地播放（URL/音频帧）
```

关键设计决策：
- **识别与翻译在服务端串联**，不在客户端做 ASR——服务端已有 provider 抽象、
  审计与限流位置，且两端（坐席/访客）能力不对等（访客端弱设备）。
- **TTS 优先走"服务端合成、WebRTC 下发"**的备选被否决为 Phase 3：先落地
  "字幕 + 客户端播放"（TTS 返回音频 URL 或 base64，客户端 Audio 播放），
  避开服务端混音/回声消除复杂度；WebRTC 音轨下发在 Phase 3 与远程协助
  媒体桥接（RA-7）共用 SFU-lite 基建。
- **每说话方一条 pipeline**，双向翻译 = 两条 pipeline 独立运行，避免
  单 pipeline 语言状态互相污染。

### 1.3 会话语言偏好（Phase 1 刀一落地存储，刀三落地 viewer 角色双面）

自动翻译的开关是**会话级**的：坐席为某个客服会话设一次目标语言，该会话之后
到达的消息才走自动翻译。因此偏好存储是 Phase 1 的前置依赖（Phase 0 的
`translate` 端点是"一次性、无状态"口径，不依赖任何存储）。

```
双面单一注册点（AuthMiddleware 认证即可，与 translate 端点同款；
读向由服务端按认证主体推导，不接受请求方自报）：
  ├─ agent/admin/service 主体 → agent 读向（坐席读译文的目标语言）
  └─ end_user 主体 → visitor 读向（访客读译文的目标语言；
       强制会话绑定：token 的 session_id 必须与路径一致，否则 403）
       GET    /api/v1/translation/preferences/:session_id   查询（未设置=200 空串）
       PUT    /api/v1/translation/preferences/:session_id   upsert 目标语言
       DELETE /api/v1/translation/preferences/:session_id   清除（幂等）
            └─ modules/translation
                 ├─ application.PreferenceService（校验/规范化：BCP-47 子集、小写）
                 └─ infra.GormPreferenceRepository
                      （pg 迁移 000016：(session_id, viewer_role) 复合唯一 /
                        sqlite AutoMigrate）
```

设计口径（与 assist/macro 等模块同款，避免同一仓两套租户语义）：

- **租户/工作区取自认证 ctx**，不接受请求方自报；读写面按 scope 收紧，
  scope 为空即不过滤（本地 dev / 存量链路兼容）。
- **跨 scope 命中与不存在同语义**（不回显存在性）：upsert 撞上他租户同
  `(conversation_session_id, viewer_role)` 的行时返回 409，而不是覆盖或
  报错"已存在"。同一会话两个读向各一条偏好（刀三：agent 与 visitor 各自
  独立读写、互不覆盖）。
- **偏好表与会话表解耦**：允许先于首条消息设置偏好（会话由首条消息建行），
  因此本表不挂 conversations 外键。
- **未设置不是错误**：读取返回 `target_lang=""` + 200；清除无行也返回 200。
  自动翻译链路据此"无偏好即不翻译"（见 §1.4）。
- 语言标签与 `translate` 端点同口径：BCP-47 常用子集、统一小写
  （`zh-CN` → `zh-cn`），非法标签 400。

### 1.4 会话消息自动翻译帧（Phase 1 刀二/刀三，服务端双向已落地）

```
访客 WS text-message 落库（刀二，hub 路径，agent 读向）
  └─ hub 异步（与 AI 首答同样走 goroutine，不阻塞广播）
       ├─ 读该会话语言偏好：无偏好 → 静默跳过（不发帧、不记错误）
       ├─ Translate(原文 → target_lang)：与 Phase 0 同 provider/同出站参数
       └─ 广播 message-translated（按 session_id，与原文广播同通道）
            Data: {original, content, source_lang, target_lang}

坐席 HTTP 发送消息成功（刀三，发送口路径，visitor 读向）
  └─ ConversationWorkspaceHandler.SendMessage 落库/广播后异步旁路
       （同一 RealtimeTranslateService 契约、同一帧型；30s 超时）
```

帧契约要点：

- **独立帧而非改写 `text-message`**：不识别新帧的既有客户端行为完全不变
  （core SDK 的 switch 落 default 忽略），译文与原文并存、可对照折叠。
- **关联靠 `original`**：翻译是落库后的异步旁路，没有消息 ID 可挂（当前
  会话写入口只返回 error，不回传 message id）。客户端按"内容相同、且是本会话
  最近一条未挂译文的消息"匹配；同内容连续重复消息的匹配结果可能后到覆盖，
  属已知边界。
- **失败只记 Warn**：翻译是增强能力，不影响消息主链路；provider 未配置
  （`ErrTranslationUnavailable`）视为"功能未开启"静默跳过，避免未部署 AI 的
  部署每条消息刷警告。
- **方向边界**：两个方向都已落地（刀二 hub 落库路径 = 访客 → 坐席，消费
  agent 读向；刀三 `ConversationWorkspaceHandler.SendMessage` 发送口路径
  = 坐席 → 访客，消费 visitor 读向）。同一应用服务
  （`RealtimeTranslateService`）构造期绑定读向的两个实例，非新链路。

### 1.5 历史消息批量标注（Phase 1 收尾，已落地）

工作台历史分页（`GET /api/omni/sessions/:id/messages`，注意 omni 面无
`/v1` 前缀）在该会话设有
agent 读向偏好时，把页内**访客侧**（sender ≠ agent）未带译文的非空消息
合并为一次批量翻译，译文以 §4.4 的 metadata 保留键
（`translation`/`translation_lang`）附在响应 DTO 上：

```
ListMessages 加载 + 时间序整理
  └─ annotateHistory（同步，响应前）
       ├─ 过滤：sender == agent / 空内容 / 已带 translation 键 → 跳过
       ├─ HistoryTranslateService（agent 读向）：无偏好 → 静默原样返回
       ├─ application.BatchTranslate：多段合并单次 LLM 调用
       │    （编号分段协议 <<<SEG n>>>，标记序列必须恰好 1..N，
       │      失配/注入扰动 → 自动退回逐条 Translate，fail fast 上抛）
       └─ 译文只写响应 DTO，不回写存储；任何失败静默（历史加载不受影响）
```

口径要点：

- **只写响应不落库**：偏好随时可改，译文落库会造成语言陈旧与回滚困难；
  代价是每次翻页都重新翻译（成本治理见 §3.2 Phase 3 配额）。
- **批量退路保底**：分段协议失配（模型漏段/乱序/空段/正文注入段标记）时
  整批退回逐条 `Translate`，行为不差于单条链路；provider 错误 fail fast。
- **已知边界**：会话消息存储（`models.Message`）暂无 metadata 列——访客
  补拉 DTO 的 metadata 字段走另一条装配路径，工作台历史标注为响应级、
  不持久化；存储列与跨页缓存留后续刀。
- **段数上限**：单批 ≤ `MaxBatchTexts`（20），超限返回哨兵错误
  `ErrTranslationBatchTooLarge`；历史分页 ≤ 200 由调用方分批。

### 1.6 客户端消费面（Phase 1 消费半边，已落地）

服务端在 Phase 1 刀二/刀三/收尾把译文都产出到了，剩下的是两端各取所需：

```
core SDK（访客侧，实时）
  └─ WS message-translated 帧 → 独立 message-translated 事件（§4.5）
       └─ 渲染方按 original 关联到最近一条未挂译文的消息，原文兜底
  └─ 历史补拉（VisitorMessage.metadata）→ readMessageTranslation 读 §4.4 键

管理端工作台（坐席侧，实时 + 历史）
  └─ 顶部语言下拉 → GET/PUT/DELETE 偏好端点（管理端主体落 agent 读向）
       └─ 切语言后重拉历史（译文只写响应不落库，必须重拉才生效）
  └─ 访客消息按 metadata.translation 并排展示译文 + translation_lang 标签
       └─ 无译文键 → 只显示原文（翻译是增强，不是替代）
```

口径要点：

- **两条载体分工**：实时走 §4.5 独立帧（携带 `session_id` + 语言对，无需
  回查），历史走 §4.4 metadata 保留键（分页 DTO 天然携带，零额外请求）。
  管理端没有会话消息 WS 订阅（只有远程协助信令 socket），故它只用后者。
- **读向一致性**：管理端主体固定落 agent 读向，与服务端历史标注的读向一致；
  若误配成 visitor 读向，译文不会出现（但不会串到对面）——服务端按认证
  主体推导角色，请求方无法越权写对方读向。
- **移动端待接**：Android / iOS 目前对 `message-translated` 显式
  `UnknownIgnored`（`sdk/protocol-fixtures/11-message-translated.json`
  的 `mobile` 断言声明该偏差），接入时更新断言与 PROTOCOL §4.5 状态行。

## 2. 延迟预算与分句策略

### 2.1 预算（语音端到端，说出 → 对方看到/听到）

| 环节 | 预算（P50） | 说明 |
| --- | --- | --- |
| 采集+编码+上行 | ≤ 120ms | Opus 20ms 帧 + 网络往返 |
| ASR partial | ≤ 300ms | 流式协议天然增量；partial 只驱动"正在说"反馈 |
| 分句触发 | 0–700ms | 静音 500ms 或标点/语义边界即触发，见下 |
| LLM 翻译 | ≤ 600ms | 逐句短文本；ChatStream 首 token ≤ 300ms 可提前下发 |
| 字幕下行 | ≤ 50ms | WS 既有链路 |
| **字幕合计** | **≈ 0.8–1.7s** | 体验及格线 2s 内；final 落定 ≤ 1.5s |
| TTS 合成 | ≤ 800ms | 逐句合成；流式 TTS（chunked）Phase 3 |
| **语音合计** | **≈ 1.5–2.5s** | 同声传译的"滞后半句"体验，可接受起点 |

文本链路（Phase 0）：单次 REST + 单次 Chat，预算 P50 ≤ 800ms（30s 硬超时，
超时 504）。逐句预算超限不重试整段，只重试当前句。

### 2.2 分句策略（语音）

双触发，先到先切：
1. **静音触发（主）**：VAD 尾点静音 ≥ 500ms 切句（ASR 流多自带 VAD 事件）；
2. **边界触发（辅）**：final 文本以 。？！.?!? 结尾，或长度 ≥ 60 字切句
   （防长句霸屏与翻译超时）。

稳定化规则：
- partial 只更新"正在说"行，不进翻译；仅 final 进翻译队列；
- final 追加到达（同句二次 final）按 seq 去重覆盖；
- 翻译上下文携带**上一句原文+译文尾窗**（≤ 200 字符）控制时延与费用。

## 3. 模型选型与成本

### 3.1 选型原则

- **翻译 LLM**：与现有 `ai.provider` 体系同源（OpenAI 兼容口优先），
  逐句短翻译用小而快的模型即可（如 gpt-4o-mini / 旗-haiku 档），
  不需要旗舰模型；温度沿用全局（建议部署配置 ≤ 0.3 保一致性）。
- **ASR**：托管流式 API（Deepgram / AssemblyAI / 火山 / 阿里）起步，
  按可用区与合规二选一；self-host（faster-whisper）作降级备选，
  延迟与运维成本更高，仅在数据不出域要求下启用。
- **TTS**：OpenAI 兼容 TTS 口起步（tts-1 档），Azure/ElevenLabs 备选。

### 3.2 成本量级（公开价目近似，仅供容量规划）

按一次 30 分钟双语通话（双方各说 15 分钟，约 2.5k 词/方）估算：
- ASR 流式：约 $1–3/小时 → $0.5–1.5；
- 翻译 LLM（小模型，逐句约 300 in + 300 out tokens）：$0.01–0.05 级，可忽略；
- TTS（tts-1 档）：约 $15/1M 字符 → $0.1–0.3。
即**语音翻译主要成本在 ASR+TTS（≈$1–2/通话小时）**，文本翻译成本可忽略
（单条消息 < $0.0001）。

控制手段：按租户配置开关与配额（复用 configscope 租户覆盖模式）、
逐句预算熔断（超时句降级为仅字幕/仅原文）、会话级 token 计量落
business metrics（既有 `rt.BusinessMetrics` 口）。

## 4. 隐私与安全

1. **数据最小化**：语音 pipeline 只保留 final 文本与译文（partial 即弃）；
   译文/原文默认不落库（字幕是会态数据）；聊天翻译无服务端存储（Phase 0
   已实现为无状态）。录音/回放仅当用户显式同意（对齐远程协助 RA-4 consent）。
2. **传输**：全链路 TLS（客户端→服务端已有）；ASR/TTS 出站仅经服务端，
   密钥不落客户端；provider 域名与 key 走 `ai.*` 既有配置面与租户覆盖。
3. **提示注入**：待译文本视为数据不视为指令——system 明确"只输出译文"；
   Phase 0 提示词已含分隔与"不加解释"纪律；后续对消息内嵌 prompt 注入
   样例做 golden 用例回归。
4. **权限与审计**：翻译端点在管理面鉴权链（AuthMiddleware +
   EnforceRequestScope + RequirePrincipalKinds）之下；写操作审计沿用
   AuditMiddleware；语音链路帧按 session 归属广播（hub 既有按
   session_id 隔离），不新增跨会话通道。
5. **合规**：PII 不经第三方 ASR/TTS 需在租户级提供 self-host 降级
   （Whisper 本地 ASR + 开源 TTS），列为 Phase 3 可选项而非首发。
6. **滥用面**：输入长度（4000 字符）与频次限制；后续按租户配额
   （translation.quota）限流，超配额 429。

## 5. 备选方案与取舍

| 方案 | 描述 | 取舍 |
| --- | --- | --- |
| A. 客户端整体翻译（浏览器内置 Translator API / 端侧模型） | 端到端零服务端成本、零延迟 | 能力碎片化（浏览器覆盖不一）、无审计/一致性与术语控制、无法支撑语音播报；仅作为未来访客端"免费档"补充，不作为主链路 |
| B. 专用翻译 API（DeepL / 云厂商 MT） | 质量/延迟最优、便宜 | 引入第二套 provider 体系与出站面；通用客服语域 LLM 小模型质量已够；LLM 还能承接语气保持与占位符纪律。保留为 `ai.provider=deepl` 式扩展位，不首发 |
| C. 语音端到端模型（GPT-4o realtime / 双向语音模型） | 单模型省去 ASR/TTS 拼装 | 延迟与体验最优但成本高一个量级、供应商锁定强、无法复用既有 per-utterance 审计；Phase 3 评估 |
| D. 服务端混音下发（翻译语音走 WebRTC track） | 对方"听到"翻译音轨最自然 | 依赖服务端 SFU 媒体桥接（远程协助 RA-7 同基建）；先做客户端播放（Phase 2），D 并入 RA-7 之后（Phase 3） |

**选定路线**：Phase 0 文本 LLM 翻译（已落地）→ Phase 2 服务端 ASR+LLM+TTS
拼装、字幕先行 → Phase 3 借 RA-7 媒体基建切 WebRTC 音轨下发，并评估端到端
语音模型（C）作高端档。

## 6. 分阶段落地计划

| 阶段 | 范围 | 依赖 |
| --- | --- | --- |
| **Phase 0（已落地）** | 聊天文本翻译：`modules/translation` + `POST /api/v1/translation/translate`；mock provider 单测全覆盖 | 无新增依赖（复用 `platform/llm`） |
| **Phase 0.5（服务端半边已落地）** | 访客面同端点：路由单一注册点、AuthMiddleware 即可（访客 token 可调，`router_auth.go`）；剩 SDK 半边：`metadata.translation` 字段约定 + SDK 便捷调用 | SDK 契约（`sdk/PROTOCOL.md` + fixtures） |
| **Phase 1 刀一（已落地）** | 会话语言偏好存储 + 管理面 REST 面（`translation/infra` GORM 仓储、pg 迁移 000015、`GET/PUT/DELETE /api/v1/translation/preferences/:session_id`）；见 §1.3 | 无新增依赖 |
| **Phase 1 刀二（服务端半边已落地）** | WS 自动翻译帧：hub 在消息落库后异步翻译并广播 `message-translated`（按会话语言偏好）；见 §1.4 | 刀一的偏好存储（已就绪） |
| **Phase 1 刀三（已落地）** | 偏好表 viewer 角色维度（迁移 000016，`(session_id, viewer_role)` 复合唯一）+ 双面单一注册点（end_user 经会话绑定校验读写 visitor 读向）+ 坐席 → 访客方向自动翻译（`SendMessage` 发送口异步旁路） | 刀二 |
| **Phase 1 收尾（已落地）** | 历史消息批量子段翻译：工作台历史分页按 agent 读向批量翻译访客消息，译文以 §4.4 metadata 保留键附在响应 DTO（不落库）；见 §1.5 | 刀三 |
| **Phase 1 消费半边（已落地）** | core SDK 消费 `message-translated` 帧（`WSMessage` 联合成员 + 同名事件 + `MessageTranslation` 载荷，见 §4.5）；管理端工作台消费历史 metadata 译文并提供坐席读向语言下拉（读写偏好端点）——见 §1.6 | 刀二/刀三/收尾的服务端面（均已就绪） |
| **Phase 2 刀一（已落地）** | ASR/TTS provider 抽象与配置面：`platform/asr`（流式会话 + 事件通道契约，VAD/partial/final 事件种种类对齐 §2.2 分句策略）+ `platform/tts`（逐句整段合成契约）+ 各自 mock 与 factory（llm 同款收口：唯一构造入口、mock 不进 switch）+ `ai.asr.*`/`ai.tts.*` 配置面（provider 空 = 未启用，装配层跳过接线） | 无新增依赖（契约 + mock 零网络） |
| **Phase 2 刀二 provider 面（已落地）** | OpenAI 兼容口优先（§3.1）：`platform/asr/openai`（实时转写 WS 协议 `/v1/realtime?intent=transcription`：服务端 VAD 尾点静音 500ms 对齐 §2.2、pcm16 24kHz 单声道、协议事件 → 契约事件映射、server error/读断 → `EventError` 会话破损）+ `platform/tts/openai`（`POST {base_url}/audio/speech` 逐句整段合成，非 2xx 包装 `ErrUpstream` 供 §3.2 预算熔断识别）；两者 `base_url` 可指向兼容网关（ws URL 由 https→wss 推导），进入 factory switch（`ai.asr.provider=openai`）。语音管线（final → 分句翻译 → 字幕帧 → TTS 消费）是刀二b | 无新增依赖（gorilla/websocket 已在依赖树） |
| **Phase 2 刀二b-1 管线半边（已落地）** | 分句器 `modules/translation/domain.SentenceAssembler`（§2.2 双触发先到先切：标点/60 字上限沿字符位单趟扫描、VAD 尾点 Flush、同 seq 二次 final 覆盖、轮切换防御性收残）+ 语音管线 `application.VoicePipeline`（每说话方一条：ASR 事件流 → 分句 → 逐句翻译带上一句原文+译文尾窗 ≤ 200 字 → TTS 逐句合成；`VoiceSink` 产出接口交付层适配，partial 只透传"正在说"；翻译失败句降级原文不合成、TTS 失败句仅字幕，管线不断）+ `TranslateCommand.Context` 上下文尾窗提示词扩展。WS 面（音频上行 + 字幕帧族，过 PROTOCOL §5 流程）是刀二b-2 | 无新增依赖 |
| Phase 2 刀二b-2 | 语音链路 WS 面：音频上行通道 + 字幕/音频下行帧族（新帧类型过 PROTOCOL §5 流程：fixtures + 三端回放测试）+ 装配接线 | 刀二b-1（已就绪） |
| Phase 3 | WebRTC 音轨下发翻译语音（与 RA-7 SFU-lite 共基建）；端到端语音模型评估；租户配额与 self-host 降级 | 远程协助媒体桥接落地 |

各阶段验收：单测（mock provider 零网络）+ golden 回归（提示词劣化检测）+
验收脚本证据（对齐仓内 `scripts/test-*-acceptance.sh` 模式）。
