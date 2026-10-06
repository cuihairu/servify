// Servify Widget 外观/主题配置面门禁（node --test apps/demo-sdk/widget.test.mjs）
//
// 覆盖面：icon 三档、四角位置、三档大小、圆角、亮暗主题与 token 覆盖、
// 远程主题、品牌位、开关面板。用最小 DOM shim 直驱 widget.js 的
// createWidget（不渲染，断言 DOM 结构与 --sw-* token 值）。

import test from 'node:test';
import assert from 'node:assert/strict';
import fs from 'node:fs';
import path from 'node:path';
import url from 'node:url';

const here = path.dirname(url.fileURLToPath(import.meta.url));

// ── 最小 DOM shim ────────────────────────────────────────────────

function makeElement(tag) {
  const e = {
    tagName: tag,
    children: [],
    className: '',
    innerHTML: '',
    textContent: '',
    parentNode: null,
    style: {
      _props: {},
      setProperty(k, v) { this._props[k] = v; },
      getPropertyValue(k) { return this._props[k]; },
    },
    classList: {
      _set: new Set(),
      add(c) { this._set.add(c); },
      remove(c) { this._set.delete(c); },
      contains(c) { return this._set.has(c); },
      toggle(c, force) {
        const on = force === undefined ? !this._set.has(c) : !!force;
        if (on) this._set.add(c); else this._set.delete(c);
        return on;
      },
    },
    appendChild(c) { e.children.push(c); c.parentNode = e; return c; },
    insertBefore(c, ref) {
      c.parentNode = e;
      const i = e.children.indexOf(ref);
      if (i >= 0) e.children.splice(i, 0, c); else e.children.push(c);
      return c;
    },
    removeChild(c) {
      const i = e.children.indexOf(c);
      if (i >= 0) e.children.splice(i, 1);
      c.parentNode = null;
    },
    get lastChild() { return e.children[e.children.length - 1] || null; },
    setAttribute(k, v) { e.attrs[k] = v; },
    getAttribute(k) { return e.attrs[k] ?? null; },
    querySelector(sel) {
      const cls = sel.startsWith('.') ? sel.slice(1) : null;
      const walk = (n) => {
        for (const c of n.children || []) {
          if (cls && String(c.className).split(/\s+/).includes(cls)) return c;
          const hit = walk(c);
          if (hit) return hit;
        }
        return null;
      };
      return walk(e);
    },
    addEventListener(ev, fn) { (e._listeners[ev] ||= []).push(fn); },
    emit(ev, arg) { for (const fn of e._listeners[ev] || []) fn(arg); },
    focus() {},
    attrs: {},
    _listeners: {},
    src: '',
    type: '',
    placeholder: '',
    title: '',
    disabled: false,
  };
  return e;
}

function installDom() {
  const head = makeElement('head');
  const body = makeElement('body');
  const doc = {
    head,
    body,
    currentScript: undefined,
    getElementById: () => null,
    createElement: (tag) => makeElement(tag),
    addEventListener() {},
    readyState: 'complete',
  };
  globalThis.document = doc;
  globalThis.window = globalThis;
  globalThis.location = { protocol: 'http:', host: 'host.example' };
  // togglePanel 会 connect → stub 掉真 WebSocket
  globalThis.WebSocket = class { constructor() { this.readyState = 1; } close() {} send() {} };
  // 推荐问题拉取：空列表
  globalThis.fetch = async () => ({ ok: true, json: async () => ({ success: true, data: { questions: [] } }) });
  return { doc, head, body };
}

function loadWidget() {
  const src = fs.readFileSync(path.join(here, 'widget.js'), 'utf8');
  new Function(src)(globalThis.window ?? globalThis);
  return globalThis.ServifyWidget;
}

function varOf(mount, name) {
  return mount.style._props[name];
}

test('默认配置：bottom-right/light 主题/默认主色/标准尺寸', () => {
  installDom();
  const api = loadWidget();
  const { mount } = api.create({ baseUrl: 'http://x.example' });
  assert.ok(mount.className.includes('servify-widget'));
  assert.ok(mount.className.includes('sw-pos-bottom-right'));
  assert.equal(mount.attrs['data-servify-theme'], 'light');
  assert.equal(varOf(mount, '--sw-primary'), '#667eea');
  assert.equal(varOf(mount, '--sw-trigger-size'), '56px');
  assert.equal(varOf(mount, '--sw-panel-width'), '380px');
  assert.equal(varOf(mount, '--sw-radius-trigger'), '50%');
  const trigger = mount.children[0];
  assert.equal(trigger.className, 'sw-trigger');
  assert.ok(String(trigger.innerHTML).startsWith('<svg'), '默认图标为内置 SVG');
});

