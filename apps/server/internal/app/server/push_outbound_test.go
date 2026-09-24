package server

import (
	"context"
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/rsa"
	"crypto/x509"
	"encoding/json"
	"encoding/pem"
	"fmt"
	"strings"
	"sync"
	"testing"
	"time"

	"servify/apps/server/internal/models"
	conversationapp "servify/apps/server/internal/modules/conversation/application"
	"servify/apps/server/internal/platform/eventbus"
	"servify/apps/server/internal/platform/realtime"

	"github.com/glebarez/sqlite"
	"github.com/sirupsen/logrus"
	"gorm.io/gorm"
)

// ---- 记录型通道与在线面假体 ----

type recordingPushSender struct {
	mu     sync.Mutex
	calls  []pushCall
	notify chan struct{}
	failOn string // 非空时对该 deviceToken 返回错误
}

type pushCall struct{ token, title, body, sessionID string }

func (s *recordingPushSender) Send(_ context.Context, deviceToken, title, body string, data map[string]string) error {
	s.mu.Lock()
	s.calls = append(s.calls, pushCall{token: deviceToken, title: title, body: body, sessionID: data["session_id"]})
	s.mu.Unlock()
	if s.failOn != "" && s.failOn == deviceToken {
		s.notify <- struct{}{}
		return fmt.Errorf("gateway down")
	}
	s.notify <- struct{}{}
	return nil
}

func (s *recordingPushSender) count() int {
	s.mu.Lock()
	defer s.mu.Unlock()
	return len(s.calls)
}

func (s *recordingPushSender) last() pushCall {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.calls[len(s.calls)-1]
}

func newRecordingPushSender() *recordingPushSender {
	return &recordingPushSender{notify: make(chan struct{}, 16)}
}

func waitPush(t *testing.T, s *recordingPushSender, want int) {
	t.Helper()
	deadline := time.After(2 * time.Second)
	for s.count() < want {
		select {
		case <-s.notify:
		case <-deadline:
			t.Fatalf("timed out waiting %d pushes, got %d", want, s.count())
		}
	}
}

type fakePresence struct{ online map[string]bool }

func (f *fakePresence) IsSessionConnected(id string) bool { return f.online[id] }

// ---- 测试库 ----

func newPushOutboundDB(t *testing.T) *gorm.DB {
	t.Helper()
	db, err := gorm.Open(sqlite.Open(uniqueMemDSN("file:push_outbound_"+strings.ReplaceAll(t.Name(), "/", "_"))), &gorm.Config{})
	if err != nil {
		t.Fatalf("open sqlite: %v", err)
	}
	if err := db.AutoMigrate(&models.Session{}, &models.User{}, &models.Message{}, &models.PushToken{}); err != nil {
		t.Fatalf("auto migrate: %v", err)
	}
	return db
}

func seedPushVisitorSession(t *testing.T, db *gorm.DB, sessionID string) {
	t.Helper()
	now := time.Now()
	if err := db.Create(&models.Session{
		ID: sessionID, TenantID: "t", WorkspaceID: "w",
		Status: "active", Platform: "chat", StartedAt: now, CreatedAt: now, UpdatedAt: now,
	}).Error; err != nil {
		t.Fatalf("seed session: %v", err)
	}
	for _, tok := range []struct{ platform, token string }{
		{"android", "fcm-device-1"},
		{"ios", "apns-device-1"},
	} {
		if err := db.Create(&models.PushToken{
			TenantID: "t", WorkspaceID: "w", SessionID: sessionID,
			Platform: tok.platform, Token: tok.token,
		}).Error; err != nil {
			t.Fatalf("seed push token: %v", err)
		}
	}
}

func agentPushEvent(sessionID, content string) conversationapp.ConversationEvent {
	return conversationapp.NewConversationEvent(
		conversationapp.ConversationMessageReceivedEventName,
		sessionID,
		conversationapp.ConversationMessageDTO{
			ID: "m1", ConversationID: sessionID, Sender: "agent",
			Kind: "text", Content: content, CreatedAt: time.Now(),
		},
	)
}

