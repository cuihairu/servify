package worker

import (
	"context"
	"errors"
	"io"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"servify/apps/server/internal/app/bootstrap"
	"servify/apps/server/internal/config"
	"servify/apps/server/internal/models"
	agentdelivery "servify/apps/server/internal/modules/agent/delivery"
	automationapp "servify/apps/server/internal/modules/automation/application"
	conversationdelivery "servify/apps/server/internal/modules/conversation/delivery"
	emaildelivery "servify/apps/server/internal/modules/email/delivery"
	qualityapp "servify/apps/server/internal/modules/quality/application"
	routingdelivery "servify/apps/server/internal/modules/routing/delivery"
	webhookapp "servify/apps/server/internal/modules/webhook/application"
	"servify/apps/server/internal/services"

	"github.com/glebarez/sqlite"
	"github.com/sirupsen/logrus"
	"gorm.io/gorm"
)

// discardLogger 返回丢弃输出的 logger，既覆盖「logger 非 nil」分支又不污染测试输出。
func discardLogger() *logrus.Logger {
	l := logrus.New()
	l.SetOutput(io.Discard)
	return l
}

// preCancelledCtx 返回一个已取消的 context，配合「worker 循环卡住」的假象，
// 确定性触发 Stop 的 ctx.Err() 分支。
func preCancelledCtx() context.Context {
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	return ctx
}

func waitAtLeast(t *testing.T, deadline time.Duration, cond func() bool, msg string) {
	t.Helper()
	deadlineAt := time.Now().Add(deadline)
	for !cond() {
		if time.Now().After(deadlineAt) {
			t.Fatal(msg)
		}
		time.Sleep(5 * time.Millisecond)
	}
}

// ---------------------------------------------------------------------------
// WebhookDeliveryWorker
// ---------------------------------------------------------------------------

type stubWebhookProcessor struct {
	calls  atomic.Int64
	result int
}

func (s *stubWebhookProcessor) ProcessDueDeliveries(ctx context.Context, now time.Time) int {
	s.calls.Add(1)
	return s.result
}

func TestWebhookDeliveryWorkerConstructorDefaults(t *testing.T) {
	w := NewWebhookDeliveryWorker(&stubWebhookProcessor{}, 0, nil).(*WebhookDeliveryWorker)
	if w.interval != 30*time.Second {
		t.Fatalf("default interval = %v, want 30s", w.interval)
	}
	if w.logger == nil {
		t.Fatal("nil logger must fall back to standard logger")
	}
	if w.Name() != "webhook-delivery" {
		t.Fatalf("Name() = %q", w.Name())
	}
}

func TestWebhookDeliveryWorkerNilServiceAndDoubleStart(t *testing.T) {
	w := NewWebhookDeliveryWorker(nil, time.Minute, nil)
	if err := w.Start(); err != nil {
		t.Fatalf("Start() with nil service = %v", err)
	}
	if err := w.Stop(context.Background()); err != nil {
		t.Fatalf("Stop() before start = %v", err)
	}

	proc := &stubWebhookProcessor{}
	w2 := NewWebhookDeliveryWorker(proc, 15*time.Millisecond, discardLogger())
	if err := w2.Start(); err != nil {
		t.Fatalf("Start() = %v", err)
	}
	if err := w2.Start(); err != nil {
		t.Fatalf("second Start() must be a no-op, got %v", err)
	}
	waitAtLeast(t, 2*time.Second, func() bool { return proc.calls.Load() >= 1 }, "worker never ran")
	if err := w2.Stop(context.Background()); err != nil {
		t.Fatalf("Stop() = %v", err)
	}
}

func TestWebhookDeliveryWorkerTickerLoop(t *testing.T) {
	proc := &stubWebhookProcessor{}
	w := NewWebhookDeliveryWorker(proc, 15*time.Millisecond, discardLogger())
	if err := w.Start(); err != nil {
		t.Fatalf("Start() = %v", err)
	}
	// 初次 run() + 至少一次 ticker 触发
	waitAtLeast(t, 2*time.Second, func() bool { return proc.calls.Load() >= 2 }, "ticker loop did not re-run")
	if err := w.Stop(context.Background()); err != nil {
		t.Fatalf("Stop() = %v", err)
	}
}

