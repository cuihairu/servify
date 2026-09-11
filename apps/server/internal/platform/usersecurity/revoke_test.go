package usersecurity

import (
	"context"
	"crypto/hmac"
	"crypto/sha256"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"sync/atomic"
	"testing"
	"time"

	"servify/apps/server/internal/models"
	platformauth "servify/apps/server/internal/platform/auth"

	"github.com/glebarez/sqlite"
	"gorm.io/gorm"
)

var usersecurityDBSeq uint32

// uniqueMemoryDSN 返回带全局唯一序号的内存库 DSN，避免重跑或并行时命中同一命名库
func uniqueMemoryDSN(name string) string {
	return fmt.Sprintf("file:%s_%d?mode=memory&cache=shared", name, atomic.AddUint32(&usersecurityDBSeq, 1))
}

func TestRevokeUserTokens(t *testing.T) {
	db, err := gorm.Open(sqlite.Open(uniqueMemoryDSN("usersecurity_revoke")), &gorm.Config{})
	if err != nil {
		t.Fatalf("open sqlite: %v", err)
	}
	if err := db.AutoMigrate(&models.User{}); err != nil {
		t.Fatalf("migrate: %v", err)
	}
	if err := db.Create(&models.User{
		ID:       1,
		Username: "u1",
		Email:    "u1@example.com",
		Status:   "active",
	}).Error; err != nil {
		t.Fatalf("seed user: %v", err)
	}

	revokeAt := time.Unix(1_700_000_000, 0).UTC()
	version, err := RevokeUserTokens(context.Background(), db, 1, revokeAt)
	if err != nil {
		t.Fatalf("RevokeUserTokens() error = %v", err)
	}
	if version != 1 {
		t.Fatalf("version = %d want 1", version)
	}

	var user models.User
	if err := db.First(&user, 1).Error; err != nil {
		t.Fatalf("load user: %v", err)
	}
	if user.TokenVersion != 1 {
		t.Fatalf("token_version = %d want 1", user.TokenVersion)
	}
	if user.TokenValidAfter == nil || !user.TokenValidAfter.Equal(revokeAt) {
		t.Fatalf("token_valid_after = %v want %v", user.TokenValidAfter, revokeAt)
	}
}

func TestServiceGetUsers(t *testing.T) {
	db, err := gorm.Open(sqlite.Open(uniqueMemoryDSN("usersecurity_get_users")), &gorm.Config{})
	if err != nil {
		t.Fatalf("open sqlite: %v", err)
	}
	if err := db.AutoMigrate(&models.User{}); err != nil {
		t.Fatalf("migrate: %v", err)
	}
	users := []models.User{
		{ID: 11, Username: "u11", Email: "u11@example.com", Status: "active", Role: "admin", TokenVersion: 2},
		{ID: 12, Username: "u12", Email: "u12@example.com", Status: "inactive", Role: "agent", TokenVersion: 0},
	}
	if err := db.Create(&users).Error; err != nil {
		t.Fatalf("seed users: %v", err)
	}

	svc := NewService(db, nil)

	got, err := svc.GetUsers(context.Background(), []uint{12, 11, 12})
	if err != nil {
		t.Fatalf("GetUsers() error = %v", err)
	}
	if len(got) != 3 {
		t.Fatalf("len(GetUsers()) = %d want 3", len(got))
	}
	if got[0].ID != 12 || got[1].ID != 11 || got[2].ID != 12 {
		t.Fatalf("unexpected order: %+v", got)
	}

	if _, err := svc.GetUsers(context.Background(), []uint{11, 99}); err == nil {
		t.Fatalf("expected missing user error")
	}
}

func TestServiceListAndRevokeSession(t *testing.T) {
	db, err := gorm.Open(sqlite.Open(uniqueMemoryDSN("usersecurity_sessions")), &gorm.Config{})
	if err != nil {
		t.Fatalf("open sqlite: %v", err)
	}
	if err := db.AutoMigrate(&models.User{}, &models.UserAuthSession{}); err != nil {
		t.Fatalf("migrate: %v", err)
	}
	if err := db.Create(&models.User{
		ID:       21,
		Username: "u21",
		Email:    "u21@example.com",
		Status:   "active",
	}).Error; err != nil {
		t.Fatalf("seed user: %v", err)
	}
	if err := db.Create(&models.UserAuthSession{
		ID:           "auth-session-test",
		UserID:       21,
		Status:       "active",
		TokenVersion: 1,
	}).Error; err != nil {
		t.Fatalf("seed auth session: %v", err)
	}

	svc := NewService(db, nil)

	sessions, err := svc.ListUserSessions(context.Background(), 21)
	if err != nil {
		t.Fatalf("ListUserSessions() error = %v", err)
	}
	if len(sessions) != 1 || sessions[0].ID != "auth-session-test" {
		t.Fatalf("unexpected sessions: %+v", sessions)
	}

	session, err := svc.RevokeSession(context.Background(), 21, "auth-session-test")
	if err != nil {
		t.Fatalf("RevokeSession() error = %v", err)
	}
	if session.Status != "revoked" {
		t.Fatalf("status = %q want revoked", session.Status)
	}
	if session.TokenVersion != 2 {
		t.Fatalf("token_version = %d want 2", session.TokenVersion)
	}
	if session.RevokedAt == nil || session.RevokedAt.IsZero() {
		t.Fatalf("expected revoked_at to be set")
	}
}

