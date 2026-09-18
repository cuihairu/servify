package services

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/gorilla/websocket"
	"github.com/pion/webrtc/v4"
	"github.com/sirupsen/logrus"
	"servify/apps/server/internal/models"
	conversationdelivery "servify/apps/server/internal/modules/conversation/delivery"
	routingcontract "servify/apps/server/internal/modules/routing/contract"
)

type sessionTransferRuntime interface {
	TransferToHuman(ctx context.Context, req *routingcontract.TransferRequest) (*routingcontract.TransferResult, error)
}

type websocketAIService interface {
	ProcessQuery(ctx context.Context, query string, sessionID string) (*AIResponse, error)
	ShouldTransferToHuman(query string, sessionHistory []models.Message) bool
	GetSessionSummary(messages []models.Message) (string, error)
}

type websocketRTCService interface {
	HandleOffer(sessionID string, offer webrtc.SessionDescription) (*webrtc.SessionDescription, error)
	HandleAnswer(sessionID string, answer webrtc.SessionDescription) error
	HandleICECandidate(sessionID string, candidate webrtc.ICECandidateInit) error
}

type WebSocketMessage struct {
	Type      string      `json:"type"`
	Data      interface{} `json:"data"`
	SessionID string      `json:"session_id"`
	Timestamp time.Time   `json:"timestamp"`
}

type WebSocketClient struct {
	ID        string
	SessionID string
	Conn      *websocket.Conn
	Send      chan WebSocketMessage
	Hub       *WebSocketHub
	// pingInterval 供测试注入；零值取默认 54 秒。
	pingInterval time.Duration
}

type WebSocketHub struct {
	clients    map[string]*WebSocketClient
	broadcast  chan WebSocketMessage
	register   chan *WebSocketClient
	unregister chan *WebSocketClient
	mutex      sync.RWMutex
	// 可选：用于直接在WS层调用AI服务（未设置时则仅广播）
	aiService websocketAIService
	// 可选：用于触发“转人工”流程（未设置则仅返回提示）
	transferService sessionTransferRuntime
	// 优先使用 conversation 模块适配器持久化消息
	conversationWriter conversationdelivery.WebSocketMessageWriter
	// 可选：用于处理 WebRTC 信令
	rtcService websocketRTCService
}

// websocketAllowedOrigins 是 WS 建连的 Origin 白名单（P2-5 第一刀）。
// 空列表保持既有"放行所有来源"的行为（匿名访客来源不可枚举，如原生
// WebView）；配置了白名单后，带 Origin 头且不在名单内的建连一律拒绝。
// 经 SetWebsocketAllowedOrigins 注入；包级状态用 atomic 承载——测试与
// 多 router 装配可能并发读写，不能裸写。
var websocketAllowedOrigins atomic.Value // []string

// SetWebsocketAllowedOrigins 注入 WS 建连 Origin 白名单。
func SetWebsocketAllowedOrigins(origins []string) {
	websocketAllowedOrigins.Store(origins)
}

func websocketOriginsSnapshot() []string {
	if v := websocketAllowedOrigins.Load(); v != nil {
		return v.([]string)
	}
	return nil
}

var upgrader = websocket.Upgrader{
	CheckOrigin: func(r *http.Request) bool {
		allowed := websocketOriginsSnapshot()
		if len(allowed) == 0 {
			return true
		}
		origin := r.Header.Get("Origin")
		if origin == "" {
			// 无 Origin 头的非浏览器客户端不携带来源，无从校验，
			// 交由后续 token/租户校验兜底。
			return true
		}
		for _, o := range allowed {
			if origin == o {
				return true
			}
		}
		return false
	},
}

func NewWebSocketHub() *WebSocketHub {
	return &WebSocketHub{
		clients:    make(map[string]*WebSocketClient),
		broadcast:  make(chan WebSocketMessage),
		register:   make(chan *WebSocketClient),
		unregister: make(chan *WebSocketClient),
	}
}

// SetAIService 为WebSocketHub注入AI服务（可选）
func (h *WebSocketHub) SetAIService(ai websocketAIService) {
	h.mutex.Lock()
	defer h.mutex.Unlock()
	h.aiService = ai
}

// SetSessionTransferService 为 WebSocketHub 注入会话转接服务（可选）
func (h *WebSocketHub) SetSessionTransferService(svc sessionTransferRuntime) {
	h.mutex.Lock()
	defer h.mutex.Unlock()
	h.transferService = svc
}