func TestWebhookDeliveryWorkerLogsProcessedDeliveries(t *testing.T) {
	proc := &stubWebhookProcessor{result: 3}
	w := NewWebhookDeliveryWorker(proc, 15*time.Millisecond, discardLogger())
	if err := w.Start(); err != nil {
		t.Fatalf("Start() = %v", err)
	}
	waitAtLeast(t, 2*time.Second, func() bool { return proc.calls.Load() >= 1 }, "worker never ran")
	if err := w.Stop(context.Background()); err != nil {
		t.Fatalf("Stop() = %v", err)
	}
}

func TestWebhookDeliveryWorkerStopDuringJitter(t *testing.T) {
	proc := &stubWebhookProcessor{}
	// jitter 上界为 interval 的 10%，1h interval 保证 Stop 发生在 jitter 窗口内
	w := NewWebhookDeliveryWorker(proc, time.Hour, discardLogger())
	if err := w.Start(); err != nil {
		t.Fatalf("Start() = %v", err)
	}
	stopCtx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	if err := w.Stop(stopCtx); err != nil {
		t.Fatalf("Stop() during jitter = %v", err)
	}
	if proc.calls.Load() != 0 {
		t.Fatalf("worker must not run before jitter elapses, calls = %d", proc.calls.Load())
	}
}

func TestWebhookDeliveryWorkerStopContextError(t *testing.T) {
	_, cancel := context.WithCancel(context.Background())
	w := &WebhookDeliveryWorker{cancel: cancel, done: make(chan struct{})}
	if err := w.Stop(preCancelledCtx()); !errors.Is(err, context.Canceled) {
		t.Fatalf("Stop() with cancelled ctx = %v, want context.Canceled", err)
	}
}

// ---------------------------------------------------------------------------
// WaitingQueueWorker
// ---------------------------------------------------------------------------

type stubRoutingRuntime struct {
	mu       sync.Mutex
	queued   [][]models.WaitingRecord
	claimErr error
	calls    atomic.Int64
}

func (s *stubRoutingRuntime) ClaimWaitingRecords(ctx context.Context, now, leaseBefore time.Time, limit int) ([]models.WaitingRecord, error) {
	s.calls.Add(1)
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.claimErr != nil {
		return nil, s.claimErr
	}
	if len(s.queued) == 0 {
		return nil, nil
	}
	batch := s.queued[0]
	s.queued = s.queued[1:]
	return batch, nil
}

func (s *stubRoutingRuntime) AddToWaitingQueue(ctx context.Context, tx *gorm.DB, sessionID string, reason string, targetSkills []string, targetGroupID uint, priority string, notes string) (*models.WaitingRecord, error) {
	return nil, nil
}

func (s *stubRoutingRuntime) AssignAgent(ctx context.Context, tx *gorm.DB, cmd routingdelivery.AssignAgentCommand) (*models.TransferRecord, error) {
	return nil, nil
}

func (s *stubRoutingRuntime) GetTransferHistory(ctx context.Context, sessionID string) ([]models.TransferRecord, error) {
	return nil, nil
}

func (s *stubRoutingRuntime) ListRecentTransferHistory(ctx context.Context, limit int) ([]models.TransferRecord, error) {
	return nil, nil
}

func (s *stubRoutingRuntime) ListWaitingRecords(ctx context.Context, status string, limit int) ([]models.WaitingRecord, error) {
	return nil, nil
}

func (s *stubRoutingRuntime) GetWaitingRecord(ctx context.Context, sessionID string) (*models.WaitingRecord, error) {
	return nil, nil
}

func (s *stubRoutingRuntime) CancelWaiting(ctx context.Context, tx *gorm.DB, sessionID string, reason string) (*models.WaitingRecord, error) {
	return nil, nil
}

func (s *stubRoutingRuntime) MarkWaitingTransferred(ctx context.Context, tx *gorm.DB, sessionID string, agentID uint, assignedAt time.Time) (*models.WaitingRecord, error) {
	return nil, nil
}

func (s *stubRoutingRuntime) ReleaseWaitingClaim(ctx context.Context, sessionID string) error {
	return nil
}

type stubConversationRuntime struct {
	mu      sync.Mutex
	session *conversationdelivery.TransferSession
	loads   atomic.Int64
}

func (s *stubConversationRuntime) LoadTransferSession(ctx context.Context, sessionID string) (*conversationdelivery.TransferSession, error) {
	s.loads.Add(1)
	return s.session, nil
}

func (s *stubConversationRuntime) SyncTransferAssignment(ctx context.Context, tx *gorm.DB, sessionID string, customerID uint, agentID uint) error {
	return nil
}

