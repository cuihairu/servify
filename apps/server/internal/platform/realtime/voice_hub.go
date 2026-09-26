// voice_hub.go 语音翻译 WS 通道（Phase 2 刀二b-2，docs/realtime-translation-design.md
// §1.2）：独立于会话 WS（/api/v1/ws）的专用通道 /api/v1/ws/voice。
//
// 为什么独立通道：语音上行是持续二进制帧（20–100ms 级），挤会话 WS 的
// 256 帧下行缓冲会把慢客户端踢线语义（PROTOCOL §6.1）打穿；独立通道让
// 会话 WS 契约零变更。协议三件套（PROTOCOL.md 条目 + fixtures + 三端回放）
// 随客户端消费刀（b-3）按 §5 流程同 PR 落地，本通道在装配层配置门控
// （ai.asr.provider 非空才注册路由）。
//
// 上行：二进制帧 = pcm16 24kHz 单声道原始音频分片；文本帧忽略。
// 下行：translation-delta / translation-final / translation-audio /
// voice-error（JSON，WebSocketMessage 同形信封）。
//
// 并发不变量：Send 的投递与关闭只在 Run 事件循环（broadcast/teardown 两个
// 命令通道）——客户端泵与管线 sink 都不直接触碰 Send，否则与慢客户端踢线
// 的 close 形成 send on closed channel（进程级 panic）。
package realtime

import (
	"context"
	"encoding/base64"
	"errors"
	"net/http"
	"strings"
	"sync"
	"time"

	translationdelivery "servify/apps/server/internal/modules/translation/delivery"

	"github.com/gin-gonic/gin"
	"github.com/gorilla/websocket"
	"github.com/sirupsen/logrus"
)

// voiceTranslationRuntime 语音翻译通道的平台侧契约（translationdelivery
// .VoiceStreamStarter 结构化满足；未注入 = 通道未装配）。
type voiceTranslationRuntime interface {
	StartAudioStream(ctx context.Context, sessionID, speaker string, sink translationdelivery.VoiceStreamSink) (translationdelivery.VoiceAudioStream, error)
}

// voice-error 帧的 code 词表。
const (
	voiceErrDisabled     = "disabled"        // AI 面未配置或会话无偏好
	voiceErrASRUnavail   = "asr_unavailable" // ASR 建联失败
	voiceErrStreamBroken = "stream_broken"   // 上行流破损（ASR 会话终结）
)

// VoiceClient 语音通道单连接：一个连接 = 一个说话方的一条上行流。
type VoiceClient struct {
	ID        string
	SessionID string
	Speaker   string // visitor / agent
	Conn      *websocket.Conn
	Send      chan WebSocketMessage
	Hub       *VoiceHub

	// stream 由 readPump 在连接建立后启动；仅 readPump 触达。
	stream translationdelivery.VoiceAudioStream
	// pendingClose 收线前最后下行帧：仅 readPump 触达（置位与随 teardown
	// 上报同 goroutine），由 Run 单点排入 Send 后关闭。
	pendingClose *WebSocketMessage
	// pingInterval / readTimeout 供测试注入；零值取默认（54s / 60s）。
	pingInterval time.Duration
	readTimeout  time.Duration
}

// VoiceHub 语音翻译通道 hub：按会话聚合连接，管线产出按会话广播
// （载荷携带 speaker，客户端自行按方向过滤渲染）。
type VoiceHub struct {
	clients  map[string]*VoiceClient
	register chan *VoiceClient
	// teardown 唯一的收线通道：携带"收线前最后下行帧"（可空）。Send 的
	// 关闭权因此只握在 Run 事件循环手里（客户端泵直投 Send 会与慢客户端
	// 踢线路径的 close 形成 send on closed channel 竞态），且收线帧与
	// close 的先后由单通道单处理点保证，不受 select 多路选择顺序影响。
	teardown  chan *voiceTeardown
	broadcast chan WebSocketMessage
	mutex     sync.RWMutex

	voice          voiceTranslationRuntime
	tokenValidator func(sessionID, accessToken string) error
}

