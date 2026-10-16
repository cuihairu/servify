# 06 Voice And Protocol Expansion

范围：

- voice 协议统一入口深化
- signaling/media provider 落地
- 录音/转写外部 provider
- 常见语音协议扩展预留

约束：

- `voice` 业务层不能反向依赖具体协议 DTO
- signaling 与 media 必须继续分层

## V1 protocol-end-to-end

- [x] 为 `sip` 协议入口补 invite -> answer -> hangup 端到端测试
- [x] 为 `sip-ws` 协议入口补 invite -> dtmf -> hangup 端到端测试
- [x] 为 `pstn-provider` webhook 入口补 invite -> transfer -> hangup 测试
- [x] 为 `/api/voice/protocols` 增加契约测试

验收：

- 新协议入口有完整 HTTP/runtime/voice 链路验证，不只 adapter 单测

## V2 call-control-semantics

- [x] 在统一协议入口补 hold 事件映射
- [x] 在统一协议入口补 resume 事件映射
- [x] 在统一协议入口补 transfer 事件映射到 `voice` use case
- [x] 明确 DTMF 在 voice/conversation/automation 间的分发策略

当前 DTMF 策略：

- 协议 adapter 先统一归一为 `voiceprotocol.CallEventDTMF`
- `voice` runtime 负责承接并保留统一入口，不直接写入通话状态
- 后续如需联动 `conversation` 或 `automation`，应订阅统一 `voice` 事件，而不是反向依赖具体协议 DTO

验收：

- 常见呼叫控制事件都能走统一归一化模型

## V3 provider-implementation

- [x] 将录音 provider 从 in-memory 抽成独立 mock/provider 包
- [x] 将转写 provider 从 in-memory 抽成独立 mock/provider 包
- [x] 为 provider 增加失败重试和错误模型
- [x] 预留异步 webhook/callback 回写接口

验收：

- 录音与转写 provider 可以独立替换，不影响 voice 用例

## V4 media-topology

- [x] 抽象 RTP/SRTP adapter 占位
- [x] 明确 WebRTC 与 RTP bridge 的 contract
- [x] 预留 conference/mixer 能力接口
- [x] 预留 QoS / voice quality metrics 采集点

验收：

- 后续从点对点通话扩展到更复杂媒体拓扑时不需要推翻现有结构

## V5 protocol-reservation

- [x] 预留 `voiceprotocol.ProtocolH323`
- [x] 预留 `voiceprotocol.ProtocolFreeSWITCHEventSocket` 或同类 PBX 适配能力
- [x] 预留 hosted voice vendor webhook adapter contract
- [x] 为新协议接入编写最小实现模板文档

验收：

- voice 模块不是“只支持 SIP”的隐性设计