func TestServiceRevokeAllSessions(t *testing.T) {
	db, err := gorm.Open(sqlite.Open(uniqueMemoryDSN("usersecurity_revoke_all_sessions")), &gorm.Config{})
	if err != nil {
		t.Fatalf("open sqlite: %v", err)
	}
	if err := db.AutoMigrate(&models.User{}, &models.UserAuthSession{}); err != nil {
		t.Fatalf("migrate: %v", err)
	}
	if err := db.Create(&models.User{
		ID:       31,
		Username: "u31",
		Email:    "u31@example.com",
		Status:   "active",
	}).Error; err != nil {
		t.Fatalf("seed user: %v", err)
	}
	if err := db.Create([]models.UserAuthSession{
		{ID: "auth-a", UserID: 31, Status: "active", TokenVersion: 1},
		{ID: "auth-b", UserID: 31, Status: "active", TokenVersion: 2},
		{ID: "auth-c", UserID: 31, Status: "revoked", TokenVersion: 3},
	}).Error; err != nil {
		t.Fatalf("seed auth sessions: %v", err)
	}

	svc := NewService(db, nil)
	result, err := svc.RevokeAllSessions(context.Background(), 31, "auth-b")
	if err != nil {
		t.Fatalf("RevokeAllSessions() error = %v", err)
	}
	if result.Count != 1 || len(result.Sessions) != 1 || result.Sessions[0].ID != "auth-a" {
		t.Fatalf("unexpected result: %+v", result)
	}

	var sessions []models.UserAuthSession
	if err := db.Order("id asc").Find(&sessions, "user_id = ?", 31).Error; err != nil {
		t.Fatalf("reload sessions: %v", err)
	}
	if sessions[0].Status != "revoked" || sessions[0].TokenVersion != 2 {
		t.Fatalf("unexpected auth-a state: %+v", sessions[0])
	}
	if sessions[1].Status != "active" || sessions[1].TokenVersion != 2 {
		t.Fatalf("unexpected auth-b state: %+v", sessions[1])
	}
}

