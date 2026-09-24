/**
 * Servify Widget — 可嵌入的客服聊天组件
 *
 * 使用方式:
 *   <link rel="stylesheet" href="/demo-sdk/widget.css">
 *   <script src="/demo-sdk/servify-sdk.umd.js"></script>
 *   <script src="/demo-sdk/widget.js"></script>
 *   <script>
 *     ServifyWidget.create({
 *       baseUrl: 'http://localhost:8080',
 *       sessionId: 'optional-custom-session-id',
 *       primaryColor: '#667eea'  // optional
 *     });
 *   </script>
 *
 * 或自动初始化:
 *   <script src="/demo-sdk/servify-sdk.umd.js" data-servify-sdk></script>
 *   <script src="/demo-sdk/widget.js" data-servify-widget data-base-url="http://localhost:8080"></script>
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

  // ── Widget UI ──────────────────────────────────────────────────

  function createWidget(opts) {
    opts = opts || {};
    var baseUrl = opts.baseUrl || (location.protocol + '//' + location.host);
    var sessionId = opts.sessionId || '';
    var primaryColor = opts.primaryColor || '#667eea';
    var wsUrl = baseUrl.replace(/^http/, 'ws') + '/api/v1/ws';

    // Create DOM
    var wrap = el('div', 'servify-widget');
    wrap.setAttribute('data-servify', '');

    // Toggle button
    var btn = el('button', 'sw-trigger');
    btn.innerHTML = '<svg width="24" height="24" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2"><path d="M21 15a2 2 0 0 1-2 2H7l-4 4V5a2 2 0 0 1 2-2h14a2 2 0 0 1 2 2z"/></svg>';
    btn.setAttribute('title', '在线客服');
    btn.style.background = primaryColor;

    // Close icon
    var closeSvg = '<svg width="18" height="18" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2"><line x1="18" y1="6" x2="6" y2="18"/><line x1="6" y1="6" x2="18" y2="18"/></svg>';

    // Panel
    var panel = el('div', 'sw-panel');
    var header = el('div', 'sw-header');
    header.style.background = 'linear-gradient(135deg, ' + primaryColor + ', ' + adjustColor(primaryColor, -30) + ')';
    var headerTitle = el('div', 'sw-header-title', '在线客服');
    var headerStatus = el('div', 'sw-header-status', '未连接');
    headerStatus.style.opacity = '0.75';
    headerStatus.style.fontSize = '12px';
    var closeBtn = el('button', 'sw-close');
    closeBtn.innerHTML = closeSvg;
    header.appendChild(headerTitle);
    header.appendChild(headerStatus);
    header.appendChild(closeBtn);

    var msgs = el('div', 'sw-messages');
    var inputArea = el('div', 'sw-input-area');
    var input = el('input', 'sw-input');
    input.type = 'text';
    input.placeholder = '输入消息...';
    var sendBtn = el('button', 'sw-send-btn', '发送');
    sendBtn.style.background = primaryColor;

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
    wrap.appendChild(btn);
    wrap.appendChild(panel);

    // Inject styles
    if (!document.getElementById('servify-widget-styles')) {
      var style = document.createElement('style');
      style.id = 'servify-widget-styles';
      style.textContent = getStyles(primaryColor);
      document.head.appendChild(style);
    }

    document.body.appendChild(wrap);

    // ── Client ────────────────────────────────────────────────

    var client = new WSClient({ wsUrl: wsUrl, sessionId: sessionId });
    var panelOpen = false;
    var connected = false;
    var initialSuggestsLoaded = false;

    function togglePanel() {
      panelOpen = !panelOpen;
      if (panelOpen) {
        panel.classList.add('open');
        btn.innerHTML = closeSvg;
        btn.style.borderRadius = '50%';
        input.focus();
        if (!connected) client.connect();
        if (!initialSuggestsLoaded) {
          initialSuggestsLoaded = true;
          loadInitialSuggests();
        }
      } else {
        panel.classList.remove('open');
        btn.innerHTML = '<svg width="24" height="24" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2"><path d="M21 15a2 2 0 0 1-2 2H7l-4 4V5a2 2 0 0 1 2-2h14a2 2 0 0 1 2 2z"/></svg>';
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

    // ── 流式气泡（PROTOCOL §4.1 三段契约）──────────────────────
    // ① 若干 done=false 增量即到即拼（打字机光标）；② 终末增量 done=true 封笔；
    // ③ ai-response 终帧整体覆写幂等收口。断连时无终帧 = 流中断：
    // 定格部分内容 + 提示重试（对齐三端语义；仅终末增量的空流不算中断）。
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
    // 访客端点 GET /api/v1/sessions/{id}/messages 免认证；connected 触发
    // （首连也拉——sessionId 指向既有会话时回放历史）。WS 帧不带服务端
    // 消息 id（回显是原帧广播），游标只能由补拉响应末条 id 推进；「WS 已
    // 渲染」窗口靠内容指纹去重：渲染入口记账、补拉查账跳过。比 core 的
    // 指纹表 + 200 容量窗口简化（演示壳无窗口管理诉求；同内容多消息在
    // 补拉窗口内只显示首条）。404=会话行未建（首条消息前），失败全静默。
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
    // 首屏 = 公开知识库热门问题；发送后 = 基于刚发内容的上下文联想。
    // 公开路由失败时静默隐藏，绝不阻塞聊天主链路。
    function fetchSuggestQuestions(path) {
      if (!root.fetch) return Promise.resolve([]);
      // 曝光归因（P2-0 RQ-5）：带 WS 会话 id，服务端把曝光/转化挂到同一
      // session；无 session 时不带参，仅计匿名曝光。
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

    // Welcome message
    addMsg('bot', '您好！欢迎来到 Servify Demo。请问有什么可以帮您的？');

    // Client events
    client.on('status', function (s) {
      connected = s === 'connected';
      if (s === 'disconnected' || s === 'error') closeStreamingOnDisconnect();
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

      // AI response（终帧）：流式气泡整体覆写幂等收口；无流时照常渲染
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
          if (streaming) {
            streaming.bubble.textContent = finalText;
            streaming = null;
          } else {
            addMsg('bot', finalText);
          }
        }
        return;
      }

      // Echo of own text message (broadcast from hub)
      if (msg.type === 'text-message' && msg.data) {
        // This is broadcast back to us — skip to avoid duplicate
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

      // 流式增量（PROTOCOL §4.1）：即到即拼的打字机气泡（▌ 光标）；
      // 终末增量只封笔，等 ai-response 终帧收口；封笔后的迟到增量丢弃
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
        return;
      }

      // WebRTC 信令（PROTOCOL §4.3）：widget 非远程协助 UI，契约内帧显式静默（同移动端 V1 口径）
      if (msg.type === 'webrtc-offer' || msg.type === 'webrtc-answer' || msg.type === 'webrtc-candidate' ||
          msg.type === 'webrtc-state-change' || msg.type === 'webrtc-ice-config' || msg.type === 'data-channel-message') {
        return;
      }

      // 未知帧：契约口径是忽略而非报错（PROTOCOL §8），warn 便于调试、不进气泡
      console.warn('[ServifyWidget] unknown frame type:', msg.type);
    });

    client.on('error', function (e) {
      console.warn('[ServifyWidget] Error:', e);
    });

    return { mount: wrap, client: client };
  }

  // ── Inline CSS ─────────────────────────────────────────────────

  function getStyles(color) {
    return '\
.servify-widget { position:fixed; right:24px; bottom:24px; z-index:99999; font-family:-apple-system,BlinkMacSystemFont,"Segoe UI",Roboto,sans-serif; font-size:14px; }\
.servify-widget * { box-sizing:border-box; }\
.servify-widget .sw-trigger { width:56px; height:56px; border-radius:50%; border:none; color:#fff; cursor:pointer; box-shadow:0 4px 16px rgba(0,0,0,.25); display:flex; align-items:center; justify-content:center; transition:transform .2s,box-shadow .2s; }\
.servify-widget .sw-trigger:hover { transform:scale(1.1); box-shadow:0 6px 24px rgba(0,0,0,.3); }\
.servify-widget .sw-panel { position:absolute; right:0; bottom:68px; width:380px; height:520px; background:#fff; border-radius:16px; box-shadow:0 8px 40px rgba(0,0,0,.15); display:none; flex-direction:column; overflow:hidden; }\
.servify-widget .sw-panel.open { display:flex; }\
.servify-widget .sw-header { padding:16px; color:#fff; display:flex; align-items:center; justify-content:space-between; }\
.servify-widget .sw-header-title { font-size:16px; font-weight:600; flex:1; }\
.servify-widget .sw-header-status { margin:0 12px; }\
.servify-widget .sw-close { background:none; border:none; color:#fff; cursor:pointer; opacity:.8; padding:4px; display:flex; align-items:center; }\
.servify-widget .sw-close:hover { opacity:1; }\
.servify-widget .sw-messages { flex:1; overflow-y:auto; padding:16px; display:flex; flex-direction:column; gap:12px; background:#fafafa; }\
.servify-widget .sw-msg { max-width:80%; }\
.servify-widget .sw-bubble { padding:10px 14px; border-radius:12px; line-height:1.5; word-break:break-word; }\
.servify-widget .sw-msg-user { align-self:flex-end; }\
.servify-widget .sw-msg-user .sw-bubble { background:' + color + '; color:#fff; border-bottom-right-radius:4px; }\
.servify-widget .sw-msg-bot { align-self:flex-start; }\
.servify-widget .sw-msg-bot .sw-bubble { background:#f0f0f0; color:#333; border-bottom-left-radius:4px; }\
.servify-widget .sw-msg-system { align-self:center; max-width:100%; }\
.servify-widget .sw-msg-system .sw-bubble { background:none; color:#888; font-size:12px; text-align:center; padding:2px 8px; }\
.servify-widget .sw-suggests { display:none; padding:8px 12px 0; background:#fff; }\
.servify-widget .sw-suggest-hint { font-size:11px; color:#999; margin-bottom:6px; }\
.servify-widget .sw-suggest-chips { display:flex; flex-wrap:wrap; gap:6px; }\
.servify-widget .sw-suggest-chip { padding:5px 10px; border:1px solid #e0e0e0; border-radius:14px; background:#fff; color:#555; font-size:12px; cursor:pointer; transition:all .15s; text-align:left; }\
.servify-widget .sw-suggest-chip:hover { border-color:' + color + '; color:' + color + '; }\
.servify-widget .sw-input-area { padding:12px; border-top:1px solid #eee; display:flex; gap:8px; background:#fff; }\
.servify-widget .sw-input { flex:1; padding:10px 14px; border:1px solid #ddd; border-radius:20px; font-size:14px; outline:none; transition:border-color .2s; }\
.servify-widget .sw-input:focus { border-color:' + color + '; }\
.servify-widget .sw-send-btn { padding:10px 16px; border-radius:20px; border:none; color:#fff; cursor:pointer; font-size:14px; font-weight:500; transition:opacity .2s; }\
.servify-widget .sw-send-btn:hover { opacity:.9; }\
@media (max-width:480px) {\
  .servify-widget .sw-panel { width:calc(100vw - 32px); right:-8px; height:60vh; bottom:72px; }\
}';
  }

  function adjustColor(hex, amount) {
    var num = parseInt(hex.replace('#', ''), 16);
    var r = Math.min(255, Math.max(0, (num >> 16) + amount));
    var g = Math.min(255, Math.max(0, ((num >> 8) & 0x00FF) + amount));
    var b = Math.min(255, Math.max(0, (num & 0x0000FF) + amount));
    return '#' + ((1 << 24) + (r << 16) + (g << 8) + b).toString(16).slice(1);
  }

  // ── Exports ─────────────────────────────────────────────────────

  root.ServifyWidget = { create: createWidget };

  // Auto init
  if (document.currentScript && document.currentScript.hasAttribute('data-servify-widget')) {
    var baseUrl = document.currentScript.getAttribute('data-base-url') || (location.protocol + '//' + location.host);
    var sid = document.currentScript.getAttribute('data-session-id') || '';
    var color = document.currentScript.getAttribute('data-primary-color') || '#667eea';
    // Defer to allow DOM ready
    if (document.readyState === 'loading') {
      document.addEventListener('DOMContentLoaded', function () { createWidget({ baseUrl: baseUrl, sessionId: sid, primaryColor: color }); });
    } else {
      createWidget({ baseUrl: baseUrl, sessionId: sid, primaryColor: color });
    }
  }
})(typeof window !== 'undefined' ? window : this);
