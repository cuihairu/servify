package services

import (
	"context"
	"errors"
	"strings"
	"testing"

	"servify/apps/server/internal/models"

	"golang.org/x/crypto/bcrypt"
)

// stubLoginRiskIntel 以固定标签驱动登录风险执行分支。
type stubLoginRiskIntel struct {
	label string
}

func (s stubLoginRiskIntel) LoginNetworkLabel(_ context.Context, _ string) string {
	return s.label
}

func newLoginRiskService(t *testing.T, intel LoginRiskIntel, enforcement string, totpEnabled bool, twoFactorKillSwitch bool) *AuthService {
	t.Helper()
	db := newAuthServiceTestDB(t)
	hash, err := bcrypt.GenerateFromPassword([]byte("password123"), bcrypt.DefaultCost)
	if err != nil {
		t.Fatalf("hash password: %v", err)
	}
	user := &models.User{
		ID:          91,
		Username:    "risk-user",
		Email:       "risk-user@example.com",
		Password:    string(hash),
		Status:      "active",
		Role:        "customer",
		TotpEnabled: totpEnabled,
	}
	if err := db.Create(user).Error; err != nil {
		t.Fatalf("seed user: %v", err)
	}
	cfg := testAuthConfig()
	cfg.Security.TwoFactor.Enabled = twoFactorKillSwitch
	svc := NewAuthService(db, cfg)
	svc.WithLoginRiskEnforcement(intel, enforcement)
	return svc
}

const loginRiskMetaIP = "203.0.113.50"

func loginRiskInput() LoginInput {
	return LoginInput{Username: "risk-user", Password: "password123"}
}

func loginRiskMeta() AuthSessionMetadata {
	return AuthSessionMetadata{
		DeviceFingerprint: "fp-risk",
		UserAgent:         "risk-test/1.0",
		ClientIP:          loginRiskMetaIP,
	}
}

// TestLoginRiskEnforcementNormalization：档位归一化——大小写/空白容错，
// 未知取值落回 off（登录行为保持不变）。
func TestLoginRiskEnforcementNormalization(t *testing.T) {
	cases := map[string]string{
		"STEP_UP ":  "step_up",
		" block":    "block",
		"":          "",
		"off":       "",
		"bogus":     "",
		"  BLOCK  ": "block",
	}
	for raw, want := range cases {
		svc := NewAuthService(newAuthServiceTestDB(t), testAuthConfig())
		svc.WithLoginRiskEnforcement(stubLoginRiskIntel{label: "public"}, raw)
		if svc.loginEnforcement != want {
			t.Fatalf("enforcement %q normalized to %q, want %q", raw, svc.loginEnforcement, want)
		}
	}
}

// TestLoginRiskHighRiskClassification：风险判定——nil 情报源、空 IP 与
// 内建启发式四类标签都不是高风险；其余标签（hosting/proxy/…）是。
func TestLoginRiskHighRiskClassification(t *testing.T) {
	svc := NewAuthService(newAuthServiceTestDB(t), testAuthConfig())

	if svc.loginRiskHighRisk(context.Background(), loginRiskMetaIP) {
		t.Fatal("nil intel must not be high risk")
	}

	svc.WithLoginRiskEnforcement(stubLoginRiskIntel{label: "public"}, "block")
	if svc.loginRiskHighRisk(context.Background(), "") {
		t.Fatal("blank ip must not be high risk")
	}
	for _, safe := range []string{"public", "private", "loopback", "unknown", "  HOSTING  "} {
		label := strings.ToLower(strings.TrimSpace(safe))
		svc.WithLoginRiskEnforcement(stubLoginRiskIntel{label: safe}, "block")
		got := svc.loginRiskHighRisk(context.Background(), loginRiskMetaIP)
		want := label == "hosting"
		if got != want {
			t.Fatalf("label %q high risk = %v, want %v", safe, got, want)
		}
	}
}

// TestLoginBlockModeRejectsHighRiskSource：block 档位下高风险来源登录
// 凭据验证通过后仍被拒绝，且不创建会话。
func TestLoginBlockModeRejectsHighRiskSource(t *testing.T) {
	svc := newLoginRiskService(t, stubLoginRiskIntel{label: "hosting"}, "block", false, true)
	_, err := svc.Login(context.Background(), loginRiskInput(), loginRiskMeta())
	if !errors.Is(err, ErrLoginBlockedByRisk) {
		t.Fatalf("expected ErrLoginBlockedByRisk, got %v", err)
	}

	var count int64
	db := svc.db
	db.Model(&models.UserAuthSession{}).Where("user_id = ?", 91).Count(&count)
	if count != 0 {
		t.Fatalf("expected no session created, got %d", count)
	}
}

