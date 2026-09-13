package services

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"

	"servify/apps/server/internal/config"
	"servify/apps/server/internal/models"
	platformauth "servify/apps/server/internal/platform/auth"

	"github.com/pquerna/otp/totp"
	"golang.org/x/crypto/bcrypt"
	"gorm.io/gorm"
)

func twoFactorTestConfig(enabled bool) *config.Config {
	cfg := testAuthConfig()
	cfg.Security.TwoFactor.Enabled = enabled
	return cfg
}

func seedTOTPUser(t *testing.T, db *gorm.DB, id uint, username string) {
	t.Helper()
	hash, err := bcrypt.GenerateFromPassword([]byte("password123"), bcrypt.DefaultCost)
	if err != nil {
		t.Fatalf("hash password: %v", err)
	}
	user := &models.User{ID: id, Username: username, Email: username + "@example.com", Password: string(hash), Status: "active", Role: "agent"}
	if err := db.Create(user).Error; err != nil {
		t.Fatalf("seed user: %v", err)
	}
}

func currentTOTP(t *testing.T, secret string) string {
	t.Helper()
	code, err := totp.GenerateCode(secret, time.Now())
	if err != nil {
		t.Fatalf("generate totp code: %v", err)
	}
	return code
}

// enrollTwoFactor 走完整绑定流程，返回明文恢复码与 secret。
func enrollTwoFactor(t *testing.T, svc *AuthService, userID uint) (secret string, codes []string) {
	t.Helper()
	setup, err := svc.SetupTwoFactor(context.Background(), userID)
	if err != nil {
		t.Fatalf("SetupTwoFactor() error = %v", err)
	}
	codes, err = svc.EnableTwoFactor(context.Background(), userID, setup.Secret, currentTOTP(t, setup.Secret))
	if err != nil {
		t.Fatalf("EnableTwoFactor() error = %v", err)
	}
	return setup.Secret, codes
}

func TestTwoFactorEnrollmentAndChallengeLogin(t *testing.T) {
	db := newAuthServiceTestDB(t)
	cfg := twoFactorTestConfig(true)
	svc := NewAuthService(db, cfg)
	seedTOTPUser(t, db, 91, "totp-user")
	meta := AuthSessionMetadata{DeviceFingerprint: "fp-2fa", UserAgent: "servify-test/1.0", ClientIP: "198.51.100.8"}

	secret, codes := enrollTwoFactor(t, svc, 91)
	if len(codes) != recoveryCodeCount {
		t.Fatalf("recovery codes = %d want %d", len(codes), recoveryCodeCount)
	}

	// 绑定后重复 setup/enable 拒绝
	if _, err := svc.SetupTwoFactor(context.Background(), 91); !errors.Is(err, ErrTwoFactorAlreadyEnabled) {
		t.Fatalf("second setup err = %v want %v", err, ErrTwoFactorAlreadyEnabled)
	}

	// 登录进入挑战步：无正式 token
	outcome, err := svc.Login(context.Background(), LoginInput{Username: "totp-user", Password: "password123"}, meta)
	if err != nil {
		t.Fatalf("Login() error = %v", err)
	}
	if !outcome.TwoFactorRequired || outcome.Result != nil || outcome.ChallengeToken == "" {
		t.Fatalf("expected challenge outcome, got %+v", outcome)
	}

	// 挑战 token 是签名完好的 JWT，但 token_use 指明用途
	payload, err := (platformauth.Validator{Secret: cfg.JWT.Secret}).ValidateToken(outcome.ChallengeToken)
	if err != nil {
		t.Fatalf("validate challenge token: %v", err)
	}
	if got := payload["token_use"].(string); got != challengeTokenUse {
		t.Fatalf("challenge token_use = %q want %q", got, challengeTokenUse)
	}

	// 错误码拒绝
	if _, err := svc.VerifyTwoFactorLogin(context.Background(), TwoFactorVerifyInput{ChallengeToken: outcome.ChallengeToken, Code: "000000"}, meta); !errors.Is(err, ErrAuthInvalid2FACode) {
		t.Fatalf("wrong code err = %v want %v", err, ErrAuthInvalid2FACode)
	}

	// 正确码换到正式会话
	result, err := svc.VerifyTwoFactorLogin(context.Background(), TwoFactorVerifyInput{ChallengeToken: outcome.ChallengeToken, Code: currentTOTP(t, secret)}, meta)
	if err != nil {
		t.Fatalf("VerifyTwoFactorLogin() error = %v", err)
	}
	accessPayload, err := (platformauth.Validator{Secret: cfg.JWT.Secret}).ValidateToken(result.Token)
	if err != nil {
		t.Fatalf("validate access token: %v", err)
	}
	if got := accessPayload["token_use"].(string); got != "access" {
		t.Fatalf("access token_use = %q want access", got)
	}

	// 挑战 token 绑定发起方 IP：换 IP 重放被拒绝
	outcome2, err := svc.Login(context.Background(), LoginInput{Username: "totp-user", Password: "password123"}, meta)
	if err != nil {
		t.Fatalf("second Login() error = %v", err)
	}
	if _, err := svc.VerifyTwoFactorLogin(context.Background(), TwoFactorVerifyInput{ChallengeToken: outcome2.ChallengeToken, Code: currentTOTP(t, secret)}, AuthSessionMetadata{UserAgent: meta.UserAgent, ClientIP: "203.0.113.77"}); !errors.Is(err, ErrAuthInvalid2FAChallenge) {
		t.Fatalf("replayed challenge from another IP err = %v want %v", err, ErrAuthInvalid2FAChallenge)
	}
}