func (s *stubConversationRuntime) SyncWaitingAssignment(ctx context.Context, tx *gorm.DB, sessionID string, customerID uint) error {
	return nil
}

func (s *stubConversationRuntime) AppendSystemMessage(ctx context.Context, tx *gorm.DB, sessionID string, content string, createdAt time.Time) error {
	return nil
}

// stubAgentPicker 只用于满足 HandlerDependencies.Agents 的接口形状：
// SelectAgent 返回的坐席 UserID 等于 stub session 已绑定的坐席，
// 让 executeTransfer 走「已指派给目标坐席」的快速成功分支（不触碰 DB）。
type stubAgentPicker struct{}

func (stubAgentPicker) FindAvailableAgent(ctx context.Context, skills []string, priority string) (*agentdelivery.AgentInfo, error) {
	return nil, nil
}

func (stubAgentPicker) SelectAgent(ctx context.Context, req agentdelivery.SelectionRequest) (*agentdelivery.SelectionResult, error) {
	return &agentdelivery.SelectionResult{Agent: &agentdelivery.AgentRuntimeDTO{UserID: 9}}, nil
}

func (stubAgentPicker) GetOnlineAgent(ctx context.Context, userID uint) (*agentdelivery.AgentInfo, bool) {
	return nil, false
}

func (stubAgentPicker) ApplySessionTransfer(ctx context.Context, sessionID string, fromAgentID *uint, toAgentID uint) {
}

func newWaitingDispatcher(routing *stubRoutingRuntime, conversation *stubConversationRuntime, interval time.Duration) bootstrap.Worker {
	assigned := uint(9)
	adapter := routingdelivery.NewHandlerService(routingdelivery.HandlerDependencies{
		DB:           nil,
		Logger:       discardLogger(),
		Agents:       stubAgentPicker{},
		Routing:      routing,
		Conversation: conversation,
	})
	_ = assigned
	return NewWaitingQueueWorker(adapter, interval, discardLogger())
}

func TestWaitingQueueWorkerConstructorDefaults(t *testing.T) {
	w := NewWaitingQueueWorker(nil, 0, nil).(*WaitingQueueWorker)
	if w.interval != 30*time.Second {
		t.Fatalf("default interval = %v, want 30s", w.interval)
	}
	if w.logger == nil {
		t.Fatal("nil logger must fall back to standard logger")
	}
	if w.Name() != "waiting-queue-dispatch" {
		t.Fatalf("Name() = %q", w.Name())
	}
}

func TestWaitingQueueWorkerNilServiceAndDoubleStart(t *testing.T) {
	w := NewWaitingQueueWorker(nil, time.Minute, nil)
	if err := w.Start(); err != nil {
		t.Fatalf("Start() with nil service = %v", err)
	}
	if err := w.Stop(context.Background()); err != nil {
		t.Fatalf("Stop() before start = %v", err)
	}

	routing := &stubRoutingRuntime{}
	w2 := newWaitingDispatcher(routing, &stubConversationRuntime{}, 15*time.Millisecond)
	if err := w2.Start(); err != nil {
		t.Fatalf("Start() = %v", err)
	}
	if err := w2.Start(); err != nil {
		t.Fatalf("second Start() must be a no-op, got %v", err)
	}
	waitAtLeast(t, 2*time.Second, func() bool { return routing.calls.Load() >= 1 }, "dispatcher never ran")
	if err := w2.Stop(context.Background()); err != nil {
		t.Fatalf("Stop() = %v", err)
	}
}

func TestWaitingQueueWorkerTickerLoopWithEmptyClaims(t *testing.T) {
	routing := &stubRoutingRuntime{}
	w := newWaitingDispatcher(routing, &stubConversationRuntime{}, 15*time.Millisecond)
	if err := w.Start(); err != nil {
		t.Fatalf("Start() = %v", err)
	}
	waitAtLeast(t, 2*time.Second, func() bool { return routing.calls.Load() >= 2 }, "ticker loop did not re-claim")
	if err := w.Stop(context.Background()); err != nil {
		t.Fatalf("Stop() = %v", err)
	}
}

func TestWaitingQueueWorkerClaimErrorDoesNotStopLoop(t *testing.T) {
	routing := &stubRoutingRuntime{claimErr: errors.New("claim boom")}
	w := newWaitingDispatcher(routing, &stubConversationRuntime{}, 15*time.Millisecond)
	if err := w.Start(); err != nil {
		t.Fatalf("Start() = %v", err)
	}
	// 失败只记日志，下一轮继续
	waitAtLeast(t, 2*time.Second, func() bool { return routing.calls.Load() >= 2 }, "worker stopped after claim error")
	if err := w.Stop(context.Background()); err != nil {
		t.Fatalf("Stop() = %v", err)
	}
}

