package delivery

import (
	"context"
	"fmt"
	"sync"
	"time"

	emailinfra "servify/apps/server/internal/modules/email/infra"
	"servify/apps/server/internal/platform/channel"

	"github.com/sirupsen/logrus"
)

const (
	// pollBatchLimit 单轮最多处理的邮件数；超出部分留待下一轮（游标只推进到已处理的 UID）。
	pollBatchLimit = 100
	// seenRingSize Message-ID 去重环形集合容量：跨 UIDVALIDITY 重置与首轮的二次防线。
	seenRingSize = 1024
)

// EmitFunc 是 poller 产出一封新邮件事件的出口（bridge/观察者）。
type EmitFunc func(ctx context.Context, ev channel.InboundEvent) error

// MailPoller 轮询 IMAP 邮箱并把新邮件转换为 channel.InboundEvent。
//
// 去重三层：UID 游标（主）、UIDVALIDITY 重置检测、Message-ID 环形集合（UIDVALIDITY
// 重置后的二次防线）。首轮（或 UIDVALIDITY 变更后）只推进游标不 ingest，防止历史洪水。
// 并发安全：两条驱动路径（poll worker / Adapter.Receive goroutine）可共存，靠互斥串行。
type MailPoller struct {
	client  emailinfra.IMAPClient
	mailbox string
	emit    EmitFunc
	logger  *logrus.Logger
	nowFunc func() time.Time

	mu          sync.Mutex
	lastUID     uint32
	uidValidity uint32
	started     bool
	seen        *messageIDRing
}

func NewMailPoller(client emailinfra.IMAPClient, mailbox string, emit EmitFunc, logger *logrus.Logger) *MailPoller {
	if mailbox == "" {
		mailbox = "INBOX"
	}
	if logger == nil {
		logger = logrus.StandardLogger()
	}
	return &MailPoller{
		client:  client,
		mailbox: mailbox,
		emit:    emit,
		logger:  logger,
		nowFunc: time.Now,
		seen:    newMessageIDRing(seenRingSize),
	}
}

// PollOnce 执行一轮轮询；返回本轮 ingest 的邮件数。
// Fetch/emit 失败的邮件不推进游标，下一轮重试。
func (p *MailPoller) PollOnce(ctx context.Context) (int, error) {
	p.mu.Lock()
	defer p.mu.Unlock()

	uidValidity, err := p.client.Select(ctx, p.mailbox)
	if err != nil {
		return 0, err
	}
	if uidValidity != p.uidValidity {
		if p.started {
			p.logger.Warnf("email poller: UIDVALIDITY changed %d -> %d; resetting cursor", p.uidValidity, uidValidity)
		}
		p.uidValidity = uidValidity
		p.lastUID = 0
		p.started = false
	}
	uids, err := p.client.SearchSinceUID(ctx, p.lastUID)
	if err != nil {
		return 0, err
	}
	if !p.started {
		// 首轮：历史邮件全部标 seen，不 ingest（防止历史洪水）
		if len(uids) > 0 {
			p.lastUID = uids[len(uids)-1]
		}
		p.started = true
		p.logger.Debugf("email poller: first poll, marked %d existing messages as seen", len(uids))
		return 0, nil
	}
	if len(uids) > pollBatchLimit {
		uids = uids[:pollBatchLimit]
	}

	produced := 0
	baseline := p.lastUID
	for _, uid := range uids {
		msg, err := p.client.Fetch(ctx, uid)
		if err != nil {
			p.logger.Warnf("email poller: fetch uid %d: %v", uid, err)
			break // 连接可能已失效；游标不推进，下轮重试
		}
		p.lastUID = uid
		if msg.HasAttachment {
			p.logger.Warnf("email poller: skip uid %d with attachment (from=%s subject=%q)", uid, msg.From, msg.Subject)
			continue
		}
		eventID := eventIDFor(msg)
		if p.seen.contains(eventID) {
			continue
		}
		ev := p.toInboundEvent(msg)
		if err := p.emit(ctx, ev); err != nil {
			// 回退游标：失败的这封及后续下轮重拉；已成功 ingest 的被 ring 挡住不重复
			p.logger.Warnf("email poller: emit uid %d: %v; rewinding cursor for retry", uid, err)
			p.lastUID = baseline
			break
		}
		p.seen.add(eventID)
		produced++
	}
	return produced, nil
}

func (p *MailPoller) toInboundEvent(msg *emailinfra.MailMessage) channel.InboundEvent {
	occurredAt := msg.Date
	if occurredAt.IsZero() {
		occurredAt = p.nowFunc()
	}
	return channel.InboundEvent{
		EventID:    eventIDFor(msg),
		Channel:    "email",
		ActorID:    msg.From,
		Kind:       channel.EventKindMessage,
		OccurredAt: occurredAt,
		Payload: map[string]interface{}{
			"from":       msg.From,
			"subject":    msg.Subject,
			"text":       msg.TextBody,
			"message_id": msg.MessageID,
			"uid":        msg.UID,
		},
	}
}

// eventIDFor Message-ID 优先（跨 UIDVALIDITY 稳定）；缺失则退回 UID（ring 挡同轮重复）。
func eventIDFor(msg *emailinfra.MailMessage) string {
	if msg.MessageID != "" {
		return "email-" + msg.MessageID
	}
	return fmt.Sprintf("email-uid-%d", msg.UID)
}

// messageIDRing 是定容环形集合：add 覆盖最旧条目，contains 线性扫描（容量 1024 内足够快）。
type messageIDRing struct {
	buf []string
	idx int
}

func newMessageIDRing(size int) *messageIDRing {
	return &messageIDRing{buf: make([]string, size)}
}

func (r *messageIDRing) add(id string) {
	r.buf[r.idx] = id
	r.idx = (r.idx + 1) % len(r.buf)
}

func (r *messageIDRing) contains(id string) bool {
	if id == "" {
		return false
	}
	for _, item := range r.buf {
		if item == id {
			return true
		}
	}
	return false
}
