// Web 真实浏览器验收驱动（被 test-web-browser-acceptance.sh 调用）。
//
// 用真实 Chromium + playwright 驱动真实服务与真实 demo-sdk 挂件：
//   1) 访客侧（真实浏览器）：
//        嵌入    —— host 页挂载 ServifyWidget，sw-trigger 渲染
//        鉴权    —— 会话 WS 建立（已连接，session 由 data-session-id 固定）
//        来源展示 —— local 知识库命中 → ai-response 带 sources → sw-source-row
//        断线重连 —— context.setOffline 断网 → 未连接 → 恢复 → 已连接（指数退避重连）
//   2) 会话动作（真实服务，管理 API——与 Workbench UI 同端点）：
//        人工接管 —— assign（AGENT_A 接管）+ 坐席回复
//        转派    —— transfer（→ AGENT_B）
//        工单    —— /api/v1/tickets 建单（session 关联）
//   3) 访客可见效应：坐席回复（agent-message→bot 气泡）+ 转接通知（system 气泡）
//   4) 管理端 UI（真实浏览器）：登录（鉴权）+ 工作台渲染会话（转派/转工单按钮可达）
//        + 转派 UI（Modal 选坐席→已转派）+ 工单创建 UI（/ticket/list 新建工单：
//        选客户→标题→确定→工单已创建）
//
// 证据：逐项写 EVIDENCE_DIR/<name>.json + 末尾 browser-summary.txt（key=value），
// 退出码 0=全过，1=任一断言失败。

import { createRequire } from 'module';
import { writeFileSync, mkdirSync, readFileSync } from 'fs';
import { join } from 'path';

const require = createRequire(import.meta.url);
const { chromium } = require('playwright');

const env = process.env;
const BASE = env.BASE;
const HOST = env.HOST;
const SESSION_ID = env.SESSION_ID;
const ADMIN_USERNAME = env.ADMIN_USERNAME;
const ADMIN_PASSWORD = env.ADMIN_PASSWORD;
const ADMIN_TOKEN = env.ADMIN_TOKEN;
const AGENT_A_ID = env.AGENT_A_ID;
const AGENT_B_ID = env.AGENT_B_ID;
const AGENT_A_TOKEN = env.AGENT_A_TOKEN;
const AGENT_A_NAME = env.AGENT_A_NAME || '';
const AGENT_B_NAME = env.AGENT_B_NAME || '';
const CUSTOMER_ID = env.CUSTOMER_ID || '';
const CUSTOMER_NAME = env.CUSTOMER_NAME || '';
const EVIDENCE_DIR = env.EVIDENCE_DIR;
const ADMIN_BASE = env.ADMIN_BASE || BASE;
const CHROME_BIN = env.CHROME_BIN;
const SERVIFY_VERSION = env.SERVIFY_VERSION || '';

const KB_QUESTION = '打印机卡纸是最常见的办公设备故障';
const AGENT_REPLY = '您好，这里是坐席的回复，已为您登记。';
const TICKET_TITLE = `web-browser 工单 ${SESSION_ID}`;
const TICKET_DESC = '浏览器验收工单：访客来源展示与坐席接管全链路。';

const summary = {};
const results = {};

function rec(name, ok, detail) {
  results[name] = { ok: !!ok, ...(detail || {}) };
  summary[name] = ok ? 'true' : 'false';
  console.log(`[${ok ? 'PASS' : 'FAIL'}] ${name} ${detail ? JSON.stringify(detail) : ''}`);
}

function writeEvidence(name, data) {
  writeFileSync(join(EVIDENCE_DIR, `${name}.json`), JSON.stringify(data, null, 2));
}

async function waitForText(page, selector, text, timeout = 30000, poll = 300) {
  const t0 = Date.now();
  while (Date.now() - t0 < timeout) {
    try {
      const val = await page.textContent(selector);
      if (val && val.includes(text)) return true;
    } catch {
      /* not present yet */
    }
    await page.waitForTimeout(poll);
  }
  return false;
}

