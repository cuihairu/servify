# 聊天平台接入契约

状态：内部契约；本文不实现任何平台 connector。决策日期：2026-10-19。

## 首个平台决策

推荐首个平台为 **Telegram Bot API，采用 `getUpdates` 长轮询**。该决策可推翻：若首批客户不用 Telegram、无法授权群全量读取，或明确要求现有企业聊天平台，应重新评估。

| 候选 | 接收与全量群消息 | 验证复杂度 | 与 Servify 服务形态接缝 | 维护成本 |
|---|---|---|---|---|
| Telegram Bot API | 长轮询或 webhook；全量群消息须关闭 privacy mode 或授予 bot 管理员权限 | 长轮询低；webhook 有 secret header/path，但不是请求签名体系 | poller 可作为服务端 worker；持久化 offset 后推进 | 低至中；单一 HTTP API、显式映射更新类型 |
| Slack | Events HTTP push 或 Socket Mode；可见范围受 scope 和工作区安装约束 | 中至高：请求签名、时间戳、重试、URL challenge | push 需要公网入口/队列；Socket Mode 增加长连接 | 中；还需维护 scope、事件订阅、重试和限流 |
| Discord | Gateway WebSocket 或 interaction HTTP；intents 与权限约束消息 | 中：Gateway 心跳、session resume；HTTP interaction 签名 | 有状态长连接是新的服务接缝 | 中至高；resume、intents 和 Gateway 版本 |
| 飞书（应用机器人） | 事件订阅/长连接；需要应用权限及管理员授权 | 中：事件验证/加密配置 | 长连接可作为 worker，出站 OpenAPI 独立 | 中；应用权限和租户安装生命周期 |
| Microsoft Teams | Activity 投递；群/频道全量消息需要 RSC 与租户/资源授权 | 高：Bot Framework 鉴权与租户授权 | Activity 推送及 Microsoft 身份是新增依赖 | 高；授权流程和环境限制 |

