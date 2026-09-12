package delivery

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"sync"
	"time"

	emailinfra "servify/apps/server/internal/modules/email/infra"
	"servify/apps/server/internal/platform/channel"

	"github.com/sirupsen/logrus"
)

// Adapter 是 email 渠道的 channel.Adapter 实现：
// 入站 = IMAP 轮询经 ConversationBridge 归并；出站 = SMTP（本期无生产触发点）。
//
// 生产接线由 EmailPollWorker 周期调用 PollOnce；Receive 提供接口形态的流式消费
// （后台 goroutine 轮询 + 事件副本观察通道），两条驱动路径可共存（poller 内部互斥串行）。
type Adapter struct {
	name     string
	interval time.Duration
	poller   *MailPoller
	bridge   *ConversationBridge
	sender   emailinfra.SMTPSender
	from     string
	logger   *logrus.Logger

	mu      sync.Mutex
	observe chan channel.InboundEvent
}

var _ channel.Adapter = (*Adapter)(nil)

// AdapterDeps 是构建 email 渠道适配器的全部依赖。
type AdapterDeps struct {
	IMAP     emailinfra.IMAPClient
	SMTP     emailinfra.SMTPSender
	Ingestor ConversationIngestor
	// From 是出站发件人地址（空则出站报错）。
	From string
	// Interval 是轮询间隔；Receive 路径使用（poll worker 间隔在 worker 侧）。
	Interval time.Duration
	Logger   *logrus.Logger
}

func NewAdapter(deps AdapterDeps) *Adapter {
	if deps.Interval <= 0 {
		deps.Interval = time.Minute
	}
	if deps.Logger == nil {
		deps.Logger = logrus.StandardLogger()
	}
	a := &Adapter{
		name:     "email",
		interval: deps.Interval,
		bridge:   NewConversationBridge(deps.Ingestor),
		sender:   deps.SMTP,
		from:     deps.From,
		logger:   deps.Logger,
	}
	a.poller = NewMailPoller(deps.IMAP, "", a.ingestAndObserve, deps.Logger)
	return a
}

// ingestAndObserve 先归并进会话，再向 Receive 的观察通道非阻塞转发一份。
func (a *Adapter) ingestAndObserve(ctx context.Context, ev channel.InboundEvent) error {
	if err := a.bridge.IngestInbound(ctx, ev); err != nil {
		return err
	}
	a.mu.Lock()
	obs := a.observe
	a.mu.Unlock()
	if obs != nil {
		select {
		case obs <- ev:
		default: // 观察通道满不阻塞归并主路径
		}
	}
	return nil
}

// PollOnce 执行一轮 IMAP 轮询（poll worker 驱动入口）；返回 ingest 的邮件数。
func (a *Adapter) PollOnce(ctx context.Context) (int, error) {
	return a.poller.PollOnce(ctx)
}

// PollProcessor 是 poll worker 依赖的最小轮询面（由 *Adapter 满足）。
type PollProcessor interface {
	PollOnce(ctx context.Context) (int, error)
}

// Name 实现 channel.Adapter。
func (a *Adapter) Name() string { return a.name }

// Receive 实现 channel.Adapter：启动后台轮询 goroutine，返回已 ingest 事件的观察通道。
// ctx 取消后通道关闭。重复调用 Receive 会先停用旧通道。
func (a *Adapter) Receive(ctx context.Context) (<-chan channel.InboundEvent, error) {
	a.mu.Lock()
	if a.observe != nil {
		close(a.observe)
	}
	obs := make(chan channel.InboundEvent, 16)
	a.observe = obs
	a.mu.Unlock()

	go func() {
		defer func() {
			a.mu.Lock()
			if a.observe == obs {
				close(a.observe)
				a.observe = nil
			}
			a.mu.Unlock()
		}()
		for {
			select {
			case <-ctx.Done():
				return
			default:
			}
			if _, err := a.poller.PollOnce(ctx); err != nil {
				a.logger.Warnf("email adapter: poll: %v", err)
			}
			select {
			case <-ctx.Done():
				return
			case <-time.After(a.interval):
			}
		}
	}()
	return obs, nil
}

// Send 实现 channel.Adapter：经 SMTP 发送。
// TargetID 是收件人地址；Payload 可带 subject / text / in_reply_to。
func (a *Adapter) Send(ctx context.Context, ev channel.OutboundEvent) error {
	to, _ := ev.Payload["to"].(string)
	if to == "" {
		to = ev.TargetID
	}
	subject, _ := ev.Payload["subject"].(string)
	text, _ := ev.Payload["text"].(string)
	inReplyTo, _ := ev.Payload["in_reply_to"].(string)
	if strings.TrimSpace(to) == "" {
		return errors.New("email send: missing recipient")
	}
	if a.sender == nil {
		return errors.New("email send: smtp sender not configured")
	}
	msg, err := emailinfra.ComposeTextMessage(a.from, to, subject, text, inReplyTo)
	if err != nil {
		return err
	}
	if err := a.sender.Send(ctx, a.from, []string{to}, msg); err != nil {
		return fmt.Errorf("email send: %w", err)
	}
	return nil
}
