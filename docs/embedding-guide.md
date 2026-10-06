# Web 嵌入集成指南

面向把 Servify 客服嵌入自有网站/应用的对接方。覆盖嵌入方式选型、会话互认、
工单上下文打通、事件回调、部署形态，以及组件与聊天窗的完整外观/主题配置面。

文中全部接口以仓库当前代码为准（路由注册见 `apps/server/internal/app/server/`），
示例可直接跟跑。

## 1. 嵌入方式选型

| 方式 | 适用 | 接入成本 | 定制深度 |
| --- | --- | --- | --- |
| A. 组件直嵌（`widget.js`） | 官网/独立站加一个客服入口 | 两个 `<script>` 标签 | 外观与主题全参数化（见 §7），改样式不改代码 |
| B. iframe 嵌入演示页 | 宿主页面不允许注入第三方 JS | 一个 `<iframe>` | 同 A（iframe 内仍是 widget），但浮层定位由宿主控制 |
| C. 自建界面走 SDK（`servify-sdk.umd.js` / React / Vue 包） | 需要自定义交互形态（如把会话嵌进应用侧边栏） | 自行实现 UI | 完全自控，Servify 只提供会话/语音客户端 |

择优口径：默认选 A；无 JS 注入权限选 B；要把会话能力融进自有产品界面选 C。
本指南主体围绕 A 展开，C 的 SDK 用法见 `apps/demo-sdk/README.md` 与
`sdk/examples/{react,vue,vanilla}`。

## 2. 快速开始

```sh
# 1) 起服务（本地；compose 部署见 §6）
cd apps/server && go run ./cmd/server
# 服务地址 http://localhost:8080
```

```html
<!-- 2) 页面尾部嵌入 -->
<script src="http://localhost:8080/demo-sdk/servify-sdk.umd.js"></script>
<script src="http://localhost:8080/demo-sdk/widget.js"></script>
<script>
  ServifyWidget.create({ baseUrl: 'http://localhost:8080' });
</script>
```

打开页面，右下角出现悬浮按钮，点击展开聊天窗：AI 首答基于知识库，转人工后
坐席在管理端（`/`，admin SPA）接待。

不带 `sessionId` 时，组件为每次页面加载生成新会话 id；跨页面保持会话见 §3。

零配置形态也支持：`<script src="…/widget.js" data-servify-widget
data-base-url="http://localhost:8080"></script>`，常用参数都有对应
`data-*` 属性（全表见 §7.4）。

## 3. 会话互认与单点登录

### 3.1 应用侧指定会话（登录用户绑定固定会话）

宿主应用在渲染嵌入代码时把自己的用户标识编进 `sessionId`（如 `app_<uid>`），
同一用户在任何页面/设备打开组件都会恢复同一会话：

```html
<script>
  ServifyWidget.create({
    baseUrl: 'https://servify.example.com',
    sessionId: 'app_' + CURRENT_USER_ID,   // 宿主侧模板变量
  });
</script>
```

会话行在首条消息持久化时才建立，预先指定 id 不需要先建会话。

### 3.2 访客 token（反馈等认证面的入场券）

部分端点要求认证（如"这条回答有帮助吗"反馈 `POST /api/v1/ai/feedback`）。
访客 token 由**宿主后端**签发，浏览器不接触任何长期凭据：

```sh
# 宿主后端 → servify（service API key 走 X-API-Key 头）
curl -X POST https://servify.example.com/api/v1/guest/session \
  -H 'X-API-Key: <SERVICE_API_KEY>' \
  -H 'Content-Type: application/json' \
  -d '{"session_id": "app_42"}'
# 201 {"access_token":"<HS256>","token_type":"bearer","expires_at":1767225600}
```

- token 绑定 `session_id`，默认有效期 24h（`security.guest_token.ttl` 可调）；
  WS 握手 `/api/v1/ws?access_token=` 时服务端校验签名+时效+会话绑定三层，
  拿 A 会话的 token 握 B 会话会被拒。
- 浏览器侧把 token 交给组件：

```js
ServifyWidget.create({
  baseUrl: 'https://servify.example.com',
  sessionId: 'app_42',
  accessToken: '<上一步的 access_token>',
});
```

凭据纪律：`X-API-Key`（service principal）只在宿主后端使用；浏览器侧只持有
绑定单一会话、短期有效的访客 token。

## 4. 工单上下文打通

### 4.1 会话转工单（免认证面）

访客从聊天窗发起工单，按 session 归属租户 scope；分类/优先级/来源由服务端
固定默认值（general/normal/chat），自定义字段与标签不对访客开放：

