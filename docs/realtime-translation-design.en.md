> **English** | [中文](realtime-translation-design.md)

# Real-time Translation Design (Voice + Chat Text)

> Status: pre-research design + Phase 0 landed (chat text translation) + Phase 1 fully landed
> (knife 1 prefers storage + REST surface, knife 2 hub message-translated frame, knife 3 viewer
> role dual-side + agent → visitor direction translation, wrap-up history message batch annotation, consumer-side core
> event surface + admin console workbench rendering) + Phase 2 knife 1 landed (ASR/TTS provider
> abstraction and config surface, `platform/asr`/`platform/tts`) + knife 2 provider surface landed
> (OpenAI-compatible port into factory switch: `platform/asr/openai` real-time transcription WS +
> `platform/tts/openai` per-sentence synthesis) + knife 2b-1 pipeline half landed (sentence assembler +
> per-speaker voice pipeline + per-sentence translation context tail window) + knife 2b-2 server voice channel landed
> (dedicated WS channel `/api/v1/ws/voice`: audio uplink + subtitle/audio downlink frame family,
> wiring layer `ai.asr` config gating) + knife 2b-3 protocol trio landed (PROTOCOL §9 +
> fixtures voice frame family four samples + three-endpoint playback: core `VoiceChannel` consumer side +
> Android/iOS session core ignore assertions) + knife 2c Web voice uplink/subtitle rendering landed
> (core `MicCapture` capture + demo widget mic button/subtitle bubble/translation playback;
> Android/iOS uplink and subtitle rendering to follow in later knives per product cadence).
> This document is the design baseline for the "LLM real-time translation" capability: the overall pipeline,
> latency budget and sentence-splitting strategy, model selection and cost, privacy and security,
> alternatives and trade-offs, phased rollout plan.
>
> Interface relationship to current state: LLM egress goes uniformly through `platform/llm` (`LLMProvider`,
> openai-compatible / anthropic / mock three providers, `factory.New` the sole construction entry);
> the realtime channel reuses the WS hub (`platform/realtime/websocket_hub.go`, broadcast per session)
> and WebRTC/TURN facilities (remote-assistance voice/screen on the same link).

## 0. Goals and Non-Goals

**Goals**: in cross-language customer-service sessions, both parties communicate real-time in their own languages —
1. Chat text: the other party's messages instantly displayed in my language (translation collapsible / side-by-side with original);
2. Voice calls: I speak Chinese, the other party hears English (captions + synthesized speech dual form).

**Non-goals (this phase)**: document-level long-text translation (handled by batch); on-device offline translation models; automatic multilingual knowledge-base indexing (the existing RAG link evolves independently).

## 1. Overall Pipeline

### 1.1 Chat Text Translation (Phase 0, landed)

```
客户端（坐席 admin / 访客 SDK）
  └─ 对到达的消息调用 POST /api/v1/translation/translate {text, target_lang, source_lang?}
       └─ modules/translation（application.Service）
            ├─ 参数校验（必填/4000 字符上限/BCP-47 语言标签）
            ├─ 提示词组装（system 定纪律 + user 携语言指令与待译文本）
            └─ llm.LLMProvider.Chat（与首答/Copilot 同 provider 同参数）
                 └─ 译文回显（text/source_lang/target_lang）
```

- Server is stateless, single-message granularity; "realtime" comes from the client calling per message immediately (target
  P50 < 800ms, see latency budget); the text link does not need streaming.
- Landing point: `apps/server/internal/modules/translation/` (application/delivery,
  domain/infra reserved for later phases); routes attached to the admin surface
  (agent/admin/service + EnforceRequestScope), `docs` API annotations complete.
  (Since Phase 0.5 changed to dual-side single registration: AuthMiddleware is sufficient, no principal-kind restriction,
   see §6.)
- Reuse surface: provider config (`ai.*`), timeout/params (`llmfactory.RuntimeParams`),
  error conventions (param 400 / not-wired 503 / upstream 502 / timeout 504).

