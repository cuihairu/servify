// Package infra 提供 email 渠道的 IMAP/SMTP 基础设施实现。
package infra

import (
	"context"
	"crypto/tls"
	"errors"
	"fmt"
	"net"
	"strconv"
	"strings"
	"time"

	"servify/apps/server/internal/config"

	"github.com/emersion/go-imap/v2"
	"github.com/emersion/go-imap/v2/imapclient"
	"github.com/sirupsen/logrus"
)

// MailMessage 是一封拉取到的邮件的规范化形态（供 delivery 层消费）。
type MailMessage struct {
	UID           uint32
	MessageID     string
	From          string
	Subject       string
	Date          time.Time
	TextBody      string
	HasAttachment bool
}

// IMAPClient 是轮询器依赖的最小 IMAP 面（单测以桩替换）。
// 一次 Select 建立连接，Search/Fetch 复用；Close 幂等。
type IMAPClient interface {
	// Select 连接、登录并选中邮箱，返回 mailbox 的 UIDVALIDITY。
	Select(ctx context.Context, mailbox string) (uint32, error)
	// SearchSinceUID 返回大于 sinceUID 的 UID（升序）；sinceUID=0 返回全部。
	SearchSinceUID(ctx context.Context, sinceUID uint32) ([]uint32, error)
	// Fetch 拉取单封邮件的规范化形态。
	Fetch(ctx context.Context, uid uint32) (*MailMessage, error)
	// Close 释放连接（幂等）。
	Close() error
}

const imapDialTimeout = 15 * time.Second

// GoIMAPClient 是 IMAPClient 的 go-imap/v2 实现。
type GoIMAPClient struct {
	cfg    config.EmailConfig
	logger *logrus.Logger

	conn    net.Conn
	client  *imapclient.Client
	nowFunc func() time.Time
}

func NewGoIMAPClient(cfg config.EmailConfig, logger *logrus.Logger) *GoIMAPClient {
	if logger == nil {
		logger = logrus.StandardLogger()
	}
	return &GoIMAPClient{cfg: cfg, logger: logger, nowFunc: time.Now}
}

func (c *GoIMAPClient) address() string {
	port := c.cfg.Port
	if port <= 0 {
		port = 993
	}
	return net.JoinHostPort(c.cfg.Host, strconv.Itoa(port))
}

// Select 建立连接（幂等）并选中邮箱。
func (c *GoIMAPClient) Select(ctx context.Context, mailbox string) (uint32, error) {
	if c.client == nil {
		if err := c.connect(ctx); err != nil {
			return 0, err
		}
	}
	sel, err := c.client.Select(mailbox, &imap.SelectOptions{ReadOnly: true}).Wait()
	if err != nil {
		c.dropConn()
		return 0, fmt.Errorf("imap select %q: %w", mailbox, err)
	}
	return sel.UIDValidity, nil
}

func (c *GoIMAPClient) connect(ctx context.Context) error {
	dialer := &net.Dialer{Timeout: imapDialTimeout}
	var conn net.Conn
	var err error
	if c.cfg.UseTLS {
		conn, err = tls.DialWithDialer(dialer, "tcp", c.address(), &tls.Config{
			ServerName:         c.cfg.Host,
			InsecureSkipVerify: c.cfg.SkipVerify,
		})
	} else {
		conn, err = dialer.DialContext(ctx, "tcp", c.address())
	}
	if err != nil {
		return fmt.Errorf("imap dial %s: %w", c.address(), err)
	}
	client := imapclient.New(conn, nil)
	if err := client.Login(c.cfg.Username, c.cfg.Password).Wait(); err != nil {
		_ = conn.Close()
		return fmt.Errorf("imap login: %w", err)
	}
	c.conn = conn
	c.client = client
	return nil
}

// SearchSinceUID 搜索 UID > sinceUID 的邮件；错误时丢弃连接（下次 Select 重建）。
func (c *GoIMAPClient) SearchSinceUID(ctx context.Context, sinceUID uint32) ([]uint32, error) {
	if c.client == nil {
		return nil, errors.New("imap search: not selected")
	}
	var uidSet imap.UIDSet
	if sinceUID > 0 {
		uidSet.AddRange(imap.UID(sinceUID+1), 0) // 0 = "*"（最大 UID）
	} else {
		uidSet.AddRange(1, 0)
	}
	searchData, err := c.client.Search(&imap.SearchCriteria{UID: []imap.UIDSet{uidSet}}, nil).Wait()
	if err != nil {
		c.dropConn()
		return nil, fmt.Errorf("imap search: %w", err)
	}
	uids := searchData.AllUIDs()
	out := make([]uint32, 0, len(uids))
	for _, uid := range uids {
		out = append(out, uint32(uid))
	}
	return out, nil
}

// Fetch 拉取单封邮件：ENVELOPE 取发件人/主题/Message-ID，TEXT 节取正文，
// BODYSTRUCTURE 检测 attachment 处置类型（有附件即视为不可 ingest）。
func (c *GoIMAPClient) Fetch(ctx context.Context, uid uint32) (*MailMessage, error) {
	if c.client == nil {
		return nil, errors.New("imap fetch: not selected")
	}
	fetchCmd := c.client.Fetch(
		imap.UIDSetNum(imap.UID(uid)),
		&imap.FetchOptions{
			Envelope:      true,
			BodyStructure: &imap.FetchItemBodyStructure{Extended: true},
			BodySection:   []*imap.FetchItemBodySection{{Specifier: imap.PartSpecifierText}},
		},
	)
	buffers, err := fetchCmd.Collect()
	if err != nil {
		c.dropConn()
		return nil, fmt.Errorf("imap fetch uid %d: %w", uid, err)
	}
	if len(buffers) == 0 {
		return nil, fmt.Errorf("imap fetch uid %d: no such message", uid)
	}
	buf := buffers[0]
	msg := &MailMessage{UID: uint32(buf.UID), Date: buf.InternalDate}
	if buf.Envelope != nil {
		msg.MessageID = strings.TrimSpace(buf.Envelope.MessageID)
		msg.Subject = buf.Envelope.Subject
		msg.From = firstEmailAddress(buf.Envelope.From)
	}
	for _, section := range buf.BodySection {
		msg.TextBody = string(section.Bytes)
	}
	msg.HasAttachment = hasAttachmentDisposition(buf.BodyStructure)
	return msg, nil
}

// Close 登出并释放连接；未连接时为 no-op。
func (c *GoIMAPClient) Close() error {
	if c.client == nil {
		return nil
	}
	_ = c.client.Logout().Wait()
	c.dropConn()
	return nil
}

func (c *GoIMAPClient) dropConn() {
	if c.client != nil {
		_ = c.client.Close()
	}
	c.client = nil
	if c.conn != nil {
		_ = c.conn.Close()
		c.conn = nil
	}
}

func firstEmailAddress(addrs []imap.Address) string {
	for _, addr := range addrs {
		if addr.Mailbox == "" || addr.Host == "" {
			continue
		}
		return addr.Mailbox + "@" + addr.Host
	}
	return ""
}

func hasAttachmentDisposition(bs imap.BodyStructure) bool {
	if bs == nil {
		return false
	}
	found := false
	bs.Walk(func(path []int, part imap.BodyStructure) bool {
		if d := part.Disposition(); d != nil && strings.EqualFold(strings.TrimSpace(d.Value), "attachment") {
			found = true
			return false
		}
		return true
	})
	return found
}
