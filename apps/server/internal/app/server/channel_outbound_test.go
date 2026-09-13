package server

import (
	"bytes"
	"context"
	"fmt"
	"io"
	"mime"
	"mime/quotedprintable"
	"strings"
	"sync"
	"testing"
	"time"

	"servify/apps/server/internal/models"
	conversationapp "servify/apps/server/internal/modules/conversation/application"
	emaildelivery "servify/apps/server/internal/modules/email/delivery"
	"servify/apps/server/internal/platform/eventbus"

	"github.com/glebarez/sqlite"
	"github.com/sirupsen/logrus"
	"gorm.io/gorm"
)

// recordingSMTPSender 记录投递报文；每次 Send 后通知（供异步断言等待）。
type recordingSMTPSender struct {
	mu       sync.Mutex
	messages [][]byte
	notify   chan struct{}
}

func newRecordingSMTPSender() *recordingSMTPSender {
	return &recordingSMTPSender{notify: make(chan struct{}, 16)}
}

func (s *recordingSMTPSender) Send(_ context.Context, _ string, _ []string, msg []byte) error {
	s.mu.Lock()
	s.messages = append(s.messages, msg)
	s.mu.Unlock()
	s.notify <- struct{}{}
	return nil
}

func (s *recordingSMTPSender) count() int {
	s.mu.Lock()
	defer s.mu.Unlock()
	return len(s.messages)
}

func (s *recordingSMTPSender) last() []byte {
	s.mu.Lock()
	defer s.mu.Unlock()
	if len(s.messages) == 0 {
		return nil
	}
	return s.messages[len(s.messages)-1]
}

func newOutboundTestEnv(t *testing.T) (*ChannelOutboundDispatcher, *eventbus.InMemoryBus, *recordingSMTPSender, *outboundTestDB) {
	t.Helper()
	db := newOutboundTestDB(t)
	sender := newRecordingSMTPSender()
	adapter := emaildelivery.NewAdapter(emaildelivery.AdapterDeps{
		SMTP:     sender,
		Ingestor: nil,
		From:     "support@example.com",
		Interval: time.Minute,
		Logger:   logrus.New(),
	})
	d := NewChannelOutboundDispatcher(db.DB, adapter, logrus.New())
	bus := eventbus.NewInMemoryBus()
	d.Register(bus)
	return d, bus, sender, db
}

func waitSent(t *testing.T, sender *recordingSMTPSender, want int) {
	t.Helper()
	deadline := time.After(2 * time.Second)
	for sender.count() < want {
		select {
		case <-sender.notify:
		case <-deadline:
			t.Fatalf("timed out waiting for %d sends, got %d", want, sender.count())
		}
	}
}

// decodedMail 把报文还原成「解码后的 Subject + 换行 + 解码后的正文」便于断言。
func decodedMail(t *testing.T, raw []byte) string {
	t.Helper()
	parts := bytes.SplitN(raw, []byte("\r\n\r\n"), 2)
	header := string(parts[0])
	body := ""
	if len(parts) == 2 {
		decoded, err := io.ReadAll(quotedprintable.NewReader(bytes.NewReader(parts[1])))
		if err != nil {
			t.Fatalf("decode quoted-printable body: %v", err)
		}
		body = string(decoded)
	}
	dec := &mime.WordDecoder{}
	var subject string
	for _, line := range strings.Split(header, "\r\n") {
		if strings.HasPrefix(line, "Subject:") {
			decoded, err := dec.DecodeHeader(strings.TrimSpace(strings.TrimPrefix(line, "Subject:")))
			if err != nil {
				t.Fatalf("decode subject: %v", err)
			}
			subject += decoded
		}
	}
	return subject + "\n" + body
}

func agentMessageEvent(conversationID, content string) conversationapp.ConversationEvent {
	return conversationapp.NewConversationEvent(
		conversationapp.ConversationMessageReceivedEventName,
		conversationID,
		conversationapp.ConversationMessageDTO{
			ID:             "m1",
			ConversationID: conversationID,
			Sender:         "agent",
			Kind:           "text",
			Content:        content,
			CreatedAt:      time.Now(),
		},
	)
}

func TestOutboundDispatch_AgentReplyOnEmailSession_Sends(t *testing.T) {
	_, bus, sender, db := newOutboundTestEnv(t)
	db.seedEmailSession(t, "sess-1", 11, "c11@example.com")

	bus.Publish(context.Background(), agentMessageEvent("sess-1", "您好，问题已解决"))
	waitSent(t, sender, 1)

	raw := sender.last()
	if !bytes.Contains(raw, []byte("To: c11@example.com")) {
		t.Fatalf("missing recipient header: %s", raw)
	}
	decoded := decodedMail(t, raw)
	if !strings.Contains(decoded, "客服会话 #sess-1 的新回复") {
		t.Fatalf("missing subject: %s", decoded)
	}
	if !strings.Contains(decoded, "您好，问题已解决") {
		t.Fatalf("missing body: %s", decoded)
	}
}

func TestOutboundDispatch_NonAgentOrNonEmail_Skips(t *testing.T) {
	_, bus, sender, db := newOutboundTestEnv(t)
	db.seedEmailSession(t, "sess-email", 11, "c11@example.com")
	db.seedSession(t, "sess-web", 12, "web")

	// 访客消息：不出站
	bus.Publish(context.Background(), conversationapp.NewConversationEvent(
		conversationapp.ConversationMessageReceivedEventName, "sess-email",
		conversationapp.ConversationMessageDTO{ConversationID: "sess-email", Sender: "user", Content: "咨询"},
	))
	// AI 回复：本期不外发
	bus.Publish(context.Background(), conversationapp.NewConversationEvent(
		conversationapp.ConversationMessageReceivedEventName, "sess-email",
		conversationapp.ConversationMessageDTO{ConversationID: "sess-email", Sender: "ai", Content: "自动回复"},
	))
	// email 坐席回复但会话是 web 渠道：不出站
	bus.Publish(context.Background(), agentMessageEvent("sess-web", "web 回复"))

	time.Sleep(200 * time.Millisecond)
	if got := sender.count(); got != 0 {
		t.Fatalf("expected no sends, got %d", got)
	}
}