func newPushOutbound(t *testing.T) (*PushOutboundDispatcher, *eventbus.InMemoryBus, *recordingPushSender, *recordingPushSender, *fakePresence, *gorm.DB) {
	t.Helper()
	db := newPushOutboundDB(t)
	fcm, apns := newRecordingPushSender(), newRecordingPushSender()
	presence := &fakePresence{online: map[string]bool{}}
	d := NewPushOutboundDispatcher(db, presence, fcm, apns, logrus.New())
	bus := eventbus.NewInMemoryBus()
	d.Register(bus)
	return d, bus, fcm, apns, presence, db
}

// ---- 用例 ----

func TestPushOutbound_DispatchesOfflineVisitorOnBothChannels(t *testing.T) {
	_, bus, fcm, apns, presence, db := newPushOutbound(t)
	seedPushVisitorSession(t, db, "sess-1")
	presence.online["sess-1"] = false

	bus.Publish(context.Background(), agentPushEvent("sess-1", "你的问题已解决，请查收"))

	waitPush(t, fcm, 1)
	waitPush(t, apns, 1)
	if got := fcm.last(); got.token != "fcm-device-1" || got.sessionID != "sess-1" {
		t.Fatalf("fcm call = %+v", got)
	}
	if got := apns.last(); got.token != "apns-device-1" {
		t.Fatalf("apns call = %+v", got)
	}
}

func TestPushOutbound_SuppressesOnlineVisitor(t *testing.T) {
	_, bus, fcm, apns, presence, db := newPushOutbound(t)
	seedPushVisitorSession(t, db, "sess-1")
	presence.online["sess-1"] = true

	bus.Publish(context.Background(), agentPushEvent("sess-1", "在线时不应推送"))
	time.Sleep(100 * time.Millisecond)
	if fcm.count()+apns.count() != 0 {
		t.Fatalf("online suppression failed: fcm=%d apns=%d", fcm.count(), apns.count())
	}
}

func TestPushOutbound_SkipsNonAgentAndEmptyContent(t *testing.T) {
	_, bus, fcm, apns, _, db := newPushOutbound(t)
	seedPushVisitorSession(t, db, "sess-1")

	visitor := conversationapp.NewConversationEvent(
		conversationapp.ConversationMessageReceivedEventName, "sess-1",
		conversationapp.ConversationMessageDTO{
			ID: "m2", ConversationID: "sess-1", Sender: "visitor",
			Kind: "text", Content: "访客消息", CreatedAt: time.Now(),
		})
	bus.Publish(context.Background(), visitor)
	bus.Publish(context.Background(), agentPushEvent("sess-1", "   ")) // 空 content
	time.Sleep(100 * time.Millisecond)
	if fcm.count()+apns.count() != 0 {
		t.Fatal("non-agent/empty content must not dispatch")
	}
}

func TestPushOutbound_DeduplicatesEvent(t *testing.T) {
	_, bus, fcm, _, _, db := newPushOutbound(t)
	seedPushVisitorSession(t, db, "sess-1")

	evt := agentPushEvent("sess-1", "同一事件")
	bus.Publish(context.Background(), evt)
	bus.Publish(context.Background(), evt) // bus 重复投递
	waitPush(t, fcm, 1)
	time.Sleep(100 * time.Millisecond)
	if fcm.count() != 1 {
		t.Fatalf("dedup failed: %d", fcm.count())
	}
}

func TestPushOutbound_SkipsWithoutTokenRows(t *testing.T) {
	_, bus, fcm, apns, _, db := newPushOutbound(t)
	seedPushVisitorSession(t, db, "sess-1")
	db.Where("1 = 1").Delete(&models.PushToken{}) // 清掉注册行

	bus.Publish(context.Background(), agentPushEvent("sess-1", "无注册行"))
	time.Sleep(100 * time.Millisecond)
	if fcm.count()+apns.count() != 0 {
		t.Fatal("no tokens must not dispatch")
	}
}

func TestPushOutbound_SendFailureIsLoggedOnly(t *testing.T) {
	_, bus, fcm, apns, presence, db := newPushOutbound(t)
	seedPushVisitorSession(t, db, "sess-1")
	presence.online["sess-1"] = false
	fcm.failOn = "fcm-device-1"

	bus.Publish(context.Background(), agentPushEvent("sess-1", "通道失败仅记日志"))
	waitPush(t, fcm, 1)
	waitPush(t, apns, 1) // 另一通道不受影响
}