```sh
curl -X POST https://servify.example.com/api/v1/tickets \
  -H 'Content-Type: application/json' \
  -d '{
    "session_id": "app_42",
    "title": "流量扣费异常",
    "description": "10 月账单多扣了一次流量包",
    "ai_summary": "用户反馈 10 月账单重复扣流量包费用，AI 已引导提供订单号"
  }'
```

`ai_summary` 是宿主/AI 侧整理的会话摘要，随工单落库，坐席在工单页直接看到
上下文，不用回翻聊天记录。

### 4.2 客户业务上下文（坐席侧只读展示）

把宿主侧的业务数据（套餐/流量/订单）以客户资料形式同步进 Servify，坐席在
管理端查看，全程走服务端接口、凭据不进浏览器：

```sh
# 宿主后端定时/事件驱动同步（均为管理面接口，需 agent/admin/service 主体）
curl -X POST https://servify.example.com/api/customers \
  -H 'X-API-Key: <SERVICE_API_KEY>' -H 'Content-Type: application/json' \
  -d '{"name": "用户 42", "email": "u42@example.com"}'

curl -X POST https://servify.example.com/api/customers/<id>/notes \
  -H 'X-API-Key: <SERVICE_API_KEY>' -H 'Content-Type: application/json' \
  -d '{"content": "套餐：Pro 年付；本月流量 82%；最近订单 #10023（待发货）"}'

curl -X PUT https://servify.example.com/api/customers/<id>/tags \
  -H 'X-API-Key: <SERVICE_API_KEY>' -H 'Content-Type: application/json' \
  -d '{"tags": "pro,年付,高价值"}'
```

客户面接口一览：`POST/GET /api/customers`、`GET/PUT /api/customers/:id`、
`GET /api/customers/:id/activity`、`POST /api/customers/:id/notes`、
`PUT /api/customers/:id/tags`（权限面 `customers`；另有 stats/export/erase-data
等运营与合规端点）。当前是"宿主推、坐席读"的单向同步；聊天会话与客户档案
的自动关联（按邮箱/手机号命中）在后续批次。

## 5. 事件回调：工单状态回流应用

Servify 内置出站 webhook：内部事件总线上的白名单事件按端点配置投递到宿主
地址，后台 worker 异步投递，HMAC 签名防篡改。

```sh
# 注册端点（管理面，权限面 webhooks）
curl -X POST https://servify.example.com/api/webhooks \
  -H 'X-API-Key: <SERVICE_API_KEY>' -H 'Content-Type: application/json' \
  -d '{"url": "https://app.example.com/hooks/servify", "events": ["ticket.created","ticket.assigned","ticket.closed"]}'
```

- 可订阅事件以 `GET /api/webhooks/events` 返回的白名单为准；当前收录
  `ticket.created` / `ticket.assigned` / `ticket.closed`。
- 每次投递带 `X-Servify-Signature: t=<unix秒>,v1=<hex>` 头，`v1 =
  HMAC-SHA256(secret, "<t>.<body>")`；宿主侧先验签再处理，拒绝重放
  （时间窗校验）。
- 投递记录与补偿：`GET /api/webhooks/deliveries` 查询、
  `POST /api/webhooks/deliveries/:id/redeliver` 手工补投、
  `POST /api/webhooks/:id/test` 连通性自测、`POST /api/webhooks/:id/secret`
  轮换签名密钥。

宿主侧收到 `ticket.closed` 后即可在自己的系统里关掉对应服务请求，形成
"聊天 → 工单 → 回流"闭环。

## 6. 部署形态

### 6.1 独立部署（docker compose）

```sh
cd infra/compose
JWT_SECRET=<强随机值> SERVIFY_PUBLISH_PORT=8080 docker compose \
  -f docker-compose.yml up -d
# 需要外部知识库（weknora）时叠加：
JWT_SECRET=<…> docker compose -f docker-compose.yml -f docker-compose.weknora.yml up -d
```

- 栈内端口可用环境变量外移（宿主机端口冲突时不改容器）：
  `SERVIFY_PUBLISH_PORT` / `POSTGRES_PUBLISH_PORT` / `REDIS_PUBLISH_PORT` /
  `WEKNORA_API_PUBLISH_PORT` / `WEKNORA_WEB_PUBLISH_PORT` /
  `EMBEDDING_PUBLISH_PORT` / `ELASTICSEARCH_PUBLISH_PORT`。
- 生产部署必须显式设置 `JWT_SECRET`（config gate 拦默认值）。

### 6.2 对接地址与静态资源

- 组件与窗体的所有请求都指向 `baseUrl`（默认同源）。跨域部署时把
  `baseUrl` 指到 servify 地址即可，无需代理。
