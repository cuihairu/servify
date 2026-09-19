package services

// 本文件通过 seams.go 的包级 seam 与模块 repo 桩驱动 crypto/rand、JWT 签名、
// 密钥生成、pion WebRTC、websocket/维护 ticker 以及 app 层模块错误分支。

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"strconv"
	"strings"
	"testing"
	"time"

	"servify/apps/server/internal/models"
	analyticsapp "servify/apps/server/internal/modules/analytics/application"
	customerapp "servify/apps/server/internal/modules/customer/application"
	knowledgeapp "servify/apps/server/internal/modules/knowledge/application"
	knowledgedomain "servify/apps/server/internal/modules/knowledge/domain"

	"github.com/gorilla/websocket"
	"github.com/pion/webrtc/v4"
)

// hscovSetSeam 替换一个包级变量并在测试结束后还原。
func hscovSetSeam[T any](t *testing.T, slot *T, value T) {
	t.Helper()
	old := *slot
	*slot = value
	t.Cleanup(func() { *slot = old })
}

// hscovFailingRand 让 crypto/rand 注入点按 failPlan 依次失败/成功。
func hscovFailingRand(t *testing.T, failPlan []bool, calls *int) {
	t.Helper()
	hscovSetSeam(t, &hookRandRead, func(b []byte) (int, error) {
		shouldFail := failPlan[min(*calls, len(failPlan)-1)]
		*calls++
		if shouldFail {
			return 0, errors.New("boom: rand read")
		}
		for i := range b {
			b[i] = byte(*calls)
		}
		return len(b), nil
	})
}

func TestHSSeamsRandomHexRandError(t *testing.T) {
	calls := 0
	hscovFailingRand(t, []bool{true}, &calls)
	if _, err := randomHex(8); err == nil || !strings.Contains(err.Error(), "generate random") {
		t.Fatalf("expected wrapped rand error, got %v", err)
	}
}

func TestHSSeamsAuthSessionAndTokenIDFallback(t *testing.T) {
	calls := 0
	hscovFailingRand(t, []bool{true}, &calls)
	numericTimestamp := func(id, prefix string) bool {
		ts, ok := strings.CutPrefix(id, prefix)
		if !ok {
			return false
		}
		_, err := strconv.ParseInt(ts, 10, 64)
		return err == nil
	}
	if id := newAuthSessionID(); !numericTimestamp(id, "auth_") {
		t.Fatalf("expected timestamp fallback session id, got %q", id)
	}
	if id := newAuthTokenID(); !numericTimestamp(id, "jti_") {
		t.Fatalf("expected timestamp fallback token id, got %q", id)
	}
}

func TestHSSeamsGenerateRecoveryCodesRandError(t *testing.T) {
	calls := 0
	hscovFailingRand(t, []bool{true}, &calls)
	codes, hashes, err := generateRecoveryCodes(3)
	if err == nil || !strings.Contains(err.Error(), "generate recovery code") {
		t.Fatalf("expected recovery code rand error, got %v", err)
	}
	if codes != nil || hashes != nil {
		t.Fatalf("expected nil codes/hashes on error, got %v %v", codes, hashes)
	}
}

// TestHSSeamsEnableTwoFactorRecoveryCodeRandError：绑定期间恢复码生成失败
// 必须整体回滚——不写 totp 字段、不落任何恢复码。
func TestHSSeamsEnableTwoFactorRecoveryCodeRandError(t *testing.T) {
	db := newAuthServiceTestDB(t)
	svc := NewAuthService(db, twoFactorTestConfig(true))
	seedTOTPUser(t, db, 403, "enable-rand-err")
	setup, err := svc.SetupTwoFactor(context.Background(), 403)
	if err != nil {
		t.Fatalf("SetupTwoFactor: %v", err)
	}

	calls := 0
	hscovFailingRand(t, []bool{true}, &calls)
	if _, err := svc.EnableTwoFactor(context.Background(), 403, setup.Secret, currentTOTP(t, setup.Secret)); err == nil || !strings.Contains(err.Error(), "generate recovery code") {
		t.Fatalf("expected recovery code rand error, got %v", err)
	}

	var user models.User
	if err := db.First(&user, 403).Error; err != nil {
		t.Fatalf("reload user: %v", err)
	}
	if user.TotpEnabled || user.TotpSecret != "" {
		t.Fatalf("binding must be rolled back, got enabled=%v secret=%q", user.TotpEnabled, user.TotpSecret)
	}
	var count int64
	if err := db.Model(&models.UserRecoveryCode{}).Where("user_id = ?", 403).Count(&count).Error; err != nil {
		t.Fatalf("count recovery codes: %v", err)
	}
	if count != 0 {
		t.Fatalf("expected no recovery codes persisted, got %d", count)
	}
}

