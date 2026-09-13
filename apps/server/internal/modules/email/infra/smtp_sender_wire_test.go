package infra

import (
	"bufio"
	"crypto/tls"
	"io"
	"net"
	"strings"
	"sync"
	"testing"
	"time"
)

// fakeSMTPServer 是内存回环上的极简 SMTP 服务：只实现 GoSMTPSender 会话用到的
// EHLO/STARTTLS/AUTH/MAIL/RCPT/DATA/QUIT，用行为开关覆盖各错误分支。
type fakeSMTPServer struct {
	ln net.Listener

	greeting      string // 默认 "220 fake ready"；可改为 5xx 触发握手失败
	advertiseTLS  bool   // EHLO 里宣告 STARTTLS 并真正升级
	breakStartTLS bool   // 宣告 STARTTLS 但在客户端发起时断连（升级失败分支）
	advertiseAuth bool   // EHLO 里宣告 AUTH PLAIN
	authOK        bool
	mailOK        bool
	rcptOK        bool
	dataOK        bool   // DATA 回 354；false 回 554
	dataFinal     string // 数据结束后的应答，默认 "250 queued OK"
	swallowData   bool   // 354 后不再读取套接字（写正文阻塞到超时）
	tlsCert       *tls.Certificate

	mu      sync.Mutex
	mail    string // 收到的 MAIL FROM 信封
	rcpts   []string
	body    string // 收到的 DATA 报文
	quit    bool
	aborted bool // STARTTLS 升级被打断

	done chan struct{} // cleanup 时关闭，唤醒 swallowData 的挂起 goroutine
}

func startFakeSMTPServer(t *testing.T, mutate func(*fakeSMTPServer)) *fakeSMTPServer {
	t.Helper()
	srv := &fakeSMTPServer{
		greeting:  "220 fake ready",
		authOK:    true,
		mailOK:    true,
		rcptOK:    true,
		dataOK:    true,
		dataFinal: "250 queued OK",
		done:      make(chan struct{}),
	}
	if mutate != nil {
		mutate(srv)
	}
	cert := genSelfSignedCert(t)
	srv.tlsCert = &cert
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("listen: %v", err)
	}
	srv.ln = ln
	go func() {
		for {
			conn, err := ln.Accept()
			if err != nil {
				return
			}
			go srv.serve(conn)
		}
	}()
	t.Cleanup(func() {
		_ = ln.Close()
		close(srv.done)
	})
	return srv
}

func (s *fakeSMTPServer) port() int {
	return s.ln.Addr().(*net.TCPAddr).Port
}