- `/demo-sdk/*` 静态资源（SDK 产物 + widget）在**非 production 环境由服务端
  直接提供**；production 环境不再默认暴露（P3-3 起走 SPA 兜底），生产嵌入
  请自行托管三个文件（`servify-sdk.umd.js` / `widget.js` / `widget.css`）
  到宿主 CDN，并把 snippet 里的 `src` 指过去。
- 语音翻译通道 `/api/v1/ws/voice` 需要服务端装配 `ai.asr`；未装配时组件
  头部不出现麦克风按钮（能力探测），聊天主链路不受影响。

## 7. 组件与聊天窗主题

全部外观/主题走初始化参数，改样式不改代码。亮暗两套独立 token，
`theme: 'auto'` 跟随宿主站深浅（`prefers-color-scheme`）。

### 7.1 组件外观（悬浮按钮）

| 参数 | 类型 | 默认 | 说明 |
| --- | --- | --- | --- |
| `icon` | `string \| object` | `'chat'` | 三档：预设名 `chat/smile/headset/lifebuoy/dots/bolt`；`{image: '<svg…\|data:\|https://…>'}` 自定义上传（内联 SVG/data URL/图片直链）；`{url: 'https://…'}` URL 引用 |
| `color` | `string` | `'#667eea'` | 主色调（`primaryColor` 为兼容别名）；同时参与主题 token 自动生成 |
| `borderRadius` | `number \| string` | `'50%'` | 触发按钮圆角；数字按 px，字符串原样（如 `'12px'`、`'8px 50%'`） |
| `position` | `string` | `'bottom-right'` | `bottom-right/bottom-left/top-right/top-left` |
| `size` | `string \| number` | `'medium'` | `small(44px)/medium(56px)/large(64px)` 档位，或按钮直径 px 数字 |

### 7.2 聊天窗主题 token 全表

| token | 作用 | 亮套默认 | 暗套默认 |
| --- | --- | --- | --- |
| `primary` | 主色（发送钮/链接/焦点） | `color` | `color` 提亮一档 |
| `primaryContrast` | 主色上的文字 | 按亮度自动 | 按亮度自动 |
| `panelBg` / `panelText` | 窗体底/文字 | `#ffffff` / `#333333` | `#1f2430` / `#e5e7eb` |
| `msgsBg` | 消息区底 | `#fafafa` | `#171a23` |
| `bubbleSelfBg` / `bubbleSelfText` | 访客气泡 | 主色 / 自动对比色 | 主色 / 自动对比色 |
| `bubbleAgentBg` / `bubbleAgentText` | 坐席/AI 气泡 | `#f0f0f0` / `#333333` | `#2a3040` / `#e5e7eb` |
| `systemText` | 系统提示条 | `#888888` | `#9ca3af` |
| `metaText` | 时间/引用等次级文字 | `#999999` | `#8b93a3` |
| `inputBg` / `inputBorder` / `inputText` | 输入框 | 白 / `#dddddd` / `#333333` | `#262c3a` / `#3a4152` / `#e5e7eb` |
| `chipBg` / `chipBorder` | 建议问题 chips | 白 / `#e0e0e0` | `#262c3a` / `#3a4152` |
| `headerBg` | 窗头背景（可传渐变） | 主色渐变 | 主色渐变（提亮） |
| `font` | 字体栈 | 系统栈 | 同左 |
| `shadowPanel` / `shadowTrigger` | 窗体/按钮阴影 | 各自默认 | 各自默认 |
| `radiusPanel` / `radiusBubble` / `radiusInput` | 三处圆角 | `16/12/20px` | 同左 |

品牌主色自动生成整套：只给 `themeFromColor`（或 `color`），其余 token 按亮度
推导（气泡自色随主色、对比文字色按 WCAG 亮度切换深白）。

### 7.3 主题装载方式

| 参数 | 说明 |
| --- | --- |
| `theme` | `'light' \| 'dark' \| 'auto'`；`auto` 监听 `prefers-color-scheme` 实时切换 |
| `themeLight` / `themeDark` | 亮/暗套独立覆盖（对象，键为 §7.2 token 名），未覆盖字段保持默认 |
| `themeTokens` | 当前主题覆盖，同名字段同时盖掉亮暗两套 |
| `themeUrl` | 远程主题 JSON（`{"light":{…},"dark":{…},"brand":{…}}`），配置放服务端多站点统一改；拉取失败不阻塞，保持本地配置 |
| `brand` | `{ logo, name, welcome }` 品牌位：窗头 logo、名称、开场欢迎语；远程主题的 `brand` 可二次覆盖 |

