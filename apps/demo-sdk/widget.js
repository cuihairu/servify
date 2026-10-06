/**
 * Servify Widget — 可嵌入的客服聊天组件（外观与主题可配）
 *
 * 最小接入:
 *   <script src="/demo-sdk/servify-sdk.umd.js"></script>
 *   <script src="/demo-sdk/widget.js"></script>
 *   <script>
 *     ServifyWidget.create({ baseUrl: 'http://localhost:8080' });
 *   </script>
 *
 * 外观/主题全部走初始化参数（改样式不改代码）:
 *   ServifyWidget.create({
 *     baseUrl: 'http://localhost:8080',
 *     sessionId: 'optional-custom-session-id',  // 应用侧会话互认：同一 id 恢复同一会话
 *     accessToken: 'guest-jwt',                 // optional：guest token（访客反馈入口需要）
 *
 *     // ── 组件外观 ──
 *     icon: 'headset',           // 档1 预设: chat/smile/headset/lifebuoy/dots/bolt
 *     // icon: { image: 'data:image/svg+xml;base64,… 或 <svg…' },  // 档2 自定义上传
 *     // icon: { url: 'https://host.example/icon.svg' },           // 档3 URL 引用
 *     color: '#0f766e',          // 主色调（primaryColor 为兼容别名）
 *     borderRadius: 28,          // 触发按钮圆角（px 数字或任意 CSS 长度值）
 *     position: 'bottom-right',  // bottom-right|bottom-left|top-right|top-left
 *     size: 'medium',            // small|medium|large，或按钮直径 px 数字
 *
 *     // ── 聊天窗主题（亮暗两套独立配；theme: 'auto' 跟随宿主站深浅）──
 *     theme: 'light',            // light|dark|auto
 *     themeLight: { panelBg: '#ffffff', bubbleAgentBg: '#f0f0f0' },   // 亮套覆盖
 *     themeDark:  { panelBg: '#1f2430', bubbleAgentBg: '#2a3040' },   // 暗套覆盖
 *     themeTokens: null,         // 当前主题覆盖（同时覆盖亮暗同名字段）
 *     themeFromColor: '#0f766e', // 给一个品牌主色自动生成整套 token
 *     themeUrl: '',              // 远程主题 JSON（配置放服务端，多站点统一改）
 *
 *     // ── 品牌位 ──
 *     brand: { logo: '', name: '在线客服', welcome: '' },
 *   });
 *
 * 自动初始化（data-* 属性承载常用面）:
 *   <script src="/demo-sdk/servify-sdk.umd.js" data-servify-sdk></script>
 *   <script src="/demo-sdk/widget.js" data-servify-widget
 *           data-base-url="http://localhost:8080"
 *           data-icon="headset" data-color="#0f766e"
 *           data-position="bottom-right" data-size="medium"
 *           data-theme="auto" data-theme-url="https://host/servify-theme.json"
 *           data-brand-name="XX 客服" data-brand-logo="https://host/logo.png"
 *           data-brand-welcome="您好，有什么可以帮您？"></script>
 *
 * token 全表与两风格示例见 docs/embedding-guide.md「组件与聊天窗主题」。
 */
