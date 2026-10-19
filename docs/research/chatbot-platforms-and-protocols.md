# 聊天机器人平台与接入协议调研

调研日期：2026-10-19  
目的：评估 Servify 群聊知识沉淀是否应抽象机器人/聊天平台协议，以及 Memex 现有 bot API 能否作为基础。  
范围：优先查看 Telegram、Slack、Discord、飞书、钉钉、Microsoft Teams、Matrix 官方资料，并对照本地 Memex 设计。各平台 API 和权限会变化，落地前应再次核对对应版本文档。

## 结论摘要

主流平台并没有收敛到一个通用的机器人线协议。它们通常各自提供平台 API、事件订阅、权限模型和消息能力；通用点主要存在于应用内部的事件模型和 adapter 边界。

因此建议 Servify 抽象**平台无关的内部接入契约**，由每个平台 adapter 负责鉴权、接收方式、平台数据映射和出站 API。Memex Bot API 是这个边界上的一个可接入平台/来源，不应直接充当所有平台共同遵循的 wire protocol。知识沉淀逻辑应消费规范化事件，与具体 adapter 解耦。

尤其要避免把消息模型缩减为 `群 + 用户 + 文本`：线程/回复、消息编辑与撤回、附件、互动，以及群成员和消息读取权限，都会影响知识候选的关联、修订和来源追溯。统一模型应保留平台原始标识和原始载荷引用，平台特有字段可扩展，不能表达的能力要显式声明。

## 平台实现对比