func (s *fakeSMTPServer) serve(conn net.Conn) {
	defer conn.Close()
	if !s.reply(conn, s.greeting) {
		return
	}
	br := bufio.NewReader(conn)
	for {
		line, err := br.ReadString('\n')
		if err != nil {
			return
		}
		fields := strings.Fields(line)
		if len(fields) == 0 {
			continue
		}
		switch strings.ToUpper(fields[0]) {
		case "EHLO", "HELO":
			// 多行应答必须以 "250 "（空格）行收尾，否则 net/smtp 会一直等
			var caps []string
			if s.advertiseTLS {
				caps = append(caps, "STARTTLS")
			}
			if s.advertiseAuth {
				caps = append(caps, "AUTH PLAIN")
			}
			if len(caps) == 0 {
				caps = append(caps, "8BITMIME")
			}
			if !s.reply(conn, "250-fake greets you") {
				return
			}
			for i, capability := range caps {
				line := "250-"
				if i == len(caps)-1 {
					line = "250 "
				}
				if !s.reply(conn, line+capability) {
					return
				}
			}
		case "STARTTLS":
			if !s.reply(conn, "220 go ahead") {
				return
			}
			if s.breakStartTLS {
				s.mu.Lock()
				s.aborted = true
				s.mu.Unlock()
				return // 不升级直接断连：客户端 TLS 握手必失败
			}
			cert := *s.tlsCert
			// 握手在首次 IO 时发生；失败则整个会话结束
			conn = tls.Server(conn, &tls.Config{Certificates: []tls.Certificate{cert}})
			br = bufio.NewReader(conn) // 后续命令改从 TLS 连接读取
		case "AUTH":
			if s.authOK {
				if !s.reply(conn, "235 authenticated") {
					return
				}
			} else if !s.reply(conn, "535 authentication failed") {
				return
			}
		case "MAIL":
			s.mu.Lock()
			s.mail = strings.TrimSpace(line)
			s.mu.Unlock()
			if s.mailOK {
				if !s.reply(conn, "250 mail ok") {
					return
				}
			} else if !s.reply(conn, "550 mailbox unavailable") {
				return
			}
		case "RCPT":
			s.mu.Lock()
			s.rcpts = append(s.rcpts, strings.TrimSpace(line))
			s.mu.Unlock()
			if s.rcptOK {
				if !s.reply(conn, "250 rcpt ok") {
					return
				}
			} else if !s.reply(conn, "550 recipient rejected") {
				return
			}
		case "DATA":
			if !s.dataOK {
				if !s.reply(conn, "554 data not allowed") {
					return
				}
				continue
			}
			if !s.reply(conn, "354 end data with <CR><LF>.<CR><LF>") {
				return
			}
			if s.swallowData {
				// 不再读取套接字：客户端写正文将阻塞直至自身 deadline
				select {
				case <-s.done:
				case <-time.After(time.Minute):
				}
				return
			}
			var body strings.Builder
			for {
				dl, err := br.ReadString('\n')
				if err != nil {
					return
				}
				if strings.TrimSpace(dl) == "." {
					break
				}
				body.WriteString(dl)
			}
			s.mu.Lock()
			s.body = body.String()
			s.mu.Unlock()
			if !s.reply(conn, s.dataFinal) {
				return
			}
		case "QUIT":
			s.mu.Lock()
			s.quit = true
			s.mu.Unlock()
			_ = s.reply(conn, "221 bye")
			return
		default:
			if !s.reply(conn, "250 noop ok") {
				return
			}
		}
	}
}

func (s *fakeSMTPServer) reply(conn net.Conn, line string) bool {
	_, err := io.WriteString(conn, line+"\r\n")
	return err == nil
}

func (s *fakeSMTPServer) snapshot() (mail string, rcpts []string, body string, quit bool) {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.mail, append([]string(nil), s.rcpts...), s.body, s.quit
}

// newLoopbackSMTPSender 构造指向回环假服务的发送器。
func newLoopbackSMTPSender(port int, mutate func(*SMTPConfig)) *GoSMTPSender {
	cfg := SMTPConfig{
		Host:    "127.0.0.1",
		Port:    port,
		From:    "support@example.com",
		Timeout: 5 * time.Second,
	}
	if mutate != nil {
		mutate(&cfg)
	}
	return NewGoSMTPSender(cfg)
}

func TestGoSMTPSenderDeliversViaLoopbackSMTP(t *testing.T) {
	srv := startFakeSMTPServer(t, nil)
	sender := newLoopbackSMTPSender(srv.port(), nil)

	msg, err := ComposeTextMessage("support@example.com", "cust@example.com", "hi", "plain body", "")
	if err != nil {
		t.Fatalf("compose: %v", err)
	}
	if err := sender.Send(t.Context(), "support@example.com", []string{"cust@example.com"}, msg); err != nil {
		t.Fatalf("send: %v", err)
	}
	mail, rcpts, body, quit := srv.snapshot()
	// MAIL FROM 携带发件人信封；net/smtp 发送 "MAIL FROM:<...>"，宽松匹配
	if !strings.Contains(strings.ToUpper(mail), "SUPPORT@EXAMPLE.COM") {
		t.Fatalf("unexpected MAIL envelope: %q", mail)
	}
	if len(rcpts) != 1 || !strings.Contains(strings.ToUpper(rcpts[0]), "CUST@EXAMPLE.COM") {
		t.Fatalf("unexpected RCPT envelope: %v", rcpts)
	}
	if !strings.Contains(body, "plain body") {
		t.Fatalf("body not delivered: %q", body)
	}
	if !quit {
		t.Fatal("client must QUIT after delivery")
	}
}

