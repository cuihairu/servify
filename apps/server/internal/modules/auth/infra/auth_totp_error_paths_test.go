package infra

import (
	"context"
	"errors"
	slapp "servify/apps/server/internal/modules/auth/application"
	"strings"
	"testing"
	"time"

	"servify/apps/server/internal/models"

	"gorm.io/gorm"
)

const testTOTPSecret = "JBSWY3DPEHPK3PXP"

// TestTwoFactorNilDBGuards 覆盖 2FA 子服务所有入口的 nil-db 守卫。
func TestTwoFactorNilDBGuards(t *testing.T) {
	svc := &slapp.AuthService{}
	ctx := context.Background()

	if _, err := svc.SetupTwoFactor(ctx, 1); !errors.Is(err, slapp.ErrAuthInvalidDB) {
		t.Fatalf("SetupTwoFactor err = %v, want %v", err, slapp.ErrAuthInvalidDB)
	}
	if _, err := svc.EnableTwoFactor(ctx, 1, "s", "123456"); !errors.Is(err, slapp.ErrAuthInvalidDB) {
		t.Fatalf("EnableTwoFactor err = %v, want %v", err, slapp.ErrAuthInvalidDB)
	}
	if err := svc.DisableTwoFactor(ctx, 1, "p", "123456"); !errors.Is(err, slapp.ErrAuthInvalidDB) {
		t.Fatalf("DisableTwoFactor err = %v, want %v", err, slapp.ErrAuthInvalidDB)
	}
	if _, err := svc.RegenerateRecoveryCodes(ctx, 1, "123456"); !errors.Is(err, slapp.ErrAuthInvalidDB) {
		t.Fatalf("RegenerateRecoveryCodes err = %v, want %v", err, slapp.ErrAuthInvalidDB)
	}
	if _, err := svc.RecoveryCodesRemaining(ctx, 1); !errors.Is(err, slapp.ErrAuthInvalidDB) {
		t.Fatalf("RecoveryCodesRemaining err = %v, want %v", err, slapp.ErrAuthInvalidDB)
	}
	if _, err := svc.VerifyTwoFactorLogin(ctx, slapp.TwoFactorVerifyInput{}, slapp.AuthSessionMetadata{}); !errors.Is(err, slapp.ErrAuthInvalidDB) {
		t.Fatalf("VerifyTwoFactorLogin err = %v, want %v", err, slapp.ErrAuthInvalidDB)
	}

	// nil config 的兜底值断言随服务内部分支测试移至 application 包。
}

// TestTwoFactorSetupAccountAndGenerateError 覆盖 account 回退与 otpauth 生成失败。
func TestTwoFactorSetupAccountAndGenerateError(t *testing.T) {
	db := newAuthServiceTestDB(t)
	svc := slapp.NewService(NewGormRepository(db), twoFactorTestConfig(true))

	// email 为空 -> 回退 username
	if err := db.Create(&models.User{ID: 41, Username: "no-email", Status: "active"}).Error; err != nil {
		t.Fatalf("seed user 41: %v", err)
	}
	setup, err := svc.SetupTwoFactor(context.Background(), 41)
	if err != nil {
		t.Fatalf("SetupTwoFactor() error = %v", err)
	}
	if setup == nil || setup.Secret == "" || setup.OTPAuthURI == "" {
		t.Fatalf("setup = %+v", setup)
	}
	if !strings.Contains(setup.OTPAuthURI, "issuer=Servify") || !strings.Contains(setup.OTPAuthURI, "no-email") {
		t.Fatalf("otpauth uri = %q, want username account", setup.OTPAuthURI)
	}

	// 用户不存在
	if _, err := svc.SetupTwoFactor(context.Background(), 424242); err == nil {
		t.Fatal("expected error for missing user")
	}
}

// TestTwoFactorSetupEmptyAccount 独立库覆盖 email/username 均为空 -> totp.Generate 报错
// （users.email/username 有唯一索引，空值行必须独占一库）。
func TestTwoFactorSetupEmptyAccount(t *testing.T) {
	db := newAuthServiceTestDB(t)
	svc := slapp.NewService(NewGormRepository(db), twoFactorTestConfig(true))
	if err := db.Create(&models.User{ID: 42, Status: "active"}).Error; err != nil {
		t.Fatalf("seed user 42: %v", err)
	}
	if _, err := svc.SetupTwoFactor(context.Background(), 42); err == nil {
		t.Fatal("expected error for empty account name")
	}
}

