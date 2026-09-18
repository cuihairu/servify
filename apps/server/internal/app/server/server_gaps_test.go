package server

import (
	"context"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"

	"servify/apps/server/internal/config"
	"servify/apps/server/internal/models"
	conversationapp "servify/apps/server/internal/modules/conversation/application"
	emaildelivery "servify/apps/server/internal/modules/email/delivery"
	"servify/apps/server/internal/platform/eventbus"
	"servify/apps/server/internal/platform/voiceprotocol"

	"github.com/gin-gonic/gin"
	"github.com/sirupsen/logrus"
)

// ---- 日志捕获：用于断言防御性 Warn/Error 分支真的发生 ----

type logCapture struct {
	mu      sync.Mutex
	entries []string
	notify  chan struct{}
}

func newLogCapture() *logCapture {
	return &logCapture{notify: make(chan struct{}, 64)}
}

func (c *logCapture) Fire(e *logrus.Entry) error {
	c.mu.Lock()
	c.entries = append(c.entries, e.Message)
	c.mu.Unlock()
	select {
	case c.notify <- struct{}{}:
	default:
	}
	return nil
}

func (c *logCapture) Levels() []logrus.Level {
	return []logrus.Level{logrus.DebugLevel, logrus.InfoLevel, logrus.WarnLevel, logrus.ErrorLevel}
}

func (c *logCapture) waitContains(t *testing.T, substr string) {
	t.Helper()
	deadline := time.After(2 * time.Second)
	for {
		c.mu.Lock()
		for _, m := range c.entries {
			if strings.Contains(m, substr) {
				c.mu.Unlock()
				return
			}
		}
		c.mu.Unlock()
		select {
		case <-c.notify:
		case <-deadline:
			t.Fatalf("log entry containing %q not found; got %v", substr, c.entries)
		}
	}
}

func newCaptureLogger(c *logCapture) *logrus.Logger {
	logger := logrus.New()
	logger.SetOutput(io.Discard)
	logger.AddHook(c)
	return logger
}

// ---- outboundSeen 去重环：空 ID 守卫与回绕逐出 ----

func TestOutboundSeenGuardsAndRingEviction(t *testing.T) {
	s := newOutboundSeen(1)
	if s.seenBefore("") {
		t.Fatal("empty id must never be treated as seen")
	}
	if s.seenBefore("a") {
		t.Fatal("first sight of a must be new")
	}
	if !s.seenBefore("a") {
		t.Fatal("second sight of a must be deduplicated")
	}
	// 容量 1：b 入环必须逐出 a
	if s.seenBefore("b") {
		t.Fatal("first sight of b must be new")
	}
	if s.seenBefore("a") {
		t.Fatal("evicted id a must be forgotten after ring wraps")
	}
	// a 重新入环时把 b 逐出：b 变回「未见」
	if s.seenBefore("b") {
		t.Fatal("b was evicted when a re-entered the ring; must be treated as new")
	}
}

// ---- ChannelOutboundDispatcher 构造与 Register 防御分支 ----

func TestChannelOutboundDispatcherDefaultsLogger(t *testing.T) {
	d := NewChannelOutboundDispatcher(nil, nil, nil)
	if d.logger == nil {
		t.Fatal("nil logger must fall back to standard logger")
	}
}

func TestChannelOutboundDispatcherRegisterGuards(t *testing.T) {
	var nilDispatcher *ChannelOutboundDispatcher
	nilDispatcher.Register(eventbus.NewInMemoryBus()) // nil 接收者：无操作

	d := &ChannelOutboundDispatcher{} // db/email 均未就绪
	d.Register(nil)
	d.Register(eventbus.NewInMemoryBus())
}

// ---- dispatch 的会话/归属查询失败分支 ----