func TestWaitingQueueWorkerDispatchesClaimedRecord(t *testing.T) {
	routing := &stubRoutingRuntime{queued: [][]models.WaitingRecord{
		{{SessionID: "ws-1", Priority: "high"}},
	}}
	conversation := &stubConversationRuntime{session: &conversationdelivery.TransferSession{
		ID:         "ws-1",
		CustomerID: 7,
		Status:     "active",
	}}
	// session.AgentID 指向坐席 9，SelectAgent 也返回 9 → executeTransfer 快速成功
	agentID := uint(9)
	conversation.session.AgentID = &agentID

	w := newWaitingDispatcher(routing, conversation, 15*time.Millisecond)
	if err := w.Start(); err != nil {
		t.Fatalf("Start() = %v", err)
	}
	waitAtLeast(t, 2*time.Second, func() bool { return conversation.loads.Load() >= 1 }, "claimed record never processed")
	if err := w.Stop(context.Background()); err != nil {
		t.Fatalf("Stop() = %v", err)
	}
}

func TestWaitingQueueWorkerStopDuringJitter(t *testing.T) {
	routing := &stubRoutingRuntime{}
	w := newWaitingDispatcher(routing, &stubConversationRuntime{}, time.Hour)
	if err := w.Start(); err != nil {
		t.Fatalf("Start() = %v", err)
	}
	stopCtx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	if err := w.Stop(stopCtx); err != nil {
		t.Fatalf("Stop() during jitter = %v", err)
	}
	if routing.calls.Load() != 0 {
		t.Fatalf("worker must not run before jitter elapses, calls = %d", routing.calls.Load())
	}
}

func TestWaitingQueueWorkerStopContextError(t *testing.T) {
	_, cancel := context.WithCancel(context.Background())
	w := &WaitingQueueWorker{cancel: cancel, done: make(chan struct{})}
	if err := w.Stop(preCancelledCtx()); !errors.Is(err, context.Canceled) {
		t.Fatalf("Stop() with cancelled ctx = %v, want context.Canceled", err)
	}
}

// ---------------------------------------------------------------------------
// SurveyEmailWorker
// ---------------------------------------------------------------------------

type stubSurveyMailer struct {
	calls     atomic.Int64
	rejectDom string // 包含该域名的收件人一律投递失败
}

func (s *stubSurveyMailer) SendSurveyEmail(ctx context.Context, to, subject, textBody string) error {
	s.calls.Add(1)
	if s.rejectDom != "" && strings.Contains(to, s.rejectDom) {
		return errors.New("smtp rejected")
	}
	return nil
}

func TestSurveyEmailWorkerConstructorDefaults(t *testing.T) {
	w := NewSurveyEmailWorker(nil, nil).(*SurveyEmailWorker)
	if w.logger == nil {
		t.Fatal("nil logger must fall back to standard logger")
	}
	if w.Name() != "survey-email-send" {
		t.Fatalf("Name() = %q", w.Name())
	}
}

func TestSurveyEmailWorkerNilServiceAndStopBeforeStart(t *testing.T) {
	w := NewSurveyEmailWorker(nil, nil)
	if err := w.Start(); err != nil {
		t.Fatalf("Start() with nil service = %v", err)
	}
	if err := w.Stop(context.Background()); err != nil {
		t.Fatalf("Stop() before start = %v", err)
	}
}

func TestSurveyEmailWorkerStopDuringJitter(t *testing.T) {
	w := NewSurveyEmailWorker(&services.SatisfactionService{}, discardLogger())
	if err := w.Start(); err != nil {
		t.Fatalf("Start() = %v", err)
	}
	stopCtx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	if err := w.Stop(stopCtx); err != nil {
		t.Fatalf("Stop() during jitter = %v", err)
	}
}

func TestSurveyEmailWorkerStopContextError(t *testing.T) {
	_, cancel := context.WithCancel(context.Background())
	w := &SurveyEmailWorker{cancel: cancel, done: make(chan struct{})}
	if err := w.Stop(preCancelledCtx()); !errors.Is(err, context.Canceled) {
		t.Fatalf("Stop() with cancelled ctx = %v, want context.Canceled", err)
	}
}

