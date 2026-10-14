package infra

// auth 模块的包级 seam 覆盖：crypto/rand 与 JWT 签名失败分支
// （自 services/hscov_seam_test.go auth 段下沉）。

import (
	"context"
	"errors"
	slapp "servify/apps/server/internal/modules/auth/application"
	"strings"
	"testing"

	"servify/apps/server/internal/models"
)

// setSeam 替换一个包级变量并在测试结束后还原。
func setSeam[T any](t *testing.T, slot *T, value T) {
	t.Helper()
	old := *slot
	*slot = value
	t.Cleanup(func() { *slot = old })
}

// failingRand 让 crypto/rand 注入点按 failPlan 依次失败/成功。
func failingRand(t *testing.T, failPlan []bool, calls *int) {
	t.Helper()
	setSeam(t, &slapp.HookRandRead, func(b []byte) (int, error) {
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

// randomHex/会话 ID/token ID/oidcUsername/provisionOIDCUser 等
// 服务内部 seam 覆盖随内部分支测试移至 application 包。

func TestAuthSeamsGenerateRecoveryCodesRandError(t *testing.T) {
	calls := 0
	failingRand(t, []bool{true}, &calls)
	codes, hashes, err := slapp.GenerateRecoveryCodes(3)
	if err == nil || !strings.Contains(err.Error(), "generate recovery code") {
		t.Fatalf("expected recovery code rand error, got %v", err)
	}
	if codes != nil || hashes != nil {
		t.Fatalf("expected nil codes/hashes on error, got %v %v", codes, hashes)
	}
}

// TestAuthSeamsEnableTwoFactorRecoveryCodeRandError：绑定期间恢复码生成失败
// 必须整体回滚——不写 totp 字段、不落任何恢复码。
func TestAuthSeamsEnableTwoFactorRecoveryCodeRandError(t *testing.T) {
	db := newAuthServiceTestDB(t)
	svc := slapp.NewService(NewGormRepository(db), twoFactorTestConfig(true))
	seedTOTPUser(t, db, 403, "enable-rand-err")
	setup, err := svc.SetupTwoFactor(context.Background(), 403)
	if err != nil {
		t.Fatalf("SetupTwoFactor: %v", err)
	}

	calls := 0
	failingRand(t, []bool{true}, &calls)
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

// TestAuthSeamsRegenerateRecoveryCodesRandError：重发期间恢复码生成失败
// 必须保留旧码不做替换。
func TestAuthSeamsRegenerateRecoveryCodesRandError(t *testing.T) {
	db := newAuthServiceTestDB(t)
	svc := slapp.NewService(NewGormRepository(db), twoFactorTestConfig(true))
	seedTOTPUser(t, db, 404, "regen-rand-err")
	secret, codes := enrollTwoFactor(t, svc, 404)

	calls := 0
	failingRand(t, []bool{true}, &calls)
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

// --- JWT 签名失败：挑战 token 与 access/refresh token ---

func TestAuthSeamsLoginChallengeTokenError(t *testing.T) {
	db := newAuthServiceTestDB(t)
	svc := slapp.NewService(NewGormRepository(db), twoFactorTestConfig(true))
	seedTOTPUser(t, db, 401, "challenge-err")
	secret, _ := enrollTwoFactor(t, svc, 401)

	setSeam(t, &slapp.HookCreateHS256JWT, func(map[string]interface{}, string) (string, error) {
		return "", errors.New("boom: sign")
	})
	_, err := svc.Login(context.Background(), slapp.LoginInput{Username: "challenge-err", Password: "password123"}, slapp.AuthSessionMetadata{})
	if err == nil || !strings.Contains(err.Error(), "boom: sign") {
		t.Fatalf("expected challenge sign error, got %v", err)
	}
	_ = secret
}

func TestAuthSeamsLoginBuildAuthResultTokenErrors(t *testing.T) {
	realSign := slapp.HookCreateHS256JWT
	db := newAuthServiceTestDB(t)
	svc := slapp.NewService(NewGormRepository(db), twoFactorTestConfig(false))
	seedTOTPUser(t, db, 402, "sign-err")

	var calls int
	setSeam(t, &slapp.HookCreateHS256JWT, func(payload map[string]interface{}, secret string) (string, error) {
		calls++
		if calls == 1 { // 第一次 = access token
			return "", errors.New("boom: access sign")
		}
		return realSign(payload, secret)
	})
	_, err := svc.Login(context.Background(), slapp.LoginInput{Username: "sign-err", Password: "password123"}, slapp.AuthSessionMetadata{})
	if err == nil || !strings.Contains(err.Error(), "access sign") {
		t.Fatalf("expected access token sign error, got %v", err)
	}

	// 第二次调用 = refresh token：access 成功、refresh 失败。
	calls = 0
	setSeam(t, &slapp.HookCreateHS256JWT, func(payload map[string]interface{}, secret string) (string, error) {
		calls++
		if calls == 2 {
			return "", errors.New("boom: refresh sign")
		}
		return realSign(payload, secret)
	})
	_, err = svc.Login(context.Background(), slapp.LoginInput{Username: "sign-err", Password: "password123"}, slapp.AuthSessionMetadata{})
	if err == nil || !strings.Contains(err.Error(), "refresh sign") {
		t.Fatalf("expected refresh token sign error, got %v", err)
	}
}