test('icon 三档：预设名 / image（data URL、内联 SVG）/ url', () => {
  installDom();
  const api = loadWidget();
  const a = api.create({ baseUrl: 'http://x.example', icon: 'headset' });
  assert.ok(String(a.mount.children[0].innerHTML).includes('M3 18v-6'), 'headset 预设命中');
  assert.ok(!String(a.mount.children[0].innerHTML).includes('<img'));

  const b = api.create({ baseUrl: 'http://x.example', icon: { image: 'data:image/png;base64,AAA' } });
  assert.ok(String(b.mount.children[0].innerHTML).includes('<img src="data:image/png;base64,AAA"'));

  const c = api.create({ baseUrl: 'http://x.example', icon: { image: '<svg width="9"><path/></svg>' } });
  assert.ok(String(c.mount.children[0].innerHTML).startsWith('<svg width="9"'), '内联 SVG 直挂');

  const d = api.create({ baseUrl: 'http://x.example', icon: { url: 'https://cdn.example/i.svg' } });
  assert.ok(String(d.mount.children[0].innerHTML).includes('<img src="https://cdn.example/i.svg"'));
});

test('四角位置与非法值回退', () => {
  installDom();
  const api = loadWidget();
  for (const pos of ['bottom-right', 'bottom-left', 'top-right', 'top-left']) {
    const { mount } = api.create({ baseUrl: 'http://x.example', position: pos });
    assert.ok(mount.className.includes('sw-pos-' + pos), pos);
  }
  const fallback = api.create({ baseUrl: 'http://x.example', position: 'middle' });
  assert.ok(fallback.mount.className.includes('sw-pos-bottom-right'), '非法位置回退 bottom-right');
});

test('大小三档与数字直径；圆角数字/字符串', () => {
  installDom();
  const api = loadWidget();
  const s = api.create({ baseUrl: 'http://x.example', size: 'small' });
  assert.equal(varOf(s.mount, '--sw-trigger-size'), '44px');
  assert.equal(varOf(s.mount, '--sw-panel-width'), '340px');
  const n = api.create({ baseUrl: 'http://x.example', size: 72, borderRadius: 28 });
  assert.equal(varOf(n.mount, '--sw-trigger-size'), '72px');
  assert.equal(varOf(n.mount, '--sw-radius-trigger'), '28px');
  const r = api.create({ baseUrl: 'http://x.example', borderRadius: '12px' });
  assert.equal(varOf(r.mount, '--sw-radius-trigger'), '12px');
});

test('暗色主题与亮暗独立覆盖、themeTokens 全局覆盖', () => {
  installDom();
  const api = loadWidget();
  const dark = api.create({ baseUrl: 'http://x.example', theme: 'dark' });
  assert.equal(dark.mount.attrs['data-servify-theme'], 'dark');
  assert.equal(varOf(dark.mount, '--sw-panel-bg'), '#1f2430');
  assert.equal(varOf(dark.mount, '--sw-bubble-agent-bg'), '#2a3040');

  const lightOverride = api.create({
    baseUrl: 'http://x.example',
    theme: 'light',
    themeLight: { panelBg: '#fffbe6' },
  });
  assert.equal(varOf(lightOverride.mount, '--sw-panel-bg'), '#fffbe6');
  assert.equal(varOf(lightOverride.mount, '--sw-msgs-bg'), '#fafafa', '未覆盖字段保持默认');

  const both = api.create({
    baseUrl: 'http://x.example',
    theme: 'dark',
    themeTokens: { radiusPanel: '24px' },
  });
  assert.equal(varOf(both.mount, '--sw-radius-panel'), '24px');
});

test('theme auto 跟随宿主深浅（matchMedia 桩）', () => {
  installDom();
  const mq = { matches: true, addEventListener() {} };
  globalThis.matchMedia = () => mq;
  const api = loadWidget();
  const auto = api.create({ baseUrl: 'http://x.example', theme: 'auto' });
  assert.equal(auto.mount.attrs['data-servify-theme'], 'dark');
  mq.matches = false;
  const autoLight = api.create({ baseUrl: 'http://x.example', theme: 'auto' });
  assert.equal(autoLight.mount.attrs['data-servify-theme'], 'light');
  delete globalThis.matchMedia;
});

test('品牌主色自动生成整套：bubble 自色随主色，对比字色按亮度', () => {
  installDom();
  const api = loadWidget();
  const a = api.create({ baseUrl: 'http://x.example', color: '#0f766e' });
  assert.equal(varOf(a.mount, '--sw-primary'), '#0f766e');
  assert.equal(varOf(a.mount, '--sw-bubble-self-bg'), '#0f766e');
  assert.equal(varOf(a.mount, '--sw-primary-contrast'), '#ffffff', '深主色配白字');
  const b = api.create({ baseUrl: 'http://x.example', themeFromColor: '#fde047' });
  assert.equal(varOf(b.mount, '--sw-primary'), '#fde047');
  assert.equal(varOf(b.mount, '--sw-primary-contrast'), '#1f2430', '亮主色配深字');
});

