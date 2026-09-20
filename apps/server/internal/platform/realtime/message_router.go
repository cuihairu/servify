package realtime

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
