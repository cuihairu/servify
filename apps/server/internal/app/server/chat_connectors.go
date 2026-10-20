package server

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"net/http"
	"os"
	"strings"
	"time"

	conversationapp "servify/apps/server/internal/modules/conversation/application"
	conversationdomain "servify/apps/server/internal/modules/conversation/domain"
	platformauth "servify/apps/server/internal/platform/auth"
	"servify/apps/server/internal/platform/channel"
	"servify/apps/server/internal/platform/channel/chatconnector"
	"servify/apps/server/internal/platform/channel/memex"
	"servify/apps/server/internal/platform/channel/telegram"

	"github.com/sirupsen/logrus"
	"gorm.io/gorm"
)

// platformMemex and platformTelegram are the adapter keys a stored
// installation row's Platform may select in wireChatConnectors. Anything else
// fails that row's attachment (no silent fallback adapter).
const (
	platformMemex    = "memex"
	platformTelegram = "telegram"
)

// The raw-payload retention knobs (contract: encrypted on local storage, 30
// days maximum, payload bytes out of routine DB backups — hence a filesystem
// store, not a table). The key is 64 hex chars (AES-256); an unset key keeps
// retention off, an invalid key disables it loudly. The directory mirrors the
// upload storage convention of a relative default path.
const (
	payloadKeyEnv     = "SERVIFY_CHAT_PAYLOAD_KEY"
	payloadDirEnv     = "SERVIFY_CHAT_PAYLOAD_DIR"
	defaultPayloadDir = "./chat-payloads"
)

// chatPayloadStoreFromEnv builds the retention store from the environment.
// getenv is a seam so tests can drive every branch; a nil logger just skips
// the warning (assembly tests construct runtimes without loggers).
func chatPayloadStoreFromEnv(getenv func(string) string, logger *logrus.Logger) *channel.LocalPayloadStore {
	keyHex := strings.TrimSpace(getenv(payloadKeyEnv))
	if keyHex == "" {
		return nil
	}
	key, err := hex.DecodeString(keyHex)
	if err != nil {
		if logger != nil {
			logger.Warnf("chat payload retention disabled: %s is not valid hex: %v", payloadKeyEnv, err)
		}
		return nil
	}
	dir := strings.TrimSpace(getenv(payloadDirEnv))
	if dir == "" {
		dir = defaultPayloadDir
	}
	store, err := channel.NewLocalPayloadStore(dir, key, 0)
	if err != nil {
		if logger != nil {
			logger.Warnf("chat payload retention disabled: %v", err)
		}
		return nil
	}
	return store
}

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
// not die on the same event forever. The committed-but-unforwarded row stays
// in the replay lane (forwarded_at NULL) and is the replay tooling's input,
// not a reason to take the channel down. A settled forward is stamped on the
// row via the inbox store, which is what keeps the replay lane exact.
type chatConversationBridge struct {
	ingestor chatConversationIngestor
	inbox    *channel.InboxStore
	logger   *logrus.Logger
	now      func() time.Time
}

// InboundCommitted implements channel.InboundObserver.
func (b *chatConversationBridge) InboundCommitted(ctx context.Context, ev channel.ConnectorEvent) error {
	if err := b.forward(ctx, ev); err != nil {
		b.logIngestFailure(err, ev)
	}
	return nil
}