| 平台 | 入站方式 | 出站/交互 | 知识沉淀相关特征 |
|---|---|---|---|
| **Telegram Bot API** | `getUpdates` 长轮询或 HTTPS webhook 二选一；更新有递增 `update_id`，可用于去重/排序；长轮询以 offset 确认更新。 | HTTP Bot API；Update 类型覆盖消息、编辑消息、回调等，具体更新由 `allowed_updates` 过滤。 | 同时提供 push 和 pull 接入形态；有更新序号和消息更新类型。适配器需要维护 offset/去重，并保留 edit 等事件语义。[官方 Bot API](https://core.telegram.org/bots/api) |
| **Slack** | Events API 通过公开 HTTP Request URL 推送，或用 Socket Mode 建立 WebSocket；订阅事件受 OAuth scope 和 bot 可见范围约束。HTTP 接收端应快速返回 2xx，失败会重试，文档建议异步排队处理。 | Web API 发消息；使用 `thread_ts` 回复线程，支持 block 等富消息。 | 明确的事件 envelope、event ID、安装/授权上下文；至少一次投递意味着需要幂等。线程是消息关系，不应压扁为普通文本。[Events API](https://api.slack.com/apis/connections/events-api)、[Socket Mode](https://api.slack.com/apis/connections/socket)、[消息线程](https://api.slack.com/messaging/sending) |
| **Discord** | Gateway 是持久 WebSocket：心跳、序列号、session resume 和 intents 控制事件流；特权 intents 决定可接收的数据范围。Interactions 也可单独经 HTTP endpoint 推送。 | REST API 发消息；Interactions 支持斜杠命令、组件和交互响应。 | 事件流的连接恢复和订阅权限属于协议核心；不能假定机器人默认可读服务器全部消息。群消息采集要映射权限/intents 与安装范围。[Gateway](https://github.com/discord/discord-api-docs/blob/main/developers/events/gateway.mdx)、[Interactions](https://github.com/discord/discord-api-docs/blob/main/developers/interactions/receiving-and-responding.mdx) |
| **飞书** | 应用机器人可订阅接收消息事件；官方 SDK 支持基于长连接的事件回调能力。自定义群机器人 webhook 主要是单向推送。 | 服务端 API 发文本、富文本、图片、卡片等；应用机器人还可做会话/群管理，具体取决于权限。 | “自定义 webhook”和“应用机器人”是两种不同能力面；知识采集需应用机器人、事件权限和管理员发布/授权，不可把 webhook 推送当成双向机器人接入。[机器人概览](https://open.feishu.cn/document/client-docs/bot-v3/bot-overview)、[开放平台介绍](https://open.feishu.cn/document/client-docs/intro?from=op_develop_app) |
| **钉钉** | 能力取决于机器人类型。自定义机器人以群 webhook 为主；开放平台另有企业应用、事件订阅及机器人能力。 | 群 webhook 适合通知；应用/企业机器人可通过 OpenAPI 做消息及群会话相关操作。 | 要按机器人类型区分“只推送”与“可接收/交互”，并将组织授权和群权限留在 adapter 的能力声明中。[获取自定义机器人 Webhook](https://open.dingtalk.com/document/orgapp/custom-robot-access)、[开放平台能力中心](https://open.dingtalk.com/) |
| **Microsoft Teams** | Bot/Agent 以 Activity 事件模型收消息；群聊/频道默认主要收到 @ 提及。通过 Resource-specific Consent 可授权接收会话全部消息，受应用配置、管理员/资源授权和环境限制。 | 通过 Activity 上下文回复；Teams SDK 对消息编辑、软删除、成员变化等提供独立 handler。 | Activity 是可扩展事件信封，并非所有 app 默认获得全量消息。采集器必须检查授权范围；message update/delete 是独立事件，需驱动候选快照状态更新。[会话与群聊](https://learn.microsoft.com/en-us/microsoftteams/platform/bots/how-to/conversations/channel-and-group-conversations)、[Activity handlers](https://learn.microsoft.com/en-us/microsoftteams/platform/bots/bot-basics)、[全量消息/RSC](https://learn.microsoft.com/en-us/microsoftteams/platform/bots/how-to/conversations/channel-messages-for-bots-and-agents) |
| **Matrix Application Service** | 开放规范：homeserver 向 application service 推送批量事件事务，带 transaction ID；接收端按事务 ID 幂等，失败时 homeserver 排队并指数退避。可用 namespace 限定关注用户/房间。 | 使用 Matrix Client-Server API 发送 room events；消息本身是带 event ID、room、sender、时间和类型的事件。 | 是本次调研中最接近公开、跨实现协议的方案，但它是联邦 IM 的完整事件/身份/API 生态，不是一个能无成本套在 Slack/飞书上的 bot schema。[Application Service API](https://spec.matrix.org/v1.9/application-service-api/)、[事件模型](https://spec.matrix.org/latest/client-server-api/) |

## Memex 现有设计核对

依据 `../memex/docs/design/机器人平台.md` 和 `../memex/docs/design/knowledge-deposition-loop.md`：

- bot 是 `bot:<name>` 伪账号，加入群后从离线队列接收群 TEXT；HTTP 面为 `GET /bot/updates`、`POST /bot/ack`、`POST /bot/send`，认证使用独立 token。
- updates 支持长轮询，ack 后清队，语义是至少一次投递；重复处理需按 `msg_id` 幂等。知识闭环设计也以此为明确前提。
- 当前 bot 更新解码只承诺 TEXT，消息字段为 `msg_id/from/to/text/ts_ms`；Memex 消息协议本身没有结构化 thread/reply、reaction 或 edit 语义。撤回可见于存储状态，但 bot updates 不是一套完整的 message lifecycle event feed。
- bot 的群成员关系同时代表接收范围；群/部门 allowlist 限制主动行为，文档注明入站不裁。其授权语义与 Slack scopes、Discord intents、Teams RSC、飞书应用权限并不等价。
- Memex 的知识沉淀设计采用同一个 bot 收件箱，`msg_id` 去重；审批候选保留问题/回答源消息 ID、账号、时间快照。由于缺少 thread/edit 信号，第一期依赖显式指令和启发式窗口；撤回只在审核展示时提示来源消息状态。

这套接口对于 Memex 内部 bot 应用非常合适，且符合内网 pull/ACK 需求。若 Servify 连接 Memex，可以写一个 Memex adapter 消费这套 API；若 Memex 想接 Slack/Discord 等平台，需在 Memex 外围另建 platform connector，把外部平台事件映射到 Memex 能承载的消息/事件语义。当前接口并不天然覆盖外部平台的线程、编辑、卡片、交互和权限差异。

## 跨平台共性与不可抹平的差异

### 可统一的内部语义

1. **来源身份**：平台、平台租户/安装、机器人实例、群/会话、成员、消息，以及平台原始 ID。
2. **事件信封**：事件 ID、事件类型、平台时间、接收时间、幂等键、关联消息 ID、原始载荷引用。
3. **消息内容**：文本、富文本/卡片、附件引用、提及；统一正文可以供知识处理，但需保留原始结构用于来源回看。
4. **消息生命周期**：创建、编辑、撤回/删除；编辑和删除应成为有顺序的事件，而非隐式覆盖聊天记录。
5. **上下文关系**：会话、线程、回复/引用父消息。不存在 thread 的平台不必伪造 thread ID。
6. **接收可靠性**：确认/重试策略、事件去重、重放游标或连接恢复状态。
7. **出站操作**：发消息、回复消息/线程、支持的交互类型；调用结果应返回平台消息 ID，失败携带可分类错误/限流信息。
8. **能力和授权声明**：机器人能否读全群消息、是否只读提及、是否能读历史、能否收编辑/撤回/成员变化、能否发线程回复或卡片。能力来自平台授权结果，不是配置者的无验证声明。

### 应留在 adapter 的差异

- transport：webhook、长轮询、WebSocket、Socket Mode、平台自有事件事务。
- 平台凭据获取/刷新、回调验签、事件订阅校验、重试窗口、游标语义和限流处理。
- 平台原生的富消息、按钮、交互回调、群管理能力及消息可见性规则。
- 平台用户/群 ID、身份映射、组织授权和平台专有扩展字段。

内部统一模型不需要把这些差异伪装成所有平台共有的功能；用可选字段和能力清单表达即可。

## 对 Servify 架构的建议

Servify 已在 `README.zh.md` 预留 channel adapters 映射到 `conversation`/`routing` 的边界；群聊知识沉淀审计也建议从消息持久化后的应用层出口发布 `MessageObserved`，而不是让 knowledge domain 调平台 API。建议沿用这一方向：

```text
平台 webhook / polling / websocket
             ↓
       平台 adapter
             ↓
验证、去重、规范化、持久化/入队
             ↓
      conversation event
        ↙           ↘
     routing     knowledge capture
```

建议第一版写成内部接口/契约文档，而不是新的公开网络协议：

- `InboundEvent`：`source`、`tenant/install`、`event_id`、`event_type`、`occurred_at`、`received_at`、`conversation`、`actor`、`message`、`parent_message_id`、`raw_ref`。
- `Message`：`platform_message_id`、`kind`、`text`、`content_parts`、`attachments`、`mentions`、`edited_at`、`deleted_at`。
- `ConnectorCapabilities`：明确接收事件种类、读取范围、历史读取、回复/线程、富消息、编辑/撤回通知等能力。
- `OutboundPort`：send/reply/update/delete 中只放 Servify 真实需要且各 adapter 可实现的操作；用能力声明标记可选项，避免无意义的空实现。
- `KnowledgeSourceRef`：候选必须存原平台、会话、消息及线程定位符，同时存必要的文本快照；后续源消息编辑/撤回能更新来源状态，但不能静默改写已经审核的知识版本。

入站 adapter 应先验签/鉴权，再以平台事件 ID 幂等入队并尽快确认平台请求；业务处理异步执行。对于 polling/stream 连接则持久化游标/恢复状态后再推进确认点。标准化事件最好同时保留原始平台 payload 的受控引用，避免遇到平台扩展时丢数据；原始内容必须遵循租户隔离、访问控制与保留期限。

## 推荐决策与后续调研项

**推荐决策：先定义 Servify 内部聊天接入契约，再做 Memex connector 作为首个验证实现。暂不发布“适配所有聊天软件”的独立开放协议。** Memex 可以复用规范化消息/来源结构，但保留自己的 HTTP Bot API 和 pull/ack 特性。

进入实现前仍需确认：

1. 第一目标平台是 Memex、企业微信/飞书/钉钉，还是 Servify 自有会话？目标平台决定接入方向和授权模式。
2. 知识采集是否需要全量群消息，还是仅 @/指令/指定群？平台能否授权全量读取是产品可行性门槛。
3. 需要保留多少历史消息、附件和原始载荷，源消息撤回/编辑后候选与正式知识如何标记。
4. 是单向采集沉淀，还是还要求机器人在原群回答、追问、发送审批卡片？出站能力会改变接口范围。
5. 是否要求离线部署、出站网络受限、平台数据不离开租户边界；这可能决定 pull/长连接优于公网 webhook。

## 参考资料

- Telegram：[Bot API](https://core.telegram.org/bots/api)
- Slack：[Events API](https://api.slack.com/apis/connections/events-api)、[Socket Mode](https://api.slack.com/apis/connections/socket)、[发送消息与线程](https://api.slack.com/messaging/sending)
- Discord：[Gateway protocol](https://github.com/discord/discord-api-docs/blob/main/developers/events/gateway.mdx)、[Receiving and responding to interactions](https://github.com/discord/discord-api-docs/blob/main/developers/interactions/receiving-and-responding.mdx)
- 飞书：[机器人概览](https://open.feishu.cn/document/client-docs/bot-v3/bot-overview)、[开放平台介绍](https://open.feishu.cn/document/client-docs/intro?from=op_develop_app)
- 钉钉：[获取自定义机器人 Webhook 地址](https://open.dingtalk.com/document/orgapp/custom-robot-access)、[开放平台](https://open.dingtalk.com/)
- Microsoft Teams：[群聊与频道会话](https://learn.microsoft.com/en-us/microsoftteams/platform/bots/how-to/conversations/channel-and-group-conversations)、[Activity handlers](https://learn.microsoft.com/en-us/microsoftteams/platform/bots/bot-basics)、[Resource-specific Consent 消息接收](https://learn.microsoft.com/en-us/microsoftteams/platform/bots/how-to/conversations/channel-messages-for-bots-and-agents)
- Matrix：[Application Service API v1.9](https://spec.matrix.org/v1.9/application-service-api/)、[Client-Server API 事件模型](https://spec.matrix.org/latest/client-server-api/)
- 本地 Memex：[机器人平台设计](../../../memex/docs/design/机器人平台.md)、[群内知识沉淀闭环](../../../memex/docs/design/knowledge-deposition-loop.md)
- 本仓库：[群聊知识沉淀审计](../review/sdk-kb-audit-2026-10-19.md)、[SDK 与渠道预留](../../README.zh.md#sdk-与渠道预留)
