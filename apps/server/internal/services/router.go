package services

import (
	"context"
	"fmt"
	"github.com/google/uuid"
	"github.com/sirupsen/logrus"
	"gorm.io/gorm"
	"servify/apps/server/internal/models"
	aidelivery "servify/apps/server/internal/modules/ai/delivery"
	"sync"
	"time"
)

type MessageRouter struct {
	platforms map[string]PlatformAdapter
	aiService routerAIService
	wsHub     *WebSocketHub
	db        *gorm.DB
	mutex     sync.RWMutex
}

type routerAIService interface {
	ProcessQuery(ctx context.Context, query string, sessionID string) (*aidelivery.AIResponse, error)
}

type MessageRouterRuntime interface {
	Start() error
	Stop() error
	GetPlatformStats() map[string]interface{}
}

type PlatformAdapter interface {
	SendMessage(chatID, message string) error
	ReceiveMessage() <-chan UnifiedMessage
	GetPlatformType() PlatformType
	Start() error
	Stop() error
}

type PlatformType string

const (
	PlatformWeb      PlatformType = "web"
	PlatformTelegram PlatformType = "telegram"
	PlatformWeChat   PlatformType = "wechat"
	PlatformQQ       PlatformType = "qq"
	PlatformFeishu   PlatformType = "feishu"
)

type UnifiedMessage struct {
	ID          string                 `json:"id"`
	PlatformID  string                 `json:"platform_id"`
	UserID      string                 `json:"user_id"`
	Content     string                 `json:"content"`
	Type        MessageType            `json:"type"`
	Timestamp   time.Time              `json:"timestamp"`
	Attachments []Attachment           `json:"attachments,omitempty"`
	Metadata    map[string]interface{} `json:"metadata,omitempty"`
}

type MessageType string

const (
	MessageTypeText  MessageType = "text"
	MessageTypeImage MessageType = "image"
	MessageTypeFile  MessageType = "file"
	MessageTypeAudio MessageType = "audio"
	MessageTypeVideo MessageType = "video"
)

type Attachment struct {
	Type string `json:"type"`
	URL  string `json:"url"`
	Name string `json:"name"`
	Size int64  `json:"size"`
}

type RouteRule struct {
	Platform  PlatformType `json:"platform"`
	Condition string       `json:"condition"`
	Action    string       `json:"action"`
	Priority  int          `json:"priority"`
}

func NewMessageRouter(aiService routerAIService, wsHub *WebSocketHub, db *gorm.DB) *MessageRouter {
	return &MessageRouter{
		platforms: make(map[string]PlatformAdapter),
		aiService: aiService,
		wsHub:     wsHub,
		db:        db,
	}
}

func (r *MessageRouter) RegisterPlatform(platformID string, adapter PlatformAdapter) {
	r.mutex.Lock()
	defer r.mutex.Unlock()

	r.platforms[platformID] = adapter
	logrus.Infof("Registered platform adapter: %s", platformID)
}

func (r *MessageRouter) UnregisterPlatform(platformID string) {
	r.mutex.Lock()
	defer r.mutex.Unlock()

	if adapter, exists := r.platforms[platformID]; exists {
		adapter.Stop()
		delete(r.platforms, platformID)
		logrus.Infof("Unregistered platform adapter: %s", platformID)
	}
}

func (r *MessageRouter) Start() error {
	r.mutex.RLock()
	defer r.mutex.RUnlock()

	for platformID, adapter := range r.platforms {
		go r.handlePlatformMessages(platformID, adapter)
		if err := adapter.Start(); err != nil {
			logrus.Errorf("Failed to start platform %s: %v", platformID, err)
			return err
		}
	}

	logrus.Info("Message router started")
	return nil
}

func (r *MessageRouter) Stop() error {
	r.mutex.RLock()
	defer r.mutex.RUnlock()

	for platformID, adapter := range r.platforms {
		if err := adapter.Stop(); err != nil {
			logrus.Errorf("Failed to stop platform %s: %v", platformID, err)
		}
	}

	logrus.Info("Message router stopped")
	return nil
}

func (r *MessageRouter) handlePlatformMessages(platformID string, adapter PlatformAdapter) {
	messageChan := adapter.ReceiveMessage()

	for message := range messageChan {
		logrus.Infof("Received message from platform %s: %s", platformID, message.Content)

		// 路由消息
		if err := r.routeMessage(platformID, message); err != nil {
			logrus.Errorf("Failed to route message: %v", err)
		}
	}
}

