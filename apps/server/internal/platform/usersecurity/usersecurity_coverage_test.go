package usersecurity

import (
	"context"
	"errors"
	"fmt"
	"testing"
	"time"

	"servify/apps/server/internal/models"
	platformauth "servify/apps/server/internal/platform/auth"

	"github.com/glebarez/sqlite"
	"gorm.io/gorm"
)

func openScopeTestDB(t *testing.T, models ...any) *gorm.DB {
	t.Helper()
	db, err := gorm.Open(sqlite.Open(fmt.Sprintf("file:usersecurity_cov_%s?mode=memory&cache=shared", t.Name())), &gorm.Config{})
	if err != nil {
		t.Fatalf("open sqlite: %v", err)
	}
	if err := db.AutoMigrate(models...); err != nil {
		t.Fatalf("migrate: %v", err)
	}
	return db
}

func TestIsNotFound(t *testing.T) {
	if !IsNotFound(gorm.ErrRecordNotFound) {
		t.Fatal("IsNotFound(ErrRecordNotFound) = false")
	}
	if !IsNotFound(fmt.Errorf("wrapped: %w", gorm.ErrRecordNotFound)) {
		t.Fatal("IsNotFound(wrapped ErrRecordNotFound) = false")
	}
	if IsNotFound(errors.New("other")) {
		t.Fatal("IsNotFound(other) = true")
	}
	if IsNotFound(nil) {
		t.Fatal("IsNotFound(nil) = true")
	}
}

func TestRevokeUserTokensValidation(t *testing.T) {
	if _, err := RevokeUserTokens(context.Background(), nil, 1, time.Now()); err == nil || err.Error() != "db is required" {
		t.Fatalf("nil db error = %v", err)
	}

	db := openScopeTestDB(t, &models.User{})
	if _, err := RevokeUserTokens(context.Background(), db, 0, time.Now()); err == nil || err.Error() != "user_id required" {
		t.Fatalf("zero user error = %v", err)
	}
	if _, err := RevokeUserTokens(context.Background(), db, 99, time.Now()); err == nil || err.Error() != "user not found" {
		t.Fatalf("missing user error = %v", err)
	}
}

func TestRevokeUserTokensDefaultsZeroTime(t *testing.T) {
	db := openScopeTestDB(t, &models.User{})
	if err := db.Create(&models.User{ID: 7, Username: "u7", Email: "u7@example.com", Status: "active"}).Error; err != nil {
		t.Fatalf("seed user: %v", err)
	}

	before := time.Now().UTC().Add(-time.Minute)
	version, err := RevokeUserTokens(context.Background(), db, 7, time.Time{})
	if err != nil {
		t.Fatalf("RevokeUserTokens() error = %v", err)
	}
	if version != 1 {
		t.Fatalf("version = %d, want 1", version)
	}
	var user models.User
	if err := db.First(&user, 7).Error; err != nil {
		t.Fatalf("load user: %v", err)
	}
	if user.TokenValidAfter == nil || user.TokenValidAfter.Before(before) {
		t.Fatalf("token_valid_after = %v, want ~now", user.TokenValidAfter)
	}
}

func TestServiceNilDBReturnsInvalidDB(t *testing.T) {
	svc := NewService(nil, nil)
	ctx := context.Background()
	uid := uint(1)

	if _, err := svc.RevokeTokens(ctx, uid); !errors.Is(err, gorm.ErrInvalidDB) {
		t.Fatalf("RevokeTokens() error = %v", err)
	}
	if _, err := svc.BatchRevokeTokens(ctx, []uint{uid}); !errors.Is(err, gorm.ErrInvalidDB) {
		t.Fatalf("BatchRevokeTokens() error = %v", err)
	}
	if _, err := svc.GetUser(ctx, uid); !errors.Is(err, gorm.ErrInvalidDB) {
		t.Fatalf("GetUser() error = %v", err)
	}
	if _, err := svc.GetUsers(ctx, []uint{uid}); !errors.Is(err, gorm.ErrInvalidDB) {
		t.Fatalf("GetUsers() error = %v", err)
	}
	if _, err := svc.ListUserSessions(ctx, uid); !errors.Is(err, gorm.ErrInvalidDB) {
		t.Fatalf("ListUserSessions() error = %v", err)
	}
	if _, err := svc.RevokeSession(ctx, uid, "s"); !errors.Is(err, gorm.ErrInvalidDB) {
		t.Fatalf("RevokeSession() error = %v", err)
	}
	if _, err := svc.RevokeJWT(ctx, "token", "secret", ""); !errors.Is(err, gorm.ErrInvalidDB) {
		t.Fatalf("RevokeJWT() error = %v", err)
	}
	if _, err := svc.RevokeAllSessions(ctx, uid, ""); !errors.Is(err, gorm.ErrInvalidDB) {
		t.Fatalf("RevokeAllSessions() error = %v", err)
	}
	if _, _, err := svc.ListRevokedTokens(ctx, RevokedTokenListQuery{}); !errors.Is(err, gorm.ErrInvalidDB) {
		t.Fatalf("ListRevokedTokens() error = %v", err)
	}
}

