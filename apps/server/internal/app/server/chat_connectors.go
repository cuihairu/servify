package server

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"net/http"
	"os"
	"time"

	conversationapp "servify/apps/server/internal/modules/conversation/application"
	conversationdomain "servify/apps/server/internal/modules/conversation/domain"
	platformauth "servify/apps/server/internal/platform/auth"
	"servify/apps/server/internal/platform/channel"
	"servify/apps/server/internal/platform/channel/chatconnector"
	"servify/apps/server/internal/platform/channel/memex"

	"github.com/sirupsen/logrus"
	"gorm.io/gorm"
)

// chatConversationIngestor is the minimal conversation-module write face the
// chat connector bridge needs (satisfied by *conversationapp.Service).
type chatConversationIngestor interface {
	ResumeConversation(ctx context.Context, query conversationapp.ResumeConversationQuery) (*conversationapp.ConversationDTO, error)
	CreateConversation(ctx context.Context, cmd conversationapp.CreateConversationCommand) (*conversationapp.ConversationDTO, error)
	IngestTextMessage(ctx context.Context, cmd conversationapp.IngestTextMessageCommand) (*conversationapp.ConversationMessageDTO, error)
}

// 编译期保证生产 conversation service 满足 bridge 依赖接口。
var _ chatConversationIngestor = (*conversationapp.Service)(nil)

// ChatConversationID 归并键：sha256(tenant + "|" + workspace + "|" + platform +
// "|" + conversationPlatformID) 前 16 个 hex 字符。输入全部来自存储的安装行
// （不含 payload 字段），同一安装映射的会话跨重启稳定；scope 入键是因为
// sessions.id 是主键——两个租户映射同一平台会话时必须是两条会话，不能撞主键。
func ChatConversationID(tenantID, workspaceID, platform, conversationPlatformID string) string {
	sum := sha256.Sum256([]byte(tenantID + "|" + workspaceID + "|" + platform + "|" + conversationPlatformID))
	return "chat-" + hex.EncodeToString(sum[:])[:16]
}

// chatConversationBridge forwards committed chat connector events into the
// conversation module: resume-or-create by the installation's conversation
// mapping, then ingest the text as a customer message. The tenant/workspace
// scope is stamped from the event — which the translator filled from the
// stored installation row — never from platform payload data.
//
// An ingest failure is logged and swallowed: the inbox row is already durable
// (without the text), so a redelivery cannot re-forward and the runner must
// not die on the same event forever. The committed-but-unforwarded row is the
// replay tooling's input, not a reason to take the channel down.
type chatConversationBridge struct {
	ingestor chatConversationIngestor
	logger   *logrus.Logger
}

// InboundCommitted implements memex.InboundObserver.
func (b *chatConversationBridge) InboundCommitted(ctx context.Context, ev channel.ConnectorEvent) error {
	if b.ingestor == nil {
		return nil
	}
	if ev.Message == nil || ev.Message.Text == "" {
		return nil
	}
	ctx = platformauth.ContextWithScope(ctx, ev.TenantID, ev.WorkspaceID)
	conversationID := ChatConversationID(ev.TenantID, ev.WorkspaceID, ev.Platform, ev.Conversation.PlatformID)
	if _, err := b.ingestor.ResumeConversation(ctx, conversationapp.ResumeConversationQuery{
		ConversationID: conversationID,
	}); err != nil {
		if !errors.Is(err, gorm.ErrRecordNotFound) {
			b.logIngestFailure(err, ev)
			return nil
		}
		if _, err := b.ingestor.CreateConversation(ctx, conversationapp.CreateConversationCommand{
			ConversationID: conversationID,
			Subject:        firstNonEmpty(ev.Conversation.Name, conversationID),
			Channel: conversationdomain.ChannelBinding{
				Channel:    ev.Platform,
				ExternalID: ev.Conversation.PlatformID,
				SessionID:  conversationID,
			},
		}); err != nil {
			b.logIngestFailure(err, ev)
			return nil
		}
	}
	if _, err := b.ingestor.IngestTextMessage(ctx, conversationapp.IngestTextMessageCommand{
		ConversationID: conversationID,
		MessageID:      ev.EventID,
		Sender:         conversationdomain.ParticipantRoleCustomer,
		Content:        ev.Message.Text,
		Metadata: map[string]string{
			"source":          ev.Platform,
			"installation_id": ev.InstallationID,
			"sender":          ev.Sender.PlatformID,
			"message_id":      ev.EventID,
		},
	}); err != nil {
		b.logIngestFailure(err, ev)
	}
	return nil
}

func (b *chatConversationBridge) logIngestFailure(err error, ev channel.ConnectorEvent) {
	logger := b.logger
	if logger == nil {
		logger = logrus.StandardLogger()
	}
	logger.WithError(err).WithFields(logrus.Fields{
		"platform":     ev.Platform,
		"installation": ev.InstallationID,
		"tenant_id":    ev.TenantID,
		"workspace_id": ev.WorkspaceID,
		"conversation": ev.Conversation.PlatformID,
		"message":      ev.EventID,
	}).Error("chat connector bridge: conversation ingest failed, row stays committed for replay")
}

// wireChatConnectors builds the chat connector supervisor over the stored
// installation rows: at startup it attaches one poll runner per enabled row.
// The row's Enabled flag is the only gate (fail closed — no config switch);
// a row whose token env var is unset fails only its own attachment, and the
// runner's long poll budget comes from the row.
func wireChatConnectors(rt *Runtime, conversationService *conversationapp.Service) {
	if rt == nil || rt.DB == nil {
		return
	}
	installations := channel.NewInstallationStore(rt.DB)
	caps := channel.NewCapabilityStore(rt.DB)
	inbox := channel.NewInboxStore(rt.DB)
	bridge := &chatConversationBridge{ingestor: conversationService, logger: rt.Logger}
	rt.chatConnectors = chatconnector.NewSupervisor(installations,
		func(ctx context.Context, inst channel.ChatConnectorInstallation) (chatconnector.Runner, error) {
			// credentials live in the environment only: the row names the
			// variable, never the value (contract: no credentials in git or
			// logs)
			token := os.Getenv(inst.TokenEnvVar)
			if token == "" {
				return nil, fmt.Errorf("chat connector %s: token env var %s is unset",
					inst.InstallationID, inst.TokenEnvVar)
			}
			longPoll := time.Duration(inst.LongPollMS) * time.Millisecond
			// the HTTP timeout must outlive the long poll itself, or every
			// fully-waited pull dies client-side
			client := memex.NewClient(inst.UpdatesEndpoint, token,
				&http.Client{Timeout: longPoll + 10*time.Second})
			connector := memex.NewConnector(client, caps, inbox, memex.Config{
				Platform:       inst.Platform,
				TenantID:       inst.TenantID,
				WorkspaceID:    inst.WorkspaceID,
				InstallationID: inst.InstallationID,
				Conversation: channel.ConversationRef{
					PlatformID: inst.ConversationPlatformID,
					Kind:       inst.ConversationKind,
					IsGroup:    inst.ConversationIsGroup,
				},
			}).WithObserver(bridge)
			runner := memex.NewRunner(connector)
			runner.LongPoll = longPoll
			return runner, nil
		},
		rt.Logger)
}

func firstNonEmpty(values ...string) string {
	for _, v := range values {
		if v != "" {
			return v
		}
	}
	return ""
}
