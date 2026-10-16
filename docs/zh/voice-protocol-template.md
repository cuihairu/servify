# Voice Protocol Integration Template

新协议接入时，优先复用统一 `voiceprotocol` 契约，而不是把协议 DTO 直接带进 `voice` 业务层。

## 最小实现清单

1. 定义协议 payload
2. 实现 `voiceprotocol.CallSignalingAdapter` 或 `voiceprotocol.MediaSessionAdapter`
3. 将协议字段归一到：
   - `CallEvent`
   - `MediaEvent`
4. 在 runtime 装配阶段注册 adapter
5. 为协议入口补：
   - adapter 单测
   - HTTP -> runtime -> voice 的端到端测试

## Hosted webhook provider 额外要求

如果协议通过 webhook 接入，还应实现：

- `voiceprotocol.HostedVendorWebhookAdapter`
- 签名校验
- 固定 webhook path
- provider event id 去重策略

## 必要映射项

- invite
- answer
- hangup
- transfer
- hold
- resume
- dtmf

## 接入约束

- `voice` 业务层不能依赖协议专属 DTO
- DTMF 必须先归一为 `voiceprotocol.CallEventDTMF`
- provider callback 只回写统一模型，不直接写业务状态表
- 新协议至少补一个 `/api/voice/protocols` 契约测试