// voiceTeardown 收线命令：把 message（可空）排进该连接 Send 后立即关闭
// Send + 摘除连接，仅由 Run 事件循环执行。
type voiceTeardown struct {
	client  *VoiceClient
	message *WebSocketMessage
}

// NewVoiceHub 构造语音通道 hub。broadcast 带缓冲：管线产出是非阻塞投递
// （VoiceSink 契约），无缓冲时广播循环忙于上一帧就会把当帧丢掉；缓冲兜住
// 管线串行产出的正常节奏，真过载仍有 select-default 丢帧 + 慢客户端踢线。
func NewVoiceHub() *VoiceHub {
	return &VoiceHub{
		clients:   make(map[string]*VoiceClient),
		register:  make(chan *VoiceClient),
		teardown:  make(chan *voiceTeardown, 64),
		broadcast: make(chan WebSocketMessage, 256),
	}
}

// SetVoiceTranslationRuntime 注入语音翻译通道门面（装配层在 ai.asr 已配置
// 时接线；nil 保持通道未装配形态）。
func (h *VoiceHub) SetVoiceTranslationRuntime(runtime translationdelivery.VoiceStreamStarter) {
	h.mutex.Lock()
	h.voice = runtime
	h.mutex.Unlock()
}

func (h *VoiceHub) currentVoice() voiceTranslationRuntime {
	h.mutex.RLock()
	defer h.mutex.RUnlock()
	return h.voice
}

// SetTokenValidator 注入握手 token 校验（与会话 WS 同款：guest_token.required
// 开启时装配；nil 保持免认证行为）。
func (h *VoiceHub) SetTokenValidator(validator func(sessionID, accessToken string) error) {
	h.mutex.Lock()
	h.tokenValidator = validator
	h.mutex.Unlock()
}

func (h *VoiceHub) currentTokenValidator() func(sessionID, accessToken string) error {
	h.mutex.RLock()
	defer h.mutex.RUnlock()
	return h.tokenValidator
}

// Run 事件循环：注册/收线/按会话广播。慢客户端踢线与会话 WS 同口径
// （下行缓冲写满即断，客户端重连兜底）。
func (h *VoiceHub) Run() {
	for {
		select {
		case client := <-h.register:
			h.mutex.Lock()
			h.clients[client.ID] = client
			h.mutex.Unlock()
			logrus.Infof("Voice client %s (speaker=%s) connected to session %s", client.ID, client.Speaker, client.SessionID)

		case td := <-h.teardown:
			// 收线：已在会话表外（先前被踢线/已收线）则跳过，绝不向已关闭
			// 的 Send 投递。收线前最后下行帧先入队再 close，writePump 排干
			// 缓冲后才观察到关闭——错误帧先于 close 帧到达。
			h.mutex.Lock()
			if _, ok := h.clients[td.client.ID]; ok {
				if td.message != nil {
					select {
					case td.client.Send <- *td.message:
					default:
					}
				}
				close(td.client.Send)
				delete(h.clients, td.client.ID)
			}
			h.mutex.Unlock()

		case message := <-h.broadcast:
			h.mutex.Lock()
			for _, client := range h.clients {
				if message.SessionID == "" || client.SessionID == message.SessionID {
					select {
					case client.Send <- message:
					default:
						close(client.Send)
						delete(h.clients, client.ID)
					}
				}
			}
			h.mutex.Unlock()
		}
	}
}