### 1.2 Voice Real-time Translation (Phase 2+, target pipeline)

```
采集端（浏览器/SDK）
  ├─ getUserMedia(Audio) → ScriptProcessor → pcm16 24kHz 单声道小端分片
  │    （刀二c 落地面：raw pcm16 走独立 WS 二进制帧 `/api/v1/ws/voice`，
  │     PROTOCOL §9。选 ScriptProcessor 而非原设的 AudioWorklet+Opus：
  │     AudioWorklet 的 processor 模块经 Blob URL 加载，会被服务端
  │     `default-src 'self'` CSP 拦截；ScriptProcessor 无模块加载全浏览器
  │     可用，代价仅主线程回调——已弃用但各厂商无移除时间表。Opus 编码
  │     留给移动端/带宽受限形态再评估，Web 面先以 raw pcm16 走通）
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

Key design decisions:
- **Recognition and translation chained server-side**, no ASR on the client — the server already has the provider abstraction,
  audit and rate-limit points, and the two sides (agent/visitor) have asymmetric capabilities (visitor side is weak-device).
- **TTS "server-side synthesis, WebRTC send-down" was rejected as a fallback and set to Phase 3**: land first
  "captions + client playback" (TTS returns audio URL or base64, client Audio plays),
  avoiding server-side mixing/echo-cancellation complexity; WebRTC audio-track send-down shares SFU-lite infrastructure
  with remote-assistance media bridging (RA-7) in Phase 3 (**2026-10-15 RA-7 landed**:
  `webrtc_service.go`'s trackForwarder forwarding registry + bridging add-track renegotiation,
  audio-track send-down can reuse it — server writes TTS audio as `TrackLocalStaticRTP` attaching to a track goes
  through the same fan-out/renegotiation link, see docs/remote-assistance-status.md).
  **2026-10-15 server surface landed** (client consumption to follow): on the voice channel,
  `webrtc-request` signaling (`voice_webrtc.go`, frame vocabulary PROTOCOL §9.3) establishes a
  sendonly PCMU track (`voice_media.go`: wav decode → resample → G.711 μ-law →
  RTP 40ms cadence); translated audio routes into the track by "the opposite speaker", `VoiceSink.AudioFormat()`
  negotiates TTS synthesizes as wav (falls back to mp3 when there is no track consumer, Phase 2 frame contract zero change);
  connection failure falls back to `webrtc-error` without closing the line (pure WS fallback).
- **One pipeline per speaker**, bidirectional translation = two pipelines running independently, avoiding
  single-pipeline language-state mutual pollution.
- **Voice goes over a dedicated WS channel `/api/v1/ws/voice` (knife 2b-2 landed)**: uplink binary
  frames = pcm16 24kHz mono chunks, downlink `translation-delta` / `translation-final` /
  `translation-audio` / `voice-error` (JSON, WebSocketMessage same-shape envelope).
  Does not reuse the session WS (`/api/v1/ws`): persistent audio frames would squeeze through the
  slow-client kick semantics of its 256-frame downlink buffer (PROTOCOL §6.1); a dedicated channel keeps the
  session WS contract zero-change. The channel is gated by wiring-layer config
  (`ai.asr` not configured = route not registered, hub handshake 503 fallback); downlink
  frame payloads carry `speaker`, both parties' connections in the same session receive them (client filters by direction for rendering).
  `voice-error` frame code vocabulary: `disabled` (AI surface not configured / session no preference),
  `asr_unavailable` (ASR connection failure), `stream_broken` (uplink stream broken) — after an error
  frame the server closes the line, the client uses "connection closed" as the single signal of stream termination. The protocol trio
  (PROTOCOL §9 entries + fixtures + three-endpoint playback) lands with the client consumption knife
  (b-3) in the same PR per the §5 process.

### 1.3 Session Language Preference (Phase 1 knife 1 landed storage, knife 3 landed viewer role dual-side)

The auto-translation switch is **session-level**: the agent sets a target language once for a customer-service session,
and messages arriving after that in that session go through auto-translation. So preference storage is a prerequisite for
Phase 1 (Phase 0's `translate` endpoint is a "one-shot, stateless" spec, depending on no storage).

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

Design conventions (same style as assist/macro modules, avoiding two tenant semantics in one repo):

- **Tenant/workspace taken from the authenticated ctx**, not self-reported by the requestor; read/write surfaces tightened by scope,
  empty scope means no filtering (local dev / existing link compatibility).
- **Cross-scope hits are the same as non-existence** (no existence echo): upsert colliding with another tenant's same
  `(conversation_session_id, viewer_role)` row returns 409, rather than overwriting or
  erroring "already exists". The same session has one preference per read direction (knife 3: agent and visitor each
  read/write independently, never overwriting each other).
- **Preference table decoupled from the conversation table**: preferences may be set before the first message (the conversation row is created by the first message),
  so this table carries no conversations foreign key.
- **Unset is not an error**: read returns `target_lang=""` + 200; clearing with no row also returns 200.
  The auto-translation link follows "no preference = no translation" accordingly (see §1.4).
- Language tags follow the same convention as the `translate` endpoint: BCP-47 common subset, unified lowercase
  (`zh-CN` → `zh-cn`), invalid tag 400.

### 1.4 Session Message Auto-translation Frames (Phase 1 knife 2/knife 3, server-side dual direction landed)

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

Frame contract essentials:

- **Dedicated frame rather than rewriting `text-message`**: existing client behavior that doesn't recognize the new frame is completely unchanged
  (core SDK's switch falls to default and ignores), translation and original coexist, can be compared/collapsed.
- **Association via `original`**: translation is an async side path after persistence, there is no message ID to hang on (current
  conversation write entry only returns error, no message id echoed). The client matches by "same content and is this session's
  most recent message without a translation attached"; same-content consecutively repeated messages may be matched with late-arriving overwrite,
  a known boundary.
- **Failures only log Warn**: translation is an enhancement capability, does not affect the message main link; provider not configured
  (`ErrTranslationUnavailable`) is treated as "feature off" and silently skipped, avoiding every message flooding
  warnings in deployments where AI is not deployed.
- **Direction boundaries**: both directions landed (knife 2 hub persistence path = visitor → agent, consuming
  agent read direction; knife 3 `ConversationWorkspaceHandler.SendMessage` send-port path
  = agent → visitor, consuming visitor read direction). The same application service
  (`RealtimeTranslateService`) has two instances bound at construction to each read direction, not a new link.

### 1.5 History Message Batch Annotation (Phase 1 wrap-up, landed)

The workbench history pagination (`GET /api/omni/sessions/:id/messages`, note the omni surface has no
`/v1` prefix), when the session has an agent read direction preference set, merges the page's **visitor-side**
(sender ≠ agent) non-empty messages without translations into one batch translation; the translation is attached to the
response DTO via the §4.4 metadata retention keys (`translation`/`translation_lang`):

```
ListMessages 加载 + 时间序整理
  └─ annotateHistory（同步，响应前）
       ├─ HistoryTranslateService.TargetLang（agent 读向）：无偏好 → 静默原样返回
       ├─ 过滤：sender == agent / 空内容 / 已带 translation 键 → 跳过
       ├─ 按语言键查 messages.metadata 的 translations 缓存 → 命中即盖章
       │    响应键、不触 LLM（重翻页免重译）
       ├─ 未命中段合并一次批量翻译：application.BatchTranslate 多段合并单次
       │    LLM 调用（编号分段协议 <<<SEG n>>>，标记序列必须恰好 1..N，
       │    失配/注入扰动 → 自动退回逐条 Translate，fail fast 上抛）
       └─ 译文写响应 DTO 并按语言键持久化；任何失败静默（历史加载不受影响）
