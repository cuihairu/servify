package delivery

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"

	conversationapp "servify/apps/server/internal/modules/conversation/application"
	emailinfra "servify/apps/server/internal/modules/email/infra"
	"servify/apps/server/internal/platform/channel"

	"github.com/sirupsen/logrus"
)

// flakyIMAPClient 在 stubIMAPClient 上叠加 Select/Search 故障开关。
type flakyIMAPClient struct {
	*stubIMAPClient
	selectErr error
	searchErr error
}

func (f *flakyIMAPClient) Select(ctx context.Context, mailbox string) (uint32, error) {
	if f.selectErr != nil {
		return 0, f.selectErr
	}
	return f.stubIMAPClient.Select(ctx, mailbox)
}

func (f *flakyIMAPClient) SearchSinceUID(ctx context.Context, sinceUID uint32) ([]uint32, error) {
	if f.searchErr != nil {
		return nil, f.searchErr
	}
	return f.stubIMAPClient.SearchSinceUID(ctx, sinceUID)
}

// flakyIngestor 在 stubIngestor 上叠加 Resume/Ingest 故障开关。
type flakyIngestor struct {
	*stubIngestor
	resumeErr error
	ingestErr error
}

func (f *flakyIngestor) ResumeConversation(ctx context.Context, query conversationapp.ResumeConversationQuery) (*conversationapp.ConversationDTO, error) {
	if f.resumeErr != nil {
		return nil, f.resumeErr
	}
	return f.stubIngestor.ResumeConversation(ctx, query)
}

func (f *flakyIngestor) IngestTextMessage(ctx context.Context, cmd conversationapp.IngestTextMessageCommand) (*conversationapp.ConversationMessageDTO, error) {
	if f.ingestErr != nil {
		return nil, f.ingestErr
	}
	return f.stubIngestor.IngestTextMessage(ctx, cmd)
}

