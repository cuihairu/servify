package application

// auth 落库错误分支：trigger 注入、顺序查询失败（自 services
// more_branches/sequential_errors/trigger_error_paths 单测的 auth 段下沉）。

import (
	"context"
	"errors"
	"sync/atomic"
	"testing"
	"time"

	"servify/apps/server/internal/models"

	"golang.org/x/crypto/bcrypt"
	"gorm.io/gorm"
)

func authExecTrigger(t *testing.T, db *gorm.DB, stmt string) {
	t.Helper()
	if err := db.Exec(stmt).Error; err != nil {
		t.Fatalf("create trigger: %v", err)
	}
}

func authBcryptHash(pw string) (string, error) {
	b, err := bcrypt.GenerateFromPassword([]byte(pw), bcrypt.MinCost)
	return string(b), err
}

// authFailNthQuery 注册 gorm callback，强制第 n 次 SELECT 失败。
func authFailNthQuery(db *gorm.DB, n int32) {
	var calls int32
	bump := func(tx *gorm.DB) {
		if atomic.AddInt32(&calls, 1) == n {
			_ = tx.AddError(errors.New("forced nth query failure"))
		}
	}
	_ = db.Callback().Query().Before("gorm:query").Register("fail_nth", bump)
	_ = db.Callback().Row().Before("gorm:row").Register("fail_nth_row", bump)
}

func TestAuthService_RegisterInsertTrigger(t *testing.T) {
	// dup-flavoured insert error maps to ErrAuthUserAlreadyExists
	db := newServicesTestDB(t, &models.User{}, &models.UserAuthSession{})
	svc := NewService(db, testAuthConfig())
	authExecTrigger(t, db, "CREATE TRIGGER blk_u1 BEFORE INSERT ON users BEGIN SELECT RAISE(ABORT, 'unique violation simulation'); END;")
	if _, err := svc.Register(context.Background(), RegisterInput{
		Username: "u", Email: "u@x.com", Password: "pw123456",
	}, AuthSessionMetadata{}); err != ErrAuthUserAlreadyExists {
		t.Fatalf("expected duplicate-style error, got %v", err)
	}

	// plain insert error surfaces as-is
	db2 := newServicesTestDB(t, &models.User{}, &models.UserAuthSession{})
	svc2 := NewService(db2, testAuthConfig())
	authExecTrigger(t, db2, "CREATE TRIGGER blk_u2 BEFORE INSERT ON users BEGIN SELECT RAISE(ABORT, 'blocked'); END;")
	if _, err := svc2.Register(context.Background(), RegisterInput{
		Username: "u", Email: "u@x.com", Password: "pw123456",
	}, AuthSessionMetadata{}); err == nil || err == ErrAuthUserAlreadyExists {
		t.Fatalf("expected plain insert error, got %v", err)
	}
}

func TestAuthService_RevokeOtherTriggerError(t *testing.T) {
	db := newServicesTestDB(t, &models.User{}, &models.UserAuthSession{})
	svc := NewService(db, testAuthConfig())
	now := time.Now().UTC()
	if err := db.Create(&models.User{ID: 81, Username: "u81", Email: "u81@x.com", Password: "x", Status: "active"}).Error; err != nil {
		t.Fatalf("seed: %v", err)
	}
	if err := db.Create(&models.UserAuthSession{ID: "s81", UserID: 81, Status: "active", LastSeenAt: &now}).Error; err != nil {
		t.Fatalf("seed session: %v", err)
	}
	authExecTrigger(t, db, "CREATE TRIGGER blk_s81 BEFORE UPDATE ON user_auth_sessions BEGIN SELECT RAISE(ABORT, 'update blocked'); END;")
	if _, err := svc.RevokeOtherSessions(context.Background(), 81, "current"); err == nil {
		t.Fatal("expected revoke others update error")
	}
}