func TestServiceScopedUserAccess(t *testing.T) {
	db, err := gorm.Open(sqlite.Open(uniqueMemoryDSN("usersecurity_scope_access")), &gorm.Config{})
	if err != nil {
		t.Fatalf("open sqlite: %v", err)
	}
	if err := db.AutoMigrate(&models.User{}, &models.UserAuthSession{}, &models.Agent{}, &models.Customer{}); err != nil {
		t.Fatalf("migrate: %v", err)
	}
	if err := db.Create([]models.User{
		{ID: 41, Username: "u41", Email: "u41@example.com", Status: "active", Role: "agent"},
		{ID: 42, Username: "u42", Email: "u42@example.com", Status: "active", Role: "customer"},
	}).Error; err != nil {
		t.Fatalf("seed users: %v", err)
	}
	if err := db.Create(&models.Agent{UserID: 41, TenantID: "tenant-a", WorkspaceID: "workspace-a"}).Error; err != nil {
		t.Fatalf("seed scoped agent: %v", err)
	}
	if err := db.Create(&models.Customer{UserID: 42, TenantID: "tenant-b", WorkspaceID: "workspace-b"}).Error; err != nil {
		t.Fatalf("seed out-of-scope customer: %v", err)
	}
	if err := db.Create([]models.UserAuthSession{
		{ID: "auth-41", UserID: 41, Status: "active", TokenVersion: 1},
		{ID: "auth-42", UserID: 42, Status: "active", TokenVersion: 1},
	}).Error; err != nil {
		t.Fatalf("seed auth sessions: %v", err)
	}

	svc := NewService(db, nil)
	ctx := platformauth.ContextWithScope(context.Background(), "tenant-a", "workspace-a")

	if _, err := svc.GetUser(ctx, 41); err != nil {
		t.Fatalf("GetUser(scoped allowed) error = %v", err)
	}
	if _, err := svc.GetUser(ctx, 42); err == nil {
		t.Fatalf("expected scoped GetUser to reject cross-scope user")
	}

	users, err := svc.GetUsers(ctx, []uint{41, 41})
	if err != nil {
		t.Fatalf("GetUsers(scoped duplicates) error = %v", err)
	}
	if len(users) != 2 || users[0].ID != 41 || users[1].ID != 41 {
		t.Fatalf("unexpected scoped users: %+v", users)
	}
	if _, err := svc.GetUsers(ctx, []uint{41, 42}); err == nil {
		t.Fatalf("expected scoped GetUsers to reject mixed-scope batch")
	}

	if _, err := svc.ListUserSessions(ctx, 41); err != nil {
		t.Fatalf("ListUserSessions(scoped allowed) error = %v", err)
	}
	if _, err := svc.ListUserSessions(ctx, 42); err == nil {
		t.Fatalf("expected scoped ListUserSessions to reject cross-scope user")
	}

	if _, err := svc.RevokeTokens(ctx, 41); err != nil {
		t.Fatalf("RevokeTokens(scoped allowed) error = %v", err)
	}
	if _, err := svc.RevokeTokens(ctx, 42); err == nil {
		t.Fatalf("expected scoped RevokeTokens to reject cross-scope user")
	}

	if _, err := svc.RevokeSession(ctx, 41, "auth-41"); err != nil {
		t.Fatalf("RevokeSession(scoped allowed) error = %v", err)
	}
	if _, err := svc.RevokeSession(ctx, 42, "auth-42"); err == nil {
		t.Fatalf("expected scoped RevokeSession to reject cross-scope user")
	}

	if _, err := svc.RevokeAllSessions(ctx, 42, ""); err == nil {
		t.Fatalf("expected scoped RevokeAllSessions to reject cross-scope user")
	}
}

func TestServiceRevokeJWT(t *testing.T) {
	db, err := gorm.Open(sqlite.Open(uniqueMemoryDSN("usersecurity_revoke_jwt")), &gorm.Config{})
	if err != nil {
		t.Fatalf("open sqlite: %v", err)
	}
	if err := db.AutoMigrate(&models.RevokedToken{}); err != nil {
		t.Fatalf("migrate: %v", err)
	}

	now := time.Now().UTC()
	secret := "test-secret"
	token := createTestJWTForUserSecurity(t, map[string]interface{}{
		"jti":        "jti-usersecurity-1",
		"user_id":    9,
		"session_id": "auth-9",
		"token_use":  "access",
		"iat":        now.Unix(),
		"exp":        now.Add(30 * time.Minute).Unix(),
	}, secret)

	svc := NewService(db, nil)
	result, err := svc.RevokeJWT(context.Background(), token, secret, "security-event")
	if err != nil {
		t.Fatalf("RevokeJWT() error = %v", err)
	}
	if result.JTI != "jti-usersecurity-1" || result.UserID != 9 || result.TokenUse != "access" {
		t.Fatalf("unexpected result: %+v", result)
	}

	var revoked models.RevokedToken
	if err := db.First(&revoked, "jti = ?", "jti-usersecurity-1").Error; err != nil {
		t.Fatalf("load revoked token: %v", err)
	}
	if revoked.Reason != "security-event" {
		t.Fatalf("reason = %q want security-event", revoked.Reason)
	}
}