// TestSurveyEmailWorkerDeliversQueuedSurveys 覆盖真实的 ticker tick 路径：
// 通过注入毫秒级 scanInterval 让 Start 的 initial delay + ticker 循环真实运转，
// 一轮扫描同时覆盖投递成功（sent>0 Infof）与投递失败（Warn 后保留 queued 重试）两条分支。
// 与 scan-failure 测试并行执行。
func TestSurveyEmailWorkerDeliversQueuedSurveys(t *testing.T) {
	t.Parallel()
	db, err := gorm.Open(sqlite.Open(":memory:"), &gorm.Config{})
	if err != nil {
		t.Fatalf("open sqlite: %v", err)
	}
	if err := db.AutoMigrate(&models.User{}, &models.SatisfactionSurvey{}); err != nil {
		t.Fatalf("auto migrate: %v", err)
	}
	users := []models.User{
		{Username: "survey-blocked", Email: "blocked@example.com", Password: "x"},
		{Username: "survey-ok", Email: "ok@example.com", Password: "x"},
	}
	for i := range users {
		if err := db.Create(&users[i]).Error; err != nil {
			t.Fatalf("create user: %v", err)
		}
	}
	surveys := []models.SatisfactionSurvey{
		{CustomerID: users[0].ID, Channel: "email", Status: "queued", SurveyToken: "tok-blocked"},
		{CustomerID: users[1].ID, Channel: "email", Status: "queued", SurveyToken: "tok-ok"},
	}
	for i := range surveys {
		if err := db.Create(&surveys[i]).Error; err != nil {
			t.Fatalf("create survey: %v", err)
		}
	}

	mailer := &stubSurveyMailer{rejectDom: "blocked@example.com"}
	svc := services.NewSatisfactionService(db, discardLogger())
	svc.SetSurveyMailer(mailer)

	w := NewSurveyEmailWorker(svc, discardLogger()).(*SurveyEmailWorker)
	w.scanInterval = 20 * time.Millisecond
	if err := w.Start(); err != nil {
		t.Fatalf("Start() = %v", err)
	}
	waitAtLeast(t, 10*time.Second, func() bool { return mailer.calls.Load() >= 2 }, "worker never scanned pending surveys")
	stopCtx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	if err := w.Stop(stopCtx); err != nil {
		t.Fatalf("Stop() = %v", err)
	}

	// Poll DB to confirm the OK survey was marked sent — under CI race
	// detection the in-process SQLite commit may lag behind the mailer
	// call counter, so a single-shot read can see stale state.
	var sent models.SatisfactionSurvey
	waitAtLeast(t, 5*time.Second, func() bool {
		_ = db.Where("survey_token = ?", "tok-ok").First(&sent).Error
		return sent.Status == "sent" && sent.SentAt != nil
	}, "delivered survey was never marked sent")
	var retried models.SatisfactionSurvey
	if err := db.Where("survey_token = ?", "tok-blocked").First(&retried).Error; err != nil {
		t.Fatalf("load blocked survey: %v", err)
	}
	if retried.Status != "queued" {
		t.Fatalf("failed survey must stay queued for retry, got %q", retried.Status)
	}
}

// lockedBuffer 是并发安全的日志缓冲：logrus 的后台扫描 goroutine 写、
// 测试轮询断言读，strings.Builder 本身不支持并发访问。
type lockedBuffer struct {
	mu  sync.Mutex
	buf strings.Builder
}

func (b *lockedBuffer) Write(p []byte) (int, error) {
	b.mu.Lock()
	defer b.mu.Unlock()
	return b.buf.Write(p)
}

func (b *lockedBuffer) String() string {
	b.mu.Lock()
	defer b.mu.Unlock()
	return b.buf.String()
}