// TestLoginBlockModeSafeLabelPasses：block 档位下内建安全标签不拦截。
func TestLoginBlockModeSafeLabelPasses(t *testing.T) {
	svc := newLoginRiskService(t, stubLoginRiskIntel{label: "private"}, "block", false, true)
	outcome, err := svc.Login(context.Background(), loginRiskInput(), loginRiskMeta())
	if err != nil {
		t.Fatalf("expected direct login, got %v", err)
	}
	if outcome.TwoFactorRequired || outcome.Result == nil {
		t.Fatal("expected direct login result")
	}
}

// TestLoginStepUpWithoutTOTPRejected：step_up 档位下高风险来源、未绑定
// TOTP 的用户无第二因子可用，登录被拒绝。
func TestLoginStepUpWithoutTOTPRejected(t *testing.T) {
	svc := newLoginRiskService(t, stubLoginRiskIntel{label: "hosting"}, "step_up", false, false)
	_, err := svc.Login(context.Background(), loginRiskInput(), loginRiskMeta())
	if !errors.Is(err, ErrLoginBlockedByRisk) {
		t.Fatalf("expected ErrLoginBlockedByRisk, got %v", err)
	}
}

// TestLoginStepUpForcesChallengeDespiteKillSwitch：step_up 档位下高风险
// 来源、已绑定 TOTP 的用户即使 2FA kill-switch 关闭也进入挑战步。
func TestLoginStepUpForcesChallengeDespiteKillSwitch(t *testing.T) {
	svc := newLoginRiskService(t, stubLoginRiskIntel{label: "hosting"}, "step_up", true, false)
	outcome, err := svc.Login(context.Background(), loginRiskInput(), loginRiskMeta())
	if err != nil {
		t.Fatalf("expected challenge login, got %v", err)
	}
	if !outcome.TwoFactorRequired || outcome.ChallengeToken == "" {
		t.Fatalf("expected challenge token, got %+v", outcome)
	}
	if outcome.Result != nil {
		t.Fatal("challenge login must not carry a session result")
	}
}

// TestLoginStepUpBindsToNormalChallengePath：step_up 档位、高风险来源且
// kill-switch 已开时，与常规挑战路径一致（仍走挑战步，不重复发挑战）。
func TestLoginStepUpBindsToNormalChallengePath(t *testing.T) {
	svc := newLoginRiskService(t, stubLoginRiskIntel{label: "hosting"}, "step_up", true, true)
	outcome, err := svc.Login(context.Background(), loginRiskInput(), loginRiskMeta())
	if err != nil {
		t.Fatalf("expected challenge login, got %v", err)
	}
	if !outcome.TwoFactorRequired || outcome.ChallengeToken == "" {
		t.Fatalf("expected challenge token, got %+v", outcome)
	}
}

// TestLoginEnforcementOffKeepsLegacyBehavior：off 档位下高风险来源不拦截、
// 不挑战（kill-switch 关时直登）。
func TestLoginEnforcementOffKeepsLegacyBehavior(t *testing.T) {
	svc := newLoginRiskService(t, stubLoginRiskIntel{label: "hosting"}, "off", false, false)
	outcome, err := svc.Login(context.Background(), loginRiskInput(), loginRiskMeta())
	if err != nil {
		t.Fatalf("expected direct login, got %v", err)
	}
	if outcome.TwoFactorRequired || outcome.Result == nil {
		t.Fatal("expected legacy direct login result")
	}
}

// TestWithLoginRiskEnforcementNilReceiver：nil receiver 不 panic、原样返回。
func TestWithLoginRiskEnforcementNilReceiver(t *testing.T) {
	var svc *AuthService
	if got := svc.WithLoginRiskEnforcement(stubLoginRiskIntel{label: "hosting"}, "block"); got != nil {
		t.Fatal("nil receiver must return nil")
	}
}

// TestLoginStepUpChallengeSignError：step_up 强制挑战时挑战 token 签名
// 失败原样透传，不得降级为直登。
func TestLoginStepUpChallengeSignError(t *testing.T) {
	svc := newLoginRiskService(t, stubLoginRiskIntel{label: "hosting"}, "step_up", true, false)
	hscovSetSeam(t, &hookCreateHS256JWT, func(map[string]interface{}, string) (string, error) {
		return "", errors.New("boom: risk challenge sign")
	})
	_, err := svc.Login(context.Background(), loginRiskInput(), loginRiskMeta())
	if err == nil || !strings.Contains(err.Error(), "boom: risk challenge sign") {
		t.Fatalf("expected challenge sign error, got %v", err)
	}
}