### 7.4 `data-*` 自动初始化全表

`<script src="…/widget.js" data-servify-widget …>`（JSON 类属性值为 JSON 字符串）：

`data-base-url` `data-session-id` `data-access-token` `data-icon` `data-icon-image`
`data-icon-url` `data-color`（`data-primary-color` 别名） `data-border-radius`
`data-position` `data-size` `data-theme` `data-theme-light` `data-theme-dark`
`data-theme-tokens` `data-theme-from-color` `data-theme-url`
`data-brand-name` `data-brand-logo` `data-brand-welcome`

### 7.5 两风格示例

- 浅色品牌站：`apps/demo-sdk/examples/page-light.html`（米白底青绿主色，
  `headset` 预设图标 + 品牌位）
- 深色站：`apps/demo-sdk/examples/page-dark.html`（暗色主题 + 方形圆角按钮
  + `lifebuoy` 图标）
- 远程主题样例：`apps/demo-sdk/examples/theme.json`（亮暗两套 + 品牌位，
  经 `themeUrl` 下发的完整格式）

本地跟跑：仓库根目录起任意静态服务器指到仓库根，访问
`/apps/demo-sdk/examples/page-light.html`；`baseUrl` 里的 `:8080` 按部署地址改。

### 7.6 交互行为（内置，无需配置）

- 悬浮球多状态：收起显示图标，有未读时红色徽标计数（封顶 `99+`，弹出动画）；
  展开即清零并换关闭图标。
- 展开/收起动画：面板从悬浮球角落以缩放+位移入场（0.18s），切换图标旋入；
  `prefers-reduced-motion` 用户自动降级为无动画。
- 移动端：视口 ≤480px 时面板拉成近全高 sheet，不遮死整屏。
- 层级：组件根 `z-index: 99999`，窗体/按钮阴影走 token（§7.2）可整体调。

## 8. 体验基准对照（Crisp livechat）

体验基准参考 Crisp livechat（商业产品，仅借鉴交互模式与信息架构，不取其
代码与美术资产）。逐项对照：

| 能力 | Crisp 表现 | Servify widget 现状 | 差距处置 |
| --- | --- | --- | --- |
| 悬浮球多状态 | 收起图标/未读数徽标/hover 反馈 | 全有：图标切换、未读计数封顶 99+（弹出动画）、hover 缩放提亮 | — |
| 展开动画 | 面板从悬浮球角落平滑生长（约 0.15–0.2s，transform+opacity） | 0.18s scale+translate 入场，`transform-origin` 随四角自动 | — |
| 图标切换形态 | 打开时图标旋转变关闭 | 切换图标旋入动画（0.2s） | — |
| 窗体层级与阴影 | 高层级、深层柔和阴影 | `z-index: 99999` + 阴影 token 可配 | — |
| 移动端适配 | 小屏近全屏 sheet | ≤480px 近全高 sheet（dvh，旧内核回退 vh） | — |
| 深浅模式 | 跟随产品亮暗自动切换 | `theme: 'auto'` 监听 `prefers-color-scheme` | — |
| 品牌主题注入 | 色板/文案/位置可配 | 外观五参数 + 22 token 亮暗两套 + 远程主题 + 品牌位 | — |
| 前摄消息 | 惰性弹出气泡引导开口 | 开场欢迎语气泡（brand.welcome） | 定时/行为触发的主动弹出未做，后续批 |
| 打字指示 | 独立"正在输入"指示 | 流式打字机（AI 逐字上屏 + ▌光标） | 形态不同：这里是真实流式内容 |
| 消息类型 | 音频/文件/GIF/CSAT 卡片 | 文本 + 知识库引用行 + 帮助度反馈 + 语音字幕 | 文件/图片上传 UI 未入组件（服务端 `/api/v1/upload` 已有），后续批 |
| 帮助中心入口 | 窗内文章 tab | 建议问题 chips（`/public/suggestions/*`） | 形态不同：入口收敛为可点的问题 |
| 多语言 | locale 可配 | 界面文案当前为中文 | 多语言未做，后续批 |
| 已读回执 | Read/Unread 标记 | 未做 | 后续批 |
| 访客身份 | 邮件识别后跨设备续聊 | `sessionId` 绑定 + 访客 token（§3） | 自动身份合并（按邮箱命中客户档案）未做，后续批 |

结论：主链路（悬浮球状态、展开动画、层级阴影、移动端、深浅跟随、品牌注入）
已对齐基准；差距集中在内容形态面（前摄消息、文件上传、多语言、已读回执、
身份合并），已列后续批次。