// SetConversationMessageWriter injects the modular conversation persistence adapter.
func (h *WebSocketHub) SetConversationMessageWriter(writer conversationdelivery.WebSocketMessageWriter) {
	h.mutex.Lock()
	defer h.mutex.Unlock()
	h.conversationWriter = writer
}

// SetWebRTCService injects the WebRTC signaling runtime used by websocket events.
func (h *WebSocketHub) SetWebRTCService(rtc websocketRTCService) {
	h.mutex.Lock()
	defer h.mutex.Unlock()
	h.rtcService = rtc
}

func (h *WebSocketHub) Run() {
	for {
		select {
		case client := <-h.register:
			h.mutex.Lock()
			h.clients[client.ID] = client
			h.mutex.Unlock()
			logrus.Infof("Client %s connected", client.ID)

		case client := <-h.unregister:
			h.mutex.Lock()
			if _, ok := h.clients[client.ID]; ok {
				delete(h.clients, client.ID)
				close(client.Send)
				logrus.Infof("Client %s disconnected", client.ID)
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

func (h *WebSocketHub) HandleWebSocket(c *gin.Context) {
	sessionID := strings.TrimSpace(c.Query("session_id"))
	if sessionID == "" {
		c.AbortWithStatusJSON(http.StatusBadRequest, gin.H{
			"error":   "BadRequest",
			"message": "session_id is required",
		})
		return
	}

	conn, err := upgrader.Upgrade(c.Writer, c.Request, nil)
	if err != nil {
		logrus.Error("WebSocket upgrade failed:", err)
		return
	}

	client := &WebSocketClient{
		ID:        fmt.Sprintf("client_%d", time.Now().UnixNano()),
		SessionID: sessionID,
		Conn:      conn,
		Send:      make(chan WebSocketMessage, 256),
		Hub:       h,
	}

	h.register <- client

	go client.writePump()
	go client.readPump()
}

func (c *WebSocketClient) readPump() {
	defer func() {
		c.Hub.unregister <- c
		c.Conn.Close()
	}()

	// WebRTC SDP payloads are routinely several KB, so the generic websocket
	// ingress limit must be large enough to carry signaling messages.
	c.Conn.SetReadLimit(64 * 1024)
	c.Conn.SetReadDeadline(time.Now().Add(60 * time.Second))
	c.Conn.SetPongHandler(func(string) error {
		c.Conn.SetReadDeadline(time.Now().Add(60 * time.Second))
		return nil
	})

	for {
		_, messageBytes, err := c.Conn.ReadMessage()
		if err != nil {
			if websocket.IsUnexpectedCloseError(err, websocket.CloseGoingAway, websocket.CloseAbnormalClosure) {
				logrus.Errorf("WebSocket error: %v", err)
			}
			break
		}

		var message WebSocketMessage
		if err := json.Unmarshal(messageBytes, &message); err != nil {
			logrus.Error("Invalid message format:", err)
			continue
		}

		message.SessionID = c.SessionID
		message.Timestamp = time.Now()

		// 处理不同类型的消息
		switch message.Type {
		case "text-message":
			c.handleTextMessage(message)
		case "webrtc-offer":
			c.handleWebRTCOffer(message)
		case "webrtc-answer":
			c.handleWebRTCAnswer(message)
		case "webrtc-candidate":
			c.handleWebRTCCandidate(message)
		default:
			logrus.Warnf("Unknown message type: %s", message.Type)
		}
	}
}

func (c *WebSocketClient) writePump() {
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
				c.Conn.WriteMessage(websocket.CloseMessage, []byte{})
				return
			}

			if err := c.Conn.WriteJSON(message); err != nil {
				logrus.Error("WriteJSON error:", err)
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

func (c *WebSocketClient) handleTextMessage(message WebSocketMessage) {
	// 保存消息到数据库
	if err := c.persistTextMessage(message); err != nil {
		logrus.Warnf("Failed to persist text message: %v", err)
		// 不影响消息处理流程，继续执行
	}

	// 转发给 AI 服务处理
	go c.processMessageWithAI(message)

	// 广播消息
	c.Hub.broadcast <- message
}

func (c *WebSocketClient) handleWebRTCOffer(message WebSocketMessage) {
	logrus.Infof("Received WebRTC offer from session %s", c.SessionID)

	c.Hub.mutex.RLock()
	rtc := c.Hub.rtcService
	c.Hub.mutex.RUnlock()
	if rtc != nil {
		offer, err := asSessionDescription(message.Data)
		if err != nil {
			logrus.Errorf("Failed to decode WebRTC offer: %v", err)
			return
		}
		answer, err := rtc.HandleOffer(c.SessionID, offer)
		if err != nil {
			logrus.Errorf("Failed to handle WebRTC offer: %v", err)
			return
		}
		c.Send <- WebSocketMessage{
			Type:      "webrtc-answer",
			Data:      answer,
			SessionID: c.SessionID,
			Timestamp: time.Now(),
		}
		return
	}

	c.Hub.broadcast <- message
}

func (c *WebSocketClient) handleWebRTCAnswer(message WebSocketMessage) {
	logrus.Infof("Received WebRTC answer from session %s", c.SessionID)
	c.Hub.mutex.RLock()
	rtc := c.Hub.rtcService
	c.Hub.mutex.RUnlock()
	if rtc == nil {
		return
	}
	answer, err := asSessionDescription(message.Data)
	if err != nil {
		logrus.Errorf("Failed to decode WebRTC answer: %v", err)
		return
	}
	if err := rtc.HandleAnswer(c.SessionID, answer); err != nil {
		logrus.Errorf("Failed to handle WebRTC answer: %v", err)
	}
}

func (c *WebSocketClient) handleWebRTCCandidate(message WebSocketMessage) {
	logrus.Infof("Received ICE candidate from session %s", c.SessionID)
	c.Hub.mutex.RLock()
	rtc := c.Hub.rtcService
	c.Hub.mutex.RUnlock()
	if rtc == nil {
		return
	}
	candidate, err := asICECandidate(message.Data)
	if err != nil {
		logrus.Errorf("Failed to decode ICE candidate: %v", err)
		return
	}
	if err := rtc.HandleICECandidate(c.SessionID, candidate); err != nil {
		logrus.Errorf("Failed to handle ICE candidate: %v", err)
	}
}

func (h *WebSocketHub) SendToSession(sessionID string, message WebSocketMessage) {
	h.broadcast <- WebSocketMessage{
		Type:      message.Type,
		Data:      message.Data,
		SessionID: sessionID,
		Timestamp: time.Now(),
	}
}

func (h *WebSocketHub) GetClientCount() int {
	h.mutex.RLock()
	defer h.mutex.RUnlock()
	return len(h.clients)
}

func asSessionDescription(data interface{}) (webrtc.SessionDescription, error) {
	switch v := data.(type) {
	case webrtc.SessionDescription:
		return v, nil
	case *webrtc.SessionDescription:
		if v == nil {
			return webrtc.SessionDescription{}, fmt.Errorf("nil session description")
		}
		return *v, nil
	case map[string]interface{}:
		raw, err := json.Marshal(v)
		if err != nil {
			return webrtc.SessionDescription{}, fmt.Errorf("marshal session description: %w", err)
		}
		var desc webrtc.SessionDescription
		if err := json.Unmarshal(raw, &desc); err != nil {
			return webrtc.SessionDescription{}, fmt.Errorf("decode session description: %w", err)
		}
		return desc, nil
	default:
		return webrtc.SessionDescription{}, fmt.Errorf("unsupported session description payload %T", data)
	}
}

func asICECandidate(data interface{}) (webrtc.ICECandidateInit, error) {
	switch v := data.(type) {
	case webrtc.ICECandidateInit:
		return v, nil
	case *webrtc.ICECandidateInit:
		if v == nil {
			return webrtc.ICECandidateInit{}, fmt.Errorf("nil ICE candidate")
		}
		return *v, nil
	case map[string]interface{}:
		raw, err := json.Marshal(v)
		if err != nil {
			return webrtc.ICECandidateInit{}, fmt.Errorf("marshal ICE candidate: %w", err)
		}
		var candidate webrtc.ICECandidateInit
		if err := json.Unmarshal(raw, &candidate); err != nil {
			return webrtc.ICECandidateInit{}, fmt.Errorf("decode ICE candidate: %w", err)
		}
		return candidate, nil
	default:
		return webrtc.ICECandidateInit{}, fmt.Errorf("unsupported ICE candidate payload %T", data)
	}
}

// persistTextMessage 持久化文本消息
func (c *WebSocketClient) persistTextMessage(message WebSocketMessage) error {
	// 当前简单实现：记录到日志
	// 生产环境中应该保存到数据库中的 messages 表
	logrus.WithFields(logrus.Fields{
		"session_id": c.SessionID,
		"client_id":  c.ID,
		"type":       message.Type,
		"timestamp":  message.Timestamp,
	}).Info("Text message persisted")

	// 若未配置数据库，则直接返回
	hub := c.Hub
	hub.mutex.RLock()
	writer := hub.conversationWriter
	hub.mutex.RUnlock()

	if writer == nil {
		return nil
	}

	var content string
	switch v := message.Data.(type) {
	case map[string]interface{}:
		if s, ok := v["content"].(string); ok {
			content = s
		}
	case string:
		content = v
	default:
		// 其他格式不处理
	}
	if strings.TrimSpace(content) == "" {
		return nil
	}
	return writer.PersistTextMessage(context.Background(), c.SessionID, content)
}

// processMessageWithAI 使用 AI 处理消息
func (c *WebSocketClient) processMessageWithAI(message WebSocketMessage) {
	// 若未注入AI服务，直接返回
	h := c.Hub
	h.mutex.RLock()
	ai := h.aiService
	transferSvc := h.transferService
	writer := h.conversationWriter
	h.mutex.RUnlock()
	if ai == nil {
		logrus.WithFields(logrus.Fields{
			"session_id":   c.SessionID,
			"message_type": message.Type,
		}).Debug("AI service not configured; skipping AI processing")
		return
	}

	// 提取文本内容
	var content string
	switch v := message.Data.(type) {
	case map[string]interface{}:
		if s, ok := v["content"].(string); ok {
			content = s
		}
	case string:
		content = v
	default:
		// 非预期格式
		logrus.Warnf("Unsupported message data type for AI processing: %T", v)
		return
	}
	if strings.TrimSpace(content) == "" {
		return
	}

	// 若会话已分配人工客服，则停止 AI 自动回复（避免“人机抢答”）
	if writer != nil {
		assigned, err := writer.HasActiveHumanAgent(context.Background(), c.SessionID)
		if err == nil && assigned {
			return
		}
	}
	// 触发“转人工”流程（优先于 AI 正常回答）
	if transferSvc != nil {
		var history []models.Message
		if writer != nil {
			history, _ = writer.ListRecentMessages(context.Background(), c.SessionID, 6)
		}
		if ai.ShouldTransferToHuman(content, history) {
			go func(sessionID string) {
				ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
				defer cancel()

				result, err := transferSvc.TransferToHuman(ctx, &routingcontract.TransferRequest{
					SessionID: sessionID,
					Reason:    "user_request",
				})
				if err != nil {
					c.Hub.SendToSession(sessionID, WebSocketMessage{
						Type: "ai-response",
						Data: map[string]interface{}{
							"content":    "转接人工客服失败：" + err.Error(),
							"confidence": 1.0,
							"source":     "system",
						},
						SessionID: sessionID,
						Timestamp: time.Now(),
					})
					return
				}

				respText := "我来为您转接人工客服，请稍等..."
				if result.IsWaiting {
					respText = "我来为您转接人工客服，当前暂无可用客服，已进入等待队列。"
				} else if result.NewAgentID != 0 {
					respText = fmt.Sprintf("我来为您转接人工客服，已为您分配客服（ID=%d）。", result.NewAgentID)
				}
				c.Hub.SendToSession(sessionID, WebSocketMessage{
					Type: "ai-response",
					Data: map[string]interface{}{
						"content":    respText,
						"confidence": 1.0,
						"source":     "system",
					},
					SessionID: sessionID,
					Timestamp: time.Now(),
				})
			}(c.SessionID)
			return
		}
	}

	// 异步调用AI
	go func(sessionID string, text string) {
		ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
		defer cancel()
		resp, err := ai.ProcessQuery(ctx, text, sessionID)
		if err != nil {
			logrus.Errorf("AI processing failed: %v", err)
			return
		}
		// 推送AI回复
		c.Hub.SendToSession(sessionID, WebSocketMessage{
			Type: "ai-response",
			Data: map[string]interface{}{
				"content":    resp.Content,
				"confidence": resp.Confidence,
				"source":     resp.Source,
			},
			SessionID: sessionID,
			Timestamp: time.Now(),
		})
	}(c.SessionID, content)
}
