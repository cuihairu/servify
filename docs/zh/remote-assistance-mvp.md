# 远程协助最小可交付链路

本文用于收口 `Gap C / G3-3`：定义 Servify 当前阶段可对外表达的最小远程协助链路，以及对应的验收口径。

## 目标

这条最小链路只为证明以下几点，不追完整远控产品：

- Servify 已经能把客户会话、人工接管、实时协作基础、转接和工单全流程放在一条连续服务链路里
- 管理端已经有足够的现有页面与接口支撑一轮演示
- 产品叙事和实际能力不会互相打架

## 最小链路定义

当前建议把远程协助的最小可交付链路定义为：

1. 客户从 Web 入口发起会话
2. AI 或普通文字沟通无法直接解决问题
3. 客服在会话页接管当前 session
4. 客服在同一会话里继续发送消息、必要时转派给更合适坐席
5. 若需要实时协作基础，可确认 WebSocket / WebRTC runtime 已连通，语音/实时协议侧入口可用
6. 协助完成后，会话可以继续关闭，或者把问题沉淀到工单详情继续推进

这条链路强调的是：

- “升级到协助”发生在已有会话上下文里
- 协助后仍留在 Servify 内部链路，不跳到外部系统
- 现在先承认“实时协作基础已具备”，而不是虚构一个尚未实现的重型协助 UI

## 对应页面与接口

### 1. 会话接待与接管

管理端页面：

- `/conversation`

核心接口：

- `GET /api/omni/sessions/:session_id`
- `GET /api/omni/sessions/:session_id/messages`
- `POST /api/omni/sessions/:session_id/messages`
- `POST /api/omni/sessions/:session_id/assign`
- `POST /api/omni/sessions/:session_id/transfer`
- `POST /api/omni/sessions/:session_id/close`

当前产品语义：

- 这是远程协助最接近“主工作台”的现有入口
- 当前已能完成查看会话、发送消息、接管、转派、关闭等动作

### 2. 转接与调度

管理端页面：

- `/routing`

核心接口：

- `GET /api/session-transfer/waiting`
- `GET /api/session-transfer/history/:session_id`
- `POST /api/session-transfer/to-agent`
- `POST /api/session-transfer/cancel`
- `POST /api/session-transfer/process-queue`

当前产品语义：

- 当一线客服无法继续处理时，远程协助不应该断链，而是能进入转接调度

### 3. 实时协作基础

管理端页面：

- `/voice`

服务端观测/协议入口：

- `GET /api/v1/ws/stats`
- `GET /api/v1/webrtc/stats`
- `GET /api/v1/webrtc/connections`
- `GET /api/voice/protocols`
- `POST /api/voice/protocols/:protocol/call-events/:event`
- `POST /api/voice/protocols/:protocol/media-events/:event`
- `POST /api/voice/recordings/start`
- `POST /api/voice/recordings/stop`
- `GET /api/voice/transcripts`

当前产品语义：

- 这部分证明系统具备实时协作、语音和连接观测基础
- 但当前仍主要是 runtime / protocol / operations 能力，不等于独立的“远程协助 UI”

### 4. 协助后工单全流程

管理端页面：

- `/ticket/detail/:id`

核心接口：

- `GET /api/tickets/:id`
- `GET /api/tickets/:id/conversations`
- `POST /api/tickets/:id/comments`
- `POST /api/tickets/:id/close`

当前产品语义：

- 当问题不能在会话内即时收口时，可以把上下文继续沉淀到工单
- 工单详情页可继续查看关联会话，而不是让协助记录彻底断开

## 当前不纳入 MVP 承诺的内容

以下内容当前不应被写成“已经交付”的能力：

- 独立的“远程协助”菜单或工作台
- 标准化 co-browsing / 屏幕共享操作界面
- 协助中的专门状态机字段，例如 `assisting` / `assistance_completed`
- 协助结果模板、协助记录表或专门报表

这些属于后续产品增强项，不应和当前最小交付范围混在一起。

## 最小验收口径

### 自动化证据

至少应继续保留以下自动化证据：

- Web 会话与实时基础
  - `go test ./apps/server/internal/handlers -run 'Test(ConversationWorkspaceHandler_(ListMessages|SendMessage|AssignAgent|Transfer|CloseSession|GetSession)|AuthHandlerRefreshToken|AuthHandlerSelfServiceSessions)'`
- 公开入口与实时入口运行证据
  - 验收清单里已有 `GET /api/v1/ws/stats`、`GET /api/v1/webrtc/stats`、`GET /api/v1/webrtc/connections`、语音协议入口等实跑记录
- 工单关联会话
  - `ticket` 详情页与 `GET /api/tickets/:id/conversations` 已有后端测试覆盖

### 人工演示步骤

最小人工演示建议按下面顺序执行：

1. 打开 `/conversation`，选中一个活跃 session
2. 发送一条消息，确认会话上下文可持续
3. 执行“接管会话”
4. 执行一次“转派会话”或查看 `/routing` 中等待队列/转接历史
5. 验证实时基础仍在线：
   - 查看 `/voice`
   - 或调用 `GET /api/v1/ws/stats`、`GET /api/v1/webrtc/stats`
6. 打开关联工单详情 `/ticket/detail/:id`，确认会话上下文仍可追踪

### 通过标准

本轮对“远程协助 MVP”的通过定义为：

- 会话、接管、转派、关闭动作可在同一条 session 主链路里完成
- 实时基础能力有明确入口和运行证据
- 协助后可以继续进入转接或工单，而不是断在会话页
- README、文档站首页、专题页与当前实现口径一致

## 下一步增强方向

如果后续继续迭代，优先顺序建议是：

1. 在管理端增加更明确的“远程协助”入口或会话页增强态
2. 给协助过程补状态机和记录模型
3. 把演示链路升级为真正的产品验收脚本
