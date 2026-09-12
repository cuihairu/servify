package delivery

import (
	"context"
	"errors"
	"testing"

	conversationapp "servify/apps/server/internal/modules/conversation/application"
	conversationdomain "servify/apps/server/internal/modules/conversation/domain"
	"servify/apps/server/internal/platform/channel"

	"gorm.io/gorm"
)

// stubIngestor 记录调用序列；resumeByDefault 模拟「会话不存在」。
type stubIngestor struct {
	existing map[string]bool // conversationID -> 已存在
	created  []conversationapp.CreateConversationCommand
	ingested []conversationapp.IngestTextMessageCommand
	createOn bool
}

func newStubIngestor() *stubIngestor {
	return &stubIngestor{existing: map[string]bool{}, createOn: true}
}

func (s *stubIngestor) ResumeConversation(ctx context.Context, query conversationapp.ResumeConversationQuery) (*conversationapp.ConversationDTO, error) {
	if s.existing[query.ConversationID] {
		return &conversationapp.ConversationDTO{ID: query.ConversationID}, nil
	}
	return nil, gorm.ErrRecordNotFound
}

func (s *stubIngestor) CreateConversation(ctx context.Context, cmd conversationapp.CreateConversationCommand) (*conversationapp.ConversationDTO, error) {
	if !s.createOn {
		return nil, errors.New("create disabled")
	}
	s.created = append(s.created, cmd)
	s.existing[cmd.ConversationID] = true
	return &conversationapp.ConversationDTO{ID: cmd.ConversationID}, nil
}

func (s *stubIngestor) IngestTextMessage(ctx context.Context, cmd conversationapp.IngestTextMessageCommand) (*conversationapp.ConversationMessageDTO, error) {
	s.ingested = append(s.ingested, cmd)
	return &conversationapp.ConversationMessageDTO{}, nil
}

func emailEvent(from, subject, text, messageID string) channel.InboundEvent {
	return channel.InboundEvent{
		EventID: "email-" + messageID,
		Channel: "email",
		Kind:    channel.EventKindMessage,
		Payload: map[string]interface{}{
			"from": from, "subject": subject, "text": text, "message_id": messageID,
		},
	}
}

func TestConversationBridgeCreatesOnFirstMail(t *testing.T) {
	ingestor := newStubIngestor()
	bridge := NewConversationBridge(ingestor)

	from := "Customer@Example.com "
	wantID := ConversationIDForAddress(from)
	ev := emailEvent("Customer@Example.com ", "Invoice 42", "where is my invoice?", "<m1@x>")
	if err := bridge.IngestInbound(context.Background(), ev); err != nil {
		t.Fatalf("ingest: %v", err)
	}
	if len(ingestor.created) != 1 {
		t.Fatalf("expected 1 create, got %d", len(ingestor.created))
	}
	created := ingestor.created[0]
	if created.ConversationID != wantID {
		t.Fatalf("conversation id = %q, want %q", created.ConversationID, wantID)
	}
	if created.Channel.Channel != "email" || created.Channel.ExternalID != "Customer@Example.com" {
		t.Fatalf("channel binding mismatch: %+v", created.Channel)
	}
	if len(ingestor.ingested) != 1 {
		t.Fatalf("expected 1 ingest, got %d", len(ingestor.ingested))
	}
	msg := ingestor.ingested[0]
	if msg.Sender != conversationdomain.ParticipantRoleCustomer {
		t.Fatalf("sender must be customer, got %v", msg.Sender)
	}
	if msg.Metadata["source"] != "email" || msg.Metadata["message_id"] != "<m1@x>" {
		t.Fatalf("metadata mismatch: %+v", msg.Metadata)
	}
}

func TestConversationBridgeResumesExistingConversation(t *testing.T) {
	ingestor := newStubIngestor()
	bridge := NewConversationBridge(ingestor)
	id := ConversationIDForAddress("alice@example.com")
	ingestor.existing[id] = true

	if err := bridge.IngestInbound(context.Background(), emailEvent("alice@example.com", "s", "t", "<m2@x>")); err != nil {
		t.Fatalf("ingest: %v", err)
	}
	if len(ingestor.created) != 0 {
		t.Fatalf("existing conversation must not be recreated, got %d creates", len(ingestor.created))
	}
	if len(ingestor.ingested) != 1 {
		t.Fatalf("expected 1 ingest, got %d", len(ingestor.ingested))
	}
}

func TestConversationBridgeMergesByAddress(t *testing.T) {
	ingestor := newStubIngestor()
	bridge := NewConversationBridge(ingestor)

	mails := []struct{ from, mid string }{
		{"Bob@example.com", "<b1@x>"},
		{"bob@example.com", "<b2@x>"},
		{"  bob@example.com  ", "<b3@x>"},
	}
	for _, m := range mails {
		if err := bridge.IngestInbound(context.Background(), emailEvent(m.from, "s", "t", m.mid)); err != nil {
			t.Fatalf("ingest %s: %v", m.mid, err)
		}
	}
	if len(ingestor.created) != 1 {
		t.Fatalf("same address variants must merge into one conversation, got %d creates", len(ingestor.created))
	}
	if len(ingestor.ingested) != 3 {
		t.Fatalf("all three mails must ingest, got %d", len(ingestor.ingested))
	}
	first := ingestor.ingested[0].ConversationID
	for _, cmd := range ingestor.ingested {
		if cmd.ConversationID != first {
			t.Fatalf("conversation id drifted: %q vs %q", cmd.ConversationID, first)
		}
	}
}

func TestConversationBridgeRequiresFrom(t *testing.T) {
	bridge := NewConversationBridge(newStubIngestor())
	ev := channel.InboundEvent{Channel: "email", Kind: channel.EventKindMessage, Payload: map[string]interface{}{"text": "hi"}}
	if err := bridge.IngestInbound(context.Background(), ev); err == nil {
		t.Fatal("missing from must error")
	}
}

func TestConversationIDForAddressStable(t *testing.T) {
	a := ConversationIDForAddress("user@example.com")
	b := ConversationIDForAddress("USER@EXAMPLE.COM")
	if a != b || len(a) != len("email-")+16 {
		t.Fatalf("hash key mismatch: %q vs %q", a, b)
	}
	if a == ConversationIDForAddress("other@example.com") {
		t.Fatal("different addresses must not collide")
	}
}

// 编译期保证生产 conversation service 满足 bridge 依赖接口。
var _ ConversationIngestor = (*conversationapp.Service)(nil)