func TestBatchRevokeTokens(t *testing.T) {
	db := openScopeTestDB(t, &models.User{})
	users := []models.User{
		{ID: 31, Username: "b31", Email: "b31@example.com", Status: "active"},
		{ID: 32, Username: "b32", Email: "b32@example.com", Status: "active"},
	}
	if err := db.Create(&users).Error; err != nil {
		t.Fatalf("seed users: %v", err)
	}
	svc := NewService(db, nil)
	ctx := context.Background()

	got, err := svc.BatchRevokeTokens(ctx, nil)
	if err != nil || len(got) != 0 {
		t.Fatalf("BatchRevokeTokens(nil) = %v, %v", got, err)
	}

	got, err = svc.BatchRevokeTokens(ctx, []uint{32, 31, 32})
	if err != nil {
		t.Fatalf("BatchRevokeTokens() error = %v", err)
	}
	if len(got) != 2 || got[31] != 1 || got[32] != 2 {
		t.Fatalf("results = %+v", got)
	}

	if _, err := svc.BatchRevokeTokens(ctx, []uint{31, 0}); err == nil {
		t.Fatal("BatchRevokeTokens() expected zero-id error")
	}
	if _, err := svc.BatchRevokeTokens(ctx, []uint{31, 99}); err == nil {
		t.Fatal("BatchRevokeTokens() expected missing user error")
	}
}

func TestEnsureScopedUserAccessRejectsZeroUser(t *testing.T) {
	db := openScopeTestDB(t, &models.User{})
	svc := NewService(db, nil)
	if _, err := svc.RevokeTokens(context.Background(), 0); err == nil || err.Error() != "user_id required" {
		t.Fatalf("zero user error = %v", err)
	}
}

func TestScopedUserIDsValidation(t *testing.T) {
	if _, err := scopedUserIDs(context.Background(), nil, []uint{1}); !errors.Is(err, gorm.ErrInvalidDB) {
		t.Fatalf("nil db error = %v", err)
	}

	db := openScopeTestDB(t, &models.User{})
	got, err := scopedUserIDs(context.Background(), db, nil)
	if err != nil || len(got) != 0 {
		t.Fatalf("empty ids = %+v, %v", got, err)
	}
}

func TestHasRequestScope(t *testing.T) {
	if hasRequestScope(context.Background()) {
		t.Fatal("background ctx should not have request scope")
	}
	ctx := platformauth.ContextWithScope(context.Background(), "tenant-1", "")
	if !hasRequestScope(ctx) {
		t.Fatal("tenant ctx should have request scope")
	}
}

func TestClaimHelpers(t *testing.T) {
	payload := map[string]interface{}{
		"jti":       "  abc  ",
		"sub":       42.0,
		"empty":     "   ",
		"nullValue": nil,
		"count":     3.0,
		"exp":       1710000000.0,
		"junk":      "not-a-number",
	}

	if got, ok := stringClaim(payload, "jti"); !ok || got != "abc" {
		t.Fatalf("stringClaim(jti) = %q, %v", got, ok)
	}
	if got, ok := stringClaim(payload, "missing", "jti"); !ok || got != "abc" {
		t.Fatalf("stringClaim(fallback) = %q, %v", got, ok)
	}
	if _, ok := stringClaim(payload, "empty"); ok {
		t.Fatal("stringClaim(blank) should not match")
	}
	if _, ok := stringClaim(payload, "nullValue"); ok {
		t.Fatal("stringClaim(nil value) should not match")
	}
	if _, ok := stringClaim(payload, "sub"); ok {
		t.Fatal("stringClaim(non-string) should not match")
	}

	if got, ok := uintClaim(payload, "sub"); !ok || got != 42 {
		t.Fatalf("uintClaim(sub) = %d, %v", got, ok)
	}
	if _, ok := uintClaim(payload, "jti"); ok {
		t.Fatal("uintClaim(non-number) should not match")
	}
	if _, ok := uintClaim(payload, "nullValue"); ok {
		t.Fatal("uintClaim(nil value) should not match")
	}

	if got, ok := unixTimeClaim(payload, "exp"); !ok || got.Unix() != 1710000000 {
		t.Fatalf("unixTimeClaim(exp) = %v, %v", got, ok)
	}
	if _, ok := unixTimeClaim(payload, "junk"); ok {
		t.Fatal("unixTimeClaim(non-number) should not match")
	}
}

func TestMissingScopedUserIDs(t *testing.T) {
	missing := missingScopedUserIDs(
		[]uint{5, 3, 9},
		map[uint]struct{}{5: {}, 9: {}},
		map[uint]struct{}{5: {}},
	)
	if len(missing) != 2 || missing[0] != 3 || missing[1] != 9 {
		t.Fatalf("missing = %v, want [3 9]", missing)
	}
}

