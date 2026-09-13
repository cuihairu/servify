# Twilio PSTN Webhook 接入指南

Servify 通过 hosted-vendor-webhook 协议(`platform/twiliovoice`)消费 Twilio
Programmable Voice 的两类 Status Callback,把运营商通话映射进统一的
voiceprotocol 事件流并落库。

## 范围与边界

**做什么:**

- 消费 **Call Status Callback**(queued / ringing / in-progress / completed /
  busy / no-answer / failed / canceled),映射为 call 事件并维护
  `voice_calls` 状态机(started → answered → ended)。
- 消费 **Recording Status Callback**,把 `RecordingUrl` 落到
  `voice_recordings.storage_uri`(upsert 语义,先于本方记录到达也能收下)。
- Gather 结果的 `Digits` 作为 DTMF 事件转发。
- `X-Twilio-Signature` 验签(HMAC-SHA1,完整 URL + 按名字典序拼接的表单参数,
  恒等时间比较)。

**不做什么:**

- **不产生 TwiML 应答**:webhook 一律以空 200 响应(Status Callback 只需要
  2xx)。来电不会被接听、不转坐席、不放 IVR。
- **不外呼**:outbound calls 不在本通路范围。
- **不转写**:Deepgram 转写链路独立于 PSTN 回调,未接通。

## 配置

```yaml
server:
  public_base_url: "https://support.example.com"   # 必须:验签 URL 的组成部分

voice:
  pstn:
    provider: "twilio"          # disabled(默认,路由不存在)| twilio
    validate_signature: true    # 默认 true;仅本地调试时才建议关闭
  twilio:
    auth_token: "${TWILIO_AUTH_TOKEN}"   # 与 Twilio 控制台一致
```

- `auth_token` 复用 `voice.twilio.auth_token`,不新增密钥面。
- `provider: twilio` 但 `auth_token` 为空 ⇒ **启动失败**(拒绝无签名验证的
  匿名端点上线)。
- `public_base_url` 必须设置:Twilio 对完整 URL 签名,反代会改写 Host,只有
  显式配置的外部基地址才能复现签名材料。本地无反代时可留空(用请求自身的
  scheme/host 兜底)。

## Twilio 控制台侧

对目标电话号码(或 SIP domain)配置 **Voice Configuration → Call Status
Callback**:

- **When a call status changes** 勾选全部状态;
- URL: `https://<your-domain>/public/voice/webhooks/twilio`(POST);
- Recording:在 Recording Status Callback 填同一 URL;
- 该路径固定不可改——路径是签名材料的一部分。

## 幂等与重试语义

Twilio 对非 2xx 响应会指数退避重试。本通路因此:

- **事件标识确定化**:`twilio-<kind>-<CallSid>`、
  `twilio-recording-<RecordingSid>`;
- **coordinator 状态守卫**:重复 invite/hangup/hold 等,对已到达状态的通话
  直接短路(200,不重复出站 `call.*` webhook);hangup 先于 invite 到达时
  返回 500,借 Twilio 重试在 invite 落地后自愈;
- **录音完成是 upsert**:同一 RecordingSid 重复回调安全;
- 参数错误(未知 CallStatus、缺 CallSid)返回 400——重试也无法自愈的错误,
  靠 Twilio 的重试上限自然终止。

不做 per-EventID 重放台账:上面的守卫已覆盖 Twilio 的实际重试模式。

## 安全面

- 匿名端点,验签即认证;验签失败 403,不带签名头 403。
- POST only,固定路径,`/public/voice/webhooks/` 已登记进 security surface
  catalog(仅 provider 非 disabled 时路由才存在)。
- 全局限流中间件覆盖该路径;响应不回显任何内部错误细节(验签失败统一
  "webhook signature rejected")。

## 验收

`scripts/test-voice-pstn-acceptance.sh` 对真实运行的服务做端到端校验
(签名 invite → 幂等 → answer → hangup → 录音落库 → 403/400 负样本):

```bash
TWILIO_AUTH_TOKEN=<token> SERVIFY_URL=http://localhost:8080 \
  SQLITE_DB=/path/to/servify.db \
  bash scripts/test-voice-pstn-acceptance.sh
```

可选增强:`ADMIN_TOKEN=<jwt>` 追加出站断言(创建 call.started 订阅 + 本地
receiver 验证收到);`PSQL_CMD="ssh host psql -t -A servify"` 以 postgres
替代 sqlite 做落库断言。

## 已知边界(暂缓项)

- 通话音频不落 Servify 存储:`RecordingUrl` 指向 Twilio 侧媒体,如需长期
  归档须另行拉取(可在此通路之上加同步任务)。
- hold/resume/transfer 不在 Call Status Callback 的表达能力内,适配器对
  这三类事件明确报不支持。
- per-EventID 重放台账、TwiML 应答、外呼:见上文"不做什么"。