func TestAuthService_RotateTriggerError(t *testing.T) {
	db := newServicesTestDB(t, &models.User{}, &models.UserAuthSession{})
	cfg := testAuthConfig()
	svc := NewService(db, cfg)

	hash, err := authBcryptHash("pw123456")
	if err != nil {
		t.Fatalf("hash: %v", err)
	}
	if err := db.Create(&models.User{
		ID: 71, Username: "u71", Email: "u71@x.com", Password: hash, Status: "active",
	}).Error; err != nil {
		t.Fatalf("seed user: %v", err)
	}
	if err := db.Create(&models.UserAuthSession{ID: "sess-71", UserID: 71, Status: "active", TokenVersion: 0}).Error; err != nil {
		t.Fatalf("seed session: %v", err)
	}
	authExecTrigger(t, db, "CREATE TRIGGER blk_sess BEFORE UPDATE ON user_auth_sessions BEGIN SELECT RAISE(ABORT, 'update blocked'); END;")

	tok, err := createHS256JWT(map[string]interface{}{
		"token_use": "refresh", "user_id": float64(71), "session_id": "sess-71",
		"session_token_version": float64(0), "token_version": float64(0),
		"iat": float64(time.Now().Unix()),
	}, cfg.JWT.Secret)
	if err != nil {
		t.Fatalf("token: %v", err)
	}
	if _, err := svc.RefreshToken(context.Background(), tok, AuthSessionMetadata{}); err != ErrAuthInvalidRefreshToken {
		t.Fatalf("expected rotate update failure, got %v", err)
	}
}

func TestAuthService_SequentialErrors(t *testing.T) {
	ctx := context.Background()
	now := time.Now().UTC()

	// RevokeCurrentSession: reload after update fails
	db := newServicesTestDB(t, &models.User{}, &models.UserAuthSession{})
	svc := NewService(db, testAuthConfig())
	if err := db.Create(&models.User{ID: 91, Username: "u91", Email: "u91@x.com", Password: "x", Status: "active"}).Error; err != nil {
		t.Fatalf("seed user: %v", err)
	}
	if err := db.Create(&models.UserAuthSession{ID: "s91", UserID: 91, Status: "active", LastSeenAt: &now}).Error; err != nil {
		t.Fatalf("seed session: %v", err)
	}
	authFailNthQuery(db, 1)
	if _, err := svc.RevokeCurrentSession(ctx, 91, "s91"); err == nil {
		t.Fatal("expected reload error after revoke")
	}

	// RefreshToken: rotate reload fails (3rd query: user lookup, session load, reload)
	db2 := newServicesTestDB(t, &models.User{}, &models.UserAuthSession{})
	cfg := testAuthConfig()
	svc2 := NewService(db2, cfg)
	hash, err := authBcryptHash("pw123456")
	if err != nil {
		t.Fatalf("hash: %v", err)
	}
	if err := db2.Create(&models.User{
		ID: 92, Username: "u92", Email: "u92@x.com", Password: hash, Status: "active",
	}).Error; err != nil {
		t.Fatalf("seed user: %v", err)
	}
	if err := db2.Create(&models.UserAuthSession{ID: "s92", UserID: 92, Status: "active", TokenVersion: 0}).Error; err != nil {
		t.Fatalf("seed session: %v", err)
	}
	authFailNthQuery(db2, 3)
	tok, err := createHS256JWT(map[string]interface{}{
		"token_use": "refresh", "user_id": float64(92), "session_id": "s92",
		"session_token_version": float64(0), "token_version": float64(0),
		"iat": float64(time.Now().Unix()),
	}, cfg.JWT.Secret)
	if err != nil {
		t.Fatalf("token: %v", err)
	}
	if _, err := svc2.RefreshToken(ctx, tok, AuthSessionMetadata{}); err != ErrAuthInvalidRefreshToken {
		t.Fatalf("expected rotate reload failure, got %v", err)
	}
}