func (r *MessageRouter) routeMessage(platformID string, message UnifiedMessage) error {
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	// 1. 保存消息到数据库
	if err := r.persistMessage(message); err != nil {
		logrus.Warnf("Failed to persist message: %v", err)
		// 不影响消息处理流程，继续执行
	}

	// 2. 如果是 Web 平台，直接通过 WebSocket 处理
	if platformID == string(PlatformWeb) {
		return r.handleWebMessage(ctx, message)
	}

	// 3. 其他平台的消息处理
	return r.handleExternalPlatformMessage(ctx, platformID, message)
}

func (r *MessageRouter) handleWebMessage(ctx context.Context, message UnifiedMessage) error {
	// AI 处理消息
	aiResponse, err := r.aiService.ProcessQuery(ctx, message.Content, message.UserID)
	if err != nil {
		logrus.Errorf("AI processing failed: %v", err)
		return err
	}

	// 发送回复
	response := WebSocketMessage{
		Type: "ai-response",
		Data: map[string]interface{}{
			"content":    aiResponse.Content,
			"confidence": aiResponse.Confidence,
			"source":     aiResponse.Source,
		},
		SessionID: message.UserID,
		Timestamp: time.Now(),
	}

	r.wsHub.SendToSession(message.UserID, response)
	return nil
}

func (r *MessageRouter) handleExternalPlatformMessage(ctx context.Context, platformID string, message UnifiedMessage) error {
	// AI 处理消息
	aiResponse, err := r.aiService.ProcessQuery(ctx, message.Content, message.UserID)
	if err != nil {
		logrus.Errorf("AI processing failed: %v", err)
		return err
	}

	// 发送回复到原平台
	r.mutex.RLock()
	adapter, exists := r.platforms[platformID]
	r.mutex.RUnlock()

	if !exists {
		return fmt.Errorf("platform adapter not found: %s", platformID)
	}

	err = adapter.SendMessage(message.UserID, aiResponse.Content)
	if err != nil {
		return fmt.Errorf("failed to send message to platform %s: %w", platformID, err)
	}

	return nil
}

func (r *MessageRouter) BroadcastMessage(message UnifiedMessage) error {
	r.mutex.RLock()
	defer r.mutex.RUnlock()

	for platformID, adapter := range r.platforms {
		if err := adapter.SendMessage(message.UserID, message.Content); err != nil {
			logrus.Errorf("Failed to broadcast message to platform %s: %v", platformID, err)
		}
	}

	return nil
}

func (r *MessageRouter) GetPlatformStats() map[string]interface{} {
	r.mutex.RLock()
	defer r.mutex.RUnlock()

	stats := make(map[string]interface{})
	stats["total_platforms"] = len(r.platforms)
	stats["active_platforms"] = make([]string, 0, len(r.platforms))

	for platformID := range r.platforms {
		stats["active_platforms"] = append(stats["active_platforms"].([]string), platformID)
	}

	return stats
}

// persistMessage 消息持久化
func (r *MessageRouter) persistMessage(message UnifiedMessage) error {
	// 如果未配置数据库，回退为日志
	if r.db == nil {
		logrus.WithFields(logrus.Fields{
			"message_id":  message.ID,
			"platform_id": message.PlatformID,
			"user_id":     message.UserID,
			"type":        message.Type,
			"timestamp":   message.Timestamp,
		}).Info("Message persisted (log-only)")
		return nil
	}

	// 确保会话存在（以 message.UserID 作为会话标识；为空则创建新会话）
	sid := message.UserID
	if sid == "" {
		sid = uuid.NewString()
	}
	tenantID, workspaceID := routerScopeFromMetadata(message.Metadata)
	sessionTenantID, sessionWorkspaceID, err := r.ensureSession(sid, message.PlatformID, tenantID, workspaceID)
	if err != nil {
		return fmt.Errorf("ensure session: %w", err)
	}
	if tenantID == "" {
		tenantID = sessionTenantID
	}
	if workspaceID == "" {
		workspaceID = sessionWorkspaceID
	}

	// 映射到持久化模型
	m := &models.Message{
		TenantID:    tenantID,
		WorkspaceID: workspaceID,
		SessionID:   sid,
		UserID:      0, // 未绑定用户ID时留空
		Content:     message.Content,
		Type:        string(message.Type),
		Sender:      "user",
		CreatedAt:   time.Now(),
	}

	if err := r.db.Create(m).Error; err != nil {
		return fmt.Errorf("persist message: %w", err)
	}
	logrus.WithField("id", m.ID).Debug("Message stored")
	return nil
}