// TestSurveyEmailWorkerLogsScanFailures 覆盖扫描失败分支：satisfaction_surveys
// 表被删后，ProcessPendingSurveyEmails 的过期兜底 UPDATE 必然失败，worker 只
// 告警（scan failed）并 continue 到下一轮 tick，循环本身不退出。与投递测试并行。
func TestSurveyEmailWorkerLogsScanFailures(t *testing.T) {
	t.Parallel()
	db, err := gorm.Open(sqlite.Open(":memory:"), &gorm.Config{})
	if err != nil {
		t.Fatalf("open sqlite: %v", err)
	}
	if err := db.AutoMigrate(&models.User{}, &models.SatisfactionSurvey{}); err != nil {
		t.Fatalf("auto migrate: %v", err)
	}
	if err := db.Migrator().DropTable(&models.SatisfactionSurvey{}); err != nil {
		t.Fatalf("drop table: %v", err)
	}

	buf := &lockedBuffer{}
	logger := logrus.New()
	logger.SetOutput(buf)

	svc := services.NewSatisfactionService(db, logger)
	svc.SetSurveyMailer(&stubSurveyMailer{})

	w := NewSurveyEmailWorker(svc, logger).(*SurveyEmailWorker)
	w.scanInterval = 20 * time.Millisecond
	if err := w.Start(); err != nil {
		t.Fatalf("Start() = %v", err)
	}
	waitAtLeast(t, 10*time.Second, func() bool {
		return strings.Contains(buf.String(), "survey-email worker: scan failed")
	}, "survey worker never logged the scan failure")
	if err := w.Stop(context.Background()); err != nil {
		t.Fatalf("Stop() = %v", err)
	}
}

// ---------------------------------------------------------------------------
// EmailPollWorker
// ---------------------------------------------------------------------------

type producingPollProcessor struct {
	calls atomic.Int64
}

func (p *producingPollProcessor) PollOnce(ctx context.Context) (int, error) {
	p.calls.Add(1)
	return 2, nil
}

func TestEmailPollWorkerConstructorDefaults(t *testing.T) {
	w := NewEmailPollWorker(&countingPollProcessor{}, 0, nil).(*EmailPollWorker)
	if w.interval != time.Minute {
		t.Fatalf("default interval = %v, want 1m", w.interval)
	}
	if w.logger == nil {
		t.Fatal("nil logger must fall back to standard logger")
	}
}

func TestEmailPollWorkerNilProcessorAndStopBeforeStart(t *testing.T) {
	w := NewEmailPollWorker(nil, time.Minute, nil)
	if err := w.Start(); err != nil {
		t.Fatalf("Start() with nil processor = %v", err)
	}
	if err := w.Stop(context.Background()); err != nil {
		t.Fatalf("Stop() before start = %v", err)
	}
}

func TestEmailPollWorkerLogsIngestedEmails(t *testing.T) {
	proc := &producingPollProcessor{}
	w := NewEmailPollWorker(proc, 15*time.Millisecond, discardLogger())
	if err := w.Start(); err != nil {
		t.Fatalf("Start() = %v", err)
	}
	waitAtLeast(t, 2*time.Second, func() bool { return proc.calls.Load() >= 1 }, "worker never polled")
	if err := w.Stop(context.Background()); err != nil {
		t.Fatalf("Stop() = %v", err)
	}
}

func TestEmailPollWorkerStopDuringJitter(t *testing.T) {
	proc := &countingPollProcessor{}
	w := NewEmailPollWorker(proc, time.Hour, discardLogger())
	if err := w.Start(); err != nil {
		t.Fatalf("Start() = %v", err)
	}
	stopCtx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	if err := w.Stop(stopCtx); err != nil {
		t.Fatalf("Stop() during jitter = %v", err)
	}
	if proc.calls.Load() != 0 {
		t.Fatalf("worker must not poll before jitter elapses, calls = %d", proc.calls.Load())
	}
}

func TestEmailPollWorkerStopContextError(t *testing.T) {
	_, cancel := context.WithCancel(context.Background())
	w := &EmailPollWorker{cancel: cancel, done: make(chan struct{})}
	if err := w.Stop(preCancelledCtx()); !errors.Is(err, context.Canceled) {
		t.Fatalf("Stop() with cancelled ctx = %v, want context.Canceled", err)
	}
}

// ---------------------------------------------------------------------------
// QualityReviewWorker
// ---------------------------------------------------------------------------

func TestQualityReviewWorkerConstructorDefaults(t *testing.T) {
	w := NewQualityReviewWorker(nil, 0, nil).(*QualityReviewWorker)
	if w.interval != 5*time.Minute {
		t.Fatalf("default interval = %v, want 5m", w.interval)
	}
	if w.logger == nil {
		t.Fatal("nil logger must fall back to standard logger")
	}
}

func TestQualityReviewWorkerLogsProcessedSessions(t *testing.T) {
	scan := &fakeQualityScan{scans: make(chan struct{}, 8), processed: 4}
	w := NewQualityReviewWorker(scan, 15*time.Millisecond, discardLogger())
	if err := w.Start(); err != nil {
		t.Fatalf("Start() = %v", err)
	}
	select {
	case <-scan.scans:
	case <-time.After(2 * time.Second):
		t.Fatal("scan never ran")
	}
	if err := w.Stop(context.Background()); err != nil {
		t.Fatalf("Stop() = %v", err)
	}
}