func TestGoSMTPSenderUsesConfiguredFromWhenEnvelopeEmpty(t *testing.T) {
	srv := startFakeSMTPServer(t, nil)
	sender := newLoopbackSMTPSender(srv.port(), nil)

	msg, _ := ComposeTextMessage("support@example.com", "cust@example.com", "hi", "b", "")
	// from 参数为空时回落到 cfg.From
	if err := sender.Send(t.Context(), "", []string{"cust@example.com"}, msg); err != nil {
		t.Fatalf("send: %v", err)
	}
	mail, _, _, _ := srv.snapshot()
	if !strings.Contains(strings.ToUpper(mail), "SUPPORT@EXAMPLE.COM") {
		t.Fatalf("envelope from must fall back to cfg.From: %q", mail)
	}
}

func TestGoSMTPSenderSkipsBlankRecipients(t *testing.T) {
	srv := startFakeSMTPServer(t, nil)
	sender := newLoopbackSMTPSender(srv.port(), nil)
	msg, _ := ComposeTextMessage("support@example.com", "cust@example.com", "hi", "b", "")
	if err := sender.Send(t.Context(), "support@example.com", []string{"  ", "cust@example.com"}, msg); err != nil {
		t.Fatalf("send: %v", err)
	}
	_, rcpts, _, _ := srv.snapshot()
	if len(rcpts) != 1 {
		t.Fatalf("blank recipient must be skipped, rcpts=%v", rcpts)
	}
}

func TestGoSMTPSenderDefaultsPortAndFailsDialing(t *testing.T) {
	// port<=0 回落 587；本沙箱 587 不可达，Send 必须报错（dial 或超时），不能 panic
	sender := newLoopbackSMTPSender(0, func(c *SMTPConfig) { c.Timeout = 300 * time.Millisecond })
	msg, _ := ComposeTextMessage("support@example.com", "cust@example.com", "hi", "b", "")
	if err := sender.Send(t.Context(), "support@example.com", []string{"cust@example.com"}, msg); err == nil {
		t.Fatal("dialing default port 587 on loopback must fail")
	}

	// 显式指定一个确定无人监听的端口：dial 错误分支
	probe, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("listen: %v", err)
	}
	closedPort := probe.Addr().(*net.TCPAddr).Port
	_ = probe.Close()

	sender = newLoopbackSMTPSender(closedPort, func(c *SMTPConfig) { c.Timeout = time.Second })
	err = sender.Send(t.Context(), "support@example.com", []string{"cust@example.com"}, msg)
	if err == nil || !strings.Contains(err.Error(), "smtp dial") {
		t.Fatalf("expected dial error, got %v", err)
	}
}

func TestGoSMTPSenderHandshakeRejectsBadGreeting(t *testing.T) {
	srv := startFakeSMTPServer(t, func(s *fakeSMTPServer) { s.greeting = "510 go away" })
	sender := newLoopbackSMTPSender(srv.port(), nil)
	msg, _ := ComposeTextMessage("support@example.com", "cust@example.com", "hi", "b", "")
	err := sender.Send(t.Context(), "support@example.com", []string{"cust@example.com"}, msg)
	if err == nil || !strings.Contains(err.Error(), "smtp handshake") {
		t.Fatalf("expected handshake error, got %v", err)
	}
}

func TestGoSMTPSenderStartTLSUpgradesSession(t *testing.T) {
	srv := startFakeSMTPServer(t, func(s *fakeSMTPServer) { s.advertiseTLS = true })
	sender := newLoopbackSMTPSender(srv.port(), func(c *SMTPConfig) {
		c.UseSTARTTLS = true
		c.SkipVerify = true
	})
	msg, _ := ComposeTextMessage("support@example.com", "cust@example.com", "hi", "over tls", "")
	if err := sender.Send(t.Context(), "support@example.com", []string{"cust@example.com"}, msg); err != nil {
		t.Fatalf("send over starttls: %v", err)
	}
	_, _, body, _ := srv.snapshot()
	if !strings.Contains(body, "over tls") {
		t.Fatalf("body not delivered over tls: %q", body)
	}
}