// TestHSSeamsRegenerateRecoveryCodesRandError：重发期间恢复码生成失败
// 必须保留旧码不做替换。
func TestHSSeamsRegenerateRecoveryCodesRandError(t *testing.T) {
	db := newAuthServiceTestDB(t)
	svc := NewAuthService(db, twoFactorTestConfig(true))
	seedTOTPUser(t, db, 404, "regen-rand-err")
	secret, codes := enrollTwoFactor(t, svc, 404)

	calls := 0
	hscovFailingRand(t, []bool{true}, &calls)
	if _, err := svc.RegenerateRecoveryCodes(context.Background(), 404, currentTOTP(t, secret)); err == nil || !strings.Contains(err.Error(), "generate recovery code") {
		t.Fatalf("expected recovery code rand error, got %v", err)
	}

	remaining, err := svc.RecoveryCodesRemaining(context.Background(), 404)
	if err != nil {
		t.Fatalf("RecoveryCodesRemaining: %v", err)
	}
	if remaining != int64(len(codes)) {
		t.Fatalf("old codes must survive, got %d want %d", remaining, len(codes))
	}
}

func TestHSSeamsProvisionOIDCUserRandError(t *testing.T) {
	// randomHex(32) 失败必须中断本地账号创建。
	calls := 0
	hscovFailingRand(t, []bool{true, true}, &calls)
	db := newServicesTestDB(t, &models.User{})
	svc := NewAuthService(db, testAuthConfig())
	_, err := svc.provisionOIDCUser(context.Background(), OIDCIdentity{
		Subject: "s1", Email: "s1@example.com", EmailVerified: true, Name: "S One",
	}, "agent")
	if err == nil || !strings.Contains(err.Error(), "generate random") {
		t.Fatalf("expected rand error to abort provisioning, got %v", err)
	}
}

func TestHSSeamsOIDCUsernameSuffixRandError(t *testing.T) {
	calls := 0
	hscovFailingRand(t, []bool{true}, &calls)
	// suffix 生成失败时回退为纯 local part（不会 panic，也不会带随机后缀）。
	if name := oidcUsername(OIDCIdentity{Email: "Ada.Lovelace@example.com"}); name != "ada.lovelace" {
		t.Fatalf("expected bare local part fallback, got %q", name)
	}
}

func TestHSSeamsAPIKeyGenerateError(t *testing.T) {
	hscovSetSeam(t, &hookGenerateAPIKey, func() (string, string, string, error) {
		return "", "", "", errors.New("boom: keygen")
	})
	db := newServicesTestDB(t, &models.APIKey{})
	svc := NewAPIKeyService(db)
	_, _, err := svc.Create(context.Background(), &APIKeyCreateRequest{Name: "k"}, "admin")
	if err == nil || !strings.Contains(err.Error(), "keygen") {
		t.Fatalf("expected keygen error, got %v", err)
	}
}

// --- JWT 签名失败：挑战 token 与 access/refresh token ---

func TestHSSeamsLoginChallengeTokenError(t *testing.T) {
	db := newAuthServiceTestDB(t)
	svc := NewAuthService(db, twoFactorTestConfig(true))
	seedTOTPUser(t, db, 401, "challenge-err")
	secret, _ := enrollTwoFactor(t, svc, 401)

	hscovSetSeam(t, &hookCreateHS256JWT, func(map[string]interface{}, string) (string, error) {
		return "", errors.New("boom: sign")
	})
	_, err := svc.Login(context.Background(), LoginInput{Username: "challenge-err", Password: "password123"}, AuthSessionMetadata{})
	if err == nil || !strings.Contains(err.Error(), "boom: sign") {
		t.Fatalf("expected challenge sign error, got %v", err)
	}
	_ = secret
}