func TestGormRevokedTokenRetentionServiceConstruction(t *testing.T) {
	if NewGormRevokedTokenRetentionService(nil, 10) != nil {
		t.Fatal("nil db should construct nil service")
	}

	db := openScopeTestDB(t, &models.RevokedToken{})
	svc := NewGormRevokedTokenRetentionService(db, 0)
	if svc == nil || svc.batchSize != 500 {
		t.Fatalf("service = %+v, want default batch size 500", svc)
	}

	var nilSvc *GormRevokedTokenRetentionService
	deleted, err := nilSvc.Cleanup(context.Background(), time.Now())
	if err != nil || deleted != 0 {
		t.Fatalf("nil service Cleanup = %d, %v", deleted, err)
	}
}

func TestGormRevokedTokenRetentionServiceCleanup(t *testing.T) {
	db := openScopeTestDB(t, &models.RevokedToken{})
	past := time.Now().UTC().Add(-time.Hour)
	future := time.Now().UTC().Add(time.Hour)
	records := []models.RevokedToken{
		{JTI: "expired-1", UserID: 1, ExpiresAt: &past, RevokedAt: past},
		{JTI: "expired-2", UserID: 2, ExpiresAt: &past, RevokedAt: past},
		{JTI: "active-1", UserID: 3, ExpiresAt: &future, RevokedAt: past},
		{JTI: "no-expiry", UserID: 4, RevokedAt: past},
	}
	if err := db.Create(&records).Error; err != nil {
		t.Fatalf("seed revoked tokens: %v", err)
	}

	svc := NewGormRevokedTokenRetentionService(db, 10)
	deleted, err := svc.Cleanup(context.Background(), time.Now().UTC())
	if err != nil {
		t.Fatalf("Cleanup() error = %v", err)
	}
	if deleted != 2 {
		t.Fatalf("deleted = %d, want 2", deleted)
	}

	var remaining int64
	db.Model(&models.RevokedToken{}).Count(&remaining)
	if remaining != 2 {
		t.Fatalf("remaining = %d, want 2", remaining)
	}
}

func TestServiceListRevokedTokensFiltersAndPagination(t *testing.T) {
	db := openScopeTestDB(t, &models.User{}, &models.RevokedToken{})
	if err := db.Create(&models.User{ID: 41, Username: "q41", Email: "q41@example.com", Status: "active"}).Error; err != nil {
		t.Fatalf("seed user: %v", err)
	}

	past := time.Now().UTC().Add(-2 * time.Hour)
	future := time.Now().UTC().Add(2 * time.Hour)
	records := []models.RevokedToken{
		{JTI: "jti-a", UserID: 41, SessionID: "sess-1", TokenUse: "access", ExpiresAt: &past, RevokedAt: past},
		{JTI: "jti-b", UserID: 41, SessionID: "sess-2", TokenUse: "refresh", ExpiresAt: &past, RevokedAt: past},
		{JTI: "jti-c", UserID: 41, SessionID: "sess-1", TokenUse: "access", ExpiresAt: &future, RevokedAt: past},
	}
	if err := db.Create(&records).Error; err != nil {
		t.Fatalf("seed revoked tokens: %v", err)
	}

	svc := NewService(db, nil)
	ctx := context.Background()

	items, total, err := svc.ListRevokedTokens(ctx, RevokedTokenListQuery{})
	if err != nil {
		t.Fatalf("ListRevokedTokens() error = %v", err)
	}
	if total != 3 || len(items) != 3 {
		t.Fatalf("all = %d/%d, want 3/3", len(items), total)
	}

	items, _, err = svc.ListRevokedTokens(ctx, RevokedTokenListQuery{JTI: "jti-a"})
	if err != nil || len(items) != 1 {
		t.Fatalf("jti filter = %d items, err %v", len(items), err)
	}

	items, _, err = svc.ListRevokedTokens(ctx, RevokedTokenListQuery{SessionID: "sess-1"})
	if err != nil || len(items) != 2 {
		t.Fatalf("session filter = %d items, err %v", len(items), err)
	}

	items, _, err = svc.ListRevokedTokens(ctx, RevokedTokenListQuery{TokenUse: "refresh"})
	if err != nil || len(items) != 1 {
		t.Fatalf("token_use filter = %d items, err %v", len(items), err)
	}

	userID := uint(41)
	items, _, err = svc.ListRevokedTokens(ctx, RevokedTokenListQuery{UserID: &userID})
	if err != nil || len(items) != 3 {
		t.Fatalf("user filter = %d items, err %v", len(items), err)
	}

	items, _, err = svc.ListRevokedTokens(ctx, RevokedTokenListQuery{ActiveOnly: true})
	if err != nil || len(items) != 1 {
		t.Fatalf("active-only filter = %d items, err %v", len(items), err)
	}

	items, total, err = svc.ListRevokedTokens(ctx, RevokedTokenListQuery{Page: 2, PageSize: 2})
	if err != nil {
		t.Fatalf("pagination error = %v", err)
	}
	if total != 3 || len(items) != 1 {
		t.Fatalf("page 2 = %d items (total %d), want 1 (total 3)", len(items), total)
	}

	invalid := uint(999)
	if _, _, err = svc.ListRevokedTokens(ctx, RevokedTokenListQuery{UserID: &invalid}); err != nil {
		t.Fatalf("non-matching user filter error = %v", err)
	}
}