func TestTwoFactorRecoveryCodeSingleUse(t *testing.T) {
	db := newAuthServiceTestDB(t)
	svc := NewAuthService(db, twoFactorTestConfig(true))
	seedTOTPUser(t, db, 92, "rc-user")
	meta := AuthSessionMetadata{UserAgent: "servify-test/1.0", ClientIP: "198.51.100.8"}

	_, codes := enrollTwoFactor(t, svc, 92)

	outcome, err := svc.Login(context.Background(), LoginInput{Username: "rc-user", Password: "password123"}, meta)
	if err != nil {
		t.Fatalf("Login() error = %v", err)
	}
	// 恢复码（带/不带连字符均可）换会话
	if _, err := svc.VerifyTwoFactorLogin(context.Background(), TwoFactorVerifyInput{ChallengeToken: outcome.ChallengeToken, Code: strings.ToUpper(codes[0])}, meta); err != nil {
		t.Fatalf("recovery code verify error = %v", err)
	}
	if remaining, err := svc.RecoveryCodesRemaining(context.Background(), 92); err != nil || remaining != int64(recoveryCodeCount-1) {
		t.Fatalf("remaining = %d, %v want %d", remaining, err, recoveryCodeCount-1)
	}

	// 同一恢复码第二次核销必须失败（原子一次性）
	outcome2, err := svc.Login(context.Background(), LoginInput{Username: "rc-user", Password: "password123"}, meta)
	if err != nil {
		t.Fatalf("second Login() error = %v", err)
	}
	if _, err := svc.VerifyTwoFactorLogin(context.Background(), TwoFactorVerifyInput{ChallengeToken: outcome2.ChallengeToken, Code: codes[0]}, meta); !errors.Is(err, ErrAuthInvalid2FACode) {
		t.Fatalf("reuse recovery code err = %v want %v", err, ErrAuthInvalid2FACode)
	}

	// RegenerateRecoveryCodes 需要当前 TOTP 码；旧码作废
	fresh, err := svc.RegenerateRecoveryCodes(context.Background(), 92, currentTOTP(t, dbSecret(t, db, 92)))
	if err != nil {
		t.Fatalf("RegenerateRecoveryCodes() error = %v", err)
	}
	if len(fresh) != recoveryCodeCount {
		t.Fatalf("fresh codes = %d want %d", len(fresh), recoveryCodeCount)
	}
	outcome3, err := svc.Login(context.Background(), LoginInput{Username: "rc-user", Password: "password123"}, meta)
	if err != nil {
		t.Fatalf("third Login() error = %v", err)
	}
	if _, err := svc.VerifyTwoFactorLogin(context.Background(), TwoFactorVerifyInput{ChallengeToken: outcome3.ChallengeToken, Code: codes[1]}, meta); !errors.Is(err, ErrAuthInvalid2FACode) {
		t.Fatalf("stale recovery code err = %v want %v", err, ErrAuthInvalid2FACode)
	}
}

// dbSecret 读回落库的 TOTP secret（模拟认证器持有者）。
func dbSecret(t *testing.T, db *gorm.DB, userID uint) string {
	t.Helper()
	var user models.User
	if err := db.First(&user, userID).Error; err != nil {
		t.Fatalf("load user: %v", err)
	}
	return user.TotpSecret
}