func TestHSSeamsLoginBuildAuthResultTokenErrors(t *testing.T) {
	realSign := createHS256JWT
	db := newAuthServiceTestDB(t)
	svc := NewAuthService(db, twoFactorTestConfig(false))
	seedTOTPUser(t, db, 402, "sign-err")

	var calls int
	hscovSetSeam(t, &hookCreateHS256JWT, func(payload map[string]interface{}, secret string) (string, error) {
		calls++
		if calls == 1 { // 第一次 = access token
			return "", errors.New("boom: access sign")
		}
		return realSign(payload, secret)
	})
	_, err := svc.Login(context.Background(), LoginInput{Username: "sign-err", Password: "password123"}, AuthSessionMetadata{})
	if err == nil || !strings.Contains(err.Error(), "access sign") {
		t.Fatalf("expected access token sign error, got %v", err)
	}

	// 第二次调用 = refresh token：access 成功、refresh 失败。
	calls = 0
	hscovSetSeam(t, &hookCreateHS256JWT, func(payload map[string]interface{}, secret string) (string, error) {
		calls++
		if calls == 2 {
			return "", errors.New("boom: refresh sign")
		}
		return realSign(payload, secret)
	})
	_, err = svc.Login(context.Background(), LoginInput{Username: "sign-err", Password: "password123"}, AuthSessionMetadata{})
	if err == nil || !strings.Contains(err.Error(), "refresh sign") {
		t.Fatalf("expected refresh token sign error, got %v", err)
	}
}

// --- app 层模块错误分支：customer / statistics / knowledge ---

type hscovCustomerRepo struct {
	customerapp.Repository
	activityErr error
	statsErr    error
}

func (r *hscovCustomerRepo) GetCustomerActivity(ctx context.Context, customerID uint, limit int) (*customerapp.CustomerActivityDTO, error) {
	return nil, r.activityErr
}

func (r *hscovCustomerRepo) GetStats(ctx context.Context) (*customerapp.CustomerStatsDTO, error) {
	return nil, r.statsErr
}

func TestHSSeamsCustomerModuleErrors(t *testing.T) {
	repo := &hscovCustomerRepo{activityErr: errors.New("boom: activity"), statsErr: errors.New("boom: stats")}
	svc := customerapp.NewService(repo)
	ctx := context.Background()

	if _, err := svc.GetCustomerActivity(ctx, 1, 5); err == nil || !strings.Contains(err.Error(), "activity") {
		t.Fatalf("expected activity module error, got %v", err)
	}
	if _, err := svc.GetStats(ctx); err == nil || !strings.Contains(err.Error(), "stats") {
		t.Fatalf("expected stats module error, got %v", err)
	}
}

type hscovAnalyticsRepo struct {
	analyticsapp.Repository
	dashErr error
}

func (r *hscovAnalyticsRepo) GetDashboardStats(ctx context.Context) (*analyticsapp.DashboardStats, error) {
	return nil, r.dashErr
}

func TestHSSeamsStatisticsDashboardModuleError(t *testing.T) {
	repo := &hscovAnalyticsRepo{dashErr: errors.New("boom: dashboard")}
	svc := &StatisticsService{module: analyticsapp.NewService(repo), logger: newTestLogger()}
	if _, err := svc.GetDashboardStats(context.Background()); err == nil || !strings.Contains(err.Error(), "dashboard") {
		t.Fatalf("expected dashboard module error, got %v", err)
	}
}

type hscovDocRepo struct {
	knowledgeapp.DocumentRepository
	docs []knowledgedomain.Document
}

func (r *hscovDocRepo) List(ctx context.Context, filter knowledgeapp.ListDocumentsFilter) ([]knowledgedomain.Document, int64, error) {
	return r.docs, int64(len(r.docs)), nil
}

// --- WebRTC：CreateAnswer / SetLocalDescription / Close 错误 ---