test('品牌位：名称/logo/欢迎语', () => {
  installDom();
  const api = loadWidget();
  const { mount } = api.create({
    baseUrl: 'http://x.example',
    brand: { name: '渡口客服', logo: 'https://cdn.example/logo.png', welcome: '您好，渡口为您服务' },
  });
  const header = mount.children[2].children[0];
  const logo = header.querySelector('.sw-header-logo');
  assert.ok(logo, 'logo 已挂载');
  assert.equal(logo.src, 'https://cdn.example/logo.png');
  const title = header.children[1];
  assert.equal(title.textContent, '渡口客服');
  assert.equal(mount.children[0].attrs.title, '渡口客服');
  // 欢迎语是消息区第一条 bot 气泡
  const msgs = mount.children[2].children[1];
  assert.equal(msgs.children[0].children[0].textContent, '您好，渡口为您服务');
});

test('远程主题：拉取成功覆盖 token 与品牌，失败保持本地', async () => {
  installDom();
  globalThis.fetch = async (u) => (String(u).includes('good')
    ? { ok: true, json: async () => ({ light: { panelBg: '#123456' }, brand: { name: '远程站' } }) }
    : { ok: false, json: async () => ({}) });
  const api = loadWidget();
  const good = api.create({ baseUrl: 'http://x.example', themeUrl: 'http://x.example/good.json' });
  await new Promise((r) => setTimeout(r, 0));
  assert.equal(varOf(good.mount, '--sw-panel-bg'), '#123456');
  const header = good.mount.children[2].children[0];
  assert.equal(header.children[0].textContent, '远程站');

  const bad = api.create({ baseUrl: 'http://x.example', themeUrl: 'http://x.example/bad.json' });
  await new Promise((r) => setTimeout(r, 0));
  assert.equal(varOf(bad.mount, '--sw-panel-bg'), '#ffffff', '失败回退本地亮套');
});

test('开关面板：展开换关闭图标，收起还原 launcher 图标', () => {
  installDom();
  const api = loadWidget();
  const { mount } = api.create({ baseUrl: 'http://x.example', icon: 'bolt' });
  const btn = mount.children[0];
  const panel = mount.children[2];
  btn.emit('click');
  assert.ok(panel.classList.contains('open'), '面板展开');
  assert.ok(String(btn.innerHTML).includes('line x1="18"'), '展开后显示关闭图标');
  btn.emit('click');
  assert.ok(!panel.classList.contains('open'), '面板收起');
  assert.ok(String(btn.innerHTML).includes('polygon'), '收起还原 launcher 图标（bolt）');
});

test('展开/收起动画挂钩：sw-open 状态类与 aria 状态', () => {
  installDom();
  const api = loadWidget();
  const { mount } = api.create({ baseUrl: 'http://x.example' });
  const btn = mount.children[0];
  const panel = mount.children[2];
  assert.ok(!mount.classList.contains('sw-open'), '初始收起无 sw-open');
  assert.equal(btn.attrs['aria-expanded'], 'false');
  assert.equal(panel.attrs['aria-hidden'], 'true');
  btn.emit('click');
  assert.ok(mount.classList.contains('sw-open'), '展开挂 sw-open（面板入场/图标旋入动画挂钩）');
  assert.equal(btn.attrs['aria-expanded'], 'true');
  assert.equal(panel.attrs['aria-hidden'], 'false');
  btn.emit('click');
  assert.ok(!mount.classList.contains('sw-open'), '收起还原');
  assert.equal(btn.attrs['aria-expanded'], 'false');
  assert.equal(panel.attrs['aria-hidden'], 'true');
});

test('未读数：面板收起时到达的消息累计并封顶 99+，展开清零', () => {
  installDom();
  const api = loadWidget();
  const { mount, client } = api.create({ baseUrl: 'http://x.example' });
  const btn = mount.children[0];
  const badge = mount.children[1];
  const panel = mount.children[2];
  assert.equal(badge.style.display, 'none', '初始无未读');
  // 坐席/AI 内容在面板收起时到达 → 未读 +1；自己的回显与系统条不计
  client.emit('message', { type: 'agent-message', data: { content: '您好' } });
  client.emit('message', { type: 'ai-response', data: { content: '答案' } });
  client.emit('message', { type: 'text-message', data: { content: '自己说的' } });
  client.emit('message', { type: 'transfer_notification', data: { message: '客服接入' } });
  assert.equal(badge.textContent, '2', '只有坐席/AI 内容计未读');
  assert.equal(badge.style.display, 'flex');
  for (let i = 0; i < 120; i++) {
    client.emit('message', { type: 'agent-message', data: { content: 'm' + i } });
  }
  assert.equal(badge.textContent, '99+', '封顶 99+');
  btn.emit('click');
  assert.equal(badge.style.display, 'none', '展开面板清零');
  assert.equal(panel.classList.contains('open'), true);
});