func TestGoSMTPSenderStartTLSFailureWhenServerAborts(t *testing.T) {
	srv := startFakeSMTPServer(t, func(s *fakeSMTPServer) {
		s.advertiseTLS = true
		s.breakStartTLS = true
	})
	sender := newLoopbackSMTPSender(srv.port(), func(c *SMTPConfig) {
		c.UseSTARTTLS = true
		c.SkipVerify = true
		c.Timeout = 2 * time.Second
	})
	msg, _ := ComposeTextMessage("support@example.com", "cust@example.com", "hi", "b", "")
	err := sender.Send(t.Context(), "support@example.com", []string{"cust@example.com"}, msg)
	if err == nil || !strings.Contains(err.Error(), "smtp starttls") {
		t.Fatalf("expected starttls error, got %v", err)
	}
}

func TestGoSMTPSenderAuthFailure(t *testing.T) {
	srv := startFakeSMTPServer(t, func(s *fakeSMTPServer) {
		s.advertiseAuth = true
		s.authOK = false
	})
	sender := newLoopbackSMTPSender(srv.port(), func(c *SMTPConfig) {
		c.Username = "user"
		c.Password = "bad"
	})
	msg, _ := ComposeTextMessage("support@example.com", "cust@example.com", "hi", "b", "")
	err := sender.Send(t.Context(), "support@example.com", []string{"cust@example.com"}, msg)
	if err == nil || !strings.Contains(err.Error(), "smtp auth") {
		t.Fatalf("expected auth error, got %v", err)
	}
}

func TestGoSMTPSenderAuthSuccessOnLoopback(t *testing.T) {
	srv := startFakeSMTPServer(t, func(s *fakeSMTPServer) { s.advertiseAuth = true })
	sender := newLoopbackSMTPSender(srv.port(), func(c *SMTPConfig) {
		c.Username = "user"
		c.Password = "pw"
	})
	msg, _ := ComposeTextMessage("support@example.com", "cust@example.com", "hi", "b", "")
	if err := sender.Send(t.Context(), "support@example.com", []string{"cust@example.com"}, msg); err != nil {
		t.Fatalf("authenticated send must succeed on loopback, got %v", err)
	}
}

func TestGoSMTPSenderMailFromRejected(t *testing.T) {
	srv := startFakeSMTPServer(t, func(s *fakeSMTPServer) { s.mailOK = false })
	sender := newLoopbackSMTPSender(srv.port(), nil)
	msg, _ := ComposeTextMessage("support@example.com", "cust@example.com", "hi", "b", "")
	err := sender.Send(t.Context(), "support@example.com", []string{"cust@example.com"}, msg)
	if err == nil || !strings.Contains(err.Error(), "smtp mail from") {
		t.Fatalf("expected mail-from error, got %v", err)
	}
}

func TestGoSMTPSenderRcptRejected(t *testing.T) {
	srv := startFakeSMTPServer(t, func(s *fakeSMTPServer) { s.rcptOK = false })
	sender := newLoopbackSMTPSender(srv.port(), nil)
	msg, _ := ComposeTextMessage("support@example.com", "cust@example.com", "hi", "b", "")
	err := sender.Send(t.Context(), "support@example.com", []string{"cust@example.com"}, msg)
	if err == nil || !strings.Contains(err.Error(), "smtp rcpt") {
		t.Fatalf("expected rcpt error, got %v", err)
	}
}

