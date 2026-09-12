package delivery

import (
	"context"
	"sync"
	"testing"
	"time"

	emailinfra "servify/apps/server/internal/modules/email/infra"
	"servify/apps/server/internal/platform/channel"

	"github.com/sirupsen/logrus"
)

// stubIMAPClient 按 UID 表驱动返回邮件；uidValidity 可在测试中途变更模拟邮箱重建。
type stubIMAPClient struct {
	mu          sync.Mutex
	uidValidity uint32
	messages    map[uint32]*emailinfra.MailMessage
	selectCount int
	closed      bool
	fetchErr    map[uint32]error
	searchErr   error
}

func newStubIMAPClient(uidValidity uint32) *stubIMAPClient {
	return &stubIMAPClient{
		uidValidity: uidValidity,
		messages:    map[uint32]*emailinfra.MailMessage{},
		fetchErr:    map[uint32]error{},
	}
}

// put / setUIDValidity 带锁注入：供 Receive 后台 goroutine 并发读的场景使用。
func (s *stubIMAPClient) put(msg *emailinfra.MailMessage) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.messages[msg.UID] = msg
}

func (s *stubIMAPClient) setUIDValidity(v uint32) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.uidValidity = v
}

func (s *stubIMAPClient) Select(ctx context.Context, mailbox string) (uint32, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.selectCount++
	return s.uidValidity, nil
}

func (s *stubIMAPClient) SearchSinceUID(ctx context.Context, sinceUID uint32) ([]uint32, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.searchErr != nil {
		return nil, s.searchErr
	}
	var out []uint32
	for uid := range s.messages {
		if sinceUID == 0 || uid > sinceUID {
			out = append(out, uid)
		}
	}
	// 升序
	for i := 0; i < len(out); i++ {
		for j := i + 1; j < len(out); j++ {
			if out[j] < out[i] {
				out[i], out[j] = out[j], out[i]
			}
		}
	}
	return out, nil
}

func (s *stubIMAPClient) Fetch(ctx context.Context, uid uint32) (*emailinfra.MailMessage, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if err := s.fetchErr[uid]; err != nil {
		return nil, err
	}
	msg := s.messages[uid]
	if msg == nil {
		return nil, &notFoundError{}
	}
	return msg, nil
}

func (s *stubIMAPClient) Close() error {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.closed = true
	return nil
}

type notFoundError struct{}

func (*notFoundError) Error() string { return "not found" }

// recordingEmitter 记录 emit 的事件序列；可注入失败。
type recordingEmitter struct {
	mu       sync.Mutex
	events   []channel.InboundEvent
	failFrom int // >= len(events) 前正常；emit 序号 >= failFrom 时报错
}

func newRecordingEmitter() *recordingEmitter { return &recordingEmitter{failFrom: 1 << 30} }

func (r *recordingEmitter) emit(ctx context.Context, ev channel.InboundEvent) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	if len(r.events) >= r.failFrom {
		return context.DeadlineExceeded
	}
	r.events = append(r.events, ev)
	return nil
}

func (r *recordingEmitter) collected() []channel.InboundEvent {
	r.mu.Lock()
	defer r.mu.Unlock()
	return append([]channel.InboundEvent(nil), r.events...)
}

func newTestPoller(client emailinfra.IMAPClient, emitter *recordingEmitter) *MailPoller {
	logger := logrus.New()
	logger.SetLevel(logrus.PanicLevel)
	return NewMailPoller(client, "INBOX", emitter.emit, logger)
}

func plainMail(uid uint32, messageID, from string) *emailinfra.MailMessage {
	return &emailinfra.MailMessage{
		UID: uid, MessageID: messageID, From: from,
		Subject: "hello", Date: time.Date(2026, 9, 12, 8, 0, 0, 0, time.UTC),
		TextBody: "body " + messageID,
	}
}

func TestPollOnceFirstRunMarksSeenWithoutEmit(t *testing.T) {
	client := newStubIMAPClient(7)
	client.messages[10] = plainMail(10, "<a@x>", "a@example.com")
	client.messages[11] = plainMail(11, "<b@x>", "b@example.com")
	emitter := newRecordingEmitter()
	poller := newTestPoller(client, emitter)

	produced, err := poller.PollOnce(context.Background())
	if err != nil {
		t.Fatalf("first poll: %v", err)
	}
	if produced != 0 || len(emitter.collected()) != 0 {
		t.Fatalf("first poll must not emit, got produced=%d events=%d", produced, len(emitter.collected()))
	}

	// 新邮件到达后才 ingest
	client.messages[12] = plainMail(12, "<c@x>", "c@example.com")
	produced, err = poller.PollOnce(context.Background())
	if err != nil {
		t.Fatalf("second poll: %v", err)
	}
	if produced != 1 {
		t.Fatalf("expected 1 produced, got %d", produced)
	}
	events := emitter.collected()
	if len(events) != 1 || events[0].EventID != "email-<c@x>" || events[0].Channel != "email" {
		t.Fatalf("unexpected events: %+v", events)
	}
	if events[0].Payload["from"] != "c@example.com" || events[0].Payload["text"] != "body <c@x>" {
		t.Fatalf("payload mismatch: %+v", events[0].Payload)
	}
}