```

Convention essentials:

- **Persisted by language key**: the session message storage has a `metadata` column (JSON text, migration
  `000023`), the retention key `translations` stores the "language → translation" table; batch translations are
  written under the read-direction language key. **Preference changes at any time do not constitute language staleness** —
  each language key write is immutable once written (source content is immutable after sending), switching languages
  just misses the corresponding key, translate-on-demand and write, old keys untouched, no rollback needed. The cost is
  each new language re-translates the full history (cost governance see §3.2 Phase 3 quota).
- **Response key still not persisted**: §4.4's `translation`/`translation_lang` only write the response
  DTO — visitor re-pulls should not carry the agent read-direction translation, the language read direction is derived by the server from the authenticated principal.
  What is persisted is only the neutral cache table keyed by language, not encoding read-direction ownership.
- **Persistence failure silent**: translation persistence is an enhancement capability, storage failure does not affect this response's
  annotation; the next pagination miss re-translates and re-writes (idempotent, same language key first-come-first-served).
- **Batch fallback as safety net**: when the segmentation protocol mismatches (model missed segments/disorder/empty segments/segment markers injected into body text), the
  whole batch falls back to per-item `Translate`, behavior no worse than the single-item link; provider errors fail fast.
- **Segment count cap**: single batch ≤ `MaxBatchTexts` (20), over-limit returns the sentinel error
  `ErrTranslationBatchTooLarge`; history pagination ≤ 200 is split by the caller.

### 1.6 Client Consumption Surface (Phase 1 consumer half, landed)

The server produced all translations in Phase 1 knife 2/knife 3/wrap-up; what remains is each side taking what it needs:

```
core SDK（访客侧，实时）
  └─ WS message-translated 帧 → 独立 message-translated 事件（§4.5）
       └─ 渲染方按 original 关联到最近一条未挂译文的消息，原文兜底
  └─ 历史补拉（VisitorMessage.metadata）→ readMessageTranslation 读 §4.4 键

