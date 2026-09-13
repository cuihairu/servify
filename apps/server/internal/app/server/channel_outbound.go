package server

import (
	"context"
	"fmt"
	"strings"
	"sync"

	"servify/apps/server/internal/models"
	conversationapp "servify/apps/server/internal/modules/conversation/application"
	emaildelivery "servify/apps/server/internal/modules/email/delivery"
	"servify/apps/server/internal/platform/channel"
	"servify/apps/server/internal/platform/eventbus"

	"github.com/sirupsen/logrus"
	"gorm.io/gorm"
)

// outboundSeen 环形去重：bus 重复投递（redis 重连重读等）不重复发邮件。
type outboundSeen struct {
	mu   sync.Mutex
	ids  map[string]struct{}
	ring []string
	next int
}

func newOutboundSeen(capacity int) *outboundSeen {
	return &outboundSeen{
		ids:  make(map[string]struct{}, capacity),
		ring: make([]string, capacity),
	}
}

func (s *outboundSeen) seenBefore(id string) bool {
	if id == "" {
		return false
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	if _, ok := s.ids[id]; ok {
		return true
	}
	if old := s.ring[s.next]; old != "" {
		delete(s.ids, old)
	}
	s.ring[s.next] = id
	s.next = (s.next + 1) % len(s.ring)
	s.ids[id] = struct{}{}
	return false
}

// ChannelOutboundDispatcher 把坐席回复按会话渠道出站：订阅
// conversation.message_received，sender=agent 且会话来自 email 渠道时经
// email 适配器回发访客邮箱。AI 回复本期不外发（AI 首答仅 web，防重复触达）。
//
// bus 的 handler 在发布方 goroutine 内同步执行，而 SMTP 投递可达秒级，
// 因此真实发送放到独立 goroutine（失败仅记日志，不阻塞消息主链路）。
// payload 获取双路径：inmemory bus 直接断言事件对象；redis bus 只透传
// AggregateID，回查 messages 表取该会话最新一条坐席回复（webhook 同款）。
type ChannelOutboundDispatcher struct {
	db     *gorm.DB
	email  *emaildelivery.Adapter
	logger *logrus.Logger
	seen   *outboundSeen
}

func NewChannelOutboundDispatcher(db *gorm.DB, email *emaildelivery.Adapter, logger *logrus.Logger) *ChannelOutboundDispatcher {
	if logger == nil {
		logger = logrus.StandardLogger()
	}
	return &ChannelOutboundDispatcher{db: db, email: email, logger: logger, seen: newOutboundSeen(4096)}
}

// Register 订阅会话消息事件；db/email 未就绪时为无操作。
func (d *ChannelOutboundDispatcher) Register(bus eventbus.Bus) {
	if bus == nil || d == nil || d.db == nil || d.email == nil {
		return
	}
	bus.Subscribe(conversationapp.ConversationMessageReceivedEventName,
		eventbus.HandlerFunc(func(ctx context.Context, evt eventbus.Event) error {
			d.dispatch(ctx, evt)
			return nil
		}))
}

func (d *ChannelOutboundDispatcher) dispatch(ctx context.Context, evt eventbus.Event) {
	conversationID, msg, ok := d.resolveMessage(evt)
	if !ok || msg.Sender != "agent" || strings.TrimSpace(msg.Content) == "" {
		return
	}
	if d.seen.seenBefore(evt.ID()) {
		return
	}

	var session models.Session
	if err := d.db.WithContext(ctx).First(&session, "id = ?", conversationID).Error; err != nil {
		return // 会话不存在/已删：不出站
	}
	if !strings.EqualFold(strings.TrimSpace(session.Platform), "email") {
		return
	}

	var user models.User
	if err := d.db.WithContext(ctx).Select("id", "email").First(&user, session.UserID).Error; err != nil {
		d.logger.WithError(err).Warnf("outbound dispatch: session %s has no owner user", session.ID)
		return
	}
	recipient := strings.TrimSpace(user.Email)
	if recipient == "" {
		d.logger.Warnf("outbound dispatch: user %d has no email, skip outbound for session %s", user.ID, session.ID)
		return
	}

	outbound := channel.OutboundEvent{
		EventID:        evt.ID(),
		Channel:        "email",
		ConversationID: session.ID,
		TargetID:       recipient,
		Kind:           channel.EventKindMessage,
		Payload: map[string]interface{}{
			// Message-ID 未落库无法 threading，主题固定（不引用前序邮件）
			"subject": fmt.Sprintf("客服会话 #%s 的新回复", session.ID),
			"text":    msg.Content,
		},
		OccurredAt: msg.CreatedAt,
	}

	// 异步发送：不阻塞坐席回复的同步链路
	go func() {
		sendCtx, cancel := context.WithCancel(context.Background())
		defer cancel()
		if err := d.email.Send(sendCtx, outbound); err != nil {
			d.logger.WithError(err).Warnf("outbound dispatch: failed to send email reply for session %s to %s", session.ID, recipient)
		}
	}()
}

// resolveMessage 优先从事件对象取消息（inmemory bus 保真）；redis bus 只
// 透传 AggregateID，回查该会话最新一条 agent 消息近似还原。
func (d *ChannelOutboundDispatcher) resolveMessage(evt eventbus.Event) (string, conversationapp.ConversationMessageDTO, bool) {
	if convEvt, ok := evt.(conversationapp.ConversationEvent); ok {
		if msg, ok := convEvt.Payload.(conversationapp.ConversationMessageDTO); ok {
			return msg.ConversationID, msg, true
		}
		return "", conversationapp.ConversationMessageDTO{}, false
	}

	conversationID := strings.TrimPrefix(evt.AggregateID(), "conversation:")
	if conversationID == "" || conversationID == evt.AggregateID() {
		return "", conversationapp.ConversationMessageDTO{}, false
	}
	var row models.Message
	if err := d.db.WithContext(context.Background()).
		Where("session_id = ? AND sender = ?", conversationID, "agent").
		Order("created_at DESC, id DESC").
		First(&row).Error; err != nil {
		return "", conversationapp.ConversationMessageDTO{}, false
	}
	return conversationID, conversationapp.ConversationMessageDTO{
		ID:             fmt.Sprintf("%d", row.ID),
		ConversationID: row.SessionID,
		Sender:         row.Sender,
		Kind:           row.Type,
		Content:        row.Content,
		CreatedAt:      row.CreatedAt,
	}, true
}
