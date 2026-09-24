# Servify WS 协议契约（PROTOCOL）

状态：M0 首版（策划文档 [docs/mobile-sdk-design.md](../docs/mobile-sdk-design.md) 决策 D5 规定的契约事实源）。本文逐条列出 WS 消息类型、字段、方向与错误语义，全部条目经服务端源码核实（引用 `apps/server` 文件行号）。**服务端 WS 契约变更时必须同步本文与 `sdk/protocol-fixtures/` 样例集**——fixtures 被 Web core 与移动端同一套回放测试消费，改 fixtures 三端测试同时红，这是契约不被单端悄悄漂移的机制保障。

---

## 1. 传输与鉴权

| 项 | 契约 |
|---|---|
| 端点 | `GET /api/v1/ws`（`router_realtime.go:17`，publicV1 组免认证） |
| 握手参数 | `session_id`（必填，空则 HTTP 400 `BadRequest`，`websocket_hub.go:247-254`）；`access_token` 参数**服务端不读取**（访客 token 端点落地前的已知缺口，见策划文档 D6） |
| 唯一门槛 | Origin 白名单 `security.websocket_allowed_origins`（空配置放行所有来源，P2-5） |
| 帧编码 | JSON 文本帧，形 `{type, data, session_id?, timestamp?}`（`WebSocketMessage` 序列化） |
| 服务端广播语义 | 按 `session_id` 定向：`SessionID == ""` 广播全员，否则只投递同会话客户端（`websocket_hub.go:232`） |

**移动端口径**：V1 逐字段沿用该握手形态；访客 token 落地后 `access_token` 开始被消费，客户端无需改握手代码、只需带上参数（向后兼容）。

## 2. 心跳与保活（双向机制不同，勿混淆）

| 方向 | 机制 | 服务端行为 |
|---|---|---|
| 服务端 → 客户端 | RFC 6455 协议层 Ping 控制帧，每 54s（`websocket_hub.go` writePump ticker） | 不等待应用层响应 |
| 客户端 → 服务端 | 协议层 Pong（浏览器/原生库自动回，应用层不可见） | readPump 默认 pong 处理 |
| Web core 现状 | 无应用层心跳——保活完全交给协议层（浏览器对服务端 54s Ping 自动 Pong）；曾有的 JSON `{type:"system", data:{type:"ping"}}` 30s 心跳已随 core 清理移除（服务端 switch 无此 case，纯无效流量） | — |

**移动端口径**：与 Web core 同口径——协议层保活对齐服务端 54s Ping（Android OkHttp `pingInterval`、iOS `URLSessionWebSocketTask.sendPing`），不发任何应用层心跳帧。若未来服务端支持应用层心跳帧，经 fixtures 契约变更流程接入。

## 3. 客户端上行帧（服务端 readPump switch，`websocket_hub.go:321-333`）

| type | 载荷 `data` | 语义 |
|---|---|---|
| `text-message` | `{content: string}`；`data` 为裸字符串也接受（`websocket_hub.go:185-190` 双形态提取） | 客户发言。服务端持久化 → 转人工判定 → AI 首答（流式）→ 广播回显（见下行同名列）。`content` 空白则静默丢弃 |
| `webrtc-offer` / `webrtc-answer` / `webrtc-candidate` | SDP/ICE 对象 | WebRTC 信令。**移动端 V1 不消费不发送**（`createMobileCapabilitySet` voice/remote_assist 置 off） |
| 其他任意 type | — | 服务端 `Unknown message type` 警告后丢弃（不回错误帧）——客户端发错类型不会得到显式失败，必须靠本契约约束 |

## 4. 服务端下行帧（客户端按本表分发）

### 4.1 会话消息类

| type | 载荷 `data` | 语义 |
|---|---|---|
| `text-message` | 同上行（服务端把原消息广播回同会话全部客户端） | 客户自己消息的回显；客户端按 `data.content` 与本地待渲染消息去重 |
| `agent-message` | `{content: string, sender: string}`（`conversation_workspace_handler.go:146`） | 坐席发言。core 类型联合与运行时 case 均已列此帧（契约三端对齐） |
| `ai-response` | 必有 `{content: string, confidence: number, source: string}`；编排附加输出零值省略：`sources`（知识库命中数组，元素含 `document_id`/`title`/`content`/`score` 等）、`strategy`（产生方式，如 `llm`/`kp-<id>`）、`next_action`（`handoff` = 置信门建议转人工）、`handoff_reason`（如 `low_confidence`）（`websocket_hub.go` aiResponsePayload） | AI 首答终帧。增量流式时为拼接收口（见 4.2）；`next_action=handoff` 是建议元数据，转接仍由用户显式发起 |
| `ai-response-delta` | `{content_delta: string, done: bool}` | 流式增量帧。契约三段：① 若干 `done=false` 增量即到即拼；② 终末增量 `content_delta=""` + `done=true`；③ 完整 `ai-response` 终帧（内容与拼接结果一致，整体替换是幂等收口）。**流中断语义**：终末增量已到但无 ai-response 终帧 = 本次回答失败——保留已渲染部分 + 提示重试，不自动重发。**core 消费形态（随流式刀接入，三端闭环）**：增量经独立事件 `ai-stream:delta`（content 为累计全量，UI 按 id upsert 气泡）/ `ai-stream:end`（interrupted=false 移除气泡让位终帧、true 保留部分内容并提示重试）透出，不走 `message` 事件——避免稀释"完整消息"语义并隔离补拉指纹表；中断收口只在断连时（无超时器，与 Android 同口径） |

### 4.2 转人工通知类（routing 模块，`handler_adapter.go:504-522`）