管理端工作台（坐席侧，实时 + 历史）
  └─ 顶部语言下拉 → GET/PUT/DELETE 偏好端点（管理端主体落 agent 读向）
       └─ 切语言后重拉历史（响应键按请求读向现算，必须重拉才生效）
  └─ 访客消息按 metadata.translation 并排展示译文 + translation_lang 标签
       └─ 无译文键 → 只显示原文（翻译是增强，不是替代）
```

Convention essentials:

- **Two carrier division of labor**: realtime goes over §4.5 dedicated frames (carrying `session_id` + language pair, no
  lookup needed), history goes over §4.4 metadata retention keys (pagination DTO carries naturally, zero extra requests).
  The admin console has no session-message WS subscription (only the remote-assistance signaling socket), so it only uses the latter.
- **Read-direction consistency**: the admin principal always lands the agent read direction, consistent with the server-side history annotation's read direction;
  if misconfigured to the visitor read direction, translations won't appear (but won't leak to the opposite side) — the server derives the role from the authenticated principal,
  the requestor cannot escalate to write the opposite read direction.
- **Mobile pending**: Android / iOS currently explicitly `UnknownIgnored` for `message-translated`
  (`sdk/protocol-fixtures/11-message-translated.json`'s `mobile` assertion declares this deviation); when integrating, update the assertion and PROTOCOL §4.5 status line.

## 2. Latency Budget and Sentence-splitting Strategy

### 2.1 Budget (voice end-to-end, spoken → other party sees/hears)

| Stage | Budget (P50) | Notes |
| --- | --- | --- |
| Capture + encode + uplink | ≤ 120ms | Web surface is ScriptProcessor callback + pcm16 chunks (≈85–170ms granularity) + network RTT; Opus 20ms frame form see §1.2 capture-side note |
| ASR partial | ≤ 300ms | Streaming protocol naturally incremental; partial only drives "speaking now" feedback |
| Sentence-split trigger | 0–700ms | Silence 500ms or punctuation/semantic boundary triggers, see below |
| LLM translation | ≤ 600ms | Per-sentence short text; ChatStream first token ≤ 300ms can dispatch early |
| Caption downlink | ≤ 50ms | WS existing link |
| **Caption total** | **≈ 0.8–1.7s** | Experience pass line within 2s; final settled ≤ 1.5s |
| TTS synthesis | ≤ 800ms | Per-sentence synthesis; streaming TTS (chunked) Phase 3 |
| **Voice total** | **≈ 1.5–2.5s** | Simultaneous-interpretation "half-sentence lag" experience, acceptable starting point |

Text link (Phase 0): single REST + single Chat, budget P50 ≤ 800ms (30s hard timeout,
timeout 504). Per-sentence budget over-limit does not retry the whole segment, only the current sentence.

### 2.2 Sentence-splitting Strategy (Voice)

Dual trigger, first-come-first-cut:
1. **Silence trigger (primary)**: VAD tail silence ≥ 500ms cuts the sentence (ASR streams mostly carry VAD events themselves);
2. **Boundary trigger (secondary)**: final text ends with 。？！.?!?, or length ≥ 60 chars cuts the sentence
   (prevents long sentences dominating the screen and translation timeout).

Stabilization rules:
- partial only updates the "speaking now" line, never enters translation; only final enters the translation queue;
- final append arrivals (second final for the same sentence) dedupe and overwrite by seq;
- Translation context carries the **previous sentence's original + translation tail window** (≤ 200 chars) controlling latency and cost.

## 3. Model Selection and Cost

### 3.1 Selection Principles

- **Translation LLM**: same lineage as the existing `ai.provider` system (OpenAI-compatible port preferred),
  per-sentence short translation is fine with a small fast model (like gpt-4o-mini / flagship-haiku tier),
  no flagship model needed; temperature follows the global (recommend deployment config ≤ 0.3 for consistency).
- **ASR**: start with managed streaming APIs (Deepgram / AssemblyAI / Volcengine / Alibaba),
  pick one by availability zone and compliance; self-host (faster-whisper) as a degradation fallback,
  higher latency and ops cost, only enabled under data-residency requirements.
- **TTS**: start with OpenAI-compatible TTS port (tts-1 tier), Azure/ElevenLabs as fallback.

### 3.2 Cost Magnitude (public price approximation, for capacity planning only)

Estimated by one 30-minute bilingual call (each side speaks 15 minutes, ≈2.5k words/side):
- ASR streaming: ≈ $1–3/hour → $0.5–1.5;
- Translation LLM (small model, per-sentence ≈300 in + 300 out tokens): $0.01–0.05 magnitude, negligible;
- TTS (tts-1 tier): ≈ $15/1M chars → $0.1–0.3.
That is, **the main voice-translation cost is in ASR+TTS (≈$1–2/call hour)**, text translation cost is negligible
(single message < $0.0001).

Control means: per-tenant config switches and quotas (reusing configscope tenant override pattern),
per-sentence budget circuit breaking (over-limit sentences degrade to captions-only/original-only, Phase 2 knife 2d landed: single-sentence 5s
timeout goes through the existing degradation surface), session-level token metering into business metrics (existing
`rt.BusinessMetrics` port, knife 2d landed: `voice_translation_sentences_total`
+ reuse `ai_llm_tokens_total`; tenant quota rate limiting still Phase 3).

## 4. Privacy and Security

1. **Data minimization**: the voice pipeline only keeps final text and translations (partial discarded);
   translations/originals not persisted by default (captions are session-state data). Chat translation: the realtime frame link is still stateless;
   the workbench history annotation writes translations into `messages.metadata` by language key (retention key
   `translations`, §1.5), meaning **the visitor's original text's translation is persisted with history** — tenant
   data already lives in its own DB (same table as the original), but the retention-key table can be
   separately reclaimed in the tenant-side cleanup policy. Recording/playback only with explicit user consent (aligned with remote-assistance RA-4 consent).
2. **Transport**: full-link TLS (client→server already exists); ASR/TTS egress only through the server,
   keys never touch the client; provider domains and keys go through `ai.*` existing config surface and tenant overrides.
3. **Prompt injection**: the text to translate is treated as data not instructions — system explicitly "only output the translation";
   Phase 0's prompt already contains separation and "no explanation" discipline; later, for prompt-injection
   samples embedded in messages, add golden-case regression.
4. **Permissions and audit**: the translation endpoint is under the admin auth chain (AuthMiddleware +
   EnforceRequestScope + RequirePrincipalKinds); write-operation audit follows the existing
   AuditMiddleware; voice-link frames broadcast by session ownership (hub's existing per-
   session_id isolation), no new cross-session channel.
5. **Compliance**: PII must not go through third-party ASR/TTS requires a tenant-level self-host fallback
   (Whisper local ASR + open-source TTS), listed as a Phase 3 option rather than first release.
6. **Abuse surface**: input length (4000 chars) and rate limiting; later, per-tenant quota
   (translation.quota) rate limiting, 429 over quota.

## 5. Alternatives and Trade-offs

| Option | Description | Trade-off |
| --- | --- | --- |
| A. Client-side whole translation (browser built-in Translator API / on-device model) | End-to-end zero server cost, zero latency | Fragmented capabilities (uneven browser coverage), no audit/consistency or terminology control, cannot support voice playback; only as a future visitor-side "free tier" supplement, not the main link |
| B. Dedicated translation API (DeepL / cloud vendor MT) | Best quality/latency, cheap | Introduces a second provider system and egress surface; general customer-service-domain LLM small models are already good enough; the LLM also carries tone preservation and placeholder discipline. Reserved as a `ai.provider=deepl`-style extension slot, not first release |
| C. Voice end-to-end model (GPT-4o realtime / bidirectional voice model) | One model skips ASR/TTS assembly | Best latency and experience but an order of magnitude more expensive, strong vendor lock-in, cannot reuse existing per-utterance audit; Phase 3 evaluation |
| D. Server-side mix-and-send (translated voice over WebRTC track) | The other party "hearing" the translated track is most natural | Depends on server SFU media bridging (remote-assistance RA-7 same infrastructure); do client playback first (Phase 2), D merges after RA-7 (Phase 3) |

**Selected route**: Phase 0 text LLM translation (landed) → Phase 2 server ASR+LLM+TTS
assembly, captions first → Phase 3 leverage RA-7 media infrastructure to switch to WebRTC audio-track send-down, and evaluate end-to-end
voice models (C) as a premium tier.

## 6. Phased Rollout Plan

| Phase | Scope | Dependencies |
| --- | --- | --- |
| **Phase 0 (landed)** | Chat text translation: `modules/translation` + `POST /api/v1/translation/translate`; mock provider unit tests full coverage | No new dependencies (reuse `platform/llm`) |
| **Phase 0.5 (server half landed)** | Visitor-side same endpoint: single route registration point, AuthMiddleware is sufficient (visitor token callable, `router_auth.go`); remaining SDK half: `metadata.translation` field convention + SDK convenience calls | SDK contract (`sdk/PROTOCOL.md` + fixtures) |
| **Phase 1 knife 1 (landed)** | Session language preference storage + admin REST surface (`translation/infra` GORM repository, pg migration 000015, `GET/PUT/DELETE /api/v1/translation/preferences/:session_id`); see §1.3 | No new dependencies |
| **Phase 1 knife 2 (server half landed)** | WS auto-translation frames: hub asynchronously translates after message persistence and broadcasts `message-translated` (by session language preference); see §1.4 | Knife 1's preference storage (already ready) |
| **Phase 1 knife 3 (landed)** | Preference table viewer role dimension (migration 000016, `(session_id, viewer_role)` composite unique) + dual-side single registration point (end_user reads/writes visitor read direction via session-binding validation) + agent → visitor direction auto-translation (`SendMessage` send-port async side path) | Knife 2 |
| **Phase 1 wrap-up (landed)** | History message sub-segment batch translation: workbench history pagination batch-translates visitor messages per agent read direction, translation attached to response DTO via §4.4 metadata retention keys (response keys not persisted); see §1.5 | Knife 3 |
| **Phase 1 cross-page cache (landed)** | History translation persistence: `messages.metadata` JSON column (pg migration 000023), retention key `translations` caches batch translations by language key — re-pagination hits skip LLM, misses translate-on-demand and write; §4.4 response keys still only write the response (visitor re-pulls don't carry agent read-direction translations); see §1.5 | Wrap-up (already ready) |
| **Phase 1 consumer half (landed)** | Core SDK consumes `message-translated` frames (`WSMessage` union member + same-name event + `MessageTranslation` payload, see §4.5); admin console workbench consumes history metadata translations and provides agent read-direction language dropdown (read/write preference endpoints) — see §1.6 | Knife 2/knife 3/wrap-up server surfaces (all ready) |
| **Phase 2 knife 1 (landed)** | ASR/TTS provider abstraction and config surface: `platform/asr` (streaming session + event channel contract, VAD/partial/final event kinds aligned with §2.2 sentence-splitting strategy) + `platform/tts` (per-sentence whole-segment synthesis contract) + respective mocks and factory (llm-style collection: sole construction entry, mock not in switch) + `ai.asr.*`/`ai.tts.*` config surface (provider empty = not enabled, wiring layer skips assembly) | No new dependencies (contracts + mocks zero network) |
| **Phase 2 knife 2 provider surface (landed)** | OpenAI-compatible port first (§3.1): `platform/asr/openai` (real-time transcription WS protocol `/v1/realtime?intent=transcription`: server VAD tail silence 500ms aligned with §2.2, pcm16 24kHz mono, protocol event → contract event mapping, server error/read break → `EventError` session breakage) + `platform/tts/openai` (`POST {base_url}/audio/speech` per-sentence whole-segment synthesis, non-2xx wrapped `ErrUpstream` for §3.2 budget circuit-breaker identification); both `base_url` can point to a compatible gateway (ws URL derived from https→wss), enter factory switch (`ai.asr.provider=openai`). The voice pipeline (final → sentence-split translation → caption frames → TTS consumption) is knife 2b | No new dependencies (gorilla/websocket already in dependency tree) |
| **Phase 2 knife 2b-1 pipeline half (landed)** | Sentence assembler `modules/translation/domain.SentenceAssembler` (§2.2 dual-trigger first-come-first-cut: punctuation/60-char cap single-pass scan over char positions, VAD tail flush, same-seq second final overwrite, turn-switch defensive tail collection) + voice pipeline `application.VoicePipeline` (one per speaker: ASR event stream → sentence split → per-sentence translation with previous sentence original + translation tail window ≤ 200 chars → TTS per-sentence synthesis; `VoiceSink` production interface delivery-layer adaptation, partial only passes through "speaking now"; translation-failed sentences degrade to original without synthesis, TTS-failed sentences captions only, pipeline never breaks) + `TranslateCommand.Context` context tail-window prompt extension. The WS surface (audio uplink + caption frame family, via PROTOCOL §5 process) is knife 2b-2 | No new dependencies |
| **Phase 2 knife 2b-2 server voice channel (landed)** | Dedicated WS channel `/api/v1/ws/voice` (§1.2: audio frames don't squeeze the session WS downlink buffer): `delivery/voice_contract.go` (`VoiceStreamStarter`/`VoiceAudioStream` contract + app type-alias bridging) + `delivery/voice_adapter.go` (per-stream assembly of "ASR session + voice pipeline", speaker→read-direction duality: visitor speaking looks up agent read direction) + `platform/realtime/voice_hub.go` (handshake validates session_id/speaker/token, broadcast per session, slow-client kick same convention as session WS, voice-error closes line, zero frames on failure) + wiring (`ai.asr` config gating: not configured = route not registered; guest token validation same source as session WS) + security surface directory registration `public-realtime-voice` (`app/server/security_surface.go`, same trust domain as `/api/v1/ws`, rate limiting reuses its prefix entry). The downlink frame family protocol trio (PROTOCOL §9 + fixtures + three-endpoint playback) is knife 2b-3 | No new dependencies |
| **Phase 2 knife 2b-3 protocol trio (landed)** | `sdk/PROTOCOL.md` §9 (voice channel handshake/uplink form/downlink frame family four frames + `turn_seq`/`seq` semantics + `voice-error` vocabulary and connection-level semantics + three-endpoint consumption state) + `sdk/protocol-fixtures/` voice frame family four samples (12-15, `channel: "voice"` annotation) + three-endpoint playback: core `VoiceChannel` (`sdk/packages/core/src/voice.ts`: handshake parameter assembly, downlink four-frame required-field validation → `voice:delta/final/audio/error` events (payload line order snake_case), `sendAudio` binary uplink, no auto-reconnect) + fixtures replay assertions (four frames full-payload events + session channel ignoring frames entering voice channel by mistake) + Android/iOS session core `UnknownIgnored` assertions (endpoint-level deviation declaration; voice channel client and three-endpoint uplink/subtitle rendering consumption are later knives) | No new dependencies (core zero new npm dependencies) |
| **Phase 2 knife 2c Web uplink + subtitle rendering (landed)** | core `MicCapture` (`sdk/packages/core/src/voice-capture.ts`): pure-function trio (`floatToPcm16` clamp quantization / `resampleLinear` linear-interpolation resampling / `pcm16ToLeBytes` DataView explicit little-endian) + `MicCapture` capture graph (getUserMedia → ScriptProcessor → zero-gain mute → destination; generation invalidates in-flight start to prevent stop race; `capture_denied`/`capture_unavailable`/`capture_already_active` error normalization; ports injection aligned with webSocketFactory convention) + vanilla UMD global mounting (`window.Servify.MicCapture/VoiceChannel` and six more items — UMD exports get overwritten by window.Servify class assignment, named exports unreachable in browser, toolkit mounted on the class) + demo widget (`apps/demo-sdk/widget.js`): mic button (capability-probe degradation, old cached package doesn't show) + session id generated once and shared across channels + `voice:delta` live line (one per speaker) + `voice:final` final-sentence captions (translation + small-font original + degradation label) + `voice:audio` playback (autoplay-blocked degrades to ▶ button) + session channel disconnect links voice teardown. Mobile uplink/subtitle rendering still per product cadence | No new dependencies (core/vanilla zero new npm dependencies) |
| **Phase 2 knife 2d cost-control closure (landed)** | §3.2 control means landed: per-sentence budget circuit breaking — `VoicePipeline` adds per-sentence timeouts for translation/TTS (5s, far smaller than provider transport-layer 30s fallback, P50 budget 600ms/800ms margin cap), over-limit sentences follow the existing degradation surface (translation over-limit degrades to original, synthesis over-limit captions only), pipeline never breaks; in-flight sentences during session teardown produce no captions and no degradation (line close ≠ circuit break). Business metering — `VoiceObserver` observation surface (produces outcome vocabulary `translated`/`caption_only`/`degraded` + per-sentence token consumption) bridged by delivery into `rt.BusinessMetrics`'s existing port: new counter `voice_translation_sentences_total{outcome}`, tokens reuse `ai_llm_tokens_total{provider,token_type}`; `TranslateResult` gains optional metering fields `provider`/`token_usage` (omitempty, PROTOCOL §5 already noted). Tenant config switches keep existing `ai.asr` wiring gating; tenant quota rate limiting still Phase 3 | No new dependencies |
| Phase 3 | WebRTC audio-track send-down for translated voice (shared infrastructure with RA-7 SFU-lite); end-to-end voice model evaluation; tenant quota and self-host fallback | Remote-assistance media bridging landed |

Acceptance per phase: unit tests (mock provider zero network) + golden regression (prompt degradation detection) +
acceptance-script evidence (aligned with the repo's `scripts/test-*-acceptance.sh` pattern).