func TestOutboundDispatchSessionNotFoundSkips(t *testing.T) {
	_, bus, sender, _ := newOutboundTestEnv(t)
	bus.Publish(context.Background(), agentMessageEvent("sess-ghost", "无人认领"))
	time.Sleep(150 * time.Millisecond)
	if got := sender.count(); got != 0 {
		t.Fatalf("unknown session must not send, got %d", got)
	}
}

func TestOutboundDispatchOwnerLookupFailureLogs(t *testing.T) {
	db := newOutboundTestDB(t)
	capture := newLogCapture()
	d := newOutboundDispatcherWithSender(t, db, newRecordingSMTPSender(), capture)
	bus := eventbus.NewInMemoryBus()
	d.Register(bus)

	// 会话存在但指向不存在的用户：归属查询失败须告警且不出站
	now := time.Now()
	if err := db.DB.Create(&models.Session{
		ID: "sess-orphan", TenantID: "t", WorkspaceID: "w", UserID: 987654,
		Status: "active", Platform: "email", StartedAt: now, CreatedAt: now, UpdatedAt: now,
	}).Error; err != nil {
		t.Fatalf("seed session: %v", err)
	}

	bus.Publish(context.Background(), agentMessageEvent("sess-orphan", "回复"))
	capture.waitContains(t, "has no owner user")
}

func TestOutboundDispatchSendFailureLogs(t *testing.T) {
	db := newOutboundTestDB(t)
	db.seedEmailSession(t, "sess-fail", 11, "c11@example.com")

	capture := newLogCapture()
	// SMTP sender 未配置：Adapter.Send 必失败，异步 goroutine 只记日志
	adapter := emaildelivery.NewAdapter(emaildelivery.AdapterDeps{
		SMTP:     nil,
		Ingestor: nil,
		From:     "support@example.com",
		Interval: time.Minute,
		Logger:   logrus.New(),
	})
	d := NewChannelOutboundDispatcher(db.DB, adapter, newCaptureLogger(capture))
	bus := eventbus.NewInMemoryBus()
	d.Register(bus)

	bus.Publish(context.Background(), agentMessageEvent("sess-fail", "投递失败"))
	capture.waitContains(t, "failed to send email reply")
}

func newOutboundDispatcherWithSender(t *testing.T, db *outboundTestDB, sender *recordingSMTPSender, capture *logCapture) *ChannelOutboundDispatcher {
	t.Helper()
	adapter := emaildelivery.NewAdapter(emaildelivery.AdapterDeps{
		SMTP:     sender,
		Ingestor: nil,
		From:     "support@example.com",
		Interval: time.Minute,
		Logger:   logrus.New(),
	})
	return NewChannelOutboundDispatcher(db.DB, adapter, newCaptureLogger(capture))
}

// ---- resolveMessage 的三条守卫分支 ----

func TestResolveMessageGuardBranches(t *testing.T) {
	db := newOutboundTestDB(t)
	d := &ChannelOutboundDispatcher{db: db.DB}

	// ConversationEvent 但 payload 不是 ConversationMessageDTO
	nonDTO := conversationapp.NewConversationEvent(
		conversationapp.ConversationMessageReceivedEventName, "sess-x", "raw-string-payload")
	if _, _, ok := d.resolveMessage(nonDTO); ok {
		t.Fatal("non-DTO payload must not resolve")
	}

	// AggregateID 缺少 conversation: 前缀
	if _, _, ok := d.resolveMessage(&aggregateOnlyEvent{
		id: "evt-1", name: conversationapp.ConversationMessageReceivedEventName, aggregateID: "sess-raw",
	}); ok {
		t.Fatal("aggregate id without prefix must not resolve")
	}

	// 有前缀但库里查不到该会话的坐席消息
	if _, _, ok := d.resolveMessage(&aggregateOnlyEvent{
		id: "evt-2", name: conversationapp.ConversationMessageReceivedEventName, aggregateID: "conversation:ghost",
	}); ok {
		t.Fatal("missing message row must not resolve")
	}
}

// ---- surveyEmailMailer 适配器 ----