func TestHSSeamsWebRTCHandleOfferErrors(t *testing.T) {
	s, _ := newUnitWebRTCService(t)

	offerer, err := webrtc.NewPeerConnection(webrtc.Configuration{})
	if err != nil {
		t.Fatalf("offerer: %v", err)
	}
	defer offerer.Close() //nolint:errcheck
	if _, err := offerer.CreateDataChannel("d", nil); err != nil {
		t.Fatalf("data channel: %v", err)
	}
	offer, err := offerer.CreateOffer(nil)
	if err != nil {
		t.Fatalf("CreateOffer: %v", err)
	}
	if err := offerer.SetLocalDescription(offer); err != nil {
		t.Fatalf("SetLocalDescription: %v", err)
	}

	t.Run("CreateAnswer error", func(t *testing.T) {
		hscovSetSeam(t, &hookPeerConnectionCreateAnswer, func(*webrtc.PeerConnection, *webrtc.AnswerOptions) (webrtc.SessionDescription, error) {
			return webrtc.SessionDescription{}, errors.New("boom: answer")
		})
		if _, err := s.HandleOffer("sess-answer-err", offer); err == nil || !strings.Contains(err.Error(), "failed to create answer") {
			t.Fatalf("expected create answer error, got %v", err)
		}
	})

	t.Run("SetLocalDescription error", func(t *testing.T) {
		hscovSetSeam(t, &hookPeerConnectionSetLocalDescription, func(*webrtc.PeerConnection, webrtc.SessionDescription) error {
			return errors.New("boom: set local")
		})
		if _, err := s.HandleOffer("sess-local-err", offer); err == nil || !strings.Contains(err.Error(), "failed to set local description") {
			t.Fatalf("expected set local description error, got %v", err)
		}
	})
}

func TestHSSeamsWebRTCCloseError(t *testing.T) {
	s, _ := newUnitWebRTCService(t)
	if _, err := s.CreatePeerConnection("sess-close-err"); err != nil {
		t.Fatalf("CreatePeerConnection: %v", err)
	}
	hscovSetSeam(t, &hookPeerConnectionClose, func(*webrtc.PeerConnection) error {
		return errors.New("boom: close")
	})
	if err := s.CloseConnection("sess-close-err"); err != nil {
		t.Fatalf("CloseConnection must swallow close error: %v", err)
	}
	if s.GetConnectionCount() != 0 {
		t.Fatalf("connection must still be removed after close error, got %d", s.GetConnectionCount())
	}
}

// --- websocket writePump ping ticker ---

func TestHSSeamsWebSocketPingTickerPath(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = upgrader.Upgrade(w, r, nil)
	}))
	defer server.Close()

	conn, _, err := websocket.DefaultDialer.Dial("ws"+server.URL[len("http"):]+"/ws", nil)
	if err != nil {
		t.Fatalf("dial: %v", err)
	}
	if err := conn.Close(); err != nil {
		t.Fatalf("close: %v", err)
	}
	client := &WebSocketClient{
		ID: "c-hscov-ping", SessionID: "s-hscov-ping", Conn: conn,
		Send:         make(chan WebSocketMessage),
		pingInterval: 10 * time.Millisecond,
	}
	done := make(chan struct{})
	go func() {
		client.writePump()
		close(done)
	}()
	// 已关闭连接上的 ping 必然失败：writePump 只能经由 ticker 的 ping 分支退出。
	select {
	case <-done:
	case <-time.After(10 * time.Second):
		t.Fatal("writePump did not exit via ping branch")
	}
}

// --- router 轮询适配器 ticker ---

func TestHSSeamsRouterPollersTick(t *testing.T) {
	t.Run("telegram", func(t *testing.T) {
		adapter := NewTelegramAdapter("token", "chat")
		adapter.pollInterval = 2 * time.Millisecond
		if err := adapter.Start(); err != nil {
			t.Fatalf("Start: %v", err)
		}
		time.Sleep(30 * time.Millisecond) // 覆盖多个 ticker tick
		if err := adapter.Stop(); err != nil {
			t.Fatalf("Stop: %v", err)
		}
	})
	t.Run("wechat", func(t *testing.T) {
		adapter := NewWeChatAdapter("app", "secret")
		adapter.pollInterval = 2 * time.Millisecond
		if err := adapter.Start(); err != nil {
			t.Fatalf("Start: %v", err)
		}
		time.Sleep(30 * time.Millisecond)
		if err := adapter.Stop(); err != nil {
			t.Fatalf("Stop: %v", err)
		}
	})
}