// HandleVoiceWebSocket 语音通道握手：session_id 必填、speaker ∈
// {visitor, agent}（缺省 visitor）；token 校验与会话 WS 同源。
func (h *VoiceHub) HandleVoiceWebSocket(c *gin.Context) {
	sessionID := strings.TrimSpace(c.Query("session_id"))
	if sessionID == "" {
		c.AbortWithStatusJSON(http.StatusBadRequest, gin.H{
			"error":   "BadRequest",
			"message": "session_id is required",
		})
		return
	}
	speaker := strings.ToLower(strings.TrimSpace(c.Query("speaker")))
	if speaker == "" {
		speaker = translationdelivery.VoiceSpeakerVisitor
	}
	if speaker != translationdelivery.VoiceSpeakerVisitor && speaker != translationdelivery.VoiceSpeakerAgent {
		c.AbortWithStatusJSON(http.StatusBadRequest, gin.H{
			"error":   "BadRequest",
			"message": "speaker must be visitor or agent",
		})
		return
	}
	if validator := h.currentTokenValidator(); validator != nil {
		if err := validator(sessionID, c.Query("access_token")); err != nil {
			c.AbortWithStatusJSON(http.StatusUnauthorized, gin.H{
				"error":   "Unauthorized",
				"message": "invalid access_token: " + err.Error(),
			})
			return
		}
	}
	if h.currentVoice() == nil {
		c.AbortWithStatusJSON(http.StatusServiceUnavailable, gin.H{
			"error":   "Unavailable",
			"message": "voice translation is not configured",
		})
		return
	}

	conn, err := upgrader.Upgrade(c.Writer, c.Request, nil)
	if err != nil {
		logrus.Error("Voice WebSocket upgrade failed:", err)
		return
	}

	client := &VoiceClient{
		ID:        newClientID(),
		SessionID: sessionID,
		Speaker:   speaker,
		Conn:      conn,
		Send:      make(chan WebSocketMessage, 256),
		Hub:       h,
	}
	h.register <- client
	go client.writePump()
	go client.readPump()
}

// readPump 上行泵：先起管线，再持续喂音频。任何流破损都发 voice-error
// 后收线——客户端以"连接关闭"为流终止单一信号。
func (c *VoiceClient) readPump() {
	// 收线不关 conn：teardown 命令后 Run 关闭 Send，writePump 排干余帧
	// （含收线前排入的 voice-error）写 close 帧退出，由它的 defer 关 conn
	//——错误帧与 close 帧的到达顺序因此确定。本方 ReadMessage 的解堵也
	// 由该关闭完成（踢线路径 writePump 必然退出）。
	defer func() {
		if c.stream != nil {
			_ = c.stream.Close()
		}
		c.Hub.teardown <- &voiceTeardown{client: c, message: c.pendingClose}
	}()

	c.Conn.SetReadLimit(64 * 1024)
	readTimeout := c.readTimeout
	if readTimeout <= 0 {
		readTimeout = 60 * time.Second
	}
	c.Conn.SetReadDeadline(time.Now().Add(readTimeout))
	c.Conn.SetPongHandler(func(string) error {
		c.Conn.SetReadDeadline(time.Now().Add(readTimeout))
		return nil
	})

	// 管线启动：失败（未配置/无偏好/ASR 建联失败）发 voice-error 后收线。
	// 未配置形态在握手已被 503 拦下，此处 voice 必非 nil（装配期注入后
	// 不再变更）。
	stream, err := c.Hub.currentVoice().StartAudioStream(context.Background(), c.SessionID, c.Speaker, &voiceFrameSink{client: c})
	if err != nil {
		// 客户端只收固定文案（不回 provider 原始错误：可能含内部端点/主机
		// 细节）；真实原因记服务端日志。
		code := voiceErrASRUnavail
		message := "speech recognition unavailable"
		switch {
		case errors.Is(err, translationdelivery.ErrVoiceChannelInactive):
			code = voiceErrDisabled
			message = "session has no translation preference"
		case errors.Is(err, translationdelivery.ErrTranslationUnavailable):
			code = voiceErrDisabled
			message = "voice translation is not configured"
		}
		logrus.Warnf("Voice stream start failed (session=%s speaker=%s): %v", c.SessionID, c.Speaker, err)
		c.pendingClose = c.voiceErrorFrame(code, message)
		return
	}
	c.stream = stream

	for {
		messageType, payload, err := c.Conn.ReadMessage()
		if err != nil {
			if websocket.IsUnexpectedCloseError(err, websocket.CloseGoingAway, websocket.CloseAbnormalClosure) {
				logrus.Errorf("Voice WebSocket error: %v", err)
			}
			return
		}
		c.Conn.SetReadDeadline(time.Now().Add(readTimeout))
		switch messageType {
		case websocket.BinaryMessage:
			if len(payload) == 0 {
				continue
			}
			if err := c.stream.Feed(payload); err != nil {
				// 会话破损：通知客户端后收线（本方 readLoop 不再喂入）。
				logrus.Warnf("Voice stream feed failed (session=%s speaker=%s): %v", c.SessionID, c.Speaker, err)
				c.pendingClose = c.voiceErrorFrame(voiceErrStreamBroken, "audio stream broken")
				return
			}
		default:
			// 文本帧当前无控制语义：忽略（协议演进留白）。
		}
	}
}