func TestServiceScopedTokenSurface(t *testing.T) {
	db, err := gorm.Open(sqlite.Open(uniqueMemoryDSN("usersecurity_scoped_tokens")), &gorm.Config{})
	if err != nil {
		t.Fatalf("open sqlite: %v", err)
	}
	if err := db.AutoMigrate(&models.User{}, &models.Agent{}, &models.Customer{}, &models.RevokedToken{}); err != nil {
		t.Fatalf("migrate: %v", err)
	}
	if err := db.Create([]models.User{
		{ID: 61, Username: "u61", Email: "u61@example.com", Status: "active", Role: "agent"},
		{ID: 62, Username: "u62", Email: "u62@example.com", Status: "active", Role: "customer"},
	}).Error; err != nil {
		t.Fatalf("seed users: %v", err)
	}
	if err := db.Create(&models.Agent{UserID: 61, TenantID: "tenant-a", WorkspaceID: "workspace-a"}).Error; err != nil {
		t.Fatalf("seed scoped agent: %v", err)
	}
	if err := db.Create(&models.Customer{UserID: 62, TenantID: "tenant-b", WorkspaceID: "workspace-b"}).Error; err != nil {
		t.Fatalf("seed cross-scope customer: %v", err)
	}

	now := time.Now().UTC()
	expiry := now.Add(30 * time.Minute)
	if err := db.Create([]models.RevokedToken{
		{JTI: "jti-scope-61", UserID: 61, SessionID: "sess-61", TokenUse: "access", ExpiresAt: &expiry, RevokedAt: now},
		{JTI: "jti-scope-62", UserID: 62, SessionID: "sess-62", TokenUse: "access", ExpiresAt: &expiry, RevokedAt: now},
	}).Error; err != nil {
		t.Fatalf("seed revoked tokens: %v", err)
	}

	svc := NewService(db, nil)
	ctx := platformauth.ContextWithScope(context.Background(), "tenant-a", "workspace-a")

	items, total, err := svc.ListRevokedTokens(ctx, RevokedTokenListQuery{Page: 1, PageSize: 20})
	if err != nil {
		t.Fatalf("ListRevokedTokens(scoped) error = %v", err)
	}
	if total != 1 || len(items) != 1 || items[0].UserID != 61 {
		t.Fatalf("unexpected scoped revoked tokens: total=%d items=%+v", total, items)
	}

	secret := "test-secret"
	token := createTestJWTForUserSecurity(t, map[string]interface{}{
		"jti":        "jti-cross-scope",
		"user_id":    62,
		"session_id": "auth-62",
		"token_use":  "access",
		"iat":        now.Unix(),
		"exp":        expiry.Unix(),
	}, secret)
	if _, err := svc.RevokeJWT(ctx, token, secret, "cross-scope"); err == nil {
		t.Fatalf("expected scoped RevokeJWT to reject cross-scope token")
	}
}

func TestServiceListRevokedTokensAndCleanup(t *testing.T) {
	db, err := gorm.Open(sqlite.Open(uniqueMemoryDSN("usersecurity_query_cleanup")), &gorm.Config{})
	if err != nil {
		t.Fatalf("open sqlite: %v", err)
	}
	if err := db.AutoMigrate(&models.RevokedToken{}); err != nil {
		t.Fatalf("migrate: %v", err)
	}

	now := time.Now().UTC()
	expiredAt := now.Add(-1 * time.Hour)
	activeAt := now.Add(2 * time.Hour)
	if err := db.Create([]models.RevokedToken{
		{JTI: "jti-query-1", UserID: 21, SessionID: "sess-1", TokenUse: "access", ExpiresAt: &activeAt, RevokedAt: now.Add(-2 * time.Minute)},
		{JTI: "jti-query-2", UserID: 22, SessionID: "sess-2", TokenUse: "refresh", ExpiresAt: &expiredAt, RevokedAt: now.Add(-3 * time.Minute)},
	}).Error; err != nil {
		t.Fatalf("seed revoked tokens: %v", err)
	}

	svc := NewService(db, nil)
	items, total, err := svc.ListRevokedTokens(context.Background(), RevokedTokenListQuery{
		ActiveOnly: true,
		Page:       1,
		PageSize:   20,
	})
	if err != nil {
		t.Fatalf("ListRevokedTokens() error = %v", err)
	}
	if total != 1 || len(items) != 1 || items[0].JTI != "jti-query-1" {
		t.Fatalf("unexpected active revoked tokens: total=%d items=%+v", total, items)
	}

	retention := NewGormRevokedTokenRetentionService(db, 10)
	deleted, err := retention.Cleanup(context.Background(), now)
	if err != nil {
		t.Fatalf("Cleanup() error = %v", err)
	}
	if deleted != 1 {
		t.Fatalf("deleted = %d want 1", deleted)
	}
}

func createTestJWTForUserSecurity(t *testing.T, payload map[string]interface{}, secret string) string {
	t.Helper()
	header := map[string]string{"alg": "HS256", "typ": "JWT"}
	headerJSON, err := json.Marshal(header)
	if err != nil {
		t.Fatalf("marshal header: %v", err)
	}
	payloadJSON, err := json.Marshal(payload)
	if err != nil {
		t.Fatalf("marshal payload: %v", err)
	}

	enc := func(b []byte) string { return base64.RawURLEncoding.EncodeToString(b) }
	unsigned := enc(headerJSON) + "." + enc(payloadJSON)

	mac := hmac.New(sha256.New, []byte(secret))
	mac.Write([]byte(unsigned))
	return unsigned + "." + enc(mac.Sum(nil))
}
