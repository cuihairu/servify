# 04 SDK And Channel Adapters

范围：

- sdk core contracts
- current web sdk refactor
- future api sdk reservation
- future app sdk reservation
- channel adapters
- sip adapter

约束：

- 当前只实现 web sdk
- 但目录和 contracts 必须支持未来 api sdk 和 app sdk

## S1 sdk target structure

- [x] 评审当前 `sdk/packages` 结构
- [x] 确认现有包映射：
  - [x] `core`
  - [x] `react`
  - [x] `vue`
  - [x] `vanilla`
- [x] 设计目标结构：
  - [x] `core`
  - [x] `transport-http`
  - [x] `transport-websocket`
  - [x] `web-vanilla`
  - [x] `web-react`
  - [x] `web-vue`
  - [x] `api-client`
  - [x] `app-core`

验收：

- 目标结构文档化
- 不要求一次性重命名现有包

## S2 sdk core contracts

- [x] 在 `sdk/packages/core` 中增加 contracts 分层
- [x] 定义 `ClientSession`
- [x] 定义 `Transport`
- [x] 定义 `EventEmitter`
- [x] 定义 `AuthProvider`
- [x] 定义 `Capability`
- [x] 定义统一错误模型

验收：

- core 不含浏览器专属 UI 逻辑
- 已补 capability、transport、web binding 基础测试，便于后续 API/App SDK 复用 contract

## S3 transport split

- [x] 把 websocket 逻辑从泛化 core 中进一步抽到 transport contract
- [x] 预留 `transport-http`
- [x] 预留 reconnect policy
- [x] 预留 token refresh hook

验收：

- transport 与 framework binding 解耦

## S4 current web sdk refactor

- [x] `react` 仅依赖 core contracts
- [x] `vue` 仅依赖 core contracts
- [x] `vanilla` 仅依赖 core contracts
- [x] 将浏览器专属逻辑收口到 web binding

验收：

- core 仍可被未来 api/app sdk 复用

## S5 future api sdk reservation

- [x] 创建 `sdk/packages/api-client` 占位
- [x] 新增 README 说明目标用途
- [x] 约定使用场景：
  - [x] server-to-server api calls
  - [x] admin automation clients
  - [x] bot integrations

验收：

- 明确这是保留设计，不实现具体功能

## S6 future app sdk reservation

- [x] 创建 `sdk/packages/app-core` 占位
- [x] 新增 README 说明目标用途
- [x] 约定使用场景：
  - [x] mobile app session
  - [x] push token integration
  - [x] reconnect and offline queue

验收：

- 明确这是保留设计，不实现具体功能

## S7 sdk capability negotiation

- [x] 定义 capability model
- [x] 第一批 capability：
  - [x] chat
  - [x] realtime
  - [x] knowledge
  - [x] remote_assist
  - [x] voice
- [x] 在 web sdk 中先实现 capability exposure

验收：

- future api/app sdk 可共享 capability 协议

## S8 channel adapter contracts

- [x] 在服务端定义统一 `InboundEvent`
- [x] 在服务端定义统一 `OutboundEvent`
- [x] 约定 adapter responsibilities
- [x] web adapter 映射到 conversation
- [x] 预留 telegram/wecom/whatsapp adapter contract

验收：

- 新接入渠道不需要改 ticket 业务核心
- web channel 已有默认消息映射测试，后续渠道只需要实现 adapter contract

## S9 sip adapter

- [x] 创建 `internal/platform/sip`
- [x] 定义 `SIPAdapter`
- [x] 定义 `InboundCall`
- [x] 映射 invite -> inbound event
- [x] 映射 hangup -> inbound event
- [x] 映射 dtmf -> inbound event
- [x] 预留 `sip-ws` signaling adapter
- [x] 预留 `pstn-provider` signaling adapter
- [x] 抽象 `voiceprotocol.CallSignalingAdapter`
- [x] 抽象 `voiceprotocol.MediaSessionAdapter`

验收：

- SIP 通过 adapter 接入，不进入聊天逻辑分支
- 已有默认 SIP adapter，可把 invite、hangup、dtmf 归一化到统一 inbound event
- SIP、SIP-WS、PSTN webhook 可以共享 signaling adapter 语义
- runtime 已通过 `voiceprotocol.Registry` 统一注册 signaling/media adapter，并暴露统一协议事件入口