func TestQualityReviewWorkerStopDuringJitter(t *testing.T) {
	scan := &fakeQualityScan{scans: make(chan struct{}, 8)}
	w := NewQualityReviewWorker(scan, time.Hour, discardLogger())
	if err := w.Start(); err != nil {
		t.Fatalf("Start() = %v", err)
	}
	stopCtx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	if err := w.Stop(stopCtx); err != nil {
		t.Fatalf("Stop() during jitter = %v", err)
	}
	select {
	case <-scan.scans:
		t.Fatal("worker must not scan before jitter elapses")
	default:
	}
}

func TestQualityReviewWorkerStopContextError(t *testing.T) {
	_, cancel := context.WithCancel(context.Background())
	w := &QualityReviewWorker{cancel: cancel, done: make(chan struct{})}
	if err := w.Stop(preCancelledCtx()); !errors.Is(err, context.Canceled) {
		t.Fatalf("Stop() with cancelled ctx = %v, want context.Canceled", err)
	}
}

// ---------------------------------------------------------------------------
// AutomationTimerWorker
// ---------------------------------------------------------------------------

type countingTimerProcessor struct {
	calls  atomic.Int64
	result int
}

func (c *countingTimerProcessor) ProcessDueTimers(ctx context.Context, now time.Time) int {
	c.calls.Add(1)
	return c.result
}

func TestAutomationTimerWorkerConstructorDefaults(t *testing.T) {
	w := NewAutomationTimerWorker(nil, 0, nil).(*AutomationTimerWorker)
	if w.interval != 30*time.Second {
		t.Fatalf("default interval = %v, want 30s", w.interval)
	}
	if w.logger == nil {
		t.Fatal("nil logger must fall back to standard logger")
	}
}

func TestAutomationTimerWorkerTickerLoop(t *testing.T) {
	proc := &countingTimerProcessor{}
	w := NewAutomationTimerWorker(proc, 15*time.Millisecond, discardLogger())
	if err := w.Start(); err != nil {
		t.Fatalf("Start() = %v", err)
	}
	waitAtLeast(t, 2*time.Second, func() bool { return proc.calls.Load() >= 2 }, "ticker loop did not re-run")
	if err := w.Stop(context.Background()); err != nil {
		t.Fatalf("Stop() = %v", err)
	}
}

func TestAutomationTimerWorkerLogsProcessedTimers(t *testing.T) {
	proc := &countingTimerProcessor{result: 2}
	w := NewAutomationTimerWorker(proc, 15*time.Millisecond, discardLogger())
	if err := w.Start(); err != nil {
		t.Fatalf("Start() = %v", err)
	}
	waitAtLeast(t, 2*time.Second, func() bool { return proc.calls.Load() >= 1 }, "worker never ran")
	if err := w.Stop(context.Background()); err != nil {
		t.Fatalf("Stop() = %v", err)
	}
}

func TestAutomationTimerWorkerStopBeforeStart(t *testing.T) {
	w := NewAutomationTimerWorker(nil, time.Minute, nil)
	if err := w.Stop(context.Background()); err != nil {
		t.Fatalf("Stop() before start = %v", err)
	}
}

func TestAutomationTimerWorkerStopDuringJitter(t *testing.T) {
	proc := &countingTimerProcessor{}
	w := NewAutomationTimerWorker(proc, time.Hour, discardLogger())
	if err := w.Start(); err != nil {
		t.Fatalf("Start() = %v", err)
	}
	stopCtx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	if err := w.Stop(stopCtx); err != nil {
		t.Fatalf("Stop() during jitter = %v", err)
	}
	if proc.calls.Load() != 0 {
		t.Fatalf("worker must not run before jitter elapses, calls = %d", proc.calls.Load())
	}
}

func TestAutomationTimerWorkerStopContextError(t *testing.T) {
	_, cancel := context.WithCancel(context.Background())
	w := &AutomationTimerWorker{cancel: cancel, done: make(chan struct{})}
	if err := w.Stop(preCancelledCtx()); !errors.Is(err, context.Canceled) {
		t.Fatalf("Stop() with cancelled ctx = %v, want context.Canceled", err)
	}
}

// ---------------------------------------------------------------------------
// SLA / audit / revoked-token 的 jitter 期间 Stop（initial select ctx.Done 分支）
// ---------------------------------------------------------------------------

