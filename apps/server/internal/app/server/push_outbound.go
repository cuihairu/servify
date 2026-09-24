package server

import (
	"context"
	"fmt"
	"strings"

	"servify/apps/server/internal/models"
	conversationapp "servify/apps/server/internal/modules/conversation/application"
	"servify/apps/server/internal/platform/eventbus"

	"github.com/sirupsen/logrus"
	"gorm.io/gorm"
)

// PushOutboundDispatcher 把坐席回复推给离线访客的移动端：订阅
// conversation.message_received，sender=agent 且会话有推送注册行
// （push_tokens，迁移 000012）时按平台通道发送（ios=APNs / android=FCM）。
// AI 回复不外推（与 email 出站同口径，防重复触达）。
//
// 前台抑制：访客 WS 在线（hub IsSessionConnected）时跳过——正在聊天时
// 弹系统推送是体验倒退；下线/切后台才补推。
//
// bus 的 handler 在发布方 goroutine 内同步执行，推送出站可达秒级，
// 真实发送放独立 goroutine（失败仅记日志，不阻塞消息主链路）。
// payload 双路径与 ChannelOutboundDispatcher 同构：inmemory bus 直接断言
// 事件对象；redis bus 只透传 AggregateID，回查 messages 表。
// 去重复用 outboundSeen（EventID 环形窗口）。
// pushSender 是平台通道的窄接口（FCM/APNs 客户端天然满足；测试注入记录型假体）。
type pushSender interface {
	Send(ctx context.Context, deviceToken, title, body string, data map[string]string) error
}

// pushPresence 是访客在线判定的窄接口（WebSocketHub 满足；测试注入可控假体）。
type pushPresence interface {
	IsSessionConnected(sessionID string) bool
}

type PushOutboundDispatcher struct {
	db       *gorm.DB
	presence pushPresence
	fcm      pushSender // android 通道
	apns     pushSender // ios 通道
	logger   *logrus.Logger
	seen     *outboundSeen
}

// NewPushOutboundDispatcher 构建推送出站编排；fcm/apns 按凭证可用性各自
// 可为 nil（对应平台跳过），两者皆 nil 时 Register 为无操作。
func NewPushOutboundDispatcher(db *gorm.DB, presence pushPresence,
	fcm pushSender, apns pushSender, logger *logrus.Logger) *PushOutboundDispatcher {
	if logger == nil {
		logger = logrus.StandardLogger()
	}
	return &PushOutboundDispatcher{db: db, presence: presence, fcm: fcm, apns: apns, logger: logger, seen: newOutboundSeen(4096)}
}

// Register 订阅会话消息事件；无可用通道（db/双通道皆缺）时为无操作。
func (d *PushOutboundDispatcher) Register(bus eventbus.Bus) {
	if bus == nil || d == nil || d.db == nil || (d.fcm == nil && d.apns == nil) {
		return
	}
	bus.Subscribe(conversationapp.ConversationMessageReceivedEventName,
		eventbus.HandlerFunc(func(ctx context.Context, evt eventbus.Event) error {
			d.dispatch(ctx, evt)
			return nil
		}))
}

func (d *PushOutboundDispatcher) dispatch(ctx context.Context, evt eventbus.Event) {
	sessionID, msg, ok := d.resolveMessage(evt)
	if !ok || msg.Sender != "agent" || strings.TrimSpace(msg.Content) == "" {
		return
	}
	if d.seen.seenBefore(evt.ID()) {
		return
	}
	// 前台抑制：访客 WS 在线不推；在线面未装配（presence==nil）时同样
	// 跳过——没有抑制依据就不出站，宁可漏推不误推。
	if d.presence == nil || d.presence.IsSessionConnected(sessionID) {
		return
	}

	var tokens []models.PushToken
	if err := d.db.WithContext(ctx).
		Where("session_id = ?", sessionID).
		Find(&tokens).Error; err != nil || len(tokens) == 0 {
		return // 无注册行/查询失败：不出站
	}

	summary := msg.Content
	if len(summary) > 120 {
		summary = summary[:120]
	}
	title := "您有新的客服回复"
	data := map[string]string{"session_id": sessionID, "message_id": msg.ID}

	for _, row := range tokens {
		platformToken := strings.TrimSpace(row.Token)
		if platformToken == "" {
			continue
		}
		var sender pushSender
		switch row.Platform {
		case "android":
			sender = d.fcm
		case "ios":
			sender = d.apns
		}
		if sender == nil {
			continue // 未知 platform（注册白名单外残留行）：不出站
		}
		if err := sender.Send(ctx, platformToken, title, summary, data); err != nil {
			// 异步发送失败只记日志：推送可丢失（APNs/FCM 网关自身有重试语义）
			d.logger.WithError(err).Warnf("push outbound: platform=%s session=%s", row.Platform, sessionID)
		}
	}
}

// resolveMessage 与 ChannelOutboundDispatcher.resolveMessage 同构：优先事件
// 对象取消息（inmemory bus 保真）；redis bus 只透传 AggregateID，回查该会话
// 最新一条 agent 消息近似还原。
func (d *PushOutboundDispatcher) resolveMessage(evt eventbus.Event) (string, conversationapp.ConversationMessageDTO, bool) {
	if convEvt, ok := evt.(conversationapp.ConversationEvent); ok {
		if msg, ok := convEvt.Payload.(conversationapp.ConversationMessageDTO); ok {
			return msg.ConversationID, msg, true
		}
		return "", conversationapp.ConversationMessageDTO{}, false
	}
	sessionID := strings.TrimPrefix(evt.AggregateID(), "conversation:")
	if sessionID == "" || sessionID == evt.AggregateID() {
		return "", conversationapp.ConversationMessageDTO{}, false
	}
	var row models.Message
	if err := d.db.WithContext(context.Background()).
		Where("session_id = ? AND sender = ?", sessionID, "agent").
		Order("created_at DESC, id DESC").
		First(&row).Error; err != nil {
		return "", conversationapp.ConversationMessageDTO{}, false
	}
	return sessionID, conversationapp.ConversationMessageDTO{
		ID:             fmt.Sprintf("%d", row.ID),
		ConversationID: row.SessionID,
		Sender:         row.Sender,
		Kind:           row.Type,
		Content:        row.Content,
		CreatedAt:      row.CreatedAt,
	}, true
}