// ensureSession 确保会话记录存在
func (r *MessageRouter) ensureSession(sessionID string, platform string, tenantID string, workspaceID string) (string, string, error) {
	if r.db == nil || sessionID == "" {
		return tenantID, workspaceID, nil
	}
	var s models.Session
	if err := r.db.First(&s, "id = ?", sessionID).Error; err != nil {
		if err == gorm.ErrRecordNotFound {
			s = models.Session{
				ID:          sessionID,
				TenantID:    tenantID,
				WorkspaceID: workspaceID,
				Status:      "active",
				Platform:    platform,
				StartedAt:   time.Now(),
				CreatedAt:   time.Now(),
				UpdatedAt:   time.Now(),
			}
			if err := r.db.Create(&s).Error; err != nil {
				return "", "", fmt.Errorf("create session: %w", err)
			}
			return s.TenantID, s.WorkspaceID, nil
		}
		return "", "", err
	}
	if tenantID != "" && s.TenantID != "" && s.TenantID != tenantID {
		return "", "", fmt.Errorf("session %s tenant scope mismatch", sessionID)
	}
	if workspaceID != "" && s.WorkspaceID != "" && s.WorkspaceID != workspaceID {
		return "", "", fmt.Errorf("session %s workspace scope mismatch", sessionID)
	}
	if (s.TenantID == "" && tenantID != "") || (s.WorkspaceID == "" && workspaceID != "") {
		updates := map[string]interface{}{"updated_at": time.Now()}
		if s.TenantID == "" && tenantID != "" {
			updates["tenant_id"] = tenantID
		}
		if s.WorkspaceID == "" && workspaceID != "" {
			updates["workspace_id"] = workspaceID
		}
		result := r.db.Model(&models.Session{}).Where("id = ?", sessionID).Updates(updates)
		if result.Error != nil {
			return "", "", fmt.Errorf("update session scope: %w", result.Error)
		}
		if result.RowsAffected == 0 {
			return "", "", fmt.Errorf("update session scope: %w", gorm.ErrRecordNotFound)
		}
		if s.TenantID == "" && tenantID != "" {
			s.TenantID = tenantID
		}
		if s.WorkspaceID == "" && workspaceID != "" {
			s.WorkspaceID = workspaceID
		}
	}
	return s.TenantID, s.WorkspaceID, nil
}

func routerScopeFromMetadata(metadata map[string]interface{}) (string, string) {
	if metadata == nil {
		return "", ""
	}
	return metadataString(metadata, "tenant_id"), metadataString(metadata, "workspace_id")
}

func metadataString(metadata map[string]interface{}, key string) string {
	if metadata == nil {
		return ""
	}
	v, ok := metadata[key]
	if !ok || v == nil {
		return ""
	}
	switch val := v.(type) {
	case string:
		return val
	case fmt.Stringer:
		return val.String()
	default:
		return fmt.Sprint(val)
	}
}

// Telegram 适配器示例
type TelegramAdapter struct {
	botToken string
	chatID   string
	msgChan  chan UnifiedMessage
	stopChan chan struct{}
	// pollInterval 供测试注入毫秒级轮询；零值取默认 1 秒。
	pollInterval time.Duration
}

func NewTelegramAdapter(botToken, chatID string) *TelegramAdapter {
	return &TelegramAdapter{
		botToken: botToken,
		chatID:   chatID,
		msgChan:  make(chan UnifiedMessage, 100),
		stopChan: make(chan struct{}),
	}
}

func (t *TelegramAdapter) SendMessage(chatID, message string) error {
	// 实现 Telegram 消息发送
	logrus.Infof("Sending Telegram message to %s: %s", chatID, message)

	// 实际的 Telegram API 调用实现
	// 这里需要使用 Telegram Bot API
	// POST https://api.telegram.org/bot{token}/sendMessage
	// 参数: chat_id, text

	// 示例实现框架（需要安装 telegram-bot-api 库）
	/*
		url := fmt.Sprintf("https://api.telegram.org/bot%s/sendMessage", t.botToken)
		payload := map[string]interface{}{
			"chat_id": chatID,
			"text":    message,
		}

		// 发送 HTTP POST 请求
		if err := t.sendHTTPRequest(url, payload); err != nil {
			return fmt.Errorf("telegram API call failed: %w", err)
		}
	*/

	logrus.Debug("Telegram message sent successfully (stub implementation)")
	return nil
}

func (t *TelegramAdapter) ReceiveMessage() <-chan UnifiedMessage {
	return t.msgChan
}

func (t *TelegramAdapter) GetPlatformType() PlatformType {
	return PlatformTelegram
}

