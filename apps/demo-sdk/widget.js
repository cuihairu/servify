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
    // 会话 id 一处生成、多通道共用：会话 WS 与语音 WS（PROTOCOL §9 握手
    // session_id）必须指向同一会话；补拉/推荐问题的 session 归因同理。
    var sessionId = opts.sessionId || ('ws_' + Date.now());
    var primaryColor = opts.primaryColor || '#667eea';
    var wsUrl = baseUrl.replace(/^http/, 'ws') + '/api/v1/ws';
    var voiceWsUrl = baseUrl.replace(/^http/, 'ws') + '/api/v1/ws/voice';
    // 语音能力探测：旧缓存包没有 VoiceChannel/MicCapture 时按钮不出现，
    // 聊天主链路零影响。
    var voiceSdk = root.Servify && root.Servify.VoiceChannel && root.Servify.MicCapture ? root.Servify : null;

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

    // Mic icons
    var micOffSvg = '<svg width="18" height="18" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2"><path d="M12 2a3 3 0 0 0-3 3v7a3 3 0 0 0 6 0V5a3 3 0 0 0-3-3Z"/><path d="M19 10v2a7 7 0 0 1-14 0v-2"/><line x1="12" y1="19" x2="12" y2="22"/><line x1="8" y1="15" x2="16" y2="15"/><line x1="4.93" y1="4.93" x2="19.07" y2="19.07"/></svg>';
    var micOnSvg = '<svg width="18" height="18" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2"><path d="M12 2a3 3 0 0 0-3 3v7a3 3 0 0 0 6 0V5a3 3 0 0 0-3-3Z"/><path d="M19 10v2a7 7 0 0 1-14 0v-2"/><line x1="12" y1="19" x2="12" y2="22"/></svg>';

    // Panel
    var panel = el('div', 'sw-panel');
    var header = el('div', 'sw-header');
    header.style.background = 'linear-gradient(135deg, ' + primaryColor + ', ' + adjustColor(primaryColor, -30) + ')';
    var headerTitle = el('div', 'sw-header-title', '在线客服');
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
    var badge = el('div', 'sw-badge', '0');
    badge.style.display = 'none';
    wrap.appendChild(btn);
    wrap.appendChild(badge);
    wrap.appendChild(panel);

    // Inject styles
    if (!document.getElementById('servify-widget-styles')) {
      var style = document.createElement('style');
      style.id = 'servify-widget-styles';
      style.textContent = getStyles(primaryColor);
      document.head.appendChild(style);
    }

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
        btn.innerHTML = '<svg width="24" height="24" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2"><path d="M21 15a2 2 0 0 1-2 2H7l-4 4V5a2 2 0 0 1 2-2h14a2 2 0 0 1 2 2z"/></svg>';
        // Stop voice when panel closes
        if (voiceActive) stopVoice();
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

    // Welcome message
    addMsg('bot', '您好！欢迎来到 Servify Demo。请问有什么可以帮您的？');

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

  // ── Inline CSS ─────────────────────────────────────────────────

  function getStyles(color) {
    return '\
.servify-widget { position:fixed; right:24px; bottom:24px; z-index:99999; font-family:-apple-system,BlinkMacSystemFont,"Segoe UI",Roboto,sans-serif; font-size:14px; }\
.servify-widget * { box-sizing:border-box; }\
.servify-widget .sw-badge { position:absolute; right:-2px; bottom:44px; min-width:20px; height:20px; padding:0 5px; border-radius:10px; background:#e53e3e; color:#fff; font-size:12px; font-weight:600; display:flex; align-items:center; justify-content:center; box-shadow:0 2px 8px rgba(0,0,0,.3); pointer-events:none; }\
.servify-widget .sw-trigger { width:56px; height:56px; border-radius:50%; border:none; color:#fff; cursor:pointer; box-shadow:0 4px 16px rgba(0,0,0,.25); display:flex; align-items:center; justify-content:center; transition:transform .2s,box-shadow .2s; }\
.servify-widget .sw-trigger:hover { transform:scale(1.1); box-shadow:0 6px 24px rgba(0,0,0,.3); }\
.servify-widget .sw-panel { position:absolute; right:0; bottom:68px; width:380px; height:520px; background:#fff; border-radius:16px; box-shadow:0 8px 40px rgba(0,0,0,.15); display:none; flex-direction:column; overflow:hidden; }\
.servify-widget .sw-panel.open { display:flex; }\
.servify-widget .sw-header { padding:16px; color:#fff; display:flex; align-items:center; justify-content:space-between; }\
.servify-widget .sw-header-title { font-size:16px; font-weight:600; flex:1; }\
.servify-widget .sw-header-status { margin:0 12px; }\
.servify-widget .sw-mic-btn:hover { opacity:1 !important; }\
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
.servify-widget .sw-msg-voice { max-width:85%; }\
.servify-widget .sw-msg-voice-agent { align-self:flex-start; }\
.servify-widget .sw-msg-voice-visitor { align-self:flex-end; text-align:right; }\
.servify-widget .sw-voice-chip { font-size:11px; color:#999; margin-bottom:2px; }\
.servify-widget .sw-msg-voice .sw-bubble { background:#fff; border:1px solid #eee; }\
.servify-widget .sw-msg-voice-visitor .sw-bubble { background:#f8f9ff; }\
.servify-widget .sw-voice-content { font-weight:500; }\
.servify-widget .sw-voice-original { font-size:12px; color:#999; margin-top:4px; }\
.servify-widget .sw-voice-play { margin-top:6px; padding:4px 12px; border-radius:12px; border:none; background:' + color + '; color:#fff; cursor:pointer; font-size:12px; }\
.servify-widget .sw-bubble.speaking { border-color:' + color + '; box-shadow:0 0 0 2px ' + color + '33; }\
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
    if (document.readyState === 'loading') {
      document.addEventListener('DOMContentLoaded', function () { createWidget({ baseUrl: baseUrl, sessionId: sid, primaryColor: color }); });
    } else {
      createWidget({ baseUrl: baseUrl, sessionId: sid, primaryColor: color });
    }
  }
})(typeof window !== 'undefined' ? window : this);