// voiceErrorFrame 收线前的最后下行帧（连接级语义，不广播同会话其他连接）。
// 只置位到 pendingClose，由 readPump 的 defer 随 teardown 命令单点提交。
func (c *VoiceClient) voiceErrorFrame(code, message string) *WebSocketMessage {
	return &WebSocketMessage{
		Type:      "voice-error",
		Data:      map[string]interface{}{"code": code, "message": message},
		SessionID: c.SessionID,
		Timestamp: time.Now(),
	}
}

func (c *VoiceClient) writePump() {
	interval := c.pingInterval
	if interval <= 0 {
		interval = 54 * time.Second
	}
	ticker := time.NewTicker(interval)
	defer func() {
		ticker.Stop()
		c.Conn.Close()
	}()

	for {
		select {
		case message, ok := <-c.Send:
			c.Conn.SetWriteDeadline(time.Now().Add(10 * time.Second))
			if !ok {
				_ = c.Conn.WriteMessage(websocket.CloseMessage, []byte{})
				return
			}
			if err := c.Conn.WriteJSON(message); err != nil {
				logrus.Error("Voice WriteJSON error:", err)
				return
			}
		case <-ticker.C:
			c.Conn.SetWriteDeadline(time.Now().Add(10 * time.Second))
			if err := c.Conn.WriteMessage(websocket.PingMessage, nil); err != nil {
				return
			}
		}
	}
}

// voiceFrameSink 管线产出 → 通道下行帧（§1.2 分发半边）。全部方法把帧投
// 进 hub 广播通道即返回（广播循环负责慢客户端踢线），满足 VoiceSink 的
// 非阻塞要求；失败零帧原则：OnError 只记服务端日志。
type voiceFrameSink struct {
	client *VoiceClient
}

func (s *voiceFrameSink) emit(frameType string, data map[string]interface{}) {
	select {
	case s.client.Hub.broadcast <- WebSocketMessage{
		Type:      frameType,
		Data:      data,
		SessionID: s.client.SessionID,
		Timestamp: time.Now(),
	}:
	default:
		// 广播通道满：丢帧保管线（字幕是流式语义，丢旧帧可容忍）。
	}
}

// OnSpeechStart 无帧：partial 已承载"正在说"信号。
func (s *voiceFrameSink) OnSpeechStart(turnSeq int64) {
	_ = turnSeq
}

func (s *voiceFrameSink) OnPartial(turnSeq int64, text string) {
	s.emit("translation-delta", map[string]interface{}{
		"speaker":  s.client.Speaker,
		"turn_seq": turnSeq,
		"text":     text,
	})
}

func (s *voiceFrameSink) OnCaption(caption translationdelivery.VoiceCaption) {
	s.emit("translation-final", map[string]interface{}{
		"speaker":     s.client.Speaker,
		"seq":         caption.Seq,
		"original":    caption.Source,
		"content":     caption.Translated,
		"source_lang": caption.SourceLang,
		"target_lang": caption.TargetLang,
		"degraded":    caption.Degraded,
	})
}

func (s *voiceFrameSink) OnAudio(capSeq int64, audio []byte, format string) {
	s.emit("translation-audio", map[string]interface{}{
		"speaker": s.client.Speaker,
		"seq":     capSeq,
		"format":  format,
		"audio":   base64.StdEncoding.EncodeToString(audio),
	})
}

func (s *voiceFrameSink) OnError(err error) {
	// 失败零帧（message-translated 同款边界）：服务端日志可见即可，
	// 客户端不存在"翻译失败"态；流级破损经 Feed 失败 → voice-error 收线。
	logrus.Warnf("Voice pipeline error (session=%s speaker=%s): %v", s.client.SessionID, s.client.Speaker, err)
}