// TestTwoFactorEnableValidation 覆盖启用流程的输入校验与用户校验分支。
func TestTwoFactorEnableValidation(t *testing.T) {
	db := newAuthServiceTestDB(t)
	svc := slapp.NewService(NewGormRepository(db), twoFactorTestConfig(true))
	seedTOTPUser(t, db, 43, "enable-user")
	ctx := context.Background()

	if _, err := svc.EnableTwoFactor(ctx, 43, "   ", "123456"); !errors.Is(err, slapp.ErrInvalidAuthInput) {
		t.Fatalf("blank secret err = %v, want %v", err, slapp.ErrInvalidAuthInput)
	}
	if _, err := svc.EnableTwoFactor(ctx, 43, testTOTPSecret, "000000"); !errors.Is(err, slapp.ErrAuthInvalid2FACode) {
		t.Fatalf("bad code err = %v, want %v", err, slapp.ErrAuthInvalid2FACode)
	}
	if _, err := svc.EnableTwoFactor(ctx, 424242, testTOTPSecret, currentTOTP(t, testTOTPSecret)); err == nil {
		t.Fatal("expected error for missing user")
	}

	// 成功启用后重复启用被拒
	if _, err := svc.EnableTwoFactor(ctx, 43, testTOTPSecret, currentTOTP(t, testTOTPSecret)); err != nil {
		t.Fatalf("first EnableTwoFactor() error = %v", err)
	}
	if _, err := svc.EnableTwoFactor(ctx, 43, testTOTPSecret, currentTOTP(t, testTOTPSecret)); !errors.Is(err, slapp.ErrTwoFactorAlreadyEnabled) {
		t.Fatalf("second EnableTwoFactor err = %v, want %v", err, slapp.ErrTwoFactorAlreadyEnabled)
	}
}

// TestTwoFactorPersistErrors 覆盖落库失败：users 更新失败与恢复码写入失败。
func TestTwoFactorPersistErrors(t *testing.T) {
	db := newAuthServiceTestDB(t)
	svc := slapp.NewService(NewGormRepository(db), twoFactorTestConfig(true))
	seedTOTPUser(t, db, 44, "persist-a") // 未启用
	seedTOTPUser(t, db, 45, "persist-b")
	enrollTwoFactor(t, svc, 45) // 45 先正常启用
	ctx := context.Background()

	// users 表更新失败
	if err := db.Callback().Update().Before("gorm:update").Register("test:fail_user_update", func(tx *gorm.DB) {
		if tx.Statement != nil && tx.Statement.Table == "users" {
			_ = tx.AddError(errors.New("boom user update"))
		}
	}); err != nil {
		t.Fatalf("register callback: %v", err)
	}

	if _, err := svc.EnableTwoFactor(ctx, 44, testTOTPSecret, currentTOTP(t, testTOTPSecret)); err == nil {
		t.Fatal("expected users update failure during enable")
	}
	code := currentTOTP(t, dbSecret(t, db, 45))
	if err := svc.DisableTwoFactor(ctx, 45, "password123", code); err == nil {
		t.Fatal("expected users update failure during disable")
	}

	// 恢复码表缺失 -> replaceRecoveryCodes 事务失败
	db2 := newAuthServiceTestDB(t)
	svc2 := slapp.NewService(NewGormRepository(db2), twoFactorTestConfig(true))
	seedTOTPUser(t, db2, 46, "rc-fail")
	if err := db2.Migrator().DropTable("user_recovery_codes"); err != nil {
		t.Fatalf("drop recovery codes: %v", err)
	}
	if _, err := svc2.EnableTwoFactor(ctx, 46, testTOTPSecret, currentTOTP(t, testTOTPSecret)); err == nil {
		t.Fatal("expected recovery code persistence failure during enable")
	}
	// Enable 失败前 secret 已落库：regenerate 应走到恢复码事务并再次失败
	if _, err := svc2.RegenerateRecoveryCodes(ctx, 46, currentTOTP(t, testTOTPSecret)); err == nil {
		t.Fatal("expected recovery code persistence failure during regenerate")
	}
}

