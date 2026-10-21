# Web Embedding Integration Guide

For integrators embedding Servify customer service into their own website/app. Covers embedding-method selection, session identity, ticket context wiring, event callbacks, deployment forms, and the complete appearance/theme configuration surface for the widget and chat window.

All interfaces in this document are based on the repo's current code (route registration see `apps/server/internal/app/server/`), and the examples can be followed directly.

## 1. Embedding Method Selection

| Method | Applies to | Integration cost | Customization depth |
| --- | --- | --- | --- |
| A. Direct widget embed (`widget.js`) | Adding a customer-service entry to a corporate/independent site | Two `<script>` tags | Appearance and theme fully parameterized (see §7), change styles without changing code |
| B. iframe embed of the demo page | Host page does not allow injecting third-party JS | One `<iframe>` | Same as A (inside the iframe it's still the widget), but overlay positioning is controlled by the host |
| C. Self-built UI over the SDK (`servify-sdk.umd.js` / React / Vue packages) | Need a custom interaction form (e.g. embedding the session into an app sidebar) | Implement the UI yourself | Fully under your control, Servify only provides the session/voice clients |

Selection rule: default to A; choose B when there is no JS injection permission; choose C to fold session capability into your own product UI.
The main body of this guide revolves around A; for C's SDK usage see `apps/demo-sdk/README.md` and
`sdk/examples/{react,vue,vanilla}`.

## 2. Quick Start

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

Open the page, a floating button appears in the bottom-right corner, click to expand the chat window: the AI first answer is knowledge-base based, after transfer to human an
agent receives it in the admin console (`/`, the admin SPA) (agent-side receiving and transfer links see
Agent workspace).

**Don't want to hand-write a page? The repo ships two example pages that can be opened directly**
(served directly by the Servify server, snippets are identical to this section and can be followed line by line; both viewports 375/desktop have been verified):

- Light brand site: `http://localhost:8080/demo-sdk/examples/page-light.html`
- Dark site: `http://localhost:8080/demo-sdk/examples/page-dark.html`

Zero-dependency experience form (no OpenAI key, no external services): in config set the three providers `ai`/`embedding`/
`knowledge` all to `local` (with sqlite), the AI first answer goes through local extractive QA —
first `POST /api/v1/ai/knowledge/upload` (admin logged-in state) to upload a knowledge document which gets indexed,
then asking a question in the widget will hit it; note that the AI answer content on the example pages depends on the uploaded knowledge documents.

Without `sessionId`, the widget generates a new session id per page load; to keep the session across pages see §3.

A zero-config form is also supported: `<script src="…/widget.js" data-servify-widget
data-base-url="http://localhost:8080"></script>`, common parameters all have corresponding
`data-*` attributes (full table see §7.4).

## 3. Session Identity and Single Sign-On

### 3.1 App-side designated session (logged-in user bound to a fixed session)

The host app compiles its own user identifier into `sessionId` (e.g. `app_<uid>`) when rendering the embed code;
the same user opening the widget on any page/device resumes the same session:

```html
<script>
  ServifyWidget.create({
    baseUrl: 'https://servify.example.com',
    sessionId: 'app_' + CURRENT_USER_ID,   // 宿主侧模板变量
  });
</script>
```

The conversation row is only established when the first message is persisted; pre-designating the id does not require creating the session first.

### 3.2 Visitor token (the ticket for authenticated surfaces like feedback)

Some endpoints require authentication (e.g. the "was this answer helpful" feedback `POST /api/v1/ai/feedback`).
The visitor token is issued by the **host backend**; the browser never touches any long-term credentials:

```sh
# 宿主后端 → servify（service API key 走 X-API-Key 头）
curl -X POST https://servify.example.com/api/v1/guest/session \
  -H 'X-API-Key: <SERVICE_API_KEY>' \
  -H 'Content-Type: application/json' \
  -d '{"session_id": "app_42"}'
# 201 {"access_token":"<HS256>","token_type":"bearer","expires_at":1767225600}
```

- The token is bound to `session_id`, default validity 24h (`security.guest_token.ttl` adjustable);
  at WS handshake `/api/v1/ws?access_token=` the server validates three layers: signature + freshness + session binding,
  using session A's token to handshake session B will be rejected.
- Browser side hands the token to the widget:

```js
ServifyWidget.create({
  baseUrl: 'https://servify.example.com',
  sessionId: 'app_42',
  accessToken: '<上一步的 access_token>',
});
```

Credential discipline: `X-API-Key` (service principal) is used only in the host backend; the browser side only holds
a visitor token bound to a single session, valid short-term.

## 4. Ticket Context Wiring

### 4.1 Session to ticket (auth-free surface)

The visitor initiates a ticket from the chat window, assigned the session's tenant scope; category/priority/source are
server-fixed defaults (general/normal/chat), custom fields and tags are not open to visitors:

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

`ai_summary` is a session summary assembled by the host/AI side; it is persisted with the ticket, and the agent sees the
context directly on the ticket page without scrolling back through the chat log.

### 4.2 Customer business context (agent-side read-only display)

Sync the host-side business data (plan/traffic/orders) into Servify as customer profiles; the agent views them
in the admin console, all through server-side APIs, credentials never entering the browser:

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

Customer surface API overview: `POST/GET /api/customers`, `GET/PUT /api/customers/:id`,
`GET /api/customers/:id/activity`, `POST /api/customers/:id/notes`,
`PUT /api/customers/:id/tags` (permission surface `customers`; plus stats/export/erase-data
and other operations and compliance endpoints). Currently it's one-way "host pushes, agent reads" sync; automatic
association of chat sessions with customer profiles (matching by email/phone) comes in a later batch.

## 5. Event Callbacks: Ticket Status Flowing Back to the App

Servify has built-in outbound webhooks: whitelisted events on the internal event bus are delivered to host
addresses per endpoint configuration, a background worker delivers asynchronously, HMAC signatures prevent tampering.

```sh
# 注册端点（管理面，权限面 webhooks）
curl -X POST https://servify.example.com/api/webhooks \
  -H 'X-API-Key: <SERVICE_API_KEY>' -H 'Content-Type: application/json' \
  -d '{"url": "https://app.example.com/hooks/servify", "events": ["ticket.created","ticket.assigned","ticket.closed"]}'
```

- Subscribable events are governed by the whitelist returned by `GET /api/webhooks/events`; currently 12 kinds:
  `ticket.created` / `ticket.assigned` / `ticket.closed`,
  `conversation.created` / `conversation.message_received`,
  `routing.agent_assigned` / `routing.transfer_completed`,
  `call.started` / `call.held` / `call.resumed` / `call.transferred` / `call.ended`.
- Each delivery carries the `X-Servify-Signature: t=<unix秒>,v1=<hex>` header, `v1 =
  HMAC-SHA256(secret, "<t>.<body>")`; the host side verifies the signature first before processing, rejecting
  replays (time-window validation).
- Delivery records and compensation: `GET /api/webhooks/deliveries` to query,
  `POST /api/webhooks/deliveries/:id/redeliver` for manual redelivery,
  `POST /api/webhooks/:id/test` connectivity self-test, `POST /api/webhooks/:id/secret`
  to rotate the signing key.

After the host receives `ticket.closed`, it can close the corresponding service request in its own system, forming
the "chat → ticket → flow back" closed loop.

## 6. Deployment Forms

### 6.1 Standalone deployment (docker compose)

```sh
cd infra/compose
JWT_SECRET=<强随机值> SERVIFY_PUBLISH_PORT=8080 docker compose \
  -f docker-compose.yml up -d
# 需要外部知识库（weknora）时叠加：
JWT_SECRET=<…> docker compose -f docker-compose.yml -f docker-compose.weknora.yml up -d
```

- In-stack ports can be moved out via environment variables (when host ports conflict, no container changes needed):
  `SERVIFY_PUBLISH_PORT` / `POSTGRES_PUBLISH_PORT` / `REDIS_PUBLISH_PORT` /
  `WEKNORA_API_PUBLISH_PORT` / `WEKNORA_WEB_PUBLISH_PORT` /
  `EMBEDDING_PUBLISH_PORT` / `ELASTICSEARCH_PUBLISH_PORT`.
- Production deployments must explicitly set `JWT_SECRET` (config gate blocks the default value).

### 6.2 Integration addresses and static assets

- All widget and window requests point to `baseUrl` (default same-origin). For cross-domain deployment just point
  `baseUrl` at the servify address, no proxy needed.
- `/demo-sdk/*` static assets (SDK artifacts + widget) are **served directly by the server in non-production
  environments**; production no longer exposes them by default (P3-3 onwards goes through the SPA fallback); for
  production embedding please self-host the three files (`servify-sdk.umd.js` / `widget.js` / `widget.css`)
  on your own CDN and point the snippet's `src` there.
- The voice translation channel `/api/v1/ws/voice` requires the server to have `ai.asr` wired; when not wired the
  mic button does not appear in the widget header (capability probing), the main chat link is unaffected.

## 7. Widget and Chat Window Theming

All appearance/theme goes through init parameters; change styles without changing code. Independent light/dark token
sets, `theme: 'auto'` follows the host site's light/dark scheme (`prefers-color-scheme`).

### 7.1 Widget appearance (floating button)

| Parameter | Type | Default | Description |
| --- | --- | --- | --- |
| `icon` | `string \| object` | `'chat'` | Three forms: preset name `chat/smile/headset/lifebuoy/dots/bolt`; `{image: '<svg…\|data:\|https://…>'}` custom upload (inline SVG/data URL/image direct link); `{url: 'https://…'}` URL reference |
| `color` | `string` | `'#667eea'` | Primary color (`primaryColor` as a compatibility alias); also participates in automatic theme token generation |
| `borderRadius` | `number \| string` | `'50%'` | Trigger button corner radius; numbers as px, strings as-is (e.g. `'12px'`, `'8px 50%'`) |
| `position` | `string` | `'bottom-right'` | `bottom-right/bottom-left/top-right/top-left` |
| `size` | `string \| number` | `'medium'` | `small(44px)/medium(56px)/large(64px)` tiers, or a button diameter in px |

### 7.2 Chat window theme token full table

| token | Role | Light default | Dark default |
| --- | --- | --- | --- |
| `primary` | Primary color (send button/links/focus) | `color` | `color` brightened one step |
| `primaryContrast` | Text on primary color | Auto by luminance | Auto by luminance |
| `panelBg` / `panelText` | Window background/text | `#ffffff` / `#333333` | `#1f2430` / `#e5e7eb` |
| `msgsBg` | Message area background | `#fafafa` | `#171a23` |
| `bubbleSelfBg` / `bubbleSelfText` | Visitor bubble | Primary color / auto contrast color | Primary color / auto contrast color |
| `bubbleAgentBg` / `bubbleAgentText` | Agent/AI bubble | `#f0f0f0` / `#333333` | `#2a3040` / `#e5e7eb` |
| `systemText` | System hint bar | `#888888` | `#9ca3af` |
| `metaText` | Secondary text like timestamps/quotes | `#999999` | `#8b93a3` |
| `inputBg` / `inputBorder` / `inputText` | Input box | White / `#dddddd` / `#333333` | `#262c3a` / `#3a4152` / `#e5e7eb` |
| `chipBg` / `chipBorder` | Suggested question chips | White / `#e0e0e0` | `#262c3a` / `#3a4152` |
| `headerBg` | Window header background (gradient can be passed) | Primary color gradient | Primary color gradient (brightened) |
| `font` | Font stack | System stack | Same as left |
| `shadowPanel` / `shadowTrigger` | Window/button shadows | Each their own default | Each their own default |
| `radiusPanel` / `radiusBubble` / `radiusInput` | Three corner radii | `16/12/20px` | Same as left |

The brand primary color auto-generates the full set: only pass `themeFromColor` (or `color`), the remaining tokens
are derived by luminance (visitor bubble color follows the primary, contrast text color switches dark/white by
WCAG luminance).

### 7.3 Theme loading methods

| Parameter | Description |
| --- | --- |
| `theme` | `'light' \| 'dark' \| 'auto'`; `auto` listens to `prefers-color-scheme` and switches in real time |
| `themeLight` / `themeDark` | Independent light/dark overrides (object, keys are §7.2 token names), fields not overridden keep defaults |
| `themeTokens` | Current-theme override, same-named fields override both light and dark sets simultaneously |
| `themeUrl` | Remote theme JSON (`{"light":{…},"dark":{…},"brand":{…}}`), config lives server-side for unified multi-site changes; fetch failure does not block, local config is kept |
| `brand` | `{ logo, name, welcome }` brand slot: window header logo, name, opening welcome message; the remote theme's `brand` can override it a second time |

### 7.4 `data-*` auto-initialization full table

The full table of `data-*` configs for `<script src="…/widget.js" data-servify-widget …>` (19 items; JSON-type attribute values are JSON strings) is in [Web SDK docs](./web-sdk.md#_4-widget-data-配置全表), which is the sole authority.

### 7.5 Two-style examples

After starting the service they are served directly by Servify (no separate static server needed):

- Light brand site: <http://localhost:8080/demo-sdk/examples/page-light.html>
  (off-white background with teal primary, `headset` preset icon + brand slot)
- Dark site: <http://localhost:8080/demo-sdk/examples/page-dark.html>
  (dark theme + square rounded button + `lifebuoy` icon)

Both pages ship narrow-viewport responsiveness (≤480px the widget auto-switches to a near-full-height sheet, page cards single column);
the embed block source carries a snippet comment marked「与 §2 一致」("consistent with §2"), view the page source in the browser to follow line by line.
Source files: `apps/demo-sdk/examples/page-light.html` / `page-dark.html`.

- Remote theme sample: `apps/demo-sdk/examples/theme.json` (light/dark sets + brand slot,
  the complete format delivered via `themeUrl`)

To follow along locally: start any static server at the repo root pointing to the repo root, visit
`/apps/demo-sdk/examples/page-light.html`; change the `:8080` in `baseUrl` per your deployment address.

### 7.6 Interaction behavior (built-in, no configuration needed)

- Floating ball multi-state: collapsed shows the icon, with unread a red badge count (capped at `99+`, popping animation);
  expanding clears it and swaps to the close icon.
- Expand/collapse animation: the panel enters from the floating ball's corner with scale+translate (0.18s), the
  swap icon rotates in; `prefers-reduced-motion` users auto-degrade to no animation.
- Mobile: at viewport ≤480px the panel stretches into a near-full-height sheet, not covering the whole screen dead.
- Layering: widget root `z-index: 99999`, window/button shadows go through tokens (§7.2) and can be adjusted wholesale.

## 8. Experience Baseline Comparison (Crisp livechat)

The experience baseline references Crisp livechat (a commercial product; only interaction patterns and information
architecture are borrowed, not its code or art assets). Item-by-item comparison:

| Capability | Crisp behavior | Servify widget status | Gap handling |
| --- | --- | --- | --- |
| Floating ball multi-state | Collapsed icon/unread badge/hover feedback | All present: icon switching, unread count capped 99+ (popping animation), hover scale + brighten | — |
| Expand animation | Panel smoothly grows from the floating ball's corner (≈0.15–0.2s, transform+opacity) | 0.18s scale+translate entry, `transform-origin` follows the four corners automatically | — |
| Icon switching form | Icon rotates into a close icon when opened | Swap icon rotate-in animation (0.2s) | — |
| Window layering and shadow | High stacking level, deep soft shadows | `z-index: 99999` + shadow tokens configurable | — |
| Mobile adaptation | Small screen near-full-screen sheet | ≤480px near-full-height sheet (dvh, older kernels fall back to vh) | — |
| Light/dark mode | Auto-switches with the product's light/dark | `theme: 'auto'` listens to `prefers-color-scheme` | — |
| Brand theme injection | Palette/copy/position configurable | Five appearance params + 23 tokens in light/dark sets + remote theme + brand slot | — |
| Proactive messages | Lazy popping bubble to prompt the opening | Opening welcome bubble (brand.welcome) | Timed/behavior-triggered proactive popping not done, later batch |
| Typing indicator | Standalone "typing" indicator | Streaming typewriter (AI char-by-char on screen + ▌ cursor) | Form differs: here it is real streaming content |
| Message types | Audio/file/GIF/CSAT cards | Text + knowledge-base citation lines + helpfulness feedback + voice captions | File/image upload UI not in the widget (server `/api/v1/upload` already exists), later batch |
| Help center entry | In-window articles tab | Suggested question chips (`/public/suggestions/*`) | Form differs: the entry converges into clickable questions |
| Multilingual | locale configurable | UI copy currently Chinese | Multilingual not done, later batch |
| Read receipts | Read/Unread markers | Not done | Later batch |
| Visitor identity | Cross-device chat continuation after email identification | `sessionId` binding + visitor token (§3) | Automatic identity merging (matching customer profiles by email) not done, later batch |

Conclusion: the main link (floating ball states, expand animation, layering shadows, mobile, light/dark following,
brand injection) is aligned with the baseline; gaps concentrate on the content-form surface (proactive messages,
file upload, multilingual, read receipts, identity merging), already listed for later batches.