func TestSurveyEmailMailerComposeErrorAndSend(t *testing.T) {
	sender := newRecordingSMTPSender()

	// from 为空：ComposeTextMessage 失败，错误原样返回
	broken := &surveyEmailMailer{sender: sender, from: ""}
	if err := broken.SendSurveyEmail(context.Background(), "to@example.com", "subj", "body"); err == nil {
		t.Fatal("empty from must fail message composition")
	}

	ok := &surveyEmailMailer{sender: sender, from: "support@example.com"}
	if err := ok.SendSurveyEmail(context.Background(), "to@example.com", "subj", "body"); err != nil {
		t.Fatalf("SendSurveyEmail() error = %v", err)
	}
	waitSent(t, sender, 1)
	if !strings.Contains(string(sender.last()), "To: to@example.com") {
		t.Fatalf("unexpected message: %s", sender.last())
	}
}

// ---- scoped AI handler 的 ResetCircuitBreaker 委托分支 ----

func TestScopedAIHandlerResetCircuitBreakerDelegates(t *testing.T) {
	handler := NewScopedAIHandlerService(config.GetDefaultConfig(), logrus.New(), openScopedAITestDB(t), stubFallbackAIHandler{}, nil, nil)
	if !handler.ResetCircuitBreaker() {
		t.Fatal("expected fallback circuit breaker reset to succeed")
	}
}

// ---- buildQualityService：LLM 打分两分支 + rules-only ----

func TestBuildRuntimeQualityLLMScorerWithAPIKey(t *testing.T) {
	cfg := newRuntimeTestConfig(t)
	cfg.Quality.Enabled = true
	cfg.Quality.LLM.Enabled = true
	cfg.AI.OpenAI.APIKey = "quality-test-key"

	rt, err := BuildRuntime(cfg, logrus.New(), newRuntimeTestDB(t), nil, eventbus.NewInMemoryBus())
	if err != nil {
		t.Fatalf("BuildRuntime() error = %v", err)
	}
	defer func() { _ = rt.Stop(context.Background()) }()

	if rt.QualityScanForWorker() == nil {
		t.Fatal("expected quality service for worker")
	}
	if rt.QualityHandlerService == nil {
		t.Fatal("expected quality handler service to be wired")
	}
}

func TestBuildRuntimeQualityLLMMissingKeyWarns(t *testing.T) {
	cfg := newRuntimeTestConfig(t)
	cfg.Quality.Enabled = true
	cfg.Quality.LLM.Enabled = true // APIKey 留空

	buf := &strings.Builder{}
	logger := logrus.New()
	logger.SetOutput(buf)

	rt, err := BuildRuntime(cfg, logger, newRuntimeTestDB(t), nil, eventbus.NewInMemoryBus())
	if err != nil {
		t.Fatalf("BuildRuntime() error = %v", err)
	}
	defer func() { _ = rt.Stop(context.Background()) }()

	if rt.QualityScanForWorker() == nil {
		t.Fatal("quality must fall back to rules-only instead of disappearing")
	}
	if !strings.Contains(buf.String(), "falling back to rules-only") {
		t.Fatalf("expected LLM fallback warning, got %q", buf.String())
	}
}

func TestBuildRuntimeQualityRulesOnly(t *testing.T) {
	cfg := newRuntimeTestConfig(t)
	cfg.Quality.Enabled = true
	cfg.Quality.LLM.Enabled = false

	rt, err := BuildRuntime(cfg, logrus.New(), newRuntimeTestDB(t), nil, eventbus.NewInMemoryBus())
	if err != nil {
		t.Fatalf("BuildRuntime() error = %v", err)
	}
	defer func() { _ = rt.Stop(context.Background()) }()

	if rt.QualityScanForWorker() == nil {
		t.Fatal("expected rules-only quality service")
	}
}

// ---- worker accessors：nil 与非 nil 分支 ----

