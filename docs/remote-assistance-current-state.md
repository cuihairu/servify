# 远程协助现状盘点

> ⚠️ **本文已被 [remote-assistance-status.md](./remote-assistance-status.md) 取代**：assist 权限组现已挂 `/api/assist/remote-assist/*` 七端点（远程协助已落地，`router_management.go` RequireResourcePermission("assist")）。本文保留为 2026-09「能力盘点/差距识别」时点存档。

本文用于收口 `Gap C / G3-2`：盘点 Servify 当前在服务端、管理端和产品表达层面，哪些能力已经可以作为远程协助基础，哪些仍然只是底层能力，哪些产品入口还缺失。

## 结论先行

当前仓库已经具备远程协助所需的多块基础能力，但它们还没有被收束成一个单独的“远程协助工作台”或一条清晰的产品操作流。

可以把现状理解为：

- 服务端已经有会话、消息、转接、建议、语音、WebSocket、WebRTC 统计等基础能力
- 管理端已经有会话管理、路由管理、语音管理、工单详情等分散入口
- 管理端会话页已经补上“发起协助 / 结束协助”的最小入口，并能展示会话级 WebSocket / WebRTC 连接状态
- 产品叙事已经开始统一强调远程协助
- 但管理端还没有一个明确叫“远程协助”的页面，也还没有把“发起协助 -> 持续接管 -> 转接/建单”的操作路径串成单一入口

## 服务端现有入口

### 1. 会话与人工接管主路径

当前最接近远程协助主链路的服务端入口是会话与转接相关 API：

- `GET /api/omni/sessions/:session_id`
- `GET /api/omni/sessions/:session_id/messages`
- `POST /api/omni/sessions/:session_id/messages`
- `POST /api/omni/sessions/:session_id/assign`
- `POST /api/omni/sessions/:session_id/transfer`
- `POST /api/omni/sessions/:session_id/close`

这些入口说明：

- Servify 已经有“围绕 session 连续处理问题”的主线
- 人工接管、消息承接和转接都发生在同一个会话上下文里
- 这条链路天然适合承接“远程协助前、中、后”的业务状态

### 2. 转接与等待队列

当前路由层还有一组更偏运营调度的入口：

- `GET /api/session-transfer/waiting`
- `GET /api/session-transfer/history/:session_id`
- `POST /api/session-transfer/to-agent`
- `POST /api/session-transfer/cancel`
- `POST /api/session-transfer/process-queue`

这些入口说明：

- 系统已经支持把“一个会话交给谁继续处理”单独管理
- 远程协助结束后，继续转接给更合适处理人的能力已经有后端基础
- 但它当前更像 routing / queue 能力，还不是以“远程协助”命名的产品流程

### 3. 建议与辅助能力

`assist` 权限下当前挂的是建议能力：

- `GET /api/assist/suggest`
- `POST /api/assist/suggest`

这意味着：

- “assist” 这个权限名已经存在
- 但当前 `/api/assist/*` 更偏建议与知识辅助，不等于完整的远程协助产品入口

### 4. 实时 runtime 与观测入口

当前服务端已暴露远程协助相关的实时基础能力：

- `GET /api/v1/ws`
- `GET /api/v1/ws/stats`
- `GET /api/v1/webrtc/stats`
- `GET /api/v1/webrtc/connections`

同时还有语音协议与记录相关入口：

- `GET /api/voice/protocols`
- `POST /api/voice/recordings/start`
- `POST /api/voice/recordings/stop`
- `GET /api/voice/recordings/:recordingID`
- `POST /api/voice/transcripts`
- `GET /api/voice/transcripts`

这些入口说明：

- 实时连接、WebRTC 连接观测、语音录制和转写都已经是系统能力
- 但其中很多仍是“基础 runtime / protocol 能力”，不是直接面向客服的远程协助产品交互

## 管理端现有入口

### 1. 已有的相关页面

管理端当前没有单独的“远程协助”菜单，但能力分散在以下页面：

- `/conversation`
  - 会话查看、消息读取与发送
  - 最小远程协助入口：发起/结束协助、信令状态提示、远端媒体占位
- `/routing`
  - 等待队列、转接历史、转坐席处理
- `/voice`
  - 协议、录音、转写相关管理
- `/ticket/detail/:id`
  - 会话与工单关联查看
- `/security`
  - 会话安全与设备/IP 风险视图

### 2. 管理端已接到的服务调用

从前端 service 看，当前已经接了：

- `conversation.ts`
  - 获取会话
  - 获取/发送消息
  - 指派坐席
  - 转接会话
  - 关闭会话
- `sessionTransfer.ts`
  - 获取等待队列
  - 获取转接历史
  - 发起转接
  - 取消转接
  - 处理队列
- `voice.ts`
  - 列协议
  - 开始/停止录音
  - 查询录音
  - 查询转写

这说明管理端已有一组远程协助能力，只是分散、还没被产品层统一命名和聚合。

## 当前最明显的缺口

### 1. 缺统一产品入口

当前没有：

- “远程协助”独立菜单
- 远程协助状态页
- 脱离会话页后仍可持续操作的专门协助工作台

因此用户现在能在会话页进入最小协助态，但看到的仍不是一条完整、独立的产品工作流。

### 2. 缺状态机与操作语义

当前代码里已经有：

- 会话
- 指派
- 转接
- 关闭
- 工单

但还缺明确的产品语义，例如：

- 何时算“发起远程协助”
- 协助中和普通人工会话有什么区别
- 协助完成后是结束、转接还是建工单

### 3. 缺最小演示链路

目前可以证明：

- WebSocket / WebRTC runtime 可用
- 会话与消息可用
- 转接可用
- 工单可继续推进

但还缺一条面向产品演示的最小链路说明，例如：

1. 客户进入 Web 会话
2. AI 首答失败或需要指导
3. 人工接管
4. 发起远程协助
5. 完成操作或联合排查
6. 继续转接或转工单

### 4. 缺管理端专门视图

当前仍没有明确证据表明管理端已经具备：

- 完整的远程协助专用面板
- 协助过程状态提示
- 协助后结果记录字段
- 协助与工单之间的显式衔接视图

这也是 `G3-3` 需要继续定义的范围。

## 当前判断

因此，当前最准确的产品口径应当是：

- Servify 已经具备远程协助所需的实时与会话基础能力
- 这些能力已经可以支撑“从聊天升级到实时协作”的产品方向
- 但管理端与产品体验层仍缺统一入口、清晰状态机和最小可交付演示链路

下一步应继续推进：

1. 定义最小可交付的远程协助链路
2. 明确演示步骤、验收证据和页面入口
3. 决定是先做统一会话页增强，还是单独增加远程协助页面