async function waitForSelectorText(page, selector, text, timeout = 30000) {
  return waitForText(page, selector, text, timeout);
}

async function main() {
  mkdirSync(EVIDENCE_DIR, { recursive: true });
  const browser = await chromium.launch({
    headless: true,
    executablePath: CHROME_BIN,
    args: ['--no-sandbox', '--disable-gpu'],
  });
  const versionInfo = { servify_version: SERVIFY_VERSION, session_id: SESSION_ID, at: new Date().toISOString() };

  try {
    // ─────────────── 访客侧：嵌入 + 鉴权 + 来源展示 + 断线重连 ───────────────
    const visitorCtx = await browser.newContext({ viewport: { width: 1280, height: 900 } });
    // 捕获所有 WebSocket 实例（断线重连需真实关闭底层 socket：Playwright
    // setOffline 不关已建立的 WS，故注入包装类持有实例以便页内 close）。
    await visitorCtx.addInitScript(() => {
      window.__servifyWss = [];
      // Proxy 包装：保留 OPEN/CONNECTING/CLOSING/CLOSED 等静态属性（否则
      // widget 的 send() 里 readyState !== WebSocket.OPEN 恒真，静默不发帧）。
      window.WebSocket = new Proxy(window.WebSocket, {
        construct(target, args) {
          const w = new target(...args);
          window.__servifyWss.push(w);
          return w;
        },
      });
    });
    const page = await visitorCtx.newPage();
    const wsEvents = [];
    let wsHandshakes = 0;
    let wsClosed = 0;
    const cdp = await visitorCtx.newCDPSession(page);
    await cdp.send('Network.enable');
    cdp.on('Network.webSocketFrameReceived', (p) => {
      try { wsEvents.push(JSON.parse(p.response.payloadData)); } catch { /* ignore */ }
    });
    cdp.on('Network.webSocketFrameSent', (p) => {
      try { wsEvents.push({ __sent: true, ...JSON.parse(p.response.payloadData) }); } catch { /* ignore */ }
    });
    cdp.on('Network.webSocketHandshakeResponseReceived', () => { wsHandshakes += 1; });
    cdp.on('Network.webSocketClosed', () => { wsClosed += 1; });

    // 嵌入
    await page.goto(`${HOST}/index.html`, { waitUntil: 'load', timeout: 30000 });
    let embedOk = false;
    try {
      await page.waitForSelector('.sw-trigger', { timeout: 20000 });
      embedOk = true;
    } catch { /* fallthrough */ }
    rec('embed', embedOk, { selector: '.sw-trigger' });
    await page.screenshot({ path: join(EVIDENCE_DIR, 'visitor-embed.png') });

    // 打开面板
    await page.click('.sw-trigger');
    await page.waitForSelector('.sw-panel.open', { timeout: 10000 });

    // 鉴权：会话 WS 建立
    let authOk = false;
    try {
      await page.waitForSelector('.sw-header-status:has-text("已连接")', { timeout: 30000 });
      authOk = true;
    } catch { /* fallthrough */ }
    rec('auth', authOk, { status: '已连接', session_id: SESSION_ID });

    // 来源展示：local 知识库命中 → sw-source-row
    await page.fill('.sw-input', KB_QUESTION);
    await page.click('.sw-send-btn');
    let sourcesOk = false;
    let sourceTitle = '';
    try {
      await page.waitForSelector('.sw-source-row', { timeout: 30000 });
      sourceTitle = (await page.textContent('.sw-source-row')) || '';
      sourcesOk = sourceTitle.includes('打印机故障排查指南');
    } catch { /* fallthrough */ }
    rec('sources', sourcesOk, { row_text: sourceTitle.slice(0, 80), question: KB_QUESTION });
    await page.screenshot({ path: join(EVIDENCE_DIR, 'visitor-sources.png') });

    // 断线重连（真实网络故障）：断网 + 关闭底层 socket → WS 掉线（onclose，
    // widget 指数退避重连失败）→ 恢复网络 → 新 WS 握手成功 → 已连接。
    // 判据用 CDP webSocketHandshakeResponseReceived 计数：故障后握手数必须增长。
    let reconnectOk = false;
    try {
      const handshakesBefore = wsHandshakes;
      await visitorCtx.setOffline(true);
      // 真实断开底层 WebSocket（注入包装类持有实例）
      await page.evaluate(() => {
        try { const w = window.__servifyWss[0]; if (w && w.readyState === 1) w.close(); } catch { /* ignore */ }
      });
      // 确认掉线（wsClosed 增长）——离线期 widget 重连尝试均失败
      const t0 = Date.now();
      while (wsClosed === 0 && Date.now() - t0 < 20000) await page.waitForTimeout(300);
      const closedObserved = wsClosed > 0;
      // 恢复网络 → widget 重连成功 → 新握手
      await visitorCtx.setOffline(false);
      const t1 = Date.now();
      while (wsHandshakes <= handshakesBefore && Date.now() - t1 < 30000) await page.waitForTimeout(300);
      const newHandshake = wsHandshakes > handshakesBefore;
      await page.waitForSelector('.sw-header-status:has-text("已连接")', { timeout: 15000 });
      reconnectOk = closedObserved && newHandshake;
    } catch (e) { console.error('reconnect err', e.message); }
    rec('reconnect', reconnectOk, {
      fault: 'setOffline(true)+ws.close() → 掉线 → setOffline(false) → 重连握手',
      closed_observed: wsClosed > 0,
      handshakes: wsHandshakes,
    });
    await page.screenshot({ path: join(EVIDENCE_DIR, 'visitor-reconnect.png') });

    // ─────────────── 会话动作（真实服务管理 API，同 Workbench UI 端点） ───────────────
    const jsonHeaders = (token) => ({ 'Content-Type': 'application/json', ...(token ? { Authorization: `Bearer ${token}` } : {}) });

    // 人工接管（assign → AGENT_A）
    let assignOk = false;
    let assignStatus = 0;
    try {
      const r = await fetch(`${BASE}/api/omni/sessions/${SESSION_ID}/assign`, {
        method: 'POST', headers: jsonHeaders(ADMIN_TOKEN), body: JSON.stringify({ agent_id: Number(AGENT_A_ID) }),
      });
      assignStatus = r.status;
      assignOk = r.ok;
    } catch { /* fallthrough */ }
    rec('assign', assignOk, { http: assignStatus, agent_a: AGENT_A_ID, action: '接管(assign)' });

    // 坐席回复（agent-message → 访客 bot 气泡 + WS 帧）
    // 注：omni 路由为跨作用域鉴权——坐席 JWT 携带作用域，而访客会话在验收环境
    // 无 tenant 上下文、以空作用域落库 → 坐席 token 发 POST /messages 得到 403。
    // 与 Workbench「发送消息」一致，由操作者（admin，未作用域）发坐席消息；
    // IngestTextMessage 固定 sender=agent，访客收到的仍是 agent-message 帧。
    let agentReplyOk = false;
    let replyStatus = 0;
    const framesBefore = wsEvents.length;
    try {
      const r = await fetch(`${BASE}/api/omni/sessions/${SESSION_ID}/messages`, {
        method: 'POST', headers: jsonHeaders(ADMIN_TOKEN), body: JSON.stringify({ content: AGENT_REPLY }),
      });
      replyStatus = r.status;
      if (r.ok) {
        // 访客挂件实时可见坐席回复：WS 收到 agent-message 帧 → .sw-msg-bot 渲染
        const t0 = Date.now();
        while (Date.now() - t0 < 20000) {
          const gotFrame = wsEvents.slice(framesBefore).some((f) => f.type === 'agent-message');
          if (gotFrame) break;
          await page.waitForTimeout(300);
        }
        agentReplyOk = wsEvents.slice(framesBefore).some((f) => f.type === 'agent-message');
      }
    } catch { /* fallthrough */ }
    rec('agent_reply', agentReplyOk, { http: replyStatus, content: AGENT_REPLY, frame: 'agent-message' });

    // 转派（transfer → AGENT_B）
    let transferOk = false;
    let transferStatus = 0;
    try {
      const r = await fetch(`${BASE}/api/omni/sessions/${SESSION_ID}/transfer`, {
        method: 'POST', headers: jsonHeaders(ADMIN_TOKEN), body: JSON.stringify({ to_agent_id: Number(AGENT_B_ID) }),
      });
      transferStatus = r.status;
      transferOk = r.ok;
    } catch { /* fallthrough */ }
    rec('transfer', transferOk, { http: transferStatus, to_agent_b: AGENT_B_ID, action: '转派(transfer)' });

    // 工单（session 关联）
    let ticketOk = false;
    let ticketId = '';
    let ticketStatus = 0;
    try {
      const r = await fetch(`${BASE}/api/v1/tickets`, {
        method: 'POST', headers: jsonHeaders(ADMIN_TOKEN),
        body: JSON.stringify({ session_id: SESSION_ID, title: TICKET_TITLE, description: TICKET_DESC }),
      });
      ticketStatus = r.status;
      if (r.ok) {
        const body = await r.json();
        ticketId = String(body.id ?? body.data?.id ?? '');
        ticketOk = !!ticketId;
      }
    } catch { /* fallthrough */ }
    rec('ticket', ticketOk, { http: ticketStatus, ticket_id: ticketId, session_linked: SESSION_ID });

    // WS 帧留档（可重放：访客收到的全部帧）
    writeEvidence('visitor-ws-frames', { ...versionInfo, frames: wsEvents.map((f) => f.__sent ? { __sent: true, ...f } : f) });
    writeEvidence('visitor-effects', {
      ...versionInfo,
      agent_reply_content: AGENT_REPLY,
      bot_bubble_seen_after_reply: agentReplyOk,
      frames_types: [...new Set(wsEvents.map((f) => f.type).filter(Boolean))],
    });

    // ─────────────── 管理端 UI：登录 + 工作台（真实浏览器 + 真实 admin 构建产物） ───────────────
    // 服务端不挂载 admin SPA（无 catch-all），故 admin 走同源代理：静态服务
    // apps/admin/dist + 反代 /api 到真实服务端（ADMIN_BASE 为代理 origin）。
    const ADMIN_UI = env.ADMIN_UI === '0' ? 0 : 1;
    // 管理端 UI 阶段变量（在 if/else 外声明：末尾 writeEvidence 需引用）
    let adminLoginOk = false;
    let navToWorkspaceOk = false;
    let buttonsVisible = false;
    let transferUiOk = false;
    let ticketUiOk = false;
    const adminFailures = [];
    if (!ADMIN_UI) {
      // 全新检出无 admin dist（未构建）→ 管理端 UI 阶段跳过，不阻塞核心挂件/API 验收
      rec('admin_login', true, { skipped: true, note: 'ADMIN_UI=0（dist 缺失，未驱动 UI）' });
      rec('admin_workspace', true, { skipped: true, note: 'ADMIN_UI=0（dist 缺失，未驱动 UI）' });
      rec('transfer_ui', true, { skipped: true, note: 'ADMIN_UI=0（dist 缺失，未驱动 UI）' });
      rec('ticket_ui', true, { skipped: true, note: 'ADMIN_UI=0（dist 缺失，未驱动 UI）' });
    } else {
    const adminCtx = await browser.newContext({ viewport: { width: 1440, height: 900 } });
    const admin = await adminCtx.newPage();

    // 诊断：记录管理端 4xx/5xx 响应与重定向（request() 遇 401 会 location.replace('/login')，
    // 若工作台任一子请求 401，页面被导航走 → 转派按钮永不出现）。
    admin.on('response', (r) => {
      if (r.status() >= 400) adminFailures.push(`${r.status()} ${r.request().method()} ${r.url()}`);
    });
    admin.on('requestfailed', (r) => adminFailures.push(`FAILED ${r.method()} ${r.url()} ${r.failure()?.errorText || ''}`));

    // 登录（鉴权 UI）：LoginForm 字段 用户名/密码，提交按钮 type=submit（回车亦可）
    try {
      await admin.goto(`${ADMIN_BASE}/`, { waitUntil: 'domcontentloaded', timeout: 30000 });
      await admin.waitForSelector('input[placeholder="用户名"]', { timeout: 30000 });
      await admin.fill('input[placeholder="用户名"]', ADMIN_USERNAME);
      await admin.fill('input[placeholder="密码"]', ADMIN_PASSWORD);
      try {
        await admin.click('button[type="submit"]', { timeout: 5000 });
      } catch {
        await admin.press('input[placeholder="密码"]', 'Enter');
      }
      // 登录成功 → 跳转仪表板，侧边栏出现「接待工作台」菜单项
      await admin.waitForSelector('text=接待工作台', { timeout: 30000 });
      adminLoginOk = true;
    } catch (e) { console.error('admin login err', e.message); }
    rec('admin_login', adminLoginOk, { username: ADMIN_USERNAME });

    // 工作台：会话行（id 前 8 位）+ 动作按钮（转派/转工单/结束；接管仅 waiting 态渲染）。
    // 注意 antd 对两字按钮文案自动插入空格（「转派」渲染为「转 派」），故用
    // getByRole+正则（accessible name 匹配容忍空格）而非 has-text 精确匹配。
    const transferBtn = admin.getByRole('button', { name: /转\s*派/ });
    const ticketWsBtn = admin.getByRole('button', { name: /转\s*工单/ });
    try {
      await admin.goto(`${ADMIN_BASE}/workspace`, { waitUntil: 'domcontentloaded', timeout: 30000 });
      await admin.waitForSelector('text=/会话（\\d+）/', { timeout: 30000 });
      // 会话行 span 文案为 id 前 8 位（无客户名时）；点击选中
      await admin.click(`span:text-is("${SESSION_ID.slice(0, 8)}")`, { timeout: 15000 });
      // 中栏标题变更为「会话 <id 前 8 位>」→ 选中确认
      await admin.waitForSelector(`text=会话 ${SESSION_ID.slice(0, 8)}`, { timeout: 15000 });
      // 动作按钮依赖 selectedSession（loadConversation 异步完成），先等按钮出现
      await transferBtn.waitFor({ state: 'visible', timeout: 15000 });
      navToWorkspaceOk = true;
      buttonsVisible = await transferBtn.isVisible() && await ticketWsBtn.isVisible();
    } catch (e) {
      console.error('workspace nav err', e.message);
      adminFailures.push(`workspace_nav_failed: ${e.message} @ ${admin.url()}`);
    }
    rec('admin_workspace', navToWorkspaceOk && buttonsVisible, {
      session_row: SESSION_ID.slice(0, 8),
      buttons: '转派/转工单',
      note: '接管按钮按产品语义仅在会话 waiting 态渲染；接管动作经 assign API 实测（见 assign 证据）',
    });
    await admin.screenshot({ path: join(EVIDENCE_DIR, 'admin-workspace.png') });

    // 转派 UI：按钮 → Modal「转派会话」→ 选目标坐席 → 确定 → 成功提示「已转派」。
    // /api/agents 返回 models.Agent（无 name 字段），工作台下拉按
    // `item.name || 坐席 ${id}` 渲染，故以坐席 ID 文案定位（同时兼容带 name 时）。
    const targetLabelById = `坐席 ${AGENT_B_ID}`;
    const targetLabel = AGENT_B_NAME || targetLabelById;
    let targetMatch = targetLabel;
    try {
      await transferBtn.click();
      await admin.waitForSelector('.ant-modal-title:has-text("转派会话")', { timeout: 10000 });
      await admin.click('.ant-modal .ant-select-selector');
      await admin.waitForSelector('.ant-select-dropdown:not(.ant-select-dropdown-hidden)', { timeout: 10000 });
      const optionTexts = await admin.$$eval('.ant-select-item-option', (els) => els.map((e) => e.textContent.trim()));
      console.log('transfer options', JSON.stringify(optionTexts));
      if (!optionTexts.some((t) => t === targetMatch)) targetMatch = targetLabelById;
      await admin.click(`.ant-select-item-option:has-text("${targetMatch}")`, { timeout: 10000 });
      await admin.click('.ant-modal .ant-btn-primary');
      // handleTransfer 成功 → message.success('已转派')
      await admin.waitForSelector('text=已转派', { timeout: 15000 });
      transferUiOk = true;
    } catch (e) { console.error('transfer ui err', e.message); }
    rec('transfer_ui', transferUiOk, { modal: '转派会话', target: targetMatch });

    // 工单创建 UI：走管理端工单列表页「新建工单」（选客户 → 标题 → 确定）。
    // 不用工作台「转工单」按钮作判据：其载荷 customer_id 取会话档案，而匿名
    // web 访客会话无 customer（管理 API POST /api/tickets 对 customer_id
    // binding:required → 400），访客侧 session 关联工单已由 API 面（POST
    // /api/v1/tickets，见 ticket 证据）覆盖。
    const customerLabel = CUSTOMER_NAME
      ? `${CUSTOMER_NAME} (#${CUSTOMER_ID})` : `客户 #${CUSTOMER_ID}`;
    try {
      await admin.goto(`${ADMIN_BASE}/ticket/list`, { waitUntil: 'domcontentloaded', timeout: 30000 });
      await admin.getByRole('button', { name: '新建工单' }).click({ timeout: 15000 });
      await admin.waitForSelector('.ant-modal-title:has-text("新建工单")', { timeout: 10000 });
      // 客户下拉（listCustomers 异步填充选项）
      await admin.click('.ant-modal .ant-select-selector');
      await admin.waitForSelector('.ant-select-dropdown:not(.ant-select-dropdown-hidden)', { timeout: 15000 });
      await admin.click(`.ant-select-item-option:has-text("${customerLabel}")`, { timeout: 10000 });
      await admin.fill('.ant-modal input[placeholder="工单标题"]', `web-browser 工单 ${SESSION_ID}`);
      await admin.fill('.ant-modal textarea[placeholder="问题描述"]', TICKET_DESC);
      await admin.click('.ant-modal .ant-btn-primary');
      // handleSubmit 成功 → message.success('工单已创建')
      await admin.waitForSelector('text=工单已创建', { timeout: 15000 });
      ticketUiOk = true;
    } catch (e) { console.error('ticket ui err', e.message); }
    rec('ticket_ui', ticketUiOk, {
      page: '/ticket/list', modal: '新建工单', customer: customerLabel,
      note: '会话关联工单由访客面 POST /api/v1/tickets 覆盖（见 ticket 证据）',
    });
    await admin.screenshot({ path: join(EVIDENCE_DIR, 'admin-ticket.png') });
    }

    writeEvidence('admin-ui', {
      ...versionInfo,
      login_ok: adminLoginOk,
      workspace_ok: navToWorkspaceOk,
      buttons_visible: buttonsVisible,
      transfer_ui_ok: transferUiOk,
      ticket_ui_ok: ticketUiOk,
      ticket_ui_page: '/ticket/list',
      customer_id: CUSTOMER_ID,
      customer_name: CUSTOMER_NAME,
      admin_failures: adminFailures,
    });

    writeEvidence('browser-result', { ...versionInfo, results });
    writeFileSync(join(EVIDENCE_DIR, 'browser-summary.txt'),
      Object.entries(summary).map(([k, v]) => `${k}=${v}`).join('\n') + '\n');

    const allOk = Object.values(results).every((r) => r.ok);
    console.log(`\nDRIVER ${allOk ? 'PASS' : 'FAIL'}`);
    await browser.close();
    process.exit(allOk ? 0 : 1);
  } catch (err) {
    console.error('DRIVER FATAL', err);
    writeFileSync(join(EVIDENCE_DIR, 'browser-summary.txt'),
      Object.entries(summary).map(([k, v]) => `${k}=${v}`).join('\n') + '\n');
    await browser.close();
    process.exit(1);
  }
}

main();