func TestRuntimeWorkerAccessorsNilOnEmptyRuntime(t *testing.T) {
	rt := &Runtime{Config: config.GetDefaultConfig()}
	// 注：WebhookDeliveryForWorker 会把 nil 的 *webhookapp.Service 装进接口
	// （typed nil），故此处不断言它；其覆盖走 BuildRuntime 的非 nil 断言。
	if rt.EmailPollAdapterForWorker() != nil {
		t.Fatal("expected nil email adapter on empty runtime")
	}
	if rt.QualityScanForWorker() != nil {
		t.Fatal("expected nil quality scanner on empty runtime")
	}
	if rt.WaitingQueueForWorker() != nil {
		t.Fatal("expected nil waiting dispatcher on empty runtime")
	}
	if rt.AutomationTimersForWorker() != nil {
		t.Fatal("expected nil timer processor on empty runtime")
	}
}

func TestBuildRuntimeWorkerAccessorsReturnWiredServices(t *testing.T) {
	cfg := newRuntimeTestConfig(t)
	cfg.Routing.Enabled = true

	rt, err := BuildRuntime(cfg, logrus.New(), newRuntimeTestDB(t), nil, eventbus.NewInMemoryBus())
	if err != nil {
		t.Fatalf("BuildRuntime() error = %v", err)
	}
	defer func() { _ = rt.Stop(context.Background()) }()

	if rt.WebhookDeliveryForWorker() == nil {
		t.Fatal("expected webhook processor")
	}
	if rt.WaitingQueueForWorker() != rt.transferHandler {
		t.Fatal("expected waiting dispatcher to be the transfer handler")
	}
	if rt.SurveysForWorker() == nil {
		t.Fatal("expected satisfaction service for worker")
	}
	if rt.AutomationTimersForWorker() == nil {
		t.Fatal("expected automation timer processor")
	}
}

// ---- email 渠道装配（wireEmailRuntime + survey mailer 注入）----

func TestBuildRuntimeWiresEmailChannel(t *testing.T) {
	cfg := newRuntimeTestConfig(t)
	cfg.Email.Enabled = true
	cfg.Email.SMTP.From = "" // 触发 Username 回退
	cfg.Email.Username = "support@example.com"

	rt, err := BuildRuntime(cfg, logrus.New(), newRuntimeTestDB(t), nil, eventbus.NewInMemoryBus())
	if err != nil {
		t.Fatalf("BuildRuntime() error = %v", err)
	}
	defer func() { _ = rt.Stop(context.Background()) }()

	if rt.smtpSender == nil || rt.emailAdapter == nil {
		t.Fatal("expected smtp sender and email adapter to be wired")
	}
	if rt.EmailPollAdapterForWorker() == nil {
		t.Fatal("expected email poll adapter for worker")
	}
	if rt.SurveysForWorker() == nil {
		t.Fatal("expected satisfaction service to be wired")
	}
}

// ---- OIDC：fail-fast 错误与 discovery 成功装配 ----

func TestBuildRuntimeOIDCEnabledWithoutIssuerFails(t *testing.T) {
	cfg := newRuntimeTestConfig(t)
	cfg.OIDC.Enabled = true // issuer/client 留空

	rt, err := BuildRuntime(cfg, logrus.New(), newRuntimeTestDB(t), nil, eventbus.NewInMemoryBus())
	if err == nil {
		t.Fatal("expected BuildRuntime to fail on OIDC enabled without issuer")
	}
	if !strings.Contains(err.Error(), "oidc provider") {
		t.Fatalf("expected oidc provider error, got %v", err)
	}
	if rt != nil {
		t.Fatal("runtime must be nil on failure")
	}
}