func TestOutboundDispatch_MissingEmail_Skips(t *testing.T) {
	_, bus, sender, db := newOutboundTestEnv(t)
	db.seedEmailSession(t, "sess-noemail", 13, "")

	bus.Publish(context.Background(), agentMessageEvent("sess-noemail", "回复"))
	time.Sleep(200 * time.Millisecond)
	if got := sender.count(); got != 0 {
		t.Fatalf("expected no sends without email, got %d", got)
	}
}

func TestOutboundDispatch_DuplicateEvent_SendsOnce(t *testing.T) {
	_, bus, sender, db := newOutboundTestEnv(t)
	db.seedEmailSession(t, "sess-dup", 11, "c11@example.com")

	evt := agentMessageEvent("sess-dup", "仅一次")
	// 同一事件重复投递（redis 重连重读场景）
	_ = bus.Publish(context.Background(), evt)
	waitSent(t, sender, 1)
	_ = bus.Publish(context.Background(), evt)

	time.Sleep(200 * time.Millisecond)
	if got := sender.count(); got != 1 {
		t.Fatalf("duplicate event must send once, got %d", got)
	}
}

func TestOutboundDispatch_FallbackQueryFromAggregateID(t *testing.T) {
	db := newOutboundTestDB(t)
	sender := newRecordingSMTPSender()
	adapter := emaildelivery.NewAdapter(emaildelivery.AdapterDeps{
		SMTP:     sender,
		Ingestor: nil,
		From:     "support@example.com",
		Interval: time.Minute,
		Logger:   logrus.New(),
	})
	d := NewChannelOutboundDispatcher(db.DB, adapter, logrus.New())
	db.seedEmailSession(t, "sess-fb", 11, "c11@example.com")
	// 模拟 redis bus：事件只透传 AggregateID，回查 messages 表最新 agent 消息
	if err := db.DB.Create(&models.Message{
		TenantID: "t", WorkspaceID: "w", SessionID: "sess-fb",
		UserID: 11, Content: "回查的坐席回复", Type: "text", Sender: "agent",
		CreatedAt: time.Now(),
	}).Error; err != nil {
		t.Fatalf("seed message: %v", err)
	}

	d.dispatch(context.Background(), &aggregateOnlyEvent{
		id:          "evt-fb",
		name:        conversationapp.ConversationMessageReceivedEventName,
		aggregateID: "conversation:sess-fb",
	})
	waitSent(t, sender, 1)
	if !strings.Contains(decodedMail(t, sender.last()), "回查的坐席回复") {
		t.Fatalf("fallback body mismatch: %s", sender.last())
	}
}

// aggregateOnlyEvent 模拟 redis bus 的 streamEvent（只有接口元数据、无 payload）。
type aggregateOnlyEvent struct {
	id          string
	name        string
	aggregateID string
}

func (e *aggregateOnlyEvent) ID() string            { return e.id }
func (e *aggregateOnlyEvent) Name() string          { return e.name }
func (e *aggregateOnlyEvent) OccurredAt() time.Time { return time.Now() }
func (e *aggregateOnlyEvent) TenantID() string      { return "" }
func (e *aggregateOnlyEvent) AggregateID() string   { return e.aggregateID }

// ---- 测试 DB 辅助 ----

type outboundTestDB struct{ DB *gorm.DB }

func newOutboundTestDB(t *testing.T) *outboundTestDB {
	t.Helper()
	db, err := gorm.Open(sqlite.Open(uniqueMemDSN("file:channel_outbound_"+strings.ReplaceAll(t.Name(), "/", "_"))), &gorm.Config{})
	if err != nil {
		t.Fatalf("open sqlite: %v", err)
	}
	if err := db.AutoMigrate(&models.Session{}, &models.User{}, &models.Message{}); err != nil {
		t.Fatalf("auto migrate: %v", err)
	}
	return &outboundTestDB{DB: db}
}

func (w *outboundTestDB) seedSession(t *testing.T, id string, userID uint, platform string) {
	t.Helper()
	now := time.Now()
	if err := w.DB.Create(&models.User{ID: userID, Username: fmt.Sprintf("customer-%d", userID), Email: ""}).Error; err != nil {
		t.Fatalf("seed user: %v", err)
	}
	if err := w.DB.Create(&models.Session{
		ID: id, TenantID: "t", WorkspaceID: "w", UserID: userID,
		Status: "active", Platform: platform, StartedAt: now, CreatedAt: now, UpdatedAt: now,
	}).Error; err != nil {
		t.Fatalf("seed session: %v", err)
	}
}

// seedEmailSession 建 email 渠道会话与归属用户（email 允许为空）。
func (w *outboundTestDB) seedEmailSession(t *testing.T, id string, userID uint, email string) {
	t.Helper()
	w.seedSession(t, id, userID, "email")
	if email != "" {
		if err := w.DB.Model(&models.User{}).Where("id = ?", userID).Update("email", email).Error; err != nil {
			t.Fatalf("seed email: %v", err)
		}
	}
}
