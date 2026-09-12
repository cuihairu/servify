package infra

import (
	"context"
	"errors"
	"strings"
	"testing"
)

func TestGoSMTPSenderFailsFastWithoutHost(t *testing.T) {
	sender := NewGoSMTPSender(SMTPConfig{})
	if err := sender.Send(context.Background(), "a@x.com", []string{"b@y.com"}, []byte("Subject: s\r\n\r\nbody")); !errors.Is(err, ErrSMTPHostMissing) {
		t.Fatalf("expected ErrSMTPHostMissing, got %v", err)
	}
}

func TestGoSMTPSenderRejectsEmptyRecipients(t *testing.T) {
	sender := NewGoSMTPSender(SMTPConfig{Host: "smtp.example.com"})
	if err := sender.Send(context.Background(), "a@x.com", nil, []byte("Subject: s\r\n\r\nbody")); err == nil {
		t.Fatal("empty recipients must error before dialing")
	}
}

func TestComposeTextMessageHeaders(t *testing.T) {
	msg, err := ComposeTextMessage("from@example.com", "to@example.com", "plain subject", "hello world", "")
	if err != nil {
		t.Fatalf("compose: %v", err)
	}
	text := string(msg)
	for _, want := range []string{
		"From: from@example.com\r\n",
		"To: to@example.com\r\n",
		"Subject: plain subject\r\n",
		"MIME-Version: 1.0\r\n",
		"Content-Type: text/plain; charset=utf-8\r\n",
		"hello world",
	} {
		if !strings.Contains(text, want) {
			t.Fatalf("message missing %q:\n%q", want, text)
		}
	}
	if strings.Contains(text, "In-Reply-To") {
		t.Fatalf("no In-Reply-To expected without reply target:\n%q", text)
	}
}

func TestComposeTextMessageInReplyTo(t *testing.T) {
	msg, err := ComposeTextMessage("from@example.com", "to@example.com", "Re: hi", "reply body", "<abc@example.com>")
	if err != nil {
		t.Fatalf("compose: %v", err)
	}
	text := string(msg)
	if !strings.Contains(text, "In-Reply-To: <abc@example.com>\r\n") || !strings.Contains(text, "References: <abc@example.com>\r\n") {
		t.Fatalf("reply headers missing:\n%q", text)
	}
}

func TestComposeTextMessageRequiresEnvelope(t *testing.T) {
	if _, err := ComposeTextMessage("", "to@x.com", "s", "b", ""); err == nil {
		t.Fatal("missing from must error")
	}
	if _, err := ComposeTextMessage("a@x.com", "", "s", "b", ""); err == nil {
		t.Fatal("missing to must error")
	}
}

func TestComposeTextMessageQuotesSpecialChars(t *testing.T) {
	msg, err := ComposeTextMessage("a@x.com", "b@y.com", "s", "value with = sign and ünïcode", "")
	if err != nil {
		t.Fatalf("compose: %v", err)
	}
	text := string(msg)
	// quoted-printable 转义 = 为 =3D、非 ASCII 为 =XX 序列
	if !strings.Contains(text, "=3D") {
		t.Fatalf("bare = must be QP-escaped:\n%q", text)
	}
	if !strings.Contains(text, "=C3=BC") {
		t.Fatalf("non-ascii must be QP-encoded:\n%q", text)
	}
}
