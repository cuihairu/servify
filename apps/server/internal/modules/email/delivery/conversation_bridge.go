package delivery

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"strings"

	conversationapp "servify/apps/server/internal/modules/conversation/application"
	conversationdomain "servify/apps/server/internal/modules/conversation/domain"
	"servify/apps/server/internal/platform/channel"

	"gorm.io/gorm"
)

// ConversationIngestor 是 conversation 模块为 email 渠道暴露的最小写面
// （由 *conversationapp.Service 满足）。
type ConversationIngestor interface {
	ResumeConversation(ctx context.Context, query conversationapp.ResumeConversationQuery) (*conversationapp.ConversationDTO, error)
	CreateConversation(ctx context.Context, cmd conversationapp.CreateConversationCommand) (*conversationapp.ConversationDTO, error)
	IngestTextMessage(ctx context.Context, cmd conversationapp.IngestTextMessageCommand) (*conversationapp.ConversationMessageDTO, error)
}

// ConversationBridge 把入站邮件归并为 conversation 会话：
// 同一发件人地址始终落到同一 conversationID（哈希归并键，跨重启稳定）。
type ConversationBridge struct {
	ingestor ConversationIngestor
}

func NewConversationBridge(ingestor ConversationIngestor) *ConversationBridge {
	return &ConversationBridge{ingestor: ingestor}
}

// ConversationIDForAddress 归并键：sha256(lower(address)) 前 16 个 hex 字符。
// 不落库——同一地址哈希确定，天然幂等。
func ConversationIDForAddress(address string) string {
	sum := sha256.Sum256([]byte(strings.ToLower(strings.TrimSpace(address))))
	return "email-" + hex.EncodeToString(sum[:])[:16]
}

// IngestInbound resume-or-create 会话并把邮件正文写为客户消息。
func (b *ConversationBridge) IngestInbound(ctx context.Context, ev channel.InboundEvent) error {
	from, _ := ev.Payload["from"].(string)
	from = strings.TrimSpace(from)
	subject, _ := ev.Payload["subject"].(string)
	text, _ := ev.Payload["text"].(string)
	messageID, _ := ev.Payload["message_id"].(string)
	if from == "" {
		return errors.New("email ingest: missing from address")
	}
	conversationID := ConversationIDForAddress(from)
	if _, err := b.ingestor.ResumeConversation(ctx, conversationapp.ResumeConversationQuery{
		ConversationID: conversationID,
	}); err != nil {
		if !errors.Is(err, gorm.ErrRecordNotFound) {
			return err
		}
		if _, err := b.ingestor.CreateConversation(ctx, conversationapp.CreateConversationCommand{
			ConversationID: conversationID,
			Subject:        subject,
			Channel: conversationdomain.ChannelBinding{
				Channel:    "email",
				ExternalID: from,
				SessionID:  conversationID,
			},
		}); err != nil {
			return err
		}
	}
	_, err := b.ingestor.IngestTextMessage(ctx, conversationapp.IngestTextMessageCommand{
		ConversationID: conversationID,
		MessageID:      ev.EventID,
		Sender:         conversationdomain.ParticipantRoleCustomer,
		Content:        text,
		Metadata: map[string]string{
			"source":     "email",
			"from":       from,
			"subject":    subject,
			"message_id": messageID,
		},
	})
	return err
}