// waitPolls 等待底层 stub 完成至少 n 轮 Select（轮询而非 sleep 对齐）。
func waitPolls(t *testing.T, stub *stubIMAPClient, n int, deadline <-chan time.Time) {
	t.Helper()
	for {
		stub.mu.Lock()
		count := stub.selectCount
		stub.mu.Unlock()
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

func TestNewMailPollerDefaultsMailboxAndLogger(t *testing.T) {
	poller := NewMailPoller(newStubIMAPClient(1), "", nil, nil)
	if poller.mailbox != "INBOX" {
		t.Fatalf("empty mailbox must default to INBOX, got %q", poller.mailbox)
	}
	if poller.logger == nil {
		t.Fatal("nil logger must fall back to standard logger")
	}
	if poller.nowFunc == nil {
		t.Fatal("nowFunc must default to time.Now")
	}
}

func TestPollOnceSelectErrorSurfaces(t *testing.T) {
	client := &flakyIMAPClient{stubIMAPClient: newStubIMAPClient(1), selectErr: errors.New("imap down")}
	poller := newTestPoller(client, newRecordingEmitter())
	if _, err := poller.PollOnce(context.Background()); err == nil || err.Error() != "imap down" {
		t.Fatalf("select error must surface, got %v", err)
	}
}

func TestPollOnceSearchErrorSurfaces(t *testing.T) {
	client := &flakyIMAPClient{stubIMAPClient: newStubIMAPClient(1), searchErr: errors.New("search exploded")}
	poller := newTestPoller(client, newRecordingEmitter())
	if _, err := poller.PollOnce(context.Background()); err == nil || err.Error() != "search exploded" {
		t.Fatalf("search error must surface, got %v", err)
	}
}

func TestPollOnceTruncatesLargeBatch(t *testing.T) {
	client := newStubIMAPClient(7)
	for uid := uint32(1); uid <= 3; uid++ {
		client.messages[uid] = plainMail(uid, "<base-"+string(rune('a'+uid-1))+"@x>", "base@example.com")
	}
	emitter := newRecordingEmitter()
	poller := newTestPoller(client, emitter)
	if _, err := poller.PollOnce(context.Background()); err != nil {
		t.Fatalf("baseline poll: %v", err)
	}

	// 一轮塞进 107 封新邮件：只能处理前 100 封，剩余留待下轮
	for uid := uint32(4); uid <= 110; uid++ {
		client.messages[uid] = plainMail(uid, "<m"+uint32String(uid)+"@x>", "bulk@example.com")
	}
	produced, err := poller.PollOnce(context.Background())
	if err != nil {
		t.Fatalf("bulk poll: %v", err)
	}
	if produced != pollBatchLimit {
		t.Fatalf("batch must truncate to %d, got %d", pollBatchLimit, produced)
	}
	poller.mu.Lock()
	advanced := poller.lastUID
	poller.mu.Unlock()
	if advanced != 103 {
		t.Fatalf("cursor must stop at uid 103, got %d", advanced)
	}

	produced, err = poller.PollOnce(context.Background())
	if err != nil {
		t.Fatalf("remainder poll: %v", err)
	}
	if produced != 7 {
		t.Fatalf("remainder poll must deliver 7, got %d", produced)
	}
}

// uint32String 免 strconv 的极简十进制（测试 UID 均为小数值）。
func uint32String(v uint32) string {
	if v < 10 {
		return string(rune('0' + v))
	}
	return uint32String(v/10) + string(rune('0'+v%10))
}

func TestPollOnceDedupesSameMessageIDWithinBatch(t *testing.T) {
	client := newStubIMAPClient(7)
	emitter := newRecordingEmitter()
	poller := newTestPoller(client, emitter)
	if _, err := poller.PollOnce(context.Background()); err != nil {
		t.Fatal(err)
	}

	client.messages[50] = plainMail(50, "<same@x>", "twin@example.com")
	client.messages[51] = plainMail(51, "<same@x>", "twin@example.com")
	produced, err := poller.PollOnce(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if produced != 1 {
		t.Fatalf("same Message-ID in one batch must emit once, got %d", produced)
	}
	if !poller.seen.contains("email-<same@x>") {
		t.Fatal("emitted event id must be recorded in dedup ring")
	}
}

func TestPollOnceZeroDateFallsBackToClock(t *testing.T) {
	client := newStubIMAPClient(7)
	emitter := newRecordingEmitter()
	poller := newTestPoller(client, emitter)
	if _, err := poller.PollOnce(context.Background()); err != nil {
		t.Fatal(err)
	}

	fixed := time.Date(2026, 9, 13, 10, 0, 0, 0, time.UTC)
	poller.nowFunc = func() time.Time { return fixed }

	undated := plainMail(60, "<nodate@x>", "nodate@example.com")
	undated.Date = time.Time{}
	client.messages[60] = undated
	if _, err := poller.PollOnce(context.Background()); err != nil {
		t.Fatal(err)
	}
	events := emitter.collected()
	if len(events) != 1 {
		t.Fatalf("expected 1 event, got %d", len(events))
	}
	if !events[0].OccurredAt.Equal(fixed) {
		t.Fatalf("zero date must fall back to clock, got %v", events[0].OccurredAt)
	}
}

func TestEventIDForUsesUIDWhenMessageIDMissing(t *testing.T) {
	msg := &emailinfra.MailMessage{UID: 42}
	if got := eventIDFor(msg); got != "email-uid-42" {
		t.Fatalf("uid fallback event id = %q", got)
	}
	msg.MessageID = "<real@x>"
	if got := eventIDFor(msg); got != "email-<real@x>" {
		t.Fatalf("message id event id = %q", got)
	}
}

func TestMessageIDRingSemantics(t *testing.T) {
	ring := newMessageIDRing(4)
	if ring.contains("") {
		// contains("") 恒 false：空 id 不进 ring，也不该误判重复
		t.Fatal("empty id must never match")
	}
	if ring.contains("missing") {
		t.Fatal("fresh ring must not contain ids")
	}
	ring.add("a")
	ring.add("b")
	if !ring.contains("a") || !ring.contains("b") {
		t.Fatal("added ids must be found")
	}
	// 容量 2 的环：c 挤掉 a
	small := newMessageIDRing(2)
	small.add("a")
	small.add("b")
	small.add("c")
	if small.contains("a") {
		t.Fatal("oldest entry must be evicted")
	}
	if !small.contains("c") {
		t.Fatal("newest entry must survive eviction")
	}
}

func TestConversationBridgeSurfacesResumeError(t *testing.T) {
	ingestor := &flakyIngestor{stubIngestor: newStubIngestor(), resumeErr: errors.New("db on fire")}
	bridge := NewConversationBridge(ingestor)
	err := bridge.IngestInbound(context.Background(), emailEvent("a@example.com", "s", "t", "<m@x>"))
	if err == nil || err.Error() != "db on fire" {
		t.Fatalf("non-notfound resume error must surface, got %v", err)
	}
}

func TestConversationBridgeSurfacesCreateError(t *testing.T) {
	ingestor := newStubIngestor()
	ingestor.createOn = false
	bridge := NewConversationBridge(ingestor)
	err := bridge.IngestInbound(context.Background(), emailEvent("a@example.com", "s", "t", "<m@x>"))
	if err == nil || err.Error() != "create disabled" {
		t.Fatalf("create failure must surface, got %v", err)
	}
}

func TestConversationBridgeSurfacesIngestError(t *testing.T) {
	ingestor := &flakyIngestor{stubIngestor: newStubIngestor(), ingestErr: errors.New("write refused")}
	bridge := NewConversationBridge(ingestor)
	err := bridge.IngestInbound(context.Background(), emailEvent("a@example.com", "s", "t", "<m@x>"))
	if err == nil || err.Error() != "write refused" {
		t.Fatalf("ingest failure must surface, got %v", err)
	}
}

func TestNewAdapterDefaultsIntervalLoggerAndName(t *testing.T) {
	adapter := NewAdapter(AdapterDeps{})
	if got := adapter.Name(); got != "email" {
		t.Fatalf("name = %q", got)
	}
	if adapter.interval != time.Minute {
		t.Fatalf("non-positive interval must default to 1m, got %v", adapter.interval)
	}
	if adapter.logger == nil {
		t.Fatal("nil logger must fall back to standard logger")
	}
	if adapter.poller.mailbox != "INBOX" {
		t.Fatalf("adapter poller mailbox = %q", adapter.poller.mailbox)
	}
}

func TestAdapterPollOnceDelegatesToPoller(t *testing.T) {
	client := newStubIMAPClient(5)
	adapter := newTestAdapter(client, &stubSender{}, newStubIngestor())

	produced, err := adapter.PollOnce(context.Background())
	if err != nil || produced != 0 {
		t.Fatalf("first poll = (%d, %v)", produced, err)
	}
	client.messages[2] = plainMail(2, "<del@x>", "del@example.com")
	produced, err = adapter.PollOnce(context.Background())
	if err != nil || produced != 1 {
		t.Fatalf("second poll = (%d, %v), want (1, nil)", produced, err)
	}
}

func TestAdapterReceiveClosesPreviousChannel(t *testing.T) {
	adapter := newTestAdapter(newStubIMAPClient(1), &stubSender{}, newStubIngestor())
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	first, err := adapter.Receive(ctx)
	if err != nil {
		t.Fatalf("first receive: %v", err)
	}
	second, err := adapter.Receive(ctx)
	if err != nil {
		t.Fatalf("second receive: %v", err)
	}
	// 旧通道同步关闭
	select {
	case _, ok := <-first:
		if ok {
			t.Fatal("replaced channel must be closed, not deliver events")
		}
	case <-time.After(2 * time.Second):
		t.Fatal("replaced channel was never closed")
	}
	if adapter.observe != second {
		t.Fatal("adapter must track the newest observation channel")
	}
}

func TestAdapterReceiveStopsOnContextCancel(t *testing.T) {
	adapter := newTestAdapter(newStubIMAPClient(1), &stubSender{}, newStubIngestor())
	ctx, cancel := context.WithCancel(context.Background())
	events, err := adapter.Receive(ctx)
	if err != nil {
		t.Fatalf("receive: %v", err)
	}
	cancel()
	deadline := time.After(2 * time.Second)
	for {
		select {
		case _, ok := <-events:
			if !ok {
				return // 通道已随 goroutine 退出关闭
			}
		case <-deadline:
			t.Fatal("observation channel must close after ctx cancel")
		}
	}
}

func TestAdapterReceiveLogsPollErrorsAndKeepsRunning(t *testing.T) {
	stub := newStubIMAPClient(1)
	client := &flakyIMAPClient{stubIMAPClient: stub, searchErr: errors.New("permanent imap outage")}
	adapter := newTestAdapter(client, &stubSender{}, newStubIngestor())
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	if _, err := adapter.Receive(ctx); err != nil {
		t.Fatalf("receive: %v", err)
	}
	// 每轮 poll 都失败：goroutine 只记日志不退出
	waitPolls(t, stub, 3, time.After(2*time.Second))
	select {
	case _, ok := <-adapter.observe:
		if !ok {
			t.Fatal("poll errors must not kill the receive loop")
		}
	default:
	}
}

func TestAdapterSendFailsWhenFromMissing(t *testing.T) {
	// sender 就绪但 From 为空：ComposeTextMessage 必须拒绝空信封
	adapter := NewAdapter(AdapterDeps{SMTP: &stubSender{}, Interval: time.Millisecond, Logger: logrus.New()})
	err := adapter.Send(context.Background(), channel.OutboundEvent{
		TargetID: "cust@example.com",
		Payload:  map[string]interface{}{"text": "hi"},
	})
	if err == nil || !strings.Contains(err.Error(), "from is required") {
		t.Fatalf("empty From must fail compose, got %v", err)
	}
}

func TestAdapterIngestAndObserveSurfacesBridgeError(t *testing.T) {
	adapter := newTestAdapter(newStubIMAPClient(1), &stubSender{}, newStubIngestor())
	bad := channel.InboundEvent{EventID: "e1", Payload: map[string]interface{}{"text": "no from here"}}
	if err := adapter.ingestAndObserve(context.Background(), bad); err == nil {
		t.Fatal("bridge ingest failure must surface")
	}
	// observe 未开启时成功归并不阻塞
	good := emailEvent("a@example.com", "s", "t", "<ok@x>")
	if err := adapter.ingestAndObserve(context.Background(), good); err != nil {
		t.Fatalf("ingest without observer must succeed, got %v", err)
	}
}

func TestAdapterIngestAndObserveDropsWhenObserverFull(t *testing.T) {
	adapter := newTestAdapter(newStubIMAPClient(1), &stubSender{}, newStubIngestor())
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	obs, err := adapter.Receive(ctx)
	if err != nil {
		t.Fatalf("receive: %v", err)
	}

	for i := 0; i < cap(obs)+1; i++ {
		ev := emailEvent("bulk@example.com", "s", "t", "<bulk-"+uint32String(uint32(i))+"@x>")
		if err := adapter.ingestAndObserve(ctx, ev); err != nil {
			t.Fatalf("observe must never block the ingest path: %v", err)
		}
	}
	if len(obs) != cap(obs) {
		t.Fatalf("observer must hold %d buffered events, got %d", cap(obs), len(obs))
	}
}