// TestTwoFactorRegenerateValidation 覆盖重发恢复码的用户/状态/校验码分支。
func TestTwoFactorRegenerateValidation(t *testing.T) {
	db := newAuthServiceTestDB(t)
	svc := slapp.NewService(NewGormRepository(db), twoFactorTestConfig(true))
	seedTOTPUser(t, db, 47, "regen-user")
	ctx := context.Background()

	if _, err := svc.RegenerateRecoveryCodes(ctx, 424242, "123456"); err == nil {
		t.Fatal("expected error for missing user")
	}
	if _, err := svc.RegenerateRecoveryCodes(ctx, 47, "123456"); !errors.Is(err, slapp.ErrTwoFactorNotEnabled) {
		t.Fatalf("err = %v, want %v", err, slapp.ErrTwoFactorNotEnabled)
	}
	enrollTwoFactor(t, svc, 47)
	if _, err := svc.RegenerateRecoveryCodes(ctx, 47, "000000"); !errors.Is(err, slapp.ErrAuthInvalid2FACode) {
		t.Fatalf("err = %v, want %v", err, slapp.ErrAuthInvalid2FACode)
	}
	fresh, err := svc.RegenerateRecoveryCodes(ctx, 47, currentTOTP(t, dbSecret(t, db, 47)))
	if err != nil || len(fresh) != 10 {
		t.Fatalf("RegenerateRecoveryCodes() = %d codes, %v", len(fresh), err)
	}
}

// TestTwoFactorVerifyChallengePaths 覆盖挑战步的各拒绝分支与会话创建失败。
func TestTwoFactorVerifyChallengePaths(t *testing.T) {
	db := newAuthServiceTestDB(t)
	svc := slapp.NewService(NewGormRepository(db), twoFactorTestConfig(true))
	seedTOTPUser(t, db, 48, "verify-user")
	seedTOTPUser(t, db, 49, "noverify-user") // 不启用 2FA
	meta := slapp.AuthSessionMetadata{UserAgent: "ua-verify", ClientIP: "198.51.100.8"}
	ctx := context.Background()

	// 非法 token / 错误用途 token
	if _, err := svc.VerifyTwoFactorLogin(ctx, slapp.TwoFactorVerifyInput{ChallengeToken: "garbage"}, meta); !errors.Is(err, slapp.ErrAuthInvalid2FAChallenge) {
		t.Fatalf("garbage token err = %v", err)
	}
	accessTok, err := slapp.HookCreateHS256JWT(map[string]interface{}{
		"user_id": 48, "token_use": "access", "exp": time.Now().Add(time.Hour).Unix(),
	}, testAuthConfig().JWT.Secret)
	if err != nil {
		t.Fatalf("craft access token: %v", err)
	}
	if _, err := svc.VerifyTwoFactorLogin(ctx, slapp.TwoFactorVerifyInput{ChallengeToken: accessTok}, meta); !errors.Is(err, slapp.ErrAuthInvalid2FAChallenge) {
		t.Fatalf("access token err = %v, want challenge rejection", err)
	}

	// 未启用 2FA 的活跃用户拿到挑战 token 后仍被拒（不带指纹声明的挑战 token，
	// 指纹校验按“非空才比对”规则跳过，落点到未绑定用户的拒绝分支）。
	challenge, err := slapp.HookCreateHS256JWT(map[string]interface{}{
		"user_id": 49, "token_use": "2fa_challenge", "exp": time.Now().Add(time.Hour).Unix(),
	}, testAuthConfig().JWT.Secret)
	if err != nil {
		t.Fatalf("craft challenge token: %v", err)
	}
	if _, err := svc.VerifyTwoFactorLogin(ctx, slapp.TwoFactorVerifyInput{ChallengeToken: challenge, Code: "123456"}, meta); !errors.Is(err, slapp.ErrAuthInvalid2FAChallenge) {
		t.Fatalf("non-enrolled user err = %v, want %v", err, slapp.ErrAuthInvalid2FAChallenge)
	}

	// 正常用户：ua 指纹不符 / 用户被删 / 用户被封禁 / 会话创建失败
	secret, _ := enrollTwoFactor(t, svc, 48)
	outcome, err := svc.Login(ctx, slapp.LoginInput{Username: "verify-user", Password: "password123"}, meta)
	if err != nil {
		t.Fatalf("Login() error = %v", err)
	}

	if _, err := svc.VerifyTwoFactorLogin(ctx, slapp.TwoFactorVerifyInput{ChallengeToken: outcome.ChallengeToken, Code: currentTOTP(t, secret)},
		slapp.AuthSessionMetadata{UserAgent: "different-ua", ClientIP: meta.ClientIP}); !errors.Is(err, slapp.ErrAuthInvalid2FAChallenge) {
		t.Fatalf("ua mismatch err = %v, want %v", err, slapp.ErrAuthInvalid2FAChallenge)
	}

	bannedOut, err := svc.Login(ctx, slapp.LoginInput{Username: "verify-user", Password: "password123"}, meta)
	if err != nil {
		t.Fatalf("second Login() error = %v", err)
	}
	if err := db.Model(&models.User{}).Where("id = ?", 48).Update("status", "banned").Error; err != nil {
		t.Fatalf("ban user: %v", err)
	}
	if _, err := svc.VerifyTwoFactorLogin(ctx, slapp.TwoFactorVerifyInput{ChallengeToken: bannedOut.ChallengeToken, Code: currentTOTP(t, secret)}, meta); !errors.Is(err, slapp.ErrAuthUserDisabled) {
		t.Fatalf("banned user err = %v, want %v", err, slapp.ErrAuthUserDisabled)
	}
	if err := db.Model(&models.User{}).Where("id = ?", 48).Update("status", "active").Error; err != nil {
		t.Fatalf("unban user: %v", err)
	}

	missingOut, err := svc.Login(ctx, slapp.LoginInput{Username: "verify-user", Password: "password123"}, meta)
	if err != nil {
		t.Fatalf("third Login() error = %v", err)
	}
	if err := db.Where("id = ?", 48).Delete(&models.User{}).Error; err != nil {
		t.Fatalf("delete user: %v", err)
	}
	if _, err := svc.VerifyTwoFactorLogin(ctx, slapp.TwoFactorVerifyInput{ChallengeToken: missingOut.ChallengeToken, Code: currentTOTP(t, secret)}, meta); !errors.Is(err, slapp.ErrAuthInvalid2FAChallenge) {
		t.Fatalf("deleted user err = %v, want %v", err, slapp.ErrAuthInvalid2FAChallenge)
	}
}