| type | 载荷 `data` | 语义 |
|---|---|---|
| `transfer_notification` | `{message: string, agent_id: number, timestamp}` | 会话已分配坐席（含等待队列派发）。状态机 → `agent_chatting` |
| `waiting_notification` | `{message: string, timestamp}` | 已入等待队列。状态机 → `waiting_human` |

**转人工状态机**（策划文档 §4 同源，事件全部为真实帧）：

```
ai_answering ──(transfer_notification)──> agent_chatting
ai_answering ──(waiting_notification)──> waiting_human
waiting_human ──(transfer_notification)──> agent_chatting
agent_chatting ──(增量补拉发现会话 closed)──> closed
任意状态 ──(WS 断连)──> reconnecting ──(恢复)──> 原状态 + 增量补拉
```

### 4.3 WebRTC 信令类（移动端 V1 不消费，列出仅为契约完整性）

`webrtc-answer`（offer 的 SDP 应答）、`webrtc-candidate`（ICE 候选）、`webrtc-ice-config`（ICE 服务器下发，与管理面 `GET /api/v1/rtc/ice-servers` 同形）、`webrtc-state-change`（连接状态）、`data-channel-message`。

## 5. 服务端不发送的帧（客户端契约不含——类型与运行时均已收敛）

以下帧类型在 core 的历史版本中曾声明于 `WSMessage.type` 联合并带运行时 case，但服务端**零发射点**（全仓广播点核查结论，策划文档 D2）。core 清理刀已将它们从类型联合与 `websocket.ts` switch 中整体移除——现契约面 = §3-§4 所列服务端真实发射集，本表保留为边界记录，防止未来误把幻影帧加回：

| 已移除声明 | 死因 |
|---|---|
| `session_update` | 服务端零发射。会话关闭/状态变化客户端经增量补拉感知（§6 #2） |
| `agent_status` | 服务端零发射。assigned/typing 形状曾设计但从未接线；随之移除的还有 core 事件 `session_updated`/`agent_assigned`/`agent_typing` 与 react/vue/vanilla/react-native 四包对它们的消费 |
| `typing` | 服务端零发射。`channel/types.go` 的 `EventKindTyping` 是内部事件总线事件，不是 WS 帧 |
| `message` | 服务端只发 `text-message`（§4.1 回显），无独立 `message` 帧 |
| `error` | 服务端 WS 路径不回错误帧：JSON 解析失败静默 continue、未知类型警告丢弃、HTTP 阶段错误走 400 响应体 |
| `system` | 服务端零发射。JSON 心跳已移除（见 §2），客户端不再发送也就无需该类型 |

新增帧类型的唯一入口：服务端广播点落地 + 本文档新增条目 + fixtures 样例 + 三端回放测试同步（§8 流程），禁止客户端单侧预留。

## 6. 客户端必须知道的边界语义

1. **慢客户端强制断开**：服务端下行缓冲 256 帧，写满即 `close(client.Send)` 踢线（`websocket_hub.go:234-236`）——客户端必须及时消费下行；被踢后走重连。
2. **无历史重发**：WS 断连期间服务端不缓存不重放；恢复后靠增量补拉对账（访客补拉端点 `GET /api/v1/sessions/:session_id/messages?after_id=` 见策划文档 §10 #1）。移动 SDK 双端（M3 刀 10）与 Web core（含 react/vue/vanilla，随 core 补拉刀）均已在连接成功后自动对账：connected 触发（首连也拉——固定 session_id 回放既有历史）、`after_id` 游标续拉、指纹表去重「游标确立前 WS 已渲染」窗口（补拉渲染不入表、收尾不清表）、404/IO/HTTP 全失败面静默。
3. **服务端无应用层 ACK**：客户端发送 `text-message` 成功的判据是收到自己的回显帧；超时未收到 = 发送失败（本地标记 + 手动重发，对齐 Web 行为）。
4. **JSON 解析失败静默丢弃**：服务端 readPump 对畸形帧 `continue`，无错误回执。

## 7. 命名约定（冻结现状，不借机改名）

现存混用：kebab-case（`text-message`/`agent-message`/`ai-response`/`ai-response-delta`/`webrtc-*`）与 snake_case（`transfer_notification`/`waiting_notification`）。改名是服务端 breaking change，V1 不做；本契约与 fixtures 按逐帧现状冻结，每帧标注所属命名族，新增帧优先 kebab-case。

## 8. fixtures 互验约定（M0 落地物）

- 位置：`sdk/protocol-fixtures/`，与 core、Android、iOS 测试共用同一套 JSON 样例；
- 每个样例 = `{name, direction, frame | frames, expectations}`：单帧场景用 `frame`（原始 WS JSON），多帧场景（流式三段契约）用 `frames` 数组按序排列；`expectations` 是两端共用的断言词汇表——`kind`（协议语义分类：`visitor-echo`/`agent-message`/`ai-final`/`ai-stream-complete`/`ai-stream-interrupted`/`transfer`/`waiting`/`unknown-ignored`/`webrtc-ignored-by-mobile`）、`assert`（关键字段与拼接/缺省断言）、`state`（转人工状态机合法转移对，见 §4.2）；
- 两端断言的"一致"指：同一 `kind` 与 `assert` 语义在各自已实现的契约范围内必须得出相同结论；已知的端级偏差在样例内显式声明（如 `webrtc-ignored-by-mobile` 对 core 是消费事件、对移动端是忽略）；
- 必备样例集：ai-response 全字段/最小字段两态、ai-response-delta 三段完整流 + 流中断样例、transfer/waiting_notification、agent-message、text-message 回显去重、未知类型帧（断言忽略而非报错）、webrtc-offer（移动端不消费的显式分叉样例）、慢客户端边界（文档级用例）；
- 服务端 WS 广播点变更 → 同一 PR 更新本文 + fixtures → 三端回放测试同时验证。
