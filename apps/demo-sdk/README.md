# Servify Web SDK

`apps/demo-sdk` 保存浏览器可直接引用的 SDK 产物，以及一个轻量聊天挂件示例。

## 目录说明

- `servify-sdk.esm.js`：由 `sdk/packages/vanilla/dist/index.esm.js` 同步而来
- `servify-sdk.umd.js`：由 `sdk/packages/vanilla/dist/index.js` 同步而来，挂载到 `window.Servify`
- `index.d.ts`：由 `sdk/packages/vanilla/dist/index.d.ts` 同步而来
- `widget.js` / `widget.css`：演示用轻量挂件壳层，保留在仓库中，不由 SDK 构建自动产出

## 当前协议口径

- 默认实时通道：`/api/v1/ws`
- 聊天主链路：WebSocket-first，不再依赖旧的 `/api/sessions`、`/api/messages`
- AI：`/api/v1/ai/query`、`/api/v1/ai/status`
- 上传：`/api/v1/upload`
- 满意度：`/api/satisfactions`
- 会话查询与关闭：`/api/omni/sessions/*`
- 当前服务端未公开支持：
  - 队列 REST API
  - WebRTC call REST API
  - 旧式 REST 会话创建

## 浏览器直接使用

```html
<script src="/demo-sdk/servify-sdk.umd.js"></script>
<script>
  const client = new Servify({
    apiUrl: 'http://localhost:8080',
    wsUrl: 'ws://localhost:8080/api/v1/ws',
    customerName: 'Demo User',
    customerEmail: 'demo@example.com',
    debug: true,
  });

  await client.init();
  await client.startChat({ message: '你好，我需要帮助。' });
  await client.sendMessage('请帮我排查一下当前问题。');
  client.on('webrtc:state', (state) => console.log('remote assist state:', state));
</script>
```

## 远程协助

当前服务端采用会话级 WebRTC 信令模型，SDK 会把服务端 `webrtc-state-change` 归一到 `webrtc:state` 事件。

```ts
const peer = await client.startRemoteAssist({
  captureScreen: true,
  audio: false,
});

client.on('webrtc:answer', (answer) => client.acceptRemoteAnswer(answer));
client.on('webrtc:candidate', (candidate) => client.addRemoteIce(candidate));
client.on('webrtc:track', (event) => {
  const [stream] = event.streams;
  console.log('remote media stream:', stream);
});

await client.endRemoteAssist();
```

当前不承诺：

- 完整 co-browsing UI
- 专门坐席协助工作台
- 双端标准化演示系统

## 语音翻译（PROTOCOL §9）

语音实时翻译走独立 WS 通道 `/api/v1/ws/voice`（音频上行 + 字幕/译文音频下行，
两通道零共享）。SDK 侧由 `window.Servify` 上的两件工具承载：

- `Servify.VoiceChannel`：语音通道客户端——`voice:delta`/`voice:final`/`voice:audio`/`voice:error`
  四个下行事件 + `sendAudio()` 二进制上行；无自动重连（重启说话是用户显式动作）。
- `Servify.MicCapture`：麦克风采集 → pcm16 24kHz 单声道小端分片（ScriptProcessor
  路线，CSP `default-src 'self'` 友好；AudioWorklet 的 Blob URL 模块会被拦）。

```js
const channel = new Servify.VoiceChannel({
  url: 'ws://localhost:8080/api/v1/ws/voice',
  sessionId: 'ws_1699000000000', // 与会话通道同一 session id
  speaker: 'visitor',
});

channel.on('voice:final', (u) => console.log('字幕', u.speaker, u.content, u.degraded));
channel.on('voice:audio', (u) => new Audio('data:audio/mpeg;base64,' + u.audio).play());

await channel.connect();
const capture = new Servify.MicCapture();
await capture.start((chunk) => channel.sendAudio(chunk)); // Uint8Array 小端分片
// 收线
await capture.stop();
channel.disconnect();
```

`widget.js` 已内建端到端形态：面板头部出现麦克风按钮（能力探测——旧缓存包
没有上述两类时按钮不出现），点击起会话：`voice:delta` 渲染"正在说"直播行、
`voice:final` 渲染终句字幕（译文 + 原文小字 + 未翻译标注）、`voice:audio` 自动
播放（被浏览器自动播放策略拦截时退化为 ▶ 按钮）、会话通道断线或面板关闭
联动语音收线。需要服务端装配 `ai.asr`（未装配时路由不存在，语音连接失败提示）。

## 示例入口

- React：`sdk/examples/react`
- Vue：`sdk/examples/vue`
- Vanilla：`sdk/examples/vanilla`

## 重建与同步

```sh
npm -C sdk run build
bash ./scripts/sync-sdk-to-demo.sh
```

也可以直接执行：

```sh
make demo-sync-sdk
```