func TestPollOnceUIDValidityResetBehavesLikeFirstRun(t *testing.T) {
	client := newStubIMAPClient(7)
	client.messages[5] = plainMail(5, "<old@x>", "old@example.com")
	emitter := newRecordingEmitter()
	poller := newTestPoller(client, emitter)
	if _, err := poller.PollOnce(context.Background()); err != nil {
		t.Fatalf("first poll: %v", err)
	}

	// 邮箱重建：UIDVALIDITY 变化 + 新 UID 空间的同名邮件
	client.uidValidity = 99
	delete(client.messages, 5)
	client.messages[1] = plainMail(1, "<new@x>", "someone@example.com")
	produced, err := poller.PollOnce(context.Background())
	if err != nil {
		t.Fatalf("poll after reset: %v", err)
	}
	if produced != 0 {
		t.Fatalf("poll after UIDVALIDITY reset should behave like first run, got %d", produced)
	}
	client.messages[2] = plainMail(2, "<after@x>", "later@example.com")
	if produced, _ = poller.PollOnce(context.Background()); produced != 1 {
		t.Fatalf("expected 1 after re-baseline, got %d", produced)
	}
}

func TestPollOnceMessageIDRingGuardsAfterReset(t *testing.T) {
	client := newStubIMAPClient(7)
	client.messages[5] = plainMail(5, "<dup@x>", "dup@example.com")
	emitter := newRecordingEmitter()
	poller := newTestPoller(client, emitter)
	if _, err := poller.PollOnce(context.Background()); err != nil {
		t.Fatal(err)
	}
	if produced, _ := poller.PollOnce(context.Background()); produced != 0 {
		t.Fatalf("stable mailbox should produce nothing, got %d", produced)
	}

	// UIDVALIDITY 重置后同一 Message-ID 以新 UID 出现：ring 必须挡住
	client.uidValidity = 42
	delete(client.messages, 5)
	client.messages[900] = plainMail(900, "<dup@x>", "dup@example.com")
	poller.mu.Lock()
	poller.uidValidity = 0 // 触发重置路径但保留 seen ring
	poller.mu.Unlock()
	produced, err := poller.PollOnce(context.Background())
	// 重置当轮按首轮语义不 emit；但该邮件已进 ring——下一轮（已基线化）也不应重复
	if err != nil {
		t.Fatal(err)
	}
	_ = produced
	client.messages[901] = plainMail(901, "<fresh@x>", "fresh@example.com")
	if produced, _ = poller.PollOnce(context.Background()); produced != 1 {
		t.Fatalf("fresh mail should ingest, got %d", produced)
	}
	for _, ev := range emitter.collected() {
		if ev.EventID == "email-<dup@x>" {
			t.Fatalf("duplicate Message-ID ingested twice after UIDVALIDITY reset")
		}
	}
}

func TestPollOnceSkipsAttachmentsWithoutAdvancingPast(t *testing.T) {
	client := newStubIMAPClient(7)
	emitter := newRecordingEmitter()
	poller := newTestPoller(client, emitter)
	if _, err := poller.PollOnce(context.Background()); err != nil {
		t.Fatal(err)
	}

	withAtt := plainMail(20, "<att@x>", "att@example.com")
	withAtt.HasAttachment = true
	client.messages[20] = withAtt
	client.messages[21] = plainMail(21, "<ok@x>", "ok@example.com")

	produced, err := poller.PollOnce(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if produced != 1 {
		t.Fatalf("attachment must be skipped, expected 1 produced, got %d", produced)
	}
	events := emitter.collected()
	if len(events) != 1 || events[0].EventID != "email-<ok@x>" {
		t.Fatalf("unexpected events: %+v", events)
	}
}

func TestPollOnceFetchErrorRetainsCursor(t *testing.T) {
	client := newStubIMAPClient(7)
	emitter := newRecordingEmitter()
	poller := newTestPoller(client, emitter)
	if _, err := poller.PollOnce(context.Background()); err != nil {
		t.Fatal(err)
	}

	client.fetchErr[30] = &notFoundError{}
	client.messages[30] = plainMail(30, "<fail@x>", "fail@example.com")
	if produced, _ := poller.PollOnce(context.Background()); produced != 0 {
		t.Fatalf("fetch error should produce nothing, got %d", produced)
	}
	// 错误清除后同轮邮件可重试
	delete(client.fetchErr, 30)
	produced, err := poller.PollOnce(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if produced != 1 {
		t.Fatalf("cursor must not advance past failed fetch, got %d", produced)
	}
}

func TestPollOnceEmitFailureRetriesNextPoll(t *testing.T) {
	client := newStubIMAPClient(7)
	emitter := newRecordingEmitter()
	poller := newTestPoller(client, emitter)
	if _, err := poller.PollOnce(context.Background()); err != nil {
		t.Fatal(err)
	}

	client.messages[40] = plainMail(40, "<emit-fail@x>", "emit@example.com")
	emitter.failFrom = 0 // 注入 emit 失败
	if produced, _ := poller.PollOnce(context.Background()); produced != 0 {
		t.Fatalf("emit failure should produce nothing, got %d", produced)
	}
	emitter.failFrom = 1 << 30
	produced, err := poller.PollOnce(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if produced != 1 || len(emitter.collected()) != 1 {
		t.Fatalf("failed emit should retry next poll, got produced=%d", produced)
	}
}
