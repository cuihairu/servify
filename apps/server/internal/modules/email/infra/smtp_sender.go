package infra

import (
	"bytes"
	"context"
	"crypto/tls"
	"errors"
	"fmt"
	"io"
	"mime"
	"mime/quotedprintable"
	"net"
	"net/smtp"
	"strconv"
	"strings"
	"time"
)

// ErrSMTPHostMissing 表示出站 SMTP 未配置 host（fail fast，不静默丢弃）。
var ErrSMTPHostMissing = errors.New("smtp host is not configured")

// SMTPConfig 是出站 SMTP 发送参数（自 config.EmailSMTPConfig 归一化而来）。
type SMTPConfig struct {
	Host        string
	Port        int
	Username    string
	Password    string
	From        string
	UseSTARTTLS bool
	SkipVerify  bool
	// Timeout 是连接与整次会话的总预算；零值默认 15s。
	Timeout time.Duration
}

// SMTPSender 发送已组装好的 RFC 5322 报文（单测以桩替换）。
type SMTPSender interface {
	Send(ctx context.Context, from string, to []string, msg []byte) error
}

const smtpDefaultTimeout = 15 * time.Second

// GoSMTPSender 是 SMTPSender 的 net/smtp 实现（STARTTLS + AUTH PLAIN）。
type GoSMTPSender struct {
	cfg SMTPConfig
}

func NewGoSMTPSender(cfg SMTPConfig) *GoSMTPSender {
	if cfg.Timeout <= 0 {
		cfg.Timeout = smtpDefaultTimeout
	}
	return &GoSMTPSender{cfg: cfg}
}

// Send 经 SMTP 投递报文；host 未配置直接报错。
func (s *GoSMTPSender) Send(ctx context.Context, from string, to []string, msg []byte) error {
	if strings.TrimSpace(s.cfg.Host) == "" {
		return ErrSMTPHostMissing
	}
	if from == "" {
		from = s.cfg.From
	}
	if len(to) == 0 {
		return errors.New("smtp send: no recipients")
	}
	port := s.cfg.Port
	if port <= 0 {
		port = 587
	}
	timeout := s.cfg.Timeout
	dialer := &net.Dialer{Timeout: timeout}
	dialCtx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()
	conn, err := dialer.DialContext(dialCtx, "tcp", net.JoinHostPort(s.cfg.Host, strconv.Itoa(port)))
	if err != nil {
		return fmt.Errorf("smtp dial %s:%d: %w", s.cfg.Host, port, err)
	}
	deadline := time.Now().Add(timeout)
	_ = conn.SetDeadline(deadline)

	client, err := smtp.NewClient(conn, s.cfg.Host)
	if err != nil {
		_ = conn.Close()
		return fmt.Errorf("smtp handshake: %w", err)
	}
	defer client.Close()

	if s.cfg.UseSTARTTLS {
		if ok, _ := client.Extension("STARTTLS"); ok {
			if err := client.StartTLS(&tls.Config{
				ServerName:         s.cfg.Host,
				InsecureSkipVerify: s.cfg.SkipVerify,
			}); err != nil {
				return fmt.Errorf("smtp starttls: %w", err)
			}
		}
	}
	if s.cfg.Username != "" {
		if ok, mechs := client.Extension("AUTH"); ok && strings.Contains(mechs, "PLAIN") {
			if err := client.Auth(smtp.PlainAuth("", s.cfg.Username, s.cfg.Password, s.cfg.Host)); err != nil {
				return fmt.Errorf("smtp auth: %w", err)
			}
		}
	}
	if err := client.Mail(from); err != nil {
		return fmt.Errorf("smtp mail from: %w", err)
	}
	for _, rcpt := range to {
		if strings.TrimSpace(rcpt) == "" {
			continue
		}
		if err := client.Rcpt(rcpt); err != nil {
			return fmt.Errorf("smtp rcpt %s: %w", rcpt, err)
		}
	}
	w, err := client.Data()
	if err != nil {
		return fmt.Errorf("smtp data: %w", err)
	}
	if _, err := w.Write(msg); err != nil {
		_ = w.Close()
		return fmt.Errorf("smtp write body: %w", err)
	}
	if err := w.Close(); err != nil {
		return fmt.Errorf("smtp finish data: %w", err)
	}
	return client.Quit()
}

// composeQuotedPrintable 将正文以 quoted-printable 编码写入 w。
// 包级 var seam：生产默认实现写入 *bytes.Buffer 恒不失败（bytes.Buffer.Write
// 恒返 nil），测试注入失败 writer / 失败实现以覆盖两条错误分支。
var composeQuotedPrintable = func(w io.Writer, text string) error {
	qp := quotedprintable.NewWriter(w)
	if _, err := qp.Write([]byte(text)); err != nil {
		return err
	}
	return qp.Close()
}

// ComposeTextMessage 组装纯文本 RFC 5322 报文：
// Subject 按 RFC 2047 Q 编码，正文 quoted-printable；inReplyTo 非空时透传 In-Reply-To/References。
func ComposeTextMessage(from, to, subject, textBody, inReplyTo string) ([]byte, error) {
	if strings.TrimSpace(from) == "" {
		return nil, errors.New("compose message: from is required")
	}
	if strings.TrimSpace(to) == "" {
		return nil, errors.New("compose message: to is required")
	}
	header := &bytes.Buffer{}
	fmt.Fprintf(header, "From: %s\r\n", from)
	fmt.Fprintf(header, "To: %s\r\n", to)
	fmt.Fprintf(header, "Subject: %s\r\n", mime.QEncoding.Encode("utf-8", subject))
	fmt.Fprintf(header, "Date: %s\r\n", time.Now().Format(time.RFC1123Z))
	fmt.Fprintf(header, "MIME-Version: 1.0\r\n")
	fmt.Fprintf(header, "Content-Type: text/plain; charset=utf-8\r\n")
	fmt.Fprintf(header, "Content-Transfer-Encoding: quoted-printable\r\n")
	if strings.TrimSpace(inReplyTo) != "" {
		fmt.Fprintf(header, "In-Reply-To: %s\r\n", inReplyTo)
		fmt.Fprintf(header, "References: %s\r\n", inReplyTo)
	}
	header.WriteString("\r\n")

	body := &bytes.Buffer{}
	if err := composeQuotedPrintable(body, textBody); err != nil {
		return nil, fmt.Errorf("compose message: %w", err)
	}
	return append(header.Bytes(), body.Bytes()...), nil
}