func TestGoSMTPSenderDataCommandRejected(t *testing.T) {
	srv := startFakeSMTPServer(t, func(s *fakeSMTPServer) { s.dataOK = false })
	sender := newLoopbackSMTPSender(srv.port(), nil)
	msg, _ := ComposeTextMessage("support@example.com", "cust@example.com", "hi", "b", "")
	err := sender.Send(t.Context(), "support@example.com", []string{"cust@example.com"}, msg)
	if err == nil || !strings.Contains(err.Error(), "smtp data") {
		t.Fatalf("expected data error, got %v", err)
	}
}

func TestGoSMTPSenderDataFinalRejected(t *testing.T) {
	srv := startFakeSMTPServer(t, func(s *fakeSMTPServer) { s.dataFinal = "552 message too big" })
	sender := newLoopbackSMTPSender(srv.port(), nil)
	msg, _ := ComposeTextMessage("support@example.com", "cust@example.com", "hi", "b", "")
	err := sender.Send(t.Context(), "support@example.com", []string{"cust@example.com"}, msg)
	if err == nil || !strings.Contains(err.Error(), "smtp finish data") {
		t.Fatalf("expected finish-data error, got %v", err)
	}
}

func TestGoSMTPSenderWriteBodyTimesOutWhenServerStopsReading(t *testing.T) {
	srv := startFakeSMTPServer(t, func(s *fakeSMTPServer) { s.swallowData = true })
	sender := newLoopbackSMTPSender(srv.port(), func(c *SMTPConfig) { c.Timeout = 700 * time.Millisecond })
	// 大正文塞满回环套接字缓冲后写阻塞到 deadline
	msg, _ := ComposeTextMessage("support@example.com", "cust@example.com", "hi", strings.Repeat("x", 8<<20), "")
	err := sender.Send(t.Context(), "support@example.com", []string{"cust@example.com"}, msg)
	if err == nil || !strings.Contains(err.Error(), "smtp write body") {
		t.Fatalf("expected write-body error, got %v", err)
	}
}

func TestGoSMTPSenderDeadlineExceededWithoutResponse(t *testing.T) {
	// 连接建立后服务器完全不应答 DATA（ swallow 分支已覆盖写超时）；
	// 这里覆盖会话总预算：greeting 正常但后续全部沉默。
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("listen: %v", err)
	}
	t.Cleanup(func() { _ = ln.Close() })
	go func() {
		conn, err := ln.Accept()
		if err != nil {
			return
		}
		defer conn.Close()
		_, _ = io.WriteString(conn, "220 silent fake\r\n")
		// 之后不读取也不应答：EHLO/MAIL 等全部阻塞到 deadline
		select {
		case <-time.After(time.Minute):
		}
	}()

	sender := newLoopbackSMTPSender(ln.Addr().(*net.TCPAddr).Port, func(c *SMTPConfig) { c.Timeout = 400 * time.Millisecond })
	msg, _ := ComposeTextMessage("support@example.com", "cust@example.com", "hi", "b", "")
	if err := sender.Send(t.Context(), "support@example.com", []string{"cust@example.com"}, msg); err == nil {
		t.Fatal("silent server must surface a deadline error")
	}
}

func TestComposeTextMessageNeverAcceptsEmptyEnvelopeSilently(t *testing.T) {
	// 空白字符信封同样必须被拒绝（TrimSpace 语义）
	if _, err := ComposeTextMessage("   ", "to@x.com", "s", "b", ""); err == nil {
		t.Fatal("whitespace-only from must error")
	}
	if _, err := ComposeTextMessage("a@x.com", "  ", "s", "b", ""); err == nil {
		t.Fatal("whitespace-only to must error")
	}
	msg, err := ComposeTextMessage("a@x.com", "b@y.com", "", "body", "  ")
	if err != nil {
		t.Fatalf("compose with empty subject and blank in-reply-to: %v", err)
	}
	if strings.Contains(string(msg), "In-Reply-To") {
		t.Fatalf("blank in-reply-to must be ignored:\n%q", string(msg))
	}
	if !strings.Contains(string(msg), "Subject: \r\n") {
		t.Fatalf("empty subject must stay empty:\n%q", string(msg))
	}
}