func TestSLAMonitorWorkerStopDuringJitter(t *testing.T) {
	w := NewSLAMonitorWorker(&blockingSLAService{started: make(chan struct{})}, time.Hour, discardLogger())
	if err := w.Start(); err != nil {
		t.Fatalf("Start() = %v", err)
	}
	stopCtx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	if err := w.Stop(stopCtx); err != nil {
		t.Fatalf("Stop() during jitter = %v", err)
	}
}

func TestAuditCleanupWorkerStopDuringJitter(t *testing.T) {
	w := NewAuditCleanupWorker(&countingCleanupService{}, time.Hour, discardLogger())
	if err := w.Start(); err != nil {
		t.Fatalf("Start() = %v", err)
	}
	stopCtx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	if err := w.Stop(stopCtx); err != nil {
		t.Fatalf("Stop() during jitter = %v", err)
	}
}

func TestRevokedTokenCleanupWorkerStopDuringJitter(t *testing.T) {
	w := NewRevokedTokenCleanupWorker(&countingCleanupService{}, time.Hour, discardLogger())
	if err := w.Start(); err != nil {
		t.Fatalf("Start() = %v", err)
	}
	stopCtx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	if err := w.Stop(stopCtx); err != nil {
		t.Fatalf("Stop() during jitter = %v", err)
	}
}

// ---------------------------------------------------------------------------
// RegisterDefaultWorkers：全部依赖非 nil + 审计/令牌吊销清理开启
// ---------------------------------------------------------------------------

type fullRuntimeWorkerDeps struct{}

func (fullRuntimeWorkerDeps) StatisticsServiceForWorker() *services.StatisticsService {
	return &services.StatisticsService{}
}

func (fullRuntimeWorkerDeps) SLAServiceForWorker() *services.SLAService {
	return &services.SLAService{}
}

func (fullRuntimeWorkerDeps) WebhookDeliveryForWorker() webhookapp.Processor {
	return &stubWebhookProcessor{}
}

func (fullRuntimeWorkerDeps) EmailPollAdapterForWorker() emaildelivery.PollProcessor {
	return &countingPollProcessor{}
}

func (fullRuntimeWorkerDeps) QualityScanForWorker() *qualityapp.QualityService {
	return &qualityapp.QualityService{}
}

func (fullRuntimeWorkerDeps) WaitingQueueForWorker() *routingdelivery.HandlerServiceAdapter {
	return &routingdelivery.HandlerServiceAdapter{}
}

func (fullRuntimeWorkerDeps) SurveysForWorker() *services.SatisfactionService {
	return &services.SatisfactionService{}
}

func (fullRuntimeWorkerDeps) AutomationTimersForWorker() automationapp.TimerProcessor {
	return fakeTimerProcessor{}
}

func TestRegisterDefaultWorkersRegistersAllWorkers(t *testing.T) {
	app, err := bootstrap.BuildApp(config.GetDefaultConfig())
	if err != nil {
		t.Fatalf("BuildApp() error = %v", err)
	}
	cfg := config.GetDefaultConfig()
	cfg.Security.Audit.Enabled = true
	cfg.Security.TokenRevocation.Enabled = true

	db, err := gorm.Open(sqlite.Open(":memory:"), &gorm.Config{})
	if err != nil {
		t.Fatalf("open sqlite: %v", err)
	}

	RegisterDefaultWorkers(app, cfg, db, fullRuntimeWorkerDeps{})

	want := map[string]bool{
		"statistics-daily-stats":  true,
		"sla-monitor":             true,
		"webhook-delivery":        true,
		"email-poll":              true,
		"quality-review-scan":     true,
		"waiting-queue-dispatch":  true,
		"survey-email-send":       true,
		"automation-timer":        true,
		"audit-retention-cleanup": true,
		"revoked-token-cleanup":   true,
	}
	if len(app.Workers) != len(want) {
		names := make([]string, 0, len(app.Workers))
		for _, w := range app.Workers {
			names = append(names, w.Name())
		}
		t.Fatalf("expected %d workers, got %d: %v", len(want), len(app.Workers), names)
	}
	for _, w := range app.Workers {
		if !want[w.Name()] {
			t.Fatalf("unexpected worker %q", w.Name())
		}
		delete(want, w.Name())
	}
	if len(want) != 0 {
		names := make([]string, 0, len(want))
		for name := range want {
			names = append(names, name)
		}
		t.Fatalf("missing workers: %v", names)
	}
}