// TestTwoFactorVerifySessionCreateError 覆盖挑战通过后会话创建失败。
func TestTwoFactorVerifySessionCreateError(t *testing.T) {
	db := newAuthServiceTestDB(t)
	svc := slapp.NewService(NewGormRepository(db), twoFactorTestConfig(true))
	seedTOTPUser(t, db, 50, "verify-sess")
	meta := slapp.AuthSessionMetadata{UserAgent: "ua-sess", ClientIP: "198.51.100.8"}
	ctx := context.Background()

	secret, _ := enrollTwoFactor(t, svc, 50)
	outcome, err := svc.Login(ctx, slapp.LoginInput{Username: "verify-sess", Password: "password123"}, meta)
	if err != nil {
		t.Fatalf("Login() error = %v", err)
	}
	if err := db.Migrator().DropTable("user_auth_sessions"); err != nil {
		t.Fatalf("drop sessions: %v", err)
	}
	if _, err := svc.VerifyTwoFactorLogin(ctx, slapp.TwoFactorVerifyInput{ChallengeToken: outcome.ChallengeToken, Code: currentTOTP(t, secret)}, meta); err == nil {
		t.Fatal("expected session create failure")
	}
}

// TestTwoFactorDisableMissingUser 覆盖解绑时用户缺失分支。
func TestTwoFactorDisableMissingUser(t *testing.T) {
	db := newAuthServiceTestDB(t)
	svc := slapp.NewService(NewGormRepository(db), twoFactorTestConfig(true))
	if err := svc.DisableTwoFactor(context.Background(), 424242, "pw", "123456"); err == nil {
		t.Fatal("expected error for missing user")
	}
}
