package services

import (
	"context"
	"testing"

	"servify/apps/server/internal/models"

	"golang.org/x/crypto/bcrypt"
)

// TestAuthServiceLoginSessionCreateError 用真实 bcrypt 哈希让登录走到会话创建，
// 再以缺失的会话表驱动 Login 的会话创建失败分支。
func TestAuthServiceLoginSessionCreateError(t *testing.T) {
	db := newServicesTestDB(t, &models.User{}, &models.UserAuthSession{})
	svc := NewAuthService(db, testAuthConfig())
	hash, err := bcrypt.GenerateFromPassword([]byte("pw123456"), bcrypt.MinCost)
	if err != nil {
		t.Fatalf("hash password: %v", err)
	}
	if err := db.Create(&models.User{
		ID: 63, Username: "u63", Email: "u63@x.com", Password: string(hash), Status: "active",
	}).Error; err != nil {
		t.Fatalf("seed: %v", err)
	}
	if err := db.Migrator().DropTable("user_auth_sessions"); err != nil {
		t.Fatalf("drop sessions: %v", err)
	}

	if _, err := svc.Login(context.Background(), LoginInput{Username: "u63", Password: "pw123456"}, AuthSessionMetadata{}); err == nil {
		t.Fatal("expected session create error during login")
	}
}