func TestTwoFactorKillSwitch(t *testing.T) {
	db := newAuthServiceTestDB(t)
	// kill-switch 关闭（默认）
	svc := NewAuthService(db, twoFactorTestConfig(false))
	seedTOTPUser(t, db, 93, "ks-user")
	meta := AuthSessionMetadata{UserAgent: "servify-test/1.0", ClientIP: "198.51.100.8"}

	if _, err := svc.SetupTwoFactor(context.Background(), 93); !errors.Is(err, ErrTwoFactorDisabled) {
		t.Fatalf("setup with switch off err = %v want %v", err, ErrTwoFactorDisabled)
	}
	if _, err := svc.EnableTwoFactor(context.Background(), 93, "JBSWY3DPEHPK3PXP", "123456"); !errors.Is(err, ErrTwoFactorDisabled) {
		t.Fatalf("enable with switch off err = %v want %v", err, ErrTwoFactorDisabled)
	}

	// 已启用用户在开关关闭时降级单因子直登
	enabledSvc := NewAuthService(db, twoFactorTestConfig(true))
	enrollTwoFactor(t, enabledSvc, 93)
	directLogin, err := svc.Login(context.Background(), LoginInput{Username: "ks-user", Password: "password123"}, meta)
	if err != nil {
		t.Fatalf("Login() with switch off error = %v", err)
	}
	if directLogin.TwoFactorRequired || directLogin.Result == nil || directLogin.Result.Token == "" {
		t.Fatalf("expected degraded direct login, got %+v", directLogin)
	}

	// disable 不受 kill-switch 限制
	// 用 TOTP 码 + 密码解绑（开关关闭仍可解绑）
	code := currentTOTP(t, dbSecret(t, db, 93))
	if err := svc.DisableTwoFactor(context.Background(), 93, "password123", code); err != nil {
		t.Fatalf("DisableTwoFactor() error = %v", err)
	}
	var user models.User
	if err := db.First(&user, 93).Error; err != nil {
		t.Fatalf("load user: %v", err)
	}
	if user.TotpEnabled || user.TotpSecret != "" {
		t.Fatalf("expected totp cleared, got enabled=%v secret=%q", user.TotpEnabled, user.TotpSecret)
	}
	if remaining, err := svc.RecoveryCodesRemaining(context.Background(), 93); err != nil || remaining != 0 {
		t.Fatalf("recovery codes after disable = %d, %v want 0", remaining, err)
	}
	if err := svc.DisableTwoFactor(context.Background(), 93, "password123", "123456"); !errors.Is(err, ErrTwoFactorNotEnabled) {
		t.Fatalf("disable when not enabled err = %v want %v", err, ErrTwoFactorNotEnabled)
	}
}

func TestTwoFactorDisableGuards(t *testing.T) {
	db := newAuthServiceTestDB(t)
	svc := NewAuthService(db, twoFactorTestConfig(true))
	seedTOTPUser(t, db, 94, "dg-user")
	enrollTwoFactor(t, svc, 94)
	secret := dbSecret(t, db, 94)

	// 密码错误
	if err := svc.DisableTwoFactor(context.Background(), 94, "wrong-password", currentTOTP(t, secret)); !errors.Is(err, ErrAuthInvalidCredentials) {
		t.Fatalf("wrong password err = %v want %v", err, ErrAuthInvalidCredentials)
	}
	// 密码对、验证码错
	if err := svc.DisableTwoFactor(context.Background(), 94, "password123", "000000"); !errors.Is(err, ErrAuthInvalid2FACode) {
		t.Fatalf("wrong code err = %v want %v", err, ErrAuthInvalid2FACode)
	}
	// 全对才解绑
	if err := svc.DisableTwoFactor(context.Background(), 94, "password123", currentTOTP(t, secret)); err != nil {
		t.Fatalf("DisableTwoFactor() error = %v", err)
	}

	// 解绑/启用都会推进 token_valid_after 踢会话——验证 enable 后 refresh token 失效：
	// 重新启用时旧 access token 的 token_version 已过期（TokenValidAfter 由 session 校验路径处理），
	// 这里只验证字段被推进。
	var user models.User
	if err := db.First(&user, 94).Error; err != nil {
		t.Fatalf("load user: %v", err)
	}
	if user.TokenValidAfter.IsZero() {
		t.Fatalf("expected token_valid_after to be set after disable")
	}
}

func TestTwoFactorNormalizeRecoveryCode(t *testing.T) {
	cases := map[string]string{
		"ab12-cd34": "ab12cd34",
		"AB12CD34":  "ab12cd34",
		" ab12cd34": "ab12cd34",
		"ab12cd3":   "",
		"zz12cd34":  "",
		"":          "",
	}
	for input, want := range cases {
		if got := normalizeRecoveryCode(input); got != want {
			t.Fatalf("normalizeRecoveryCode(%q) = %q want %q", input, got, want)
		}
	}
}
