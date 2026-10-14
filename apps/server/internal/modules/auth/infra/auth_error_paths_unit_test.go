package infra

// Register/Login/session 内部错误分支（自 services/auth_error_paths_unit_test.go 下沉；
// satisfaction/AI 段留在 services 包）。

import (
	"context"
	slapp "servify/apps/server/internal/modules/auth/application"
	"strings"
	"testing"

	"servify/apps/server/internal/models"
)

func TestAuthService_RegisterInternalErrors(t *testing.T) {
	// bcrypt rejects passwords longer than 72 bytes
	db := newServicesTestDB(t, &models.User{}, &models.UserAuthSession{})
	svc := slapp.NewService(NewGormRepository(db), testAuthConfig())
	if _, err := svc.Register(context.Background(), slapp.RegisterInput{
		Username: "long-pw", Email: "long@x.com", Password: strings.Repeat("x", 100),
	}, slapp.AuthSessionMetadata{}); err == nil {
		t.Fatal("expected bcrypt error for long password")
	}

	// session creation failure surfaces
	db2 := newServicesTestDB(t, &models.User{}, &models.UserAuthSession{})
	svc2 := slapp.NewService(NewGormRepository(db2), testAuthConfig())
	if err := db2.Create(&models.User{
		ID: 61, Username: "u61", Email: "u61@x.com", Password: "x", Status: "active",
	}).Error; err != nil {
		t.Fatalf("seed: %v", err)
	}
	if err := db2.Migrator().DropTable("user_auth_sessions"); err != nil {
		t.Fatalf("drop sessions: %v", err)
	}
	// register a different user: user row inserts, session insert fails
	if _, err := svc2.Register(context.Background(), slapp.RegisterInput{
		Username: "u62", Email: "u62@x.com", Password: "pw123456",
	}, slapp.AuthSessionMetadata{}); err == nil {
		t.Fatal("expected session create error")
	}
}

func TestAuthService_LoginInternalErrors(t *testing.T) {
	// user lookup error (not found variant)
	db := newServicesTestDB(t, &models.User{}, &models.UserAuthSession{})
	svc := slapp.NewService(NewGormRepository(db), testAuthConfig())
	if err := db.Migrator().DropTable("users"); err != nil {
		t.Fatalf("drop users: %v", err)
	}
	if _, err := svc.Login(context.Background(), slapp.LoginInput{Username: "u", Password: "p"}, slapp.AuthSessionMetadata{}); err == nil ||
		err == slapp.ErrAuthInvalidCredentials {
		t.Fatalf("expected query error, got %v", err)
	}

	// session creation failure during login
	db2 := newServicesTestDB(t, &models.User{}, &models.UserAuthSession{})
	svc2 := slapp.NewService(NewGormRepository(db2), testAuthConfig())
	if err := db2.Create(&models.User{
		ID: 62, Username: "u62", Email: "u62@x.com", Password: "pw123456", Status: "active",
	}).Error; err != nil {
		t.Fatalf("seed: %v", err)
	}
	if err := db2.Migrator().DropTable("user_auth_sessions"); err != nil {
		t.Fatalf("drop sessions: %v", err)
	}
	if _, err := svc2.Login(context.Background(), slapp.LoginInput{Username: "u62", Password: "pw123456"}, slapp.AuthSessionMetadata{}); err == nil {
		t.Fatal("expected session create error during login")
	}
}

func TestAuthService_SessionQueryErrors(t *testing.T) {
	db := newServicesTestDB(t, &models.User{}, &models.UserAuthSession{})
	svc := slapp.NewService(NewGormRepository(db), testAuthConfig())
	ctx := context.Background()

	if err := db.Migrator().DropTable("user_auth_sessions"); err != nil {
		t.Fatalf("drop sessions: %v", err)
	}
	if _, err := svc.ListAuthSessions(ctx, 1); err == nil {
		t.Fatal("expected list sessions error")
	}
	if _, err := svc.RevokeCurrentSession(ctx, 1, "s"); err == nil {
		t.Fatal("expected revoke current error")
	}
	if _, err := svc.RevokeOtherSessions(ctx, 1, "s"); err == nil {
		t.Fatal("expected revoke others error")
	}
	if _, err := svc.RefreshToken(ctx, "x", slapp.AuthSessionMetadata{}); err == nil {
		t.Fatal("expected refresh error")
	}
}
