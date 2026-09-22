# M1 验收矩阵（Android SDK Alpha）

对应 `docs/mobile-sdk-design.md` M1 验收条款 ①—⑤。自动化项逐条锚定测试与提交；真机手工项如实标注执行状态——**未执行的项不勾**。

## ① 契约回放测试全绿 ✅

- `FixtureReplayTest`（11 测试）：`sdk/protocol-fixtures/` 同一样例集喂 core 与 Android 双端，断言一致（M0 锚定提交 39f2a67、2b96be6，CI `android-probe` job 持续执行）。
- 当前 SDK 全量单测 65 个全绿（含 ② ④ ⑤ 各锚点，见下）。

## ② demo 宿主 ≤10 行集成 ✅

- `demo/MainActivity.kt` 核心集成 3 行：`ServifyChat.create(...)` + `chat.show(this)`（刀 3c，02bb8c1）；demo 与 demo-baseline（无 SDK 对照）同构，供体积门禁做差。

## ③ 体积门禁 ≤1.5MB ✅

- 口径：**R8/minify 后**的 release APK 增量（未混淆的 Compose 全家桶增量 5MB+，非验收口径，D9 核查记录）。
- 门禁：`scripts/check-android-sdk-size.sh`（demo 与 demo-baseline 双开 R8，字节差 ≤ 1572864）；CI `android-probe` job 独立 step，超时 15min。
- 首测基线：delta ≈ 1.19MB ≤ 1.5MB ✅（02bb8c1）。

## ④ 弱网/断网/后台切换矩阵

### 自动化锚定项（CI 持续执行）✅

| 场景 | 锚定测试 | 断言要点 |
|---|---|---|
| 断线自动重连（服务端异常断开） | `ServifyChatTest.reconnectsAfterServerDrop` | connected→Reconnecting(1)→connected；attempt 计数正确 |
| 发送超时（弱网无回显） | `ServifyChatTest.sendMessageTimesOutWithoutEchoAndEmitsSendTimeout` | SendTimeout 可重试，不伪造成功 |
| 流中断渲染降级 | `ServifyChatTest.streamInterruptionOnDisconnectFinalizesPartialWithHint` | 已渲染部分保留（翻 isStreaming=false）+ 提示行"回答中断，请重试"，提示行不计未读 |
| 重连耗尽 → 手动恢复 | `ConnectionLifecycleTest.reconnectExhaustionMarksDisconnectedAndConnectRecovers` | 耗尽→Disconnected；再次 connect→Connected |
| 握手失败（404/5xx） | `ConnectionLifecycleTest.handshakeFailureBeforeEverConnectedMarksDisconnected`、`ServifyChatTest.handshake500MarksServerUnavailableAndDisconnected` | 未曾连接成功时直接 Disconnected（退避无意义），错误分类区分 5xx/4xx |

### 真机手工项 ⏳ 待执行

> 需要真机与真实网络环境，CI 无法模拟。执行后在对应条目记录日期与结果。

- [ ] **前后台切换**：会话页前台 → Home 键退后台 30s+ → 回前台。预期：回前台后面板状态保留（历史消息完整回放、未读语义正确），连接恢复（重连或首次 connect 经由可见性触发）。后台期间到达的坐席消息计入未读。
- [ ] **飞行模式**：会话中开飞行模式 ≥ 重连耗尽时长。预期：流式中断收口（提示行出现、部分内容保留）、最终 Disconnected 且状态行提示"连接已断开"；关飞行模式后点重进/重连恢复。
- [ ] **蜂窝 ↔ WiFi 切换**：会话中切换网络。预期：TCP 断开触发 onFailure → 自动重连成功；若切换瞬间有流式回答，渲染降级同上。
- [ ] **转人工状态跨断线保持**：进入 agent_chatting 后断线重连。预期：坐席身份保持（结构性保证：连接替换不换 `SessionCore` 实例，门面 val 持有），断连期间坐席新消息不丢渲染——**断连期间的静默丢消息是 V1 已知边界**（服务端无历史重发/补拉，§10 #1 后端配套项），不做伪装。

### 已知边界（如实记录，不假装通过）

- 断连期间服务端下发的消息 V1 无补拉通道，恢复后**不回放**——后端配套项 #1/#2 落地后接 backfiller。
- `HandoffState.Closed` 无入边：进入依赖补拉语义（V1 无），设计在案不实现（防死代码）。
- 流中断后已渲染部分保留，但该次回答不再补全（PROTOCOL §4.1：无终帧 = 本次回答失败）。

## ⑤ 单元测试覆盖协议层与状态机 ✅（转移表穷举口径）

覆盖率不设 100% 目标（D10 原文），但两台状态机的转移表必须穷举：

- **转人工状态机**（shared/core `HandoffStateMachine`）：`StateMachineTest` 4 测试——3 条合法转移（ai_answering→agent_chatting、waiting_human→agent_chatting、ai_answering→waiting_human）逐条断言 + 非法转移拒绝返回 false。
- **连接状态机**（§4.4）：`ConnectionLifecycleTest` 5 测试——idle 初始态、idle→connecting→connected 显式观察、connected/connecting 期间重复 connect 幂等、握手失败→disconnected、重连耗尽→disconnected→connect 恢复；补边：connected→reconnecting→connected（`reconnectsAfterServerDrop`）、destroy→disconnected（`destroyIsIdempotentAndMarksDisconnected`）。
- 支撑件：`ReconnectPolicy`（4 测试，退避序列 + 越界 null）、`StreamingAssembler`（6 测试，含 interruptedStreamIsFlagged）。

## 后端配套依赖（不阻塞 M1 收口，进服务端排期）

§10 清单 #1（访客消息增量端点）、#2（guest token 签发端点）、#3（未读游标增强）——SDK 侧 `access_token` 握手参数已就位（`buildWsUrlCarriesAccessTokenOnlyWhenConfigured`，PROTOCOL §1 服务端当前不消费、向后兼容），端点落地后接 backfiller/pushTokenProvider。