func TestBuildRuntimeOIDCDiscoveryWiresProviderAndRoutes(t *testing.T) {
	discovery := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"issuer": "` + "http://" + r.Host + `"}`))
	}))
	defer discovery.Close()

	cfg := newRuntimeTestConfig(t)
	cfg.OIDC.Enabled = true
	cfg.OIDC.Issuer = discovery.URL
	cfg.OIDC.ClientID = "client-id"
	cfg.OIDC.ClientSecret = "client-secret"
	cfg.OIDC.RedirectURL = "http://127.0.0.1:8080/api/v1/auth/oidc/callback"

	rt, err := BuildRuntime(cfg, logrus.New(), newRuntimeTestDB(t), nil, eventbus.NewInMemoryBus())
	if err != nil {
		t.Fatalf("BuildRuntime() error = %v", err)
	}
	defer func() { _ = rt.Stop(context.Background()) }()

	if rt.OIDCProvider == nil {
		t.Fatal("expected OIDC provider to be wired")
	}

	found := false
	for _, route := range BuildRouter(rt.RouterDependencies()).Routes() {
		if route.Path == "/api/v1/auth/oidc/start" {
			found = true
		}
	}
	if !found {
		t.Fatal("expected admin SSO routes when provider is configured")
	}
}

// ---- upload 装配：provider 不可用 fail-closed 与非法 size 告警 ----

func TestBuildRouterUploadProviderUnavailableDisablesRoutes(t *testing.T) {
	gin.SetMode(gin.TestMode)
	capture := newLogCapture()
	cfg := testRouterConfig()
	cfg.Upload.Provider = "bogus-provider"

	router := BuildRouter(Dependencies{Config: cfg, Logger: newCaptureLogger(capture)})
	for _, route := range router.Routes() {
		if route.Path == "/api/v1/upload" {
			t.Fatal("upload endpoint must stay disabled when provider construction fails")
		}
	}
	capture.waitContains(t, "unavailable; upload endpoints disabled")
}

func TestBuildRouterUploadInvalidMaxFileSizeWarns(t *testing.T) {
	gin.SetMode(gin.TestMode)
	capture := newLogCapture()
	cfg := testRouterConfig()
	cfg.Upload.Provider = "local"
	cfg.Upload.StoragePath = t.TempDir()
	cfg.Upload.MaxFileSize = "not-a-size"

	router := BuildRouter(Dependencies{Config: cfg, Logger: newCaptureLogger(capture)})
	found := false
	for _, route := range router.Routes() {
		if route.Path == "/api/v1/upload" {
			found = true
		}
	}
	if !found {
		t.Fatal("expected upload endpoint despite invalid size config")
	}
	capture.waitContains(t, "invalid upload.max_file_size")
}

// ---- PSTN webhook 路由：协议匹配但缺 hosted 合同时不挂载 ----

type stubPSTNSignalingOnly struct{}

func (stubPSTNSignalingOnly) Name() string { return "stub-pstn-signaling" }
func (stubPSTNSignalingOnly) Protocol() voiceprotocol.Protocol {
	return voiceprotocol.ProtocolHostedVendorWebhook
}
func (stubPSTNSignalingOnly) MapInvite(context.Context, interface{}) (voiceprotocol.CallEvent, error) {
	return voiceprotocol.CallEvent{}, nil
}
func (stubPSTNSignalingOnly) MapAnswer(context.Context, interface{}) (voiceprotocol.CallEvent, error) {
	return voiceprotocol.CallEvent{}, nil
}
func (stubPSTNSignalingOnly) MapHold(context.Context, interface{}) (voiceprotocol.CallEvent, error) {
	return voiceprotocol.CallEvent{}, nil
}
func (stubPSTNSignalingOnly) MapResume(context.Context, interface{}) (voiceprotocol.CallEvent, error) {
	return voiceprotocol.CallEvent{}, nil
}
func (stubPSTNSignalingOnly) MapHangup(context.Context, interface{}) (voiceprotocol.CallEvent, error) {
	return voiceprotocol.CallEvent{}, nil
}
func (stubPSTNSignalingOnly) MapTransfer(context.Context, interface{}) (voiceprotocol.CallEvent, error) {
	return voiceprotocol.CallEvent{}, nil
}
func (stubPSTNSignalingOnly) MapDTMF(context.Context, interface{}) (voiceprotocol.CallEvent, error) {
	return voiceprotocol.CallEvent{}, nil
}