// forward drives one committed event through the normal flow and returns the
// failure instead of swallowing it — the replay tooling calls it directly so
// an operator sees why a row would not settle. On success the row is stamped
// forwarded, on failure it stays in the replay lane.
func (b *chatConversationBridge) forward(ctx context.Context, ev channel.ConnectorEvent) error {
	if b.ingestor == nil {
		return nil
	}
	if ev.Message == nil || ev.Message.Text == "" {
		// Nothing this bridge delivers; the row still leaves the replay lane.
		return b.settleForwarded(ctx, ev)
	}
	ctx = platformauth.ContextWithScope(ctx, ev.TenantID, ev.WorkspaceID)
	conversationID := ChatConversationID(ev.TenantID, ev.WorkspaceID, ev.Platform, ev.Conversation.PlatformID)
	if _, err := b.ingestor.ResumeConversation(ctx, conversationapp.ResumeConversationQuery{
		ConversationID: conversationID,
	}); err != nil {
		if !errors.Is(err, gorm.ErrRecordNotFound) {
			return err
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
			return err
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
		return err
	}
	return b.settleForwarded(ctx, ev)
}

// settleForwarded stamps forwarded_at on the row the event's idempotency key
// names. A stamp failure surfaces like an ingest failure: the row stays in the
// replay lane (a later replay would re-ingest at-least-once) and the failure
// is logged, never fatal.
func (b *chatConversationBridge) settleForwarded(ctx context.Context, ev channel.ConnectorEvent) error {
	if b.inbox == nil {
		return nil
	}
	now := b.now
	if now == nil {
		now = time.Now
	}
	return b.inbox.MarkForwardedByKey(ctx, ev.TenantID, ev.WorkspaceID, ev.IdempotencyKey, now())
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
// The row's platform selects the adapter (Phase 2 Memex, Phase 3 Telegram);
// the row's Enabled flag is the only gate (fail closed — no config switch);
// a row whose token env var is unset fails only its own attachment, and the
// runner's long poll budget comes from the row. The raw-payload retention
// store comes from the environment and attaches to every adapter (nil keeps
// retention off for the whole channel).
func wireChatConnectors(rt *Runtime, conversationService *conversationapp.Service) {
	if rt == nil || rt.DB == nil {
		return
	}
	installations := channel.NewInstallationStore(rt.DB)
	caps := channel.NewCapabilityStore(rt.DB)
	inbox := channel.NewInboxStore(rt.DB)
	cursors := channel.NewCursorStore(rt.DB)
	payloads := chatPayloadStoreFromEnv(os.Getenv, rt.Logger)
	rt.chatPayloads = payloads
	bridge := &chatConversationBridge{ingestor: conversationService, inbox: inbox, logger: rt.Logger}
	rt.chatReplay = &ChatInboxReplayer{
		inbox:    inbox,
		installs: installations,
		payloads: payloads,
		forward:  bridge.forward,
	}
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
			hc := &http.Client{Timeout: longPoll + 10*time.Second}
			var poller channel.Poller
			switch inst.Platform {
			case platformTelegram:
				poller = telegram.NewConnector(
					telegram.NewClient(inst.UpdatesEndpoint, token, hc),
					caps, inbox, cursors, telegramConfigFromInstallation(inst),
				).WithObserver(bridge).WithPayloadStore(payloads)
			case platformMemex:
				poller = memex.NewConnector(
					memex.NewClient(inst.UpdatesEndpoint, token, hc),
					caps, inbox, memexConfigFromInstallation(inst),
				).WithObserver(bridge).WithPayloadStore(payloads)
			default:
				return nil, fmt.Errorf("chat connector %s: platform %q has no adapter",
					inst.InstallationID, inst.Platform)
			}
			runner := channel.NewRunner(poller)
			runner.LongPoll = longPoll
			return runner, nil
		},
		rt.Logger)
}

// conversationRefFromInstallation extracts the configured conversation the
// stored installation row pins; both live polling and replay attribute
// conversation from the row, never from payload data.
func conversationRefFromInstallation(inst channel.ChatConnectorInstallation) channel.ConversationRef {
	return channel.ConversationRef{
		PlatformID: inst.ConversationPlatformID,
		Kind:       inst.ConversationKind,
		IsGroup:    inst.ConversationIsGroup,
	}
}

// telegramConfigFromInstallation is the translator scope the live poller and
// the replay tooling share for Telegram rows.
func telegramConfigFromInstallation(inst channel.ChatConnectorInstallation) telegram.Config {
	return telegram.Config{
		Platform:       inst.Platform,
		TenantID:       inst.TenantID,
		WorkspaceID:    inst.WorkspaceID,
		InstallationID: inst.InstallationID,
		Conversation:   conversationRefFromInstallation(inst),
	}
}

// memexConfigFromInstallation is the translator scope the live poller and the
// replay tooling share for Memex rows.
func memexConfigFromInstallation(inst channel.ChatConnectorInstallation) memex.Config {
	return memex.Config{
		Platform:       inst.Platform,
		TenantID:       inst.TenantID,
		WorkspaceID:    inst.WorkspaceID,
		InstallationID: inst.InstallationID,
		Conversation:   conversationRefFromInstallation(inst),
	}
}

func firstNonEmpty(values ...string) string {
	for _, v := range values {
		if v != "" {
			return v
		}
	}
	return ""
}