(function (root) {
  'use strict';

  function $(sel, ctx) { return (ctx || document).querySelector(sel); }
  function el(tag, cls, text) {
    var e = document.createElement(tag);
    if (cls) e.className = cls;
    if (text) e.textContent = text;
    return e;
  }

  // ── WebSocket-only lightweight client ──────────────────────────
  // 不依赖 SDK REST API（那些需要认证），直接通过 WebSocket 收发消息
  function WSClient(opts) {
    this.url = opts.wsUrl;
    this.sessionId = opts.sessionId || ('ws_' + Date.now());
    this.ws = null;
    this.listeners = {};
    this.reconnectAttempts = 0;
    this.maxReconnect = 5;
    this.isManualClose = false;
  }

  WSClient.prototype.on = function (event, fn) {
    if (!this.listeners[event]) this.listeners[event] = [];
    this.listeners[event].push(fn);
  };

  WSClient.prototype.emit = function (event) {
    var args = Array.prototype.slice.call(arguments, 1);
    var fns = this.listeners[event] || [];
    for (var i = 0; i < fns.length; i++) {
      try { fns[i].apply(null, args); } catch (e) { console.error('[ServifyWidget]', e); }
    }
  };

  WSClient.prototype.connect = function () {
    var self = this;
    self.isManualClose = false;

    var url = self.url + '?session_id=' + encodeURIComponent(self.sessionId);
    self.emit('status', 'connecting');

    try {
      self.ws = new WebSocket(url);
    } catch (e) {
      self.emit('status', 'error');
      self.emit('error', e);
      return;
    }

    self.ws.onopen = function () {
      self.reconnectAttempts = 0;
      self.emit('status', 'connected');
    };

    self.ws.onmessage = function (evt) {
      try {
        var msg = JSON.parse(evt.data);
        self.emit('message', msg);
      } catch (e) {
        // ignore non-JSON
      }
    };

    self.ws.onclose = function () {
      self.emit('status', 'disconnected');
      if (!self.isManualClose && self.reconnectAttempts < self.maxReconnect) {
        self.reconnectAttempts++;
        var delay = Math.min(1000 * Math.pow(2, self.reconnectAttempts - 1), 10000);
        setTimeout(function () { self.connect(); }, delay);
      }
    };

    self.ws.onerror = function () {
      self.emit('status', 'error');
    };
  };

  WSClient.prototype.send = function (text) {
    if (!this.ws || this.ws.readyState !== WebSocket.OPEN) {
      this.emit('error', new Error('未连接'));
      return;
    }
    var msg = {
      type: 'text-message',
      data: { content: text },
      session_id: this.sessionId,
      timestamp: new Date().toISOString()
    };
    this.ws.send(JSON.stringify(msg));
  };

  WSClient.prototype.disconnect = function () {
    this.isManualClose = true;
    if (this.ws) { this.ws.close(); this.ws = null; }
  };

  WSClient.prototype.isConnected = function () {
    return this.ws && this.ws.readyState === WebSocket.OPEN;
  };

  // ── 外观与主题（icon 三档 / 样式 / 亮暗 token / 品牌位）──────────

  // 档1 预设图标库：统一 24x24 stroke 风格，currentColor 继承按钮文字色
  var ICON_PRESETS = {
    chat: '<svg width="24" height="24" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2" stroke-linecap="round" stroke-linejoin="round"><path d="M21 15a2 2 0 0 1-2 2H7l-4 4V5a2 2 0 0 1 2-2h14a2 2 0 0 1 2 2z"/></svg>',
    smile: '<svg width="24" height="24" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2" stroke-linecap="round" stroke-linejoin="round"><path d="M21 11.5a8.38 8.38 0 0 1-.9 3.8 8.5 8.5 0 0 1-7.6 4.7 8.38 8.38 0 0 1-3.8-.9L3 21l1.9-5.7a8.38 8.38 0 0 1-.9-3.8 8.5 8.5 0 0 1 4.7-7.6 8.38 8.38 0 0 1 3.8-.9h.5a8.48 8.48 0 0 1 8 8v.5z"/><circle cx="9" cy="10" r="1"/><circle cx="15" cy="10" r="1"/><path d="M8.5 14.5s1.5 2 3.5 2 3.5-2 3.5-2"/></svg>',
    headset: '<svg width="24" height="24" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2" stroke-linecap="round" stroke-linejoin="round"><path d="M3 18v-6a9 9 0 0 1 18 0v6"/><path d="M21 19a2 2 0 0 1-2 2h-1a2 2 0 0 1-2-2v-3a2 2 0 0 1 2-2h3zM3 19a2 2 0 0 0 2 2h1a2 2 0 0 0 2-2v-3a2 2 0 0 0-2-2H3z"/></svg>',
    lifebuoy: '<svg width="24" height="24" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2" stroke-linecap="round" stroke-linejoin="round"><circle cx="12" cy="12" r="10"/><circle cx="12" cy="12" r="4"/><line x1="4.93" y1="4.93" x2="9.17" y2="9.17"/><line x1="14.83" y1="14.83" x2="19.07" y2="19.07"/><line x1="14.83" y1="9.17" x2="19.07" y2="4.93"/><line x1="4.93" y1="19.07" x2="9.17" y2="14.83"/></svg>',
    dots: '<svg width="24" height="24" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2" stroke-linecap="round" stroke-linejoin="round"><path d="M21 15a2 2 0 0 1-2 2H7l-4 4V5a2 2 0 0 1 2-2h14a2 2 0 0 1 2 2z"/><line x1="8" y1="10" x2="8.01" y2="10"/><line x1="12" y1="10" x2="12.01" y2="10"/><line x1="16" y1="10" x2="16.01" y2="10"/></svg>',
    bolt: '<svg width="24" height="24" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2" stroke-linecap="round" stroke-linejoin="round"><polygon points="13 2 3 14 12 14 11 22 21 10 12 10 13 2"/></svg>'
  };

  function wrapIconContent(content) {
    if (content.indexOf('<svg') === 0) return content;  // 内联 SVG 直挂
    return '<img src="' + content + '" alt="" style="width:26px;height:26px;object-fit:contain;display:block;" />';
  }

  // icon 三档解析：预设名 / {image}（内联 SVG、data URL、图片 URL 均收，
  // 覆盖「自定义上传」——宿主站把上传产物转 data URL 或直链传入）/ {url}
  function resolveIconContent(icon) {
    if (!icon) return ICON_PRESETS.chat;
    if (typeof icon === 'string') {
      if (ICON_PRESETS[icon]) return ICON_PRESETS[icon];
      if (icon.indexOf('<svg') === 0 || icon.indexOf('data:') === 0 || /^https?:\/\//.test(icon)) {
        return wrapIconContent(icon);
      }
      return ICON_PRESETS.chat;
    }
    if (icon && icon.image) return wrapIconContent(String(icon.image));
    if (icon && icon.url) return wrapIconContent(String(icon.url));
    if (icon && icon.preset && ICON_PRESETS[icon.preset]) return ICON_PRESETS[icon.preset];
    return ICON_PRESETS.chat;
  }

  var POSITIONS = ['bottom-right', 'bottom-left', 'top-right', 'top-left'];

  var SIZE_PRESETS = {
    small:  { trigger: 44, panelWidth: 340, panelHeight: 480 },
    medium: { trigger: 56, panelWidth: 380, panelHeight: 520 },
    large:  { trigger: 64, panelWidth: 440, panelHeight: 600 }
  };

  // token 名 → CSS 变量名（--sw- 前缀；值全部经 setProperty 挂在组件根上）
  var TOKEN_VARS = {
    primary: 'primary', primaryContrast: 'primary-contrast',
    panelBg: 'panel-bg', panelText: 'panel-text', msgsBg: 'msgs-bg',
    bubbleSelfBg: 'bubble-self-bg', bubbleSelfText: 'bubble-self-text',
    bubbleAgentBg: 'bubble-agent-bg', bubbleAgentText: 'bubble-agent-text',
    systemText: 'system-text', metaText: 'meta-text',
    inputBg: 'input-bg', inputBorder: 'input-border', inputText: 'input-text',
    chipBg: 'chip-bg', chipBorder: 'chip-border', headerBg: 'header-bg',
    font: 'font', shadowPanel: 'shadow-panel', shadowTrigger: 'shadow-trigger',
    radiusPanel: 'radius-panel', radiusBubble: 'radius-bubble', radiusInput: 'radius-input'
  };

  var WIDGET_FONT = '-apple-system,BlinkMacSystemFont,"Segoe UI",Roboto,sans-serif';

  function defaultLightTheme(color) {
    return {
      primary: color, primaryContrast: readableOn(color),
      panelBg: '#ffffff', panelText: '#333333', msgsBg: '#fafafa',
      bubbleSelfBg: color, bubbleSelfText: readableOn(color),
      bubbleAgentBg: '#f0f0f0', bubbleAgentText: '#333333',
      systemText: '#888888', metaText: '#999999',
      inputBg: '#ffffff', inputBorder: '#dddddd', inputText: '#333333',
      chipBg: '#ffffff', chipBorder: '#e0e0e0',
      headerBg: 'linear-gradient(135deg, ' + color + ', ' + shade(color, -24) + ')',
      font: WIDGET_FONT,
      shadowPanel: '0 8px 40px rgba(0,0,0,.15)',
      shadowTrigger: '0 4px 16px rgba(0,0,0,.25)',
      radiusPanel: '16px', radiusBubble: '12px', radiusInput: '20px'
    };
  }

  function defaultDarkTheme(color) {
    var primary = shade(color, 20);  // 深色底把主色提亮一档，避免糊底
    return {
      primary: primary, primaryContrast: readableOn(primary),
      panelBg: '#1f2430', panelText: '#e5e7eb', msgsBg: '#171a23',
      bubbleSelfBg: primary, bubbleSelfText: readableOn(primary),
      bubbleAgentBg: '#2a3040', bubbleAgentText: '#e5e7eb',
      systemText: '#9ca3af', metaText: '#8b93a3',
      inputBg: '#262c3a', inputBorder: '#3a4152', inputText: '#e5e7eb',
      chipBg: '#262c3a', chipBorder: '#3a4152',
      headerBg: 'linear-gradient(135deg, ' + primary + ', ' + shade(color, -24) + ')',
      font: WIDGET_FONT,
      shadowPanel: '0 8px 40px rgba(0,0,0,.5)',
      shadowTrigger: '0 4px 16px rgba(0,0,0,.45)',
      radiusPanel: '16px', radiusBubble: '12px', radiusInput: '20px'
    };
  }

  function normalizeThemeMode(mode) {
    return mode === 'dark' || mode === 'auto' ? mode : 'light';
  }

  // ── Widget UI ──────────────────────────────────────────────────

  function createWidget(opts) {
    opts = opts || {};
    var baseUrl = opts.baseUrl || (location.protocol + '//' + location.host);
    // 会话 id 一处生成、多通道共用：会话 WS 与语音 WS（PROTOCOL §9 握手
    // session_id）必须指向同一会话；补拉/推荐问题的 session 归因同理。
    var sessionId = opts.sessionId || ('ws_' + Date.now());
    // 主色调：color 正名，primaryColor 兼容别名；themeFromColor 显式指定时
    // 优先参与 token 自动生成（给一个品牌主色自动生成整套）。
    var primaryColor = opts.themeFromColor || opts.color || opts.primaryColor || '#667eea';
    var wsUrl = baseUrl.replace(/^http/, 'ws') + '/api/v1/ws';
    var voiceWsUrl = baseUrl.replace(/^http/, 'ws') + '/api/v1/ws/voice';
    // 语音能力探测：旧缓存包没有 VoiceChannel/MicCapture 时按钮不出现，
    // 聊天主链路零影响。
    var voiceSdk = root.Servify && root.Servify.VoiceChannel && root.Servify.MicCapture ? root.Servify : null;

    // ── 外观参数解析（四角位置/三档大小/触发按钮圆角/icon/品牌位）──
    var position = POSITIONS.indexOf(opts.position) >= 0 ? opts.position : 'bottom-right';
    var sizePreset = SIZE_PRESETS[opts.size] || null;
    var triggerSize = typeof opts.size === 'number' ? opts.size : (sizePreset ? sizePreset.trigger : 56);
    var panelWidth = sizePreset ? sizePreset.panelWidth : 380;
    var panelHeight = sizePreset ? sizePreset.panelHeight : 520;
    var radiusTrigger = typeof opts.borderRadius === 'number' ? opts.borderRadius + 'px'
      : (typeof opts.borderRadius === 'string' && opts.borderRadius ? opts.borderRadius : '50%');
    var launcherIconHtml = resolveIconContent(opts.icon);
    var brand = opts.brand || {};

    // Create DOM
    var wrap = el('div', 'servify-widget sw-pos-' + position);
    wrap.setAttribute('data-servify', '');
    wrap.style.setProperty('--sw-trigger-size', triggerSize + 'px');
    wrap.style.setProperty('--sw-panel-width', panelWidth + 'px');
    wrap.style.setProperty('--sw-panel-height', panelHeight + 'px');
    wrap.style.setProperty('--sw-radius-trigger', radiusTrigger);

    // Toggle button（icon 内容由三档解析产出；颜色走 token 不再内联）
    var btn = el('button', 'sw-trigger');
    btn.innerHTML = launcherIconHtml;
    btn.setAttribute('title', brand.name || '在线客服');

    // Close icon
    var closeSvg = '<svg width="18" height="18" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2"><line x1="18" y1="6" x2="6" y2="18"/><line x1="6" y1="6" x2="18" y2="18"/></svg>';

    // Mic icons
    var micOffSvg = '<svg width="18" height="18" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2"><path d="M12 2a3 3 0 0 0-3 3v7a3 3 0 0 0 6 0V5a3 3 0 0 0-3-3Z"/><path d="M19 10v2a7 7 0 0 1-14 0v-2"/><line x1="12" y1="19" x2="12" y2="22"/><line x1="8" y1="15" x2="16" y2="15"/><line x1="4.93" y1="4.93" x2="19.07" y2="19.07"/></svg>';
    var micOnSvg = '<svg width="18" height="18" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2"><path d="M12 2a3 3 0 0 0-3 3v7a3 3 0 0 0 6 0V5a3 3 0 0 0-3-3Z"/><path d="M19 10v2a7 7 0 0 1-14 0v-2"/><line x1="12" y1="19" x2="12" y2="22"/></svg>';

    // Panel（窗头背景/文字色走 token；品牌 logo 挂在标题前；初始收起态）
    var panel = el('div', 'sw-panel');
    panel.setAttribute('aria-hidden', 'true');
    btn.setAttribute('aria-expanded', 'false');
    var header = el('div', 'sw-header');
    var headerTitle = el('div', 'sw-header-title', brand.name || '在线客服');
    var headerStatus = el('div', 'sw-header-status', '未连接');
    headerStatus.style.opacity = '0.75';
    headerStatus.style.fontSize = '12px';

    // Mic button（仅语音能力可用时挂载）
    var micBtn = el('button', 'sw-mic-btn');
    micBtn.innerHTML = micOffSvg;
    micBtn.setAttribute('title', '语音翻译');
    micBtn.style.background = 'rgba(255,255,255,0.2)';
    micBtn.style.border = 'none';
    micBtn.style.borderRadius = '50%';
    micBtn.style.width = '32px';
    micBtn.style.height = '32px';
    micBtn.style.display = 'flex';
    micBtn.style.alignItems = 'center';
    micBtn.style.justifyContent = 'center';
    micBtn.style.cursor = 'pointer';
    micBtn.style.opacity = '0.8';
    micBtn.style.transition = 'opacity .2s, background .2s';
    micBtn.style.marginLeft = '8px';

    var closeBtn = el('button', 'sw-close');
    closeBtn.innerHTML = closeSvg;

    if (brand.logo) {
      var logoImg = document.createElement('img');
      logoImg.className = 'sw-header-logo';
      logoImg.src = brand.logo;
      logoImg.alt = '';
      header.appendChild(logoImg);
    }
    header.appendChild(headerTitle);
    header.appendChild(headerStatus);
    if (voiceSdk) header.appendChild(micBtn);
    header.appendChild(closeBtn);

    var msgs = el('div', 'sw-messages');
    var inputArea = el('div', 'sw-input-area');
    var input = el('input', 'sw-input');
    input.type = 'text';
    input.placeholder = '输入消息...';
    var sendBtn = el('button', 'sw-send-btn', '发送');

    inputArea.appendChild(input);
    inputArea.appendChild(sendBtn);
    var suggests = el('div', 'sw-suggests');
    var suggestHint = el('div', 'sw-suggest-hint', '猜你想问');
    var suggestChips = el('div', 'sw-suggest-chips');
    suggests.appendChild(suggestHint);
    suggests.appendChild(suggestChips);
    panel.appendChild(header);
    panel.appendChild(msgs);
    panel.appendChild(suggests);
    panel.appendChild(inputArea);
    var badge = el('div', 'sw-badge', '0');
    badge.style.display = 'none';
    wrap.appendChild(btn);
    wrap.appendChild(badge);
    wrap.appendChild(panel);

    // Inject styles（样式表只含变量引用，颜色全部运行时挂 token）
    if (!document.getElementById('servify-widget-styles')) {
      var style = document.createElement('style');
      style.id = 'servify-widget-styles';
      style.textContent = getStyles();
      document.head.appendChild(style);
    }

    // ── 主题应用：亮暗两套独立 token，auto 跟随宿主站深浅 ──
    var themeMode = normalizeThemeMode(opts.theme);
    var themeOverrides = { light: opts.themeLight || {}, dark: opts.themeDark || {} };
    var mediaDark = root.matchMedia ? root.matchMedia('(prefers-color-scheme: dark)') : null;

    function mergeTokens(mode) {
      var tokens = mode === 'dark' ? defaultDarkTheme(primaryColor) : defaultLightTheme(primaryColor);
      var overrides = themeOverrides[mode] || {};
      var extra = opts.themeTokens || {};
      var k;
      for (k in overrides) if (overrides.hasOwnProperty(k) && TOKEN_VARS[k]) tokens[k] = overrides[k];
      for (k in extra) if (extra.hasOwnProperty(k) && TOKEN_VARS[k]) tokens[k] = extra[k];
      return tokens;
    }

    function resolvedThemeMode() {
      if (themeMode === 'auto') return mediaDark && mediaDark.matches ? 'dark' : 'light';
      return themeMode;
    }

    function applyTheme() {
      var mode = resolvedThemeMode();
      var tokens = mergeTokens(mode);
      wrap.setAttribute('data-servify-theme', mode);
      for (var k in TOKEN_VARS) {
        if (tokens[k] !== undefined) wrap.style.setProperty('--sw-' + TOKEN_VARS[k], tokens[k]);
      }
    }

    if (mediaDark && mediaDark.addEventListener && themeMode === 'auto') {
      mediaDark.addEventListener('change', applyTheme);
    }

    // ── 品牌位（logo/名称/欢迎语；远程主题可二次覆盖）──
    var welcomeText = brand.welcome || '您好！欢迎来到 Servify Demo。请问有什么可以帮您的？';
    var welcomeBubble = null;

    function applyBrand() {
      headerTitle.textContent = brand.name || '在线客服';
      btn.setAttribute('title', brand.name || '在线客服');
      var existingLogo = header.querySelector('.sw-header-logo');
      if (brand.logo) {
        if (existingLogo) {
          existingLogo.src = brand.logo;
        } else {
          var img = document.createElement('img');
          img.className = 'sw-header-logo';
          img.src = brand.logo;
          img.alt = '';
          header.insertBefore(img, headerTitle);
        }
      } else if (existingLogo) {
        existingLogo.parentNode.removeChild(existingLogo);
      }
      if (welcomeBubble && welcomeBubble.parentNode === msgs && msgs.lastChild === welcomeBubble) {
        // 欢迎语仍是最后一条消息时才允许远程主题改写，不打断进行中的会话
        welcomeBubble.textContent = brand.welcome || welcomeText;
      }
    }

    // ── 远程主题（配置放服务端，多站点统一改）：JSON 形如
    // {"light":{...},"dark":{...},"brand":{...}}，字段名同 token 全表；
    // 拉取失败不阻塞，保持本地配置。 ──
    if (opts.themeUrl && root.fetch) {
      root.fetch(opts.themeUrl, { cache: 'no-cache' })
        .then(function (r) { return r.ok ? r.json() : null; })
        .then(function (t) {
          if (!t) return;
          if (t.light) themeOverrides.light = t.light;
          if (t.dark) themeOverrides.dark = t.dark;
          if (t.brand) {
            brand = t.brand;
            if (t.brand.welcome) welcomeText = t.brand.welcome;
          }
          applyTheme();
          applyBrand();
        })
        .catch(function () {
          if (root.console && root.console.warn) root.console.warn('[ServifyWidget] themeUrl fetch failed, keep local theme');
        });
    }

    applyTheme();

    document.body.appendChild(wrap);

    // ── 未读数（§4.3 口径；面板不可见时到达的坐席/AI 内容）──

    var unread = 0;
    function setUnread(n) {
      unread = n;
      if (n > 0) {
        badge.textContent = n > 99 ? '99+' : String(n);
        badge.style.display = 'flex';
      } else {
        badge.style.display = 'none';
      }
    }
    function bumpUnreadIfHidden() {
      if (!panelOpen) setUnread(unread + 1);
    }

    // ── 语音翻译（PROTOCOL §9）───────────────────────────────────
    // 上行：MicCapture 采集 → pcm16 24kHz 小端分片 → VoiceChannel 二进制帧。
    // 下行：voice:delta = 每说话方一条"正在说"直播行（新轮次顶旧轮次）；
    // voice:final = 终句字幕（译文 + 原文小字 + 未翻译标注）；voice:audio =
    // 译文播报（自动播放被浏览器策略拦截时退化为 ▶ 按钮）；voice-error =
    // 连接级故障（服务端必随后 close）。无自动重连（活体采集会话，重启说话
    // 是用户动作）。
    var voiceChannel = null;
    var micCapture = null;
    var voiceActive = false;
    var voiceStartPending = false;
    var voiceLiveBubbles = {}; // speaker → 正在说 直播行
    var voiceCaptions = {};    // speaker:seq → 终句字幕气泡（voice:audio 回填播放）

    function speakerLabel(speaker) {
      return speaker === 'agent' ? '客服' : '访客';
    }

    function removeVoiceBubble(b) {
      if (b && b.row && b.row.parentNode) b.row.parentNode.removeChild(b.row);
    }

    function clearLiveBubbles() {
      var live = voiceLiveBubbles;
      voiceLiveBubbles = {};
      Object.keys(live).forEach(function (k) { removeVoiceBubble(live[k]); });
    }

    function renderVoiceLive(speaker, turnSeq, text) {
      var key = speaker + ':' + turnSeq;
      var current = voiceLiveBubbles[speaker];
      if (current && current.key !== key) {
        removeVoiceBubble(current);
        delete voiceLiveBubbles[speaker];
        current = null;
      }
      if (!current) {
        var row = el('div', 'sw-msg sw-msg-voice sw-msg-voice-' + speaker);
        row.appendChild(el('div', 'sw-voice-chip', speakerLabel(speaker) + ' 正在说'));
        var body = el('div', 'sw-bubble', '');
        row.appendChild(body);
        msgs.appendChild(row);
        current = { key: key, row: row, body: body };
        voiceLiveBubbles[speaker] = current;
      }
      current.body.textContent = text + ' ▌';
      msgs.scrollTop = msgs.scrollHeight;
    }

    function renderVoiceFinal(u) {
      removeVoiceBubble(voiceLiveBubbles[u.speaker]);
      delete voiceLiveBubbles[u.speaker];
      var row = el('div', 'sw-msg sw-msg-voice sw-msg-voice-' + u.speaker);
      row.appendChild(el('div', 'sw-voice-chip', speakerLabel(u.speaker) + ' · 第' + u.seq + '句' + (u.degraded ? ' · 未翻译' : '')));
      var body = el('div', 'sw-bubble', '');
      body.appendChild(el('div', 'sw-voice-content', u.content));
      if (u.original && u.original !== u.content) {
        body.appendChild(el('div', 'sw-voice-original', u.original));
      }
      row.appendChild(body);
      msgs.appendChild(row);
      voiceCaptions[u.speaker + ':' + u.seq] = { body: body, audio: null };
      msgs.scrollTop = msgs.scrollHeight;
    }

    function attachVoiceAudio(u) {
      var cap = voiceCaptions[u.speaker + ':' + u.seq];
      if (!cap || cap.audio) return; // final 先于 audio 到达（管线串行产出 + WS 保序）
      var mime = u.format === 'wav' ? 'wav' : 'mpeg';
      var audio = new Audio('data:audio/' + mime + ';base64,' + u.audio);
      cap.audio = audio;
      audio.play().then(function () {
        cap.body.classList.add('speaking');
        audio.addEventListener('ended', function () { cap.body.classList.remove('speaking'); });
      }).catch(function () {
        // 自动播放策略拦截：退化为显式 ▶ 按钮（点击手势可解锁播放）
        var playBtn = el('button', 'sw-voice-play', '▶ 播放');
        playBtn.type = 'button';
        playBtn.addEventListener('click', function () { audio.play().catch(function () {}); });
        cap.body.appendChild(playBtn);
      });
    }

    function startMicCapture() {
      var capture = new voiceSdk.MicCapture();
      micCapture = capture;
      capture.start(function (chunk) {
        try {
          if (voiceChannel) voiceChannel.sendAudio(chunk);
        } catch (e) {
          // 上行断裂（transport_disconnected 等）：收线并提示，不静默丢帧
          addMsg('system', '语音上行中断：' + (e && e.message ? e.message : '发送失败'));
          stopVoice();
        }
      }).then(function () {
        if (micCapture !== capture) {
          // start 悬在授权弹窗期间已被收线：实例自行停掉，不留活口
          capture.stop();
          return;
        }
        voiceActive = true;
        updateMicButton();
      }).catch(function (err) {
        addMsg('system', '麦克风不可用：' + (err && err.message ? err.message : '请检查授权'));
        stopVoice();
      });
    }

    function cleanupMicCapture() {
      if (micCapture) {
        micCapture.stop().catch(function () {});
        micCapture = null;
      }
    }

    function startVoice() {
      if (!voiceSdk || voiceChannel || voiceStartPending) return;
      voiceStartPending = true;
      updateMicButton();
      var channel = new voiceSdk.VoiceChannel({
        url: voiceWsUrl,
        sessionId: sessionId,
        speaker: 'visitor',
        // access_token 仅在 guest_token.required 部署需要；签发端点
        // /api/v1/guest/session 是 service API key 面（宿主后端调用），
        // 浏览器侧拿不到——由嵌入方经 opts.accessToken 注入。
        accessToken: opts.accessToken || undefined
      });
      voiceChannel = channel;

      channel.on('connected', function () {
        voiceStartPending = false;
        startMicCapture();
      });

      channel.on('disconnected', function () {
        var wasLive = voiceActive || voiceStartPending;
        voiceStartPending = false;
        voiceActive = false;
        clearLiveBubbles();
        cleanupMicCapture();
        voiceChannel = null;
        updateMicButton();
        if (wasLive) addMsg('system', '语音连接已断开');
      });

      channel.on('voice:delta', function (u) { renderVoiceLive(u.speaker, u.turn_seq, u.text); });
      channel.on('voice:final', renderVoiceFinal);
      channel.on('voice:audio', attachVoiceAudio);
      channel.on('voice:error', function (u) {
        addMsg('system', '语音不可用：' + u.message);
        stopVoice();
      });

      channel.connect().catch(function () {
        if (voiceChannel === channel) {
          stopVoice();
          addMsg('system', '语音连接失败（服务端未装配语音翻译时路由不存在）');
        }
      });
    }

    function stopVoice() {
      voiceStartPending = false;
      voiceActive = false;
      clearLiveBubbles();
      cleanupMicCapture();
      if (voiceChannel) {
        voiceChannel.disconnect();
        voiceChannel = null;
      }
      updateMicButton();
    }

    function toggleVoice() {
      if (voiceActive || voiceStartPending) stopVoice();
      else startVoice();
    }

    function updateMicButton() {
      if (voiceActive || voiceStartPending) {
        micBtn.innerHTML = micOnSvg;
        micBtn.style.background = 'rgba(229, 62, 62, 0.8)'; // red when active
        micBtn.style.opacity = '1';
        micBtn.setAttribute('title', voiceStartPending ? '语音连接中...' : '停止语音');
      } else {
        micBtn.innerHTML = micOffSvg;
        micBtn.style.background = 'rgba(255,255,255,0.2)';
        micBtn.style.opacity = '0.8';
        micBtn.setAttribute('title', '语音翻译');
      }
    }

    micBtn.addEventListener('click', function(e) {
      e.stopPropagation();
      toggleVoice();
    });

    // ── Client ────────────────────────────────────────────────

    var client = new WSClient({ wsUrl: wsUrl, sessionId: sessionId });
    var panelOpen = false;
    var connected = false;
    var initialSuggestsLoaded = false;

    function togglePanel() {
      panelOpen = !panelOpen;
      // sw-open 挂在根上：展开动画（面板 scale/translate 入场、触发按钮图标
      // 旋入）与收起还原都由 CSS 挂钩驱动，JS 只负责状态类
      wrap.classList.toggle('sw-open', panelOpen);
      btn.setAttribute('aria-expanded', panelOpen ? 'true' : 'false');
      panel.setAttribute('aria-hidden', panelOpen ? 'false' : 'true');
      if (panelOpen) {
        panel.classList.add('open');
        btn.innerHTML = closeSvg;
        setUnread(0);
        btn.style.borderRadius = '50%';
        input.focus();
        if (!connected) client.connect();
        if (!initialSuggestsLoaded) {
          initialSuggestsLoaded = true;
          loadInitialSuggests();
        }
      } else {
        panel.classList.remove('open');
        btn.innerHTML = launcherIconHtml;
        // Stop voice when panel closes
        if (voiceActive || voiceStartPending) stopVoice();
      }
    }

    btn.addEventListener('click', togglePanel);
    closeBtn.addEventListener('click', function () {
      if (panelOpen) togglePanel();
    });

    function addMsg(role, content) {
      var m = el('div', 'sw-msg sw-msg-' + role);
      var b = el('div', 'sw-bubble', content);
      m.appendChild(b);
      msgs.appendChild(m);
      msgs.scrollTop = msgs.scrollHeight;
      return b;
    }

    // ── Citation 引用行 + 反馈条（V1.0 收敛 B3-2，§8.4/§5.3）──────
    // sources 渲染形态：📄 标题 · relevance 0.91（计划书 §8.4 口径）；
    // 反馈条只在响应带 answer_id 且嵌入方注入 accessToken 时出现（反馈
    // 端点走认证面，匿名 demo 模式不可用）。

    function appendSources(bubble, sources) {
      if (!bubble || !sources || !sources.length || !root.fetch) return;
      var box = el('div', 'sw-sources');
      for (var i = 0; i < sources.length; i++) {
        var s = sources[i] || {};
        var row = el('div', 'sw-source-row',
          '📄 ' + (s.title || s.document_id || '未命名文档') +
          (typeof s.score === 'number' ? ' · relevance ' + s.score.toFixed(2) : ''));
        box.appendChild(row);
      }
      bubble.appendChild(box);
      msgs.scrollTop = msgs.scrollHeight;
    }

    function appendFeedbackBar(bubble, answerID) {
      if (!bubble || !answerID || !root.fetch) return;
      var bar = el('div', 'sw-feedback');
      bar.appendChild(el('span', 'sw-feedback-label', '这条回答有帮助吗？'));
      var yes = el('button', 'sw-feedback-btn', '👍');
      var no = el('button', 'sw-feedback-btn', '👎');
      var settled = false;
      function submit(helpful) {
        if (settled) return;
        settled = true;
        yes.disabled = true;
        no.disabled = true;
        root.fetch(baseUrl + '/api/v1/ai/feedback', {
          method: 'POST',
          headers: {
            'Content-Type': 'application/json',
            'Authorization': 'Bearer ' + opts.accessToken
          },
          body: JSON.stringify({ answer_id: answerID, helpful: helpful, comment: '' })
        }).then(function (r) {
          bar.textContent = r.ok ? '感谢反馈！' : '反馈提交失败';
        }).catch(function () {
          bar.textContent = '反馈提交失败';
        });
      }
      yes.onclick = function () { submit(true); };
      no.onclick = function () { submit(false); };
      bar.appendChild(yes);
      bar.appendChild(no);
      bubble.appendChild(bar);
      msgs.scrollTop = msgs.scrollHeight;
    }

    // ── 流式气泡（PROTOCOL §4.1 三段契约）──────────────────────
    var streaming = null;

    function closeStreamingOnDisconnect() {
      if (!streaming) return;
      if (streaming.content) {
        streaming.bubble.textContent = streaming.content;
        rememberContent(streaming.content);
        addMsg('system', '回答中断，请重试');
      } else {
        streaming.bubble.parentNode.parentNode.removeChild(streaming.bubble.parentNode);
      }
      streaming = null;
    }

    // ── 断线补拉（PROTOCOL §6 #2；对齐 core D7 增量补拉口径）──
    var reconcileCursor = null;
    var seenContents = {};

    function rememberContent(text) {
      if (text) seenContents[text] = true;
    }

    function reconcileMessages() {
      if (!sessionId || !root.fetch) return;
      var url = baseUrl + '/api/v1/sessions/' + encodeURIComponent(sessionId) + '/messages' +
        (reconcileCursor ? '?after_id=' + encodeURIComponent(reconcileCursor) : '');
      root.fetch(url)
        .then(function (r) { return r.ok ? r.json() : null; })
        .then(function (page) {
          if (!page || !page.messages || !page.messages.length) return;
          var last = null;
          page.messages.forEach(function (m) {
            if (m && m.id) last = m.id;
            var content = m && typeof m.content === 'string' ? m.content : '';
            if (!content || seenContents[content]) return;
            seenContents[content] = true;
            var role = m.sender === 'system' ? 'system' : (m.sender === 'customer' ? 'user' : 'bot');
            addMsg(role, content);
            if (role === 'bot') bumpUnreadIfHidden();
          });
          if (page.has_more && last) {
            reconcileCursor = last;
            reconcileMessages();
          } else if (last) {
            reconcileCursor = last;
          }
        })
        .catch(function () { /* 静默：网络/HTTP 失败绝不破坏 WS 主链路 */ });
    }

    function sendMsg() {
      var text = input.value.trim();
      if (!text) return;
      addMsg('user', text);
      rememberContent(text);
      input.value = '';
      client.send(text);
      refreshSuggests(text);
    }

    // ── 客户侧推荐问题（P2-0）────────────────────────────────
    function fetchSuggestQuestions(path) {
      if (!root.fetch) return Promise.resolve([]);
      var sep = path.indexOf('?') >= 0 ? '&' : '?';
      var full = sessionId ? path + sep + 'session_id=' + encodeURIComponent(sessionId) : path;
      return root.fetch(baseUrl + full)
        .then(function (r) { return r.ok ? r.json() : null; })
        .then(function (body) {
          var data = body && body.success && body.data;
          return (data && data.questions) || [];
        })
        .catch(function () { return []; });
    }

    function renderSuggests(questions, hintText) {
      suggestChips.innerHTML = '';
      if (!questions.length) {
        suggests.style.display = 'none';
        return;
      }
      suggests.style.display = 'block';
      suggestHint.textContent = hintText;
      questions.forEach(function (q) {
        var chip = el('button', 'sw-suggest-chip', q.question);
        chip.type = 'button';
        chip.addEventListener('click', function () {
          input.value = q.question;
          sendMsg();
        });
        suggestChips.appendChild(chip);
      });
    }

    function loadInitialSuggests() {
      fetchSuggestQuestions('/public/suggestions/initial?limit=6')
        .then(function (qs) { renderSuggests(qs, '猜你想问'); });
    }

    function refreshSuggests(query) {
      fetchSuggestQuestions('/public/suggestions/next?query=' + encodeURIComponent(query) + '&limit=6')
        .then(function (qs) { renderSuggests(qs, '接下来可能想问'); });
    }

    sendBtn.addEventListener('click', sendMsg);
    input.addEventListener('keydown', function (e) {
      if (e.key === 'Enter') { e.preventDefault(); sendMsg(); }
    });

    // Welcome message（品牌位：brand.welcome 可覆盖）
    welcomeBubble = addMsg('bot', welcomeText);

    // Client events
    client.on('status', function (s) {
      connected = s === 'connected';
      if (s === 'disconnected' || s === 'error') {
        closeStreamingOnDisconnect();
        if (voiceActive) stopVoice(); // 会话通道离线，语音会话一并收线
      }
      var label = { connecting: '连接中...', connected: '已连接', disconnected: '连接断开', error: '连接失败' };
      headerStatus.textContent = label[s] || s;
      if (s === 'connected') {
        reconcileMessages();
        input.placeholder = '输入消息...';
      } else {
        input.placeholder = s === 'connecting' ? '正在连接...' : '未连接';
      }
    });

    client.on('message', function (msg) {
      if (!msg) return;

      // AI response（终帧）：流式气泡整体覆写幂等收口；无流时照常渲染。
      // 终帧带 sources/answer_id 时渲染引用行 + "是否有帮助"反馈条（V1.0
      // 收敛 B3-2，计划书 §5.3/§8.4：citation 可视化 + 反馈闭环访客入口）。
      if (msg.type === 'ai-response' && msg.data) {
        var data = msg.data;
        var finalText = '';
        if (typeof data === 'object' && data.content) {
          finalText = data.content;
        } else if (typeof data === 'string') {
          finalText = data;
        }
        if (finalText) {
          rememberContent(finalText);
          var bubble;
          if (streaming) {
            streaming.bubble.textContent = finalText;
            bubble = streaming.bubble;
            streaming = null;
          } else {
            bubble = addMsg('bot', finalText);
          }
          if (typeof data === 'object') {
            appendSources(bubble, data.sources);
            if (data.answer_id && opts.accessToken) {
              appendFeedbackBar(bubble, data.answer_id);
            }
          }
          bumpUnreadIfHidden();
        }
        return;
      }

      // Echo of own text message (broadcast from hub)
      if (msg.type === 'text-message' && msg.data) {
        return;
      }

      // 转人工通知（PROTOCOL §4.2）：坐席接入（含等待队列派发）——居中提示条
      if (msg.type === 'transfer_notification' && msg.data && typeof msg.data === 'object' && msg.data.message) {
        rememberContent(msg.data.message);
        addMsg('system', msg.data.message);
        return;
      }

      // 排队通知（PROTOCOL §4.2）：已入等待队列——同上
      if (msg.type === 'waiting_notification' && msg.data && typeof msg.data === 'object' && msg.data.message) {
        rememberContent(msg.data.message);
        addMsg('system', msg.data.message);
        return;
      }

      // 流式增量（PROTOCOL §4.1）：即到即拼的打字机气泡（▌ 光标）
      if (msg.type === 'ai-response-delta' && msg.data && typeof msg.data === 'object') {
        var d = msg.data;
        if (typeof d.content_delta === 'string' && typeof d.done === 'boolean') {
          if (!streaming) {
            streaming = { bubble: addMsg('bot', ''), content: '', done: false };
          }
          if (!streaming.done) {
            if (d.content_delta) streaming.content += d.content_delta;
            if (d.done) streaming.done = true;
            streaming.bubble.textContent = streaming.content + (streaming.done ? '' : '▌');
            msgs.scrollTop = msgs.scrollHeight;
          }
        }
        return;
      }

      // 坐席发言（PROTOCOL §4.1）：真实聊天气泡
      if (msg.type === 'agent-message' && msg.data && typeof msg.data === 'object' && msg.data.content) {
        rememberContent(msg.data.content);
        addMsg('bot', msg.data.content);
        bumpUnreadIfHidden();
        return;
      }

      // WebRTC 信令（PROTOCOL §4.3）：widget 非远程协助 UI，契约内帧显式静默
      if (msg.type === 'webrtc-offer' || msg.type === 'webrtc-answer' || msg.type === 'webrtc-candidate' ||
          msg.type === 'webrtc-state-change' || msg.type === 'webrtc-ice-config' || msg.type === 'data-channel-message') {
        return;
      }

      // 未知帧：契约口径是忽略而非报错（PROTOCOL §8）
      console.warn('[ServifyWidget] unknown frame type:', msg.type);
    });

    client.on('error', function (e) {
      console.warn('[ServifyWidget] Error:', e);
    });

    return { mount: wrap, client: client };
  }

  // ── Inline CSS（全部引用 --sw-* token；颜色/圆角/阴影运行时挂根元素）──

  function getStyles() {
    return '\
.servify-widget { position:fixed; right:24px; bottom:24px; z-index:99999; font-family:var(--sw-font); font-size:14px; }\
.servify-widget * { box-sizing:border-box; }\
.servify-widget.sw-pos-bottom-right { right:24px; bottom:24px; }\
.servify-widget.sw-pos-bottom-left { left:24px; bottom:24px; }\
.servify-widget.sw-pos-top-right { right:24px; top:24px; }\
.servify-widget.sw-pos-top-left { left:24px; top:24px; }\
.servify-widget .sw-badge { position:absolute; right:-2px; bottom:calc(var(--sw-trigger-size) - 12px); min-width:20px; height:20px; padding:0 5px; border-radius:10px; background:#e53e3e; color:#fff; font-size:12px; font-weight:600; display:flex; align-items:center; justify-content:center; box-shadow:0 2px 8px rgba(0,0,0,.3); pointer-events:none; animation:sw-badge-pop .25s ease-out; }\
.servify-widget.sw-pos-bottom-left .sw-badge, .servify-widget.sw-pos-top-left .sw-badge { right:auto; left:-2px; }\
.servify-widget.sw-pos-top-right .sw-badge, .servify-widget.sw-pos-top-left .sw-badge { top:calc(var(--sw-trigger-size) - 12px); bottom:auto; }\
.servify-widget .sw-trigger { width:var(--sw-trigger-size); height:var(--sw-trigger-size); border-radius:var(--sw-radius-trigger); border:none; background:var(--sw-primary); color:var(--sw-primary-contrast); cursor:pointer; box-shadow:var(--sw-shadow-trigger); display:flex; align-items:center; justify-content:center; transition:transform .2s,box-shadow .2s; }\
.servify-widget .sw-trigger:hover { transform:scale(1.08); filter:brightness(1.06); }\
.servify-widget.sw-open .sw-trigger > * { animation:sw-trigger-icon-in .2s ease-out; }\
.servify-widget .sw-panel { position:absolute; right:0; bottom:calc(var(--sw-trigger-size) + 12px); width:var(--sw-panel-width); height:var(--sw-panel-height); background:var(--sw-panel-bg); color:var(--sw-panel-text); border-radius:var(--sw-radius-panel); box-shadow:var(--sw-shadow-panel); display:flex; flex-direction:column; overflow:hidden; opacity:0; visibility:hidden; pointer-events:none; transform:translateY(12px) scale(.96); transform-origin:bottom right; transition:opacity .18s ease-out,transform .18s ease-out,visibility 0s linear .18s; }\
.servify-widget.sw-pos-bottom-left .sw-panel { right:auto; left:0; transform-origin:bottom left; }\
.servify-widget.sw-pos-top-right .sw-panel { bottom:auto; top:calc(var(--sw-trigger-size) + 12px); transform-origin:top right; transform:translateY(-12px) scale(.96); }\
.servify-widget.sw-pos-top-left .sw-panel { right:auto; left:0; bottom:auto; top:calc(var(--sw-trigger-size) + 12px); transform-origin:top left; transform:translateY(-12px) scale(.96); }\
.servify-widget .sw-panel.open { opacity:1; visibility:visible; pointer-events:auto; transform:none; transition-delay:0s,0s,0s; }\
.servify-widget .sw-header { padding:16px; background:var(--sw-header-bg); color:var(--sw-primary-contrast); display:flex; align-items:center; justify-content:space-between; }\
.servify-widget .sw-header-logo { width:28px; height:28px; border-radius:6px; object-fit:contain; margin-right:10px; flex-shrink:0; background:rgba(255,255,255,.85); }\
.servify-widget .sw-header-title { font-size:16px; font-weight:600; flex:1; }\
.servify-widget .sw-header-status { margin:0 12px; }\
.servify-widget .sw-mic-btn:hover { opacity:1 !important; }\
.servify-widget .sw-close { background:none; border:none; color:var(--sw-primary-contrast); cursor:pointer; opacity:.8; padding:4px; display:flex; align-items:center; }\
.servify-widget .sw-close:hover { opacity:1; }\
.servify-widget .sw-messages { flex:1; overflow-y:auto; padding:16px; display:flex; flex-direction:column; gap:12px; background:var(--sw-msgs-bg); }\
.servify-widget .sw-msg { max-width:80%; }\
.servify-widget .sw-bubble { padding:10px 14px; border-radius:var(--sw-radius-bubble); line-height:1.5; word-break:break-word; }\
.servify-widget .sw-msg-user { align-self:flex-end; }\
.servify-widget .sw-msg-user .sw-bubble { background:var(--sw-bubble-self-bg); color:var(--sw-bubble-self-text); border-bottom-right-radius:4px; }\
.servify-widget .sw-msg-bot { align-self:flex-start; }\
.servify-widget .sw-msg-bot .sw-bubble { background:var(--sw-bubble-agent-bg); color:var(--sw-bubble-agent-text); border-bottom-left-radius:4px; }\
.servify-widget .sw-sources { margin-top:8px; border-top:1px dashed var(--sw-chip-border); padding-top:6px; }\
.servify-widget .sw-source-row { font-size:12px; color:var(--sw-meta-text); line-height:1.7; white-space:nowrap; overflow:hidden; text-overflow:ellipsis; }\
.servify-widget .sw-feedback { margin-top:8px; display:flex; align-items:center; gap:8px; }\
.servify-widget .sw-feedback-label { font-size:12px; color:var(--sw-meta-text); }\
.servify-widget .sw-feedback-btn { background:none; border:1px solid var(--sw-chip-border); border-radius:6px; padding:2px 8px; font-size:13px; color:var(--sw-panel-text); cursor:pointer; line-height:1.4; transition:border-color .2s; }\
.servify-widget .sw-feedback-btn:hover { border-color:var(--sw-primary); }\
.servify-widget .sw-feedback-btn:disabled { opacity:.5; cursor:default; }\
.servify-widget .sw-msg-system { align-self:center; max-width:100%; }\
.servify-widget .sw-msg-system .sw-bubble { background:none; color:var(--sw-system-text); font-size:12px; text-align:center; padding:2px 8px; }\
.servify-widget .sw-msg-voice { max-width:85%; }\
.servify-widget .sw-msg-voice-agent { align-self:flex-start; }\
.servify-widget .sw-msg-voice-visitor { align-self:flex-end; text-align:right; }\
.servify-widget .sw-voice-chip { font-size:11px; color:var(--sw-meta-text); margin-bottom:2px; }\
.servify-widget .sw-msg-voice .sw-bubble { background:var(--sw-panel-bg); border:1px solid var(--sw-chip-border); }\
.servify-widget .sw-msg-voice-visitor .sw-bubble { background:var(--sw-chip-bg); }\
.servify-widget .sw-voice-content { font-weight:500; }\
.servify-widget .sw-voice-original { font-size:12px; color:var(--sw-meta-text); margin-top:4px; }\
.servify-widget .sw-voice-play { margin-top:6px; padding:4px 12px; border-radius:12px; border:none; background:var(--sw-primary); color:var(--sw-primary-contrast); cursor:pointer; font-size:12px; }\
.servify-widget .sw-bubble.speaking { border-color:var(--sw-primary); box-shadow:0 0 0 2px var(--sw-primary); }\
.servify-widget .sw-suggests { display:none; padding:8px 12px 0; background:var(--sw-panel-bg); }\
.servify-widget .sw-suggest-hint { font-size:11px; color:var(--sw-meta-text); margin-bottom:6px; }\
.servify-widget .sw-suggest-chips { display:flex; flex-wrap:wrap; gap:6px; }\
.servify-widget .sw-suggest-chip { padding:5px 10px; border:1px solid var(--sw-chip-border); border-radius:14px; background:var(--sw-chip-bg); color:var(--sw-panel-text); font-size:12px; cursor:pointer; transition:all .15s; text-align:left; }\
.servify-widget .sw-suggest-chip:hover { border-color:var(--sw-primary); color:var(--sw-primary); }\
.servify-widget .sw-input-area { padding:12px; border-top:1px solid var(--sw-chip-border); display:flex; gap:8px; background:var(--sw-panel-bg); }\
.servify-widget .sw-input { flex:1; padding:10px 14px; border:1px solid var(--sw-input-border); border-radius:var(--sw-radius-input); font-size:14px; background:var(--sw-input-bg); color:var(--sw-input-text); outline:none; transition:border-color .2s; }\
.servify-widget .sw-input::placeholder { color:var(--sw-meta-text); }\
.servify-widget .sw-input:focus { border-color:var(--sw-primary); }\
.servify-widget .sw-send-btn { padding:10px 16px; border-radius:var(--sw-radius-input); border:none; background:var(--sw-primary); color:var(--sw-primary-contrast); cursor:pointer; font-size:14px; font-weight:500; transition:opacity .2s; }\
.servify-widget .sw-send-btn:hover { opacity:.9; }\
@media (max-width:480px) {\
  .servify-widget .sw-panel { width:calc(100vw - 16px); height:calc(100vh - var(--sw-trigger-size) - 40px); height:calc(100dvh - var(--sw-trigger-size) - 40px); }\
  .servify-widget.sw-pos-bottom-right .sw-panel, .servify-widget.sw-pos-top-right .sw-panel { right:-8px; }\
  .servify-widget.sw-pos-bottom-left .sw-panel, .servify-widget.sw-pos-top-left .sw-panel { left:-8px; }\
}\
@media (prefers-reduced-motion:reduce) {\
  .servify-widget .sw-panel, .servify-widget .sw-trigger, .servify-widget .sw-trigger > *, .servify-widget .sw-badge { transition:none !important; animation:none !important; }\
}\
@keyframes sw-trigger-icon-in { 0% { transform:rotate(-120deg) scale(.4); opacity:0; } 100% { transform:rotate(0deg) scale(1); opacity:1; } }\
@keyframes sw-badge-pop { 0% { transform:scale(0); } 60% { transform:scale(1.15); } 100% { transform:scale(1); } }';
  }

  // ── 颜色工具（token 自动生成：主色 → 亮暗两套衍生色）────────────

  function hexToRgb(hex) {
    var num = parseInt(String(hex).replace('#', ''), 16);
    if (isNaN(num)) num = 0x667eea;
    return { r: (num >> 16) & 255, g: (num >> 8) & 255, b: num & 255 };
  }

  function rgbToHex(r, g, b) {
    var c = function (v) { return Math.min(255, Math.max(0, Math.round(v))); };
    return '#' + ((1 << 24) + (c(r) << 16) + (c(g) << 8) + c(b)).toString(16).slice(1);
  }

  // percent > 0 向白靠拢（提亮），< 0 向黑靠拢（压暗）
  function shade(hex, percent) {
    var rgb = hexToRgb(hex);
    var target = percent >= 0 ? 255 : 0;
    var p = Math.abs(percent) / 100;
    return rgbToHex(
      rgb.r + (target - rgb.r) * p,
      rgb.g + (target - rgb.g) * p,
      rgb.b + (target - rgb.b) * p
    );
  }

  function luminance(hex) {
    var rgb = hexToRgb(hex);
    var lin = function (v) {
      v /= 255;
      return v <= 0.03928 ? v / 12.92 : Math.pow((v + 0.055) / 1.055, 2.4);
    };
    return 0.2126 * lin(rgb.r) + 0.7152 * lin(rgb.g) + 0.0722 * lin(rgb.b);
  }

  // 主色上的文字色：亮色主色配深字，深色主色配白字
  function readableOn(hex) {
    return luminance(hex) > 0.4 ? '#1f2430' : '#ffffff';
  }

  // ── Exports ─────────────────────────────────────────────────────

  root.ServifyWidget = { create: createWidget };

  // Auto init（data-* 属性承载常用外观/主题面，改样式不改代码）
  if (document.currentScript && document.currentScript.hasAttribute('data-servify-widget')) {
    var cs = document.currentScript;
    function attr(name) { return cs.getAttribute('data-' + name) || ''; }
    function attrJSON(name) {
      var raw = attr(name);
      if (!raw) return null;
      try { return JSON.parse(raw); } catch (e) {
        if (root.console && root.console.warn) root.console.warn('[ServifyWidget] invalid JSON in data-' + name);
        return null;
      }
    }
    var baseUrl = attr('base-url') || (location.protocol + '//' + location.host);
    var initOpts = { baseUrl: baseUrl };
    if (attr('session-id')) initOpts.sessionId = attr('session-id');
    if (attr('access-token')) initOpts.accessToken = attr('access-token');
    // icon 三档：data-icon-image / data-icon-url 优先，其次 data-icon 预设名
    if (attr('icon-image')) initOpts.icon = { image: attr('icon-image') };
    else if (attr('icon-url')) initOpts.icon = { url: attr('icon-url') };
    else if (attr('icon')) initOpts.icon = attr('icon');
    var color = attr('color') || attr('primary-color');
    if (color) initOpts.color = color;
    var radius = attr('border-radius');
    if (radius) initOpts.borderRadius = isNaN(Number(radius)) ? radius : Number(radius);
    if (attr('position')) initOpts.position = attr('position');
    var size = attr('size');
    if (size) initOpts.size = isNaN(Number(size)) ? size : Number(size);
    if (attr('theme')) initOpts.theme = attr('theme');
    if (attrJSON('theme-light')) initOpts.themeLight = attrJSON('theme-light');
    if (attrJSON('theme-dark')) initOpts.themeDark = attrJSON('theme-dark');
    if (attrJSON('theme-tokens')) initOpts.themeTokens = attrJSON('theme-tokens');
    if (attr('theme-from-color')) initOpts.themeFromColor = attr('theme-from-color');
    if (attr('theme-url')) initOpts.themeUrl = attr('theme-url');
    var brandAttrs = {};
    if (attr('brand-name')) brandAttrs.name = attr('brand-name');
    if (attr('brand-logo')) brandAttrs.logo = attr('brand-logo');
    if (attr('brand-welcome')) brandAttrs.welcome = attr('brand-welcome');
    if (Object.keys(brandAttrs).length) initOpts.brand = brandAttrs;
    if (document.readyState === 'loading') {
      document.addEventListener('DOMContentLoaded', function () { createWidget(initOpts); });
    } else {
      createWidget(initOpts);
    }
  }
})(typeof window !== 'undefined' ? window : this);