func TestPushOutbound_UnknownPlatformRowSkipped(t *testing.T) {
	_, bus, fcm, apns, _, db := newPushOutbound(t)
	seedPushVisitorSession(t, db, "sess-1")
	if err := db.Model(&models.PushToken{}).Where("platform = ?", "android").
		Update("platform", "web").Error; err != nil {
		t.Fatalf("corrupt platform: %v", err)
	}

	bus.Publish(context.Background(), agentPushEvent("sess-1", "脏 platform 行跳过"))
	waitPush(t, apns, 1)
	time.Sleep(100 * time.Millisecond)
	if fcm.count() != 0 {
		t.Fatal("unknown platform must be skipped")
	}
}

func TestPushOutbound_FallbackQueryFromAggregateID(t *testing.T) {
	d, _, fcm, _, _, db := newPushOutbound(t)
	seedPushVisitorSession(t, db, "sess-1")
	// redis bus 只透传 AggregateID：回查 messages 表取最新 agent 消息
	if err := db.Create(&models.Message{
		SessionID: "sess-1", Sender: "agent", Type: "text",
		Content: "回查路径的消息", CreatedAt: time.Now(),
	}).Error; err != nil {
		t.Fatalf("seed message: %v", err)
	}

	// 模拟 redis bus：事件只透传 AggregateID（复用同包 aggregateOnlyEvent 假体）
	d.dispatch(context.Background(), &aggregateOnlyEvent{
		id:          "evt-push-fb",
		name:        conversationapp.ConversationMessageReceivedEventName,
		aggregateID: "conversation:sess-1",
	})

	waitPush(t, fcm, 1)
	if got := fcm.last(); got.body != "回查路径的消息" {
		t.Fatalf("fallback body = %q", got.body)
	}
}

func TestPushOutbound_NilChannelsRegisterIsNoOp(t *testing.T) {
	db := newPushOutboundDB(t)
	d := NewPushOutboundDispatcher(db, &fakePresence{online: map[string]bool{}}, nil, nil, logrus.New())
	bus := eventbus.NewInMemoryBus()
	d.Register(bus) // 双通道 nil：不订阅，发布不炸
	bus.Publish(context.Background(), agentPushEvent("sess-x", "no-op"))
}

// ---- wirePushRuntime ----

var (
	wireKeyOnce sync.Once
	wireFCMCred string
	wireECPEM   string
)

func wireTestCredentials(t *testing.T) (string, string) {
	t.Helper()
	wireKeyOnce.Do(func() {
		rsaKey, err := rsa.GenerateKey(rand.Reader, 2048)
		if err != nil {
			t.Fatalf("rsa keygen: %v", err)
		}
		der, _ := x509.MarshalPKCS8PrivateKey(rsaKey)
		rsaPEM := string(pem.EncodeToMemory(&pem.Block{Type: "PRIVATE KEY", Bytes: der}))
		raw, _ := json.Marshal(map[string]string{
			"client_email": "push@test-project.iam.gserviceaccount.com",
			"private_key":  rsaPEM,
			"project_id":   "test-project",
		})
		wireFCMCred = string(raw)

		ecKey, _ := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
		ecDer, _ := x509.MarshalPKCS8PrivateKey(ecKey)
		wireECPEM = string(pem.EncodeToMemory(&pem.Block{Type: "PRIVATE KEY", Bytes: ecDer}))
	})
	return wireFCMCred, wireECPEM
}

func TestWirePushRuntime_DisabledIsQuiet(t *testing.T) {
	rt := &Runtime{Config: testRouterConfig()}
	rt.Config.Push.Enabled = false
	if err := wirePushRuntime(rt, nil); err != nil {
		t.Fatalf("disabled must be quiet: %v", err)
	}
}

func TestWirePushRuntime_MalformedCredentialFails(t *testing.T) {
	rt := &Runtime{Config: testRouterConfig()}
	rt.Config.Push.Enabled = true
	rt.Config.Push.FCM.CredentialsJSON = "{not json"
	err := wirePushRuntime(rt, nil)
	if err == nil || !strings.Contains(err.Error(), "push.fcm") {
		t.Fatalf("err = %v", err)
	}
}

func TestWirePushRuntime_NoCredentialsIsQuiet(t *testing.T) {
	rt := &Runtime{Config: testRouterConfig()}
	rt.Config.Push.Enabled = true
	if err := wirePushRuntime(rt, nil); err != nil {
		t.Fatalf("enabled without credentials must stay quiet: %v", err)
	}
}