func (t *TelegramAdapter) Start() error {
	logrus.Info("Starting Telegram adapter")

	// 实现 Telegram webhook 或 polling
	// 这里实现长轮询获取消息的框架

	go func() {
		pollInterval := t.pollInterval
		if pollInterval <= 0 {
			pollInterval = 1 * time.Second
		}
		ticker := time.NewTicker(pollInterval)
		defer ticker.Stop()

		for {
			select {
			case <-t.stopChan:
				return
			case <-ticker.C:
				// 模拟从 Telegram API 获取消息
				// 实际实现需要调用 getUpdates API
				/*
					url := fmt.Sprintf("https://api.telegram.org/bot%s/getUpdates", t.botToken)
					updates, err := t.getUpdates(url)
					if err != nil {
						logrus.Errorf("Failed to get Telegram updates: %v", err)
						continue
					}

					for _, update := range updates {
						message := UnifiedMessage{
							ID:          fmt.Sprintf("tg_%d", update.MessageID),
							PlatformID:  "telegram",
							UserID:      update.From.ID,
							Content:     update.Text,
							Type:        MessageTypeText,
							Timestamp:   time.Now(),
						}

						select {
						case t.msgChan <- message:
						default:
							logrus.Warn("Message channel full, dropping message")
						}
					}
				*/
			}
		}
	}()

	logrus.Info("Telegram polling started")
	return nil
}

func (t *TelegramAdapter) Stop() error {
	logrus.Info("Stopping Telegram adapter")
	close(t.stopChan)
	return nil
}

// 微信适配器示例
type WeChatAdapter struct {
	appID     string
	appSecret string
	msgChan   chan UnifiedMessage
	stopChan  chan struct{}
	// pollInterval 供测试注入毫秒级轮询；零值取默认 5 秒。
	pollInterval time.Duration
}

func NewWeChatAdapter(appID, appSecret string) *WeChatAdapter {
	return &WeChatAdapter{
		appID:     appID,
		appSecret: appSecret,
		msgChan:   make(chan UnifiedMessage, 100),
		stopChan:  make(chan struct{}),
	}
}

func (w *WeChatAdapter) SendMessage(chatID, message string) error {
	// 实现微信消息发送
	logrus.Infof("Sending WeChat message to %s: %s", chatID, message)

	// 实际的微信 API 调用实现
	// 这里需要使用微信公众号/企业微信 API
	// 需要先获取 access_token，然后发送消息

	// 示例实现框架
	/*
		// 1. 获取 access_token
		accessToken, err := w.getAccessToken()
		if err != nil {
			return fmt.Errorf("failed to get WeChat access token: %w", err)
		}

		// 2. 发送消息
		url := fmt.Sprintf("https://api.weixin.qq.com/cgi-bin/message/custom/send?access_token=%s", accessToken)
		payload := map[string]interface{}{
			"touser":  chatID,
			"msgtype": "text",
			"text": map[string]string{
				"content": message,
			},
		}

		if err := w.sendHTTPRequest(url, payload); err != nil {
			return fmt.Errorf("WeChat API call failed: %w", err)
		}
	*/

	logrus.Debug("WeChat message sent successfully (stub implementation)")
	return nil
}

func (w *WeChatAdapter) ReceiveMessage() <-chan UnifiedMessage {
	return w.msgChan
}

func (w *WeChatAdapter) GetPlatformType() PlatformType {
	return PlatformWeChat
}

func (w *WeChatAdapter) Start() error {
	logrus.Info("Starting WeChat adapter")

	// 实现微信消息接收
	// 这里可以通过 webhook 或 主动查询的方式

	go func() {
		pollInterval := w.pollInterval
		if pollInterval <= 0 {
			pollInterval = 5 * time.Second
		}
		ticker := time.NewTicker(pollInterval)
		defer ticker.Stop()

		for {
			select {
			case <-w.stopChan:
				return
			case <-ticker.C:
				// 模拟从微信服务器获取消息
				// 实际实现需要处理微信的消息推送或主动查询
				/*
					// 如果使用 webhook 模式，则在 HTTP 服务器中处理
					// 如果使用轮询模式，则在这里实现查询逻辑

					messages, err := w.getMessages()
					if err != nil {
						logrus.Errorf("Failed to get WeChat messages: %v", err)
						continue
					}

					for _, msg := range messages {
						message := UnifiedMessage{
							ID:          fmt.Sprintf("wx_%s", msg.MsgID),
							PlatformID:  "wechat",
							UserID:      msg.FromUserName,
							Content:     msg.Content,
							Type:        MessageTypeText,
							Timestamp:   time.Now(),
						}

						select {
						case w.msgChan <- message:
						default:
							logrus.Warn("Message channel full, dropping WeChat message")
						}
					}
				*/
			}
		}
	}()

	logrus.Info("WeChat message receiver started")
	return nil
}

func (w *WeChatAdapter) Stop() error {
	logrus.Info("Stopping WeChat adapter")
	close(w.stopChan)
	return nil
}