Telegram 在当前判据下胜出：长轮询无需先开放公网回调及其验签/加密配置；持久 offset 适合 worker 检查点；出站是普通 Bot API 调用。但**默认并非全量群消息**：普通 bot 默认启用 privacy mode，管理员必须授权访问或关闭该模式并重新添加 bot。故必须逐群核实有效权限并留存授权，完成前禁止采集。官方资料说明了 privacy 限制及通过更高 `offset` 确认更新的语义：[Bot Features](https://core.telegram.org/bots/features)、[Bot API](https://core.telegram.org/bots/api)、[FAQ](https://core.telegram.org/bots/faq)（检索日期：2026-10-19）。

该推荐承接调研结论：平台间没有共同机器人 wire protocol，Servify 应定义内部事件模型并由 adapter 处理平台差异（调研 §1，检索日期 2026-10-19）。调研还指出权限及消息生命周期各异，不能把群聊简化成 `group + user + text`，否则线程、编辑、删除、附件和来源追溯会丢失（调研 §3–4）。

## 范围与分阶段计划

本文只定义内部契约；本次不发布公共 wire protocol，也不实现平台接入。后续先以 Memex connector 验证；其 TEXT 与至少一次 `msg_id` 语义必须如实声明，不得暗示支持编辑或线程。

1. **阶段 0——授权与保留：**核实客户需求、群管理员同意、全量消息实际权限、群白名单、保留周期和数据驻留；记录完成前关闭采集。
2. **阶段 1——契约基础：**定义外部事件键、inbox、来源引用、能力快照及游标/ACK 状态；增加脱敏观测、重放和死信处理。
3. **阶段 2——Memex 验证：**实现其 pull/ACK adapter；验证至少一次投递、去重、来源关联和租户隔离。
4. **阶段 3——Telegram 试点：**仅在阶段 0 获批后实现长轮询 worker、逐群授权和限流 `send`/`notify`，再评估其是否适合作为首个生产平台。

## 入站契约

adapter 将每条入站更新规范化为 `InboundEvent`。传输与平台原始结构留在 adapter 内。标识符为不透明字符串且限于 installation 作用域；时间为 UTC RFC3339。平台不支持的字段留空，不得伪造。

| 字段 | 类型 | 含义 |
|---|---|---|
| `platform` | string | adapter 稳定键，如 `telegram`、`memex` |
| `installation_id` | string | 租户下的平台应用/bot 安装标识 |
| `tenant_id`, `workspace_id` | string | 来自可信安装映射的 Servify 安全作用域，不信任入站 payload |
| `event_id`, `idempotency_key` | string | 平台事件标识和稳定去重键（`installation_id + event_id`） |
| `event_type` | enum | `message.created`、`message.edited`、`message.deleted`、`reaction.changed`、`member.changed`、`unknown` |
| `occurred_at`, `received_at` | timestamp | 平台发生时间与 Servify 接收时间 |
| `conversation` | object | `platform_id`、`kind`、`name?`、`is_group`、`thread_id?` |
| `sender` | object | `platform_id`、`display_name?`、`is_bot`、`servify_actor_id?` |
| `message` | object/null | `platform_id`、`kind`、`text?`、`content_parts[]`、`edited_at?`、`deleted_at?` |
| `message.content_parts[]` | object | `kind`、`text?`、`mime_type?`、`platform_file_id?`、`download_ref?`、`size_bytes?`、`checksum?`；事件不内嵌附件字节 |
| `attachments[]` | object | 规范化附件元数据及受限本地对象引用；默认不保存公开 URL |
| `parent_message_id`, `reply_to_message_id` | string/null | 平台提供时填写线程与回复关系 |
| `raw_payload_ref` | object/null | 本地加密引用：`store`、`object_id`、`sha256`、`expires_at`；不内嵌原始负载 |
| `platform_extensions` | object | 有命名空间且大小受限的平台字段；存储前脱敏 |

### 生命周期与落库

1. 先验证传输身份，将 installation 映射到租户作用域并检查群白名单，再规范化。
2. 在确认平台请求或推进轮询游标前，事务性持久化事件键和 inbox。重复 `(installation_id, idempotency_key)` 作为成功 no-op。平台提供顺序键时按会话保序。
3. inbox 提交后才投影为 Servify `ConversationMessage`；编辑/删除作为显式生命周期事件更新来源状态，不能静默改写已审核知识。未知事件隔离保存，并限制脱敏引用大小。
4. 若现有消息表无法表达平台 ID/生命周期，放入 connector 自有 inbox/source-reference 记录；不得把 `models.Message.Metadata` 冒充幂等账本。
5. 来源引用至少含平台、installation、会话、消息、发送者、时间、生命周期状态及审核所需的有限内容快照。知识候选保存引用；源消息删除后将来源标记为不可用。

既有实现接缝与限制：

- `ConversationMessage` 目前只有会话 ID、发送者、类型、内容、metadata 和创建时间（entities.go:58）。
- `AppendMessage` 会按作用域插入消息，但没有外部平台键或冲突处理（gorm_repository.go:86）。这是消息归档入口；adapter 必须先做 inbox 幂等。
- 会话历史读取带 tenant/workspace 作用域（gorm_repository.go:100）；adapter 必须传入可信作用域上下文。
- 会话事件总线有 `conversation.message_received`，且 event ID 独立于平台消息 ID（events.go:12）；不得将 bus event ID 用作入站去重键。
- 旧消息表主键为数字 ID，字段只有内容/类型/发送者等，没有平台 ID 或外部消息唯一约束（models.go:255）。

## Connector 与出站契约

```go
type Connector interface {
    Capabilities(ctx context.Context, installationID string) (Capabilities, error)
    Receive(ctx context.Context, sink InboundSink) error // poll/stream；sink 持久化后才能 ACK/推进游标
    Send(ctx context.Context, req SendRequest) (SendResult, error)
    Notify(ctx context.Context, req NotifyRequest) (SendResult, error)
}
type InboundSink interface { Accept(ctx context.Context, event InboundEvent) (AckDecision, error) }
```

`Send` 指定会话，可选线程/回复；`Notify` 是带明确目标范围和审计理由的系统/运营通知。两者都接受调用方幂等键、deadline 和结构化内容；返回平台消息 ID、接受时间及 `accepted`、`unknown`、`rejected` 状态。若 adapter 无法安全去重，`unknown` 不得盲目重试。adapter 不得隐式扩大接收人范围。

`Capabilities` 来自平台实际授权，逐 installation/群记录：`receive_mode`、`message_scope`（`mention_only`、`all_authorized_groups`、`unknown`）、`history_read`、`event_types[]`、`threads`、`replies`、`attachments`、`edits`、`deletes`、`reactions`、`send_text`、`send_rich`、`rate_limit`、`verified_at`。权限未知时关闭采集。

## 安全、凭据与数据处理

- 首个 adapter 优先使用轮询。未来若接 webhook，必须在解析/入队前验证平台签名或 secret、时间戳/重放策略和 body 大小；TLS 本身不算调用者认证。
- 凭据通过运行时环境变量或本机 secret store 注入，不进 git、日志、遥测、支持包或聊天。不要在对话中粘贴 token；限制文件权限，疑似泄露时轮换/撤销。
- 原始负载及附件默认加密存本机，**最多保留 30 天**；租户请求或源删除时可提前清除。规范化消息保留期由租户配置并在阶段 0 获批。未经单独明确的租户策略，不向第三方分析或 AI 服务发送原始消息/附件。
- Connector 负载的云备份和数据导出**默认关闭**。后续启用必须取得租户 opt-in，并说明区域/保留、加密和恢复后的删除语义；常规备份不带负载字节。
- 租户/工作区作用域从已存 installation 映射建立，不信任入站 payload。来源引用应用访问控制；群授权变更记审计。

## 限流、重试与错误分类

adapter 负责解析平台限流信号及退避。按 installation 使用 token bucket，并按会话保序；遵守平台 `Retry-After`/重试元数据。瞬时错误采用带 jitter 的指数退避（从 1 秒开始，最多 5 分钟）；达到 10 次或 24 小时后进入死信并告警，不暴露消息内容。永久错误不重试。

| 分类 | 示例 | 处理 |
|---|---|---|
| `auth` | token 过期/撤销、签名无效 | 停止 connector 并告警；修复凭据前不重试 |
| `permission` | 群未授权、没有全量消息权限 | 禁用该群；等待授权/配置变更 |
| `rate_limited` | HTTP 429/平台节流 | 遵守等待时间并降低桶速率 |
| `transient` | 超时、5xx、断线 | 有界指数退避；保留去重键 |
| `invalid` | payload 错误/不支持、附件过大 | 隔离或拒绝；不重试 |
| `conflict` | 幂等键重复 | 视为已接收并 ACK |
| `permanent` | 目标已删除、不支持操作 | 返回调用方；不重试 |
| `unknown_delivery` | 请求可能已到平台但响应超时 | 返回 `unknown`；能则查询对账，否则按运营策略决定是否重发 |

只有 inbox 已可靠落库或确认为重复时才允许入站 ACK。出站重试必须复用幂等键。传输 ACK 不代表后续知识处理成功，处理状态需独立追踪。

## 参考

- 仓内调研：聊天平台与协议；实现前重新核验平台文档。
- Telegram 官方资料：[Bot Features](https://core.telegram.org/bots/features)、[Bot API](https://core.telegram.org/bots/api)、[FAQ](https://core.telegram.org/bots/faq)，检索日期：2026-10-19。
