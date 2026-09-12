package delivery

import (
	"bytes"
	"context"
	"errors"
	"strings"
	"sync"
	"testing"
	"time"

	emailinfra "servify/apps/server/internal/modules/email/infra"
	"servify/apps/server/internal/platform/channel"

	"github.com/sirupsen/logrus"
)

// stubSender 捕获 SMTP 调用。
type stubSender struct {
	mu   sync.Mutex
	from string
	to   []string
	msg  []byte
	err  error
}

func (s *stubSender) Send(ctx context.Context, from string, to []string, msg []byte) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.err != nil {
		return s.err
	}
	s.from = from
	s.to = to
	s.msg = msg
	return nil
}

func newTestAdapter(client emailinfra.IMAPClient, sender emailinfra.SMTPSender, ingestor ConversationIngestor) *Adapter {
	logger := logrus.New()
	logger.SetLevel(logrus.PanicLevel)
	return NewAdapter(AdapterDeps{
		IMAP:     client,
		SMTP:     sender,
		Ingestor: ingestor,
		From:     "support@example.com",
		Interval: 10 * time.Millisecond,
		Logger:   logger,
	})
}

func TestAdapterImplementsChannelAdapter(t *testing.T) {
	var _ channel.Adapter = (*Adapter)(nil)
}

func TestAdapterSendComposesMessage(t *testing.T) {
	sender := &stubSender{}
	adapter := newTestAdapter(newStubIMAPClient(1), sender, newStubIngestor())

	ev := channel.OutboundEvent{
		EventID:  "out-1",
		Channel:  "email",
		TargetID: "cust@example.com",
		Kind:     channel.EventKindMessage,
		Payload: map[string]interface{}{
			"subject":     "Re: Invoice",
			"text":        "your invoice is ready",
			"in_reply_to": "<m1@x>",
		},
	}
	if err := adapter.Send(context.Background(), ev); err != nil {
		t.Fatalf("send: %v", err)
	}
	if sender.from != "support@example.com" || len(sender.to) != 1 || sender.to[0] != "cust@example.com" {
		t.Fatalf("smtp envelope mismatch: from=%q to=%v", sender.from, sender.to)
	}
	msg := string(sender.msg)
	for _, want := range []string{
		"From: support@example.com",
		"To: cust@example.com",
		"In-Reply-To: <m1@x>",
		"References: <m1@x>",
	} {
		if !strings.Contains(msg, want) {
			t.Fatalf("message missing %q:\n%s", want, msg)
		}
	}
	if !strings.Contains(msg, "Subject: Re: Invoice") && !strings.Contains(msg, "Subject: =?utf-8?") {
		t.Fatalf("subject missing:\n%s", msg)
	}
}

func TestAdapterSendValidatesInput(t *testing.T) {
	adapter := newTestAdapter(newStubIMAPClient(1), &stubSender{}, newStubIngestor())

	noRcpt := channel.OutboundEvent{Channel: "email", Kind: channel.EventKindMessage, Payload: map[string]interface{}{"text": "x"}}
	if err := adapter.Send(context.Background(), noRcpt); err == nil {
		t.Fatal("missing recipient must error")
	}
	noSender := &Adapter{logger: logrus.New()}
	err := noSender.Send(context.Background(), channel.OutboundEvent{TargetID: "a@b.c"})
	if err == nil || !strings.Contains(err.Error(), "not configured") {
		t.Fatalf("nil sender must fail fast, got %v", err)
	}
}

func TestAdapterSendSurfacesSMTPFailure(t *testing.T) {
	sender := &stubSender{err: errors.New("relay down")}
	adapter := newTestAdapter(newStubIMAPClient(1), sender, newStubIngestor())
	err := adapter.Send(context.Background(), channel.OutboundEvent{
		TargetID: "cust@example.com",
		Payload:  map[string]interface{}{"subject": "s", "text": "t"},
	})
	if err == nil || !strings.Contains(err.Error(), "relay down") {
		t.Fatalf("smtp failure must surface, got %v", err)
	}
}

func TestAdapterReceiveIngestsAndForwards(t *testing.T) {
	client := newStubIMAPClient(7)
	ingestor := newStubIngestor()
	adapter := newTestAdapter(client, &stubSender{}, ingestor)

	deadline := time.After(2 * time.Second)
	// 历史邮件在启动前就位：首轮基线把它标 seen，不产生事件
	client.put(plainMail(1, "<hist@x>", "hist@example.com"))

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	events, err := adapter.Receive(ctx)
	if err != nil {
		t.Fatalf("receive: %v", err)
	}
	waitForPolls(t, adapter, 1, deadline) // 首轮基线完成

	client.put(plainMail(2, "<live@x>", "live@example.com"))
	var got channel.InboundEvent
	select {
	case ev, ok := <-events:
		if !ok {
			t.Fatal("observation channel closed unexpectedly")
		}
		got = ev
	case <-deadline:
		t.Fatal("no event forwarded within deadline")
	}
	if got.EventID != "email-<live@x>" {
		t.Fatalf("unexpected forwarded event: %+v", got)
	}
	if len(ingestor.ingested) == 0 {
		t.Fatal("forwarded event must also be ingested into conversation")
	}
	cancel()
}

// waitForPolls 等到 poller 完成至少 n 轮（互斥串行，selectCount 单调）。
func waitForPolls(t *testing.T, adapter *Adapter, n int, deadline <-chan time.Time) {
	t.Helper()
	for {
		adapter.poller.mu.Lock()
		count := adapter.poller.client.(*stubIMAPClient).selectCount
		adapter.poller.mu.Unlock()
		if count >= n {
			return
		}
		select {
		case <-deadline:
			t.Fatalf("only %d polls completed", count)
		case <-time.After(5 * time.Millisecond):
		}
	}
}

func TestAdapterComposeMessageQuotedPrintable(t *testing.T) {
	msg, err := emailinfra.ComposeTextMessage("a@x.com", "b@y.com", "naïve subject", "line with ünïcode and\nlong text", "")
	if err != nil {
		t.Fatalf("compose: %v", err)
	}
	if !bytes.Contains(msg, []byte("Content-Transfer-Encoding: quoted-printable")) {
		t.Fatalf("missing CTE header:\n%s", msg)
	}
	if bytes.Contains(msg, []byte("ünïcode")) {
		t.Fatalf("non-ascii body must be QP-encoded:\n%s", msg)
	}
	if !bytes.Contains(msg, []byte("Subject: =?utf-8?q?na")) {
		t.Fatalf("subject must be RFC 2047 encoded:\n%s", msg)
	}
}