func TestWirePushRuntime_BuildsAndRegisters(t *testing.T) {
	fcmCred, ecPEM := wireTestCredentials(t)
	rt := &Runtime{Config: testRouterConfig()}
	rt.Config.Push.Enabled = true
	rt.Config.Push.FCM.CredentialsJSON = fcmCred
	rt.Config.Push.APNs.PrivateKey = ecPEM
	rt.Config.Push.APNs.KeyID = "K1"
	rt.Config.Push.APNs.TeamID = "T1"
	rt.Config.Push.APNs.BundleID = "com.example"
	rt.Config.Push.APNs.Sandbox = true

	hub := realtime.NewWebSocketHub()
	go hub.Run()
	if err := wirePushRuntime(rt, hub); err != nil {
		t.Fatalf("wire: %v", err)
	}
}

func TestWirePushRuntime_MalformedAPNsFails(t *testing.T) {
	rt := &Runtime{Config: testRouterConfig()}
	rt.Config.Push.Enabled = true
	rt.Config.Push.APNs.PrivateKey = "not a pem"
	err := wirePushRuntime(rt, nil)
	if err == nil || !strings.Contains(err.Error(), "push.apns") {
		t.Fatalf("err = %v", err)
	}
}

func TestPushOutbound_TruncatesLongSummary(t *testing.T) {
	_, bus, fcm, _, presence, db := newPushOutbound(t)
	seedPushVisitorSession(t, db, "sess-1")
	presence.online["sess-1"] = false

	bus.Publish(context.Background(), agentPushEvent("sess-1", strings.Repeat("长", 80)))

	waitPush(t, fcm, 1)
	if got := len(fcm.last().body); got != 120 {
		t.Fatalf("summary len = %d, want 120 (byte truncation)", got)
	}
}

func TestPushOutbound_SkipsBlankTokenRow(t *testing.T) {
	_, bus, fcm, apns, presence, db := newPushOutbound(t)
	seedPushVisitorSession(t, db, "sess-1")
	presence.online["sess-1"] = false
	if err := db.Model(&models.PushToken{}).Where("platform = ?", "android").
		Update("token", "   ").Error; err != nil {
		t.Fatalf("blank token: %v", err)
	}

	bus.Publish(context.Background(), agentPushEvent("sess-1", "空白 token 行跳过"))
	waitPush(t, apns, 1)
	time.Sleep(100 * time.Millisecond)
	if fcm.count() != 0 {
		t.Fatal("blank token must be skipped")
	}
}

func TestPushOutbound_ResolveMessageSkipPaths(t *testing.T) {
	d, _, fcm, _, _, db := newPushOutbound(t)
	seedPushVisitorSession(t, db, "sess-1")

	// ConversationEvent 但 payload 非 DTO（防御面）
	bus := eventbus.NewInMemoryBus() // 仅占位；直接调 dispatch 不经订阅
	_ = bus
	d.dispatch(context.Background(), conversationapp.NewConversationEvent(
		conversationapp.ConversationMessageReceivedEventName, "sess-1", nil))
	// redis 事件无 conversation: 前缀：不回查
	d.dispatch(context.Background(), &aggregateOnlyEvent{
		id: "evt-2", name: conversationapp.ConversationMessageReceivedEventName,
		aggregateID: "sess-1",
	})
	// 回查无 messages 行：ErrRecordNotFound → 不出站
	d.dispatch(context.Background(), &aggregateOnlyEvent{
		id: "evt-3", name: conversationapp.ConversationMessageReceivedEventName,
		aggregateID: "conversation:sess-nomsg",
	})
	time.Sleep(100 * time.Millisecond)
	if fcm.count() != 0 {
		t.Fatal("resolve skip paths must not dispatch")
	}
}

func TestBuildRuntimePushMalformedCredentialFails(t *testing.T) {
	cfg := newRuntimeTestConfig(t)
	cfg.Push.Enabled = true
	cfg.Push.FCM.CredentialsJSON = "{not json"

	_, err := BuildRuntime(cfg, logrus.New(), newRuntimeTestDB(t), nil, eventbus.NewInMemoryBus())
	if err == nil || !strings.Contains(err.Error(), "push.fcm") {
		t.Fatalf("BuildRuntime() error = %v", err)
	}
}