type stubHostedVendorAdapter struct {
	stubPSTNSignalingOnly
}

func (stubHostedVendorAdapter) WebhookPath() string { return "/public/voice/webhooks/stub" }
func (stubHostedVendorAdapter) ValidateSignature(context.Context, string, map[string]string, []byte) error {
	return nil
}
func (stubHostedVendorAdapter) MapWebhook(context.Context, interface{}) ([]voiceprotocol.CallEvent, []voiceprotocol.MediaEvent, error) {
	return nil, nil, nil
}

func TestRegisterPSTNWebhookRoutesAdapterVariants(t *testing.T) {
	gin.SetMode(gin.TestMode)
	cfg := testRouterConfig()

	// 分支一：注册了 hosted-vendor 协议，但适配器未实现 Webhook 合同 → 不挂载
	signalingOnly := voiceprotocol.NewRegistry()
	if err := signalingOnly.RegisterSignaling(stubPSTNSignalingOnly{}); err != nil {
		t.Fatalf("register signaling adapter: %v", err)
	}
	router := BuildRouter(Dependencies{Config: cfg, VoiceProtocolRegistry: signalingOnly})
	for _, route := range router.Routes() {
		if strings.HasPrefix(route.Path, "/public/voice/webhooks/") {
			t.Fatalf("webhook route must stay unmounted without hosted vendor contract, got %s", route.Path)
		}
	}

	// 分支二：完整 hosted 适配器 → 挂载 POST 路由
	hosted := voiceprotocol.NewRegistry()
	if err := hosted.RegisterSignaling(stubHostedVendorAdapter{}); err != nil {
		t.Fatalf("register hosted adapter: %v", err)
	}
	router = BuildRouter(Dependencies{Config: cfg, VoiceProtocolRegistry: hosted})
	mounted := false
	for _, route := range router.Routes() {
		if route.Method == http.MethodPost && route.Path == "/public/voice/webhooks/stub" {
			mounted = true
		}
	}
	if !mounted {
		t.Fatal("expected PSTN webhook route for hosted vendor adapter")
	}
}

// ---- voice：twilio PSTN 缺 token fail-fast / 配置齐备时装配 ----

func TestBuildRuntimeTwilioVoiceWithoutTokenFails(t *testing.T) {
	cfg := newRuntimeTestConfig(t)
	cfg.Voice.PSTN.Provider = "twilio"
	cfg.Voice.Twilio.AuthToken = ""

	_, err := BuildRuntime(cfg, logrus.New(), newRuntimeTestDB(t), nil, eventbus.NewInMemoryBus())
	if err == nil {
		t.Fatal("expected BuildRuntime to fail when twilio auth token is empty")
	}
	if !strings.Contains(err.Error(), "voice.twilio.auth_token") {
		t.Fatalf("expected auth token error, got %v", err)
	}
}

func TestBuildRuntimeWiresTwilioVoice(t *testing.T) {
	cfg := newRuntimeTestConfig(t)
	cfg.Voice.PSTN.Provider = "twilio"
	cfg.Voice.Twilio.AuthToken = "twilio-token"

	rt, err := BuildRuntime(cfg, logrus.New(), newRuntimeTestDB(t), nil, eventbus.NewInMemoryBus())
	if err != nil {
		t.Fatalf("BuildRuntime() error = %v", err)
	}
	defer func() { _ = rt.Stop(context.Background()) }()

	adapter, ok := rt.VoiceProtocolRegistry.Signaling(voiceprotocol.ProtocolHostedVendorWebhook)
	if !ok || adapter == nil {
		t.Fatal("expected hosted vendor signaling adapter to be registered")
	}
}
