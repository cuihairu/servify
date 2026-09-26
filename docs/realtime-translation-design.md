# 实时翻译设计（语音 + 聊天文本）

> 状态：预研设计 + Phase 0 已落地（聊天文本翻译）。
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
| **Phase 0（本轮，已落地）** | 聊天文本翻译：`modules/translation` + `POST /api/v1/translation/translate`（管理面）；mock provider 单测全覆盖 | 无新增依赖（复用 `platform/llm`） |
| Phase 0.5 | 访客面同端点（SDK 对坐席消息即时翻译）+ SDK `metadata.translation` 字段约定 | 访客面路由 + SDK 契约 |
| Phase 1 | WS 自动翻译帧：hub 在消息落库后异步翻译并广播 `message-translated`（按会话语言偏好）；批量子段翻译（历史消息） | 会话语言偏好存储（translation/infra） |
| Phase 2 | 语音链路 MVP：ASR 流式接入 + 分句 + 逐句翻译 + 字幕 WS 帧 + TTS 客户端播放 | ASR/TTS provider 抽象（`platform/asr`、`platform/tts`）与配置面 |
| Phase 3 | WebRTC 音轨下发翻译语音（与 RA-7 SFU-lite 共基建）；端到端语音模型评估；租户配额与 self-host 降级 | 远程协助媒体桥接落地 |

各阶段验收：单测（mock provider 零网络）+ golden 回归（提示词劣化检测）+
验收脚本证据（对齐仓内 `scripts/test-*-acceptance.sh` 模式）。
