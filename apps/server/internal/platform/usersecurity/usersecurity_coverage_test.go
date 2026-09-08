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

func TestScopedUserIDsPluckErrorAndScopeFilter(t *testing.T) {
	db := openScopeTestDB(t, &models.Agent{})
	if err := db.Migrator().DropTable(&models.Agent{}); err != nil {
		t.Fatalf("drop agents: %v", err)
	}
	ctx := platformauth.ContextWithScope(context.Background(), "tenant-x", "ws-x")
	if _, err := scopedUserIDs(ctx, db, []uint{1}); err == nil {
		t.Fatal("expected pluck error with missing agents table")
	}
	if err := ensureScopedUserAccess(ctx, db, 1); err == nil {
		t.Fatal("expected ensureScopedUserAccess to propagate pluck error")
	}

	// tenant-only scope (workspace empty)
	db2 := openScopeTestDB(t, &models.Agent{}, &models.Customer{})
	if err := db2.Create(&models.Agent{UserID: 5, TenantID: "tenant-only"}).Error; err != nil {
		t.Fatalf("seed agent: %v", err)
	}
	tenantCtx := platformauth.ContextWithScope(context.Background(), "tenant-only", "")
	allowed, err := scopedUserIDs(tenantCtx, db2, []uint{5})
	if err != nil {
		t.Fatalf("scopedUserIDs() error = %v", err)
	}
	if _, ok := allowed[5]; !ok {
		t.Fatalf("expected user 5 allowed via tenant scope, got %v", allowed)
	}
	if err := ensureScopedUserAccess(tenantCtx, db2, 5); err != nil {
		t.Fatalf("tenant-scoped access = %v", err)
	}
	if err := ensureScopedUserAccess(tenantCtx, db2, 6); err == nil {
		t.Fatal("expected ErrRecordNotFound for out-of-scope user")
	}

	// nil db and empty user list
	if _, err := scopedUserIDs(context.Background(), nil, []uint{1}); !errors.Is(err, gorm.ErrInvalidDB) {
		t.Fatalf("nil db = %v", err)
	}
	if allowed, err := scopedUserIDs(context.Background(), db2, nil); err != nil || len(allowed) != 0 {
		t.Fatalf("empty list = (%v, %v)", allowed, err)
	}
}

func TestOrderedUniqueUserIDsRejectsZero(t *testing.T) {
	if _, err := orderedUniqueUserIDs([]uint{1, 0}); err == nil {
		t.Fatal("expected zero user id rejected")
	}
	if ids, err := orderedUniqueUserIDs([]uint{}); err != nil || len(ids) != 0 {
		t.Fatalf("empty input = (%v, %v)", ids, err)
	}
	ids, err := orderedUniqueUserIDs([]uint{3, 1, 3, 2})
	if err != nil || len(ids) != 3 || ids[0] != 3 || ids[1] != 1 || ids[2] != 2 {
		t.Fatalf("dedup order = (%v, %v)", ids, err)
	}
}

func TestServiceGetUsersBranches(t *testing.T) {
	db := openScopeTestDB(t, &models.User{}, &models.Agent{}, &models.Customer{})
	if err := db.Create([]models.User{
		{ID: 71, Username: "u71", Email: "u71@example.com", Status: "active"},
		{ID: 72, Username: "u72", Email: "u72@example.com", Status: "active"},
	}).Error; err != nil {
		t.Fatalf("seed users: %v", err)
	}
	svc := NewService(db, nil)

	if users, err := svc.GetUsers(context.Background(), []uint{}); err != nil || len(users) != 0 {
		t.Fatalf("empty GetUsers = (%v, %v)", users, err)
	}

	if _, err := svc.GetUsers(context.Background(), []uint{71, 0}); err == nil {
		t.Fatal("expected zero user id rejected")
	}

	users, err := svc.GetUsers(context.Background(), []uint{72, 71, 72})
	if err != nil {
		t.Fatalf("GetUsers() error = %v", err)
	}
	if len(users) != 3 || users[0].ID != 72 || users[1].ID != 71 || users[2].ID != 72 {
		t.Fatalf("unexpected order: %v", []uint{users[0].ID, users[1].ID, users[2].ID})
	}

	if _, err := svc.GetUsers(context.Background(), []uint{71, 999}); err == nil {
		t.Fatal("expected missing user error")
	}

	if user, err := svc.GetUser(context.Background(), 999); err == nil || user != nil {
		t.Fatalf("GetUser(missing) = (%v, %v)", user, err)
	}
}

func TestServiceRevokeSessionBranches(t *testing.T) {
	db := openScopeTestDB(t, &models.UserAuthSession{})
	if err := db.AutoMigrate(&models.User{}); err != nil {
		t.Fatalf("migrate users: %v", err)
	}
	if err := db.Create(&models.User{ID: 81, Username: "u81", Email: "u81@example.com", Status: "active"}).Error; err != nil {
		t.Fatalf("seed user: %v", err)
	}
	svc := NewService(db, nil)

	if _, err := svc.RevokeSession(context.Background(), 81, "  "); err == nil {
		t.Fatal("expected empty session id rejected")
	}
	if _, err := svc.RevokeSession(context.Background(), 81, "missing"); err == nil {
		t.Fatal("expected missing session error")
	}

	if err := db.Create(&models.UserAuthSession{ID: "sess-81", UserID: 81, Status: "active"}).Error; err != nil {
		t.Fatalf("seed session: %v", err)
	}
	session, err := svc.RevokeSession(context.Background(), 81, "sess-81")
	if err != nil {
		t.Fatalf("RevokeSession() error = %v", err)
	}
	if session.Status != "revoked" {
		t.Fatalf("session status = %q", session.Status)
	}
}

func TestServiceRevokeAllSessionsBranches(t *testing.T) {
	db := openScopeTestDB(t, &models.UserAuthSession{})
	if err := db.AutoMigrate(&models.User{}); err != nil {
		t.Fatalf("migrate users: %v", err)
	}
	if err := db.Create(&models.User{ID: 91, Username: "u91", Email: "u91@example.com", Status: "active"}).Error; err != nil {
		t.Fatalf("seed user: %v", err)
	}
	svc := NewService(db, nil)

	empty, err := svc.RevokeAllSessions(context.Background(), 91, "")
	if err != nil {
		t.Fatalf("RevokeAllSessions(empty) error = %v", err)
	}
	if empty.Count != 0 {
		t.Fatalf("expected zero count, got %d", empty.Count)
	}

	if err := db.Create([]models.UserAuthSession{
		{ID: "s1", UserID: 91, Status: "active"},
		{ID: "s2", UserID: 91, Status: "active"},
	}).Error; err != nil {
		t.Fatalf("seed sessions: %v", err)
	}
	result, err := svc.RevokeAllSessions(context.Background(), 91, "s2")
	if err != nil {
		t.Fatalf("RevokeAllSessions() error = %v", err)
	}
	if result.Count != 1 {
		t.Fatalf("expected 1 revoked session, got %d", result.Count)
	}
	var remaining models.UserAuthSession
	if err := db.First(&remaining, "id = ?", "s2").Error; err != nil {
		t.Fatalf("load s2: %v", err)
	}
	if remaining.Status != "active" {
		t.Fatalf("excepted session should stay active, got %q", remaining.Status)
	}
}

func TestServiceRevokeJWTValidation(t *testing.T) {
	db := openScopeTestDB(t, &models.RevokedToken{})
	svc := NewService(db, nil)
	if _, err := svc.RevokeJWT(context.Background(), "  ", "secret", ""); err == nil {
		t.Fatal("expected empty token rejected")
	}
	if _, err := svc.RevokeJWT(context.Background(), "token", "  ", ""); err == nil {
		t.Fatal("expected empty secret rejected")
	}
	if _, err := svc.RevokeJWT(context.Background(), "bad-token", "secret", ""); err == nil {
		t.Fatal("expected invalid token rejected")
	}

	now := time.Now().UTC()
	secret := "test-secret"
	missingJTI := createTestJWTForUserSecurity(t, map[string]interface{}{
		"iat": now.Unix(),
		"exp": now.Add(time.Minute).Unix(),
	}, secret)
	if _, err := svc.RevokeJWT(context.Background(), missingJTI, secret, ""); err == nil {
		t.Fatal("expected missing jti rejected")
	}

	noExp := createTestJWTForUserSecurity(t, map[string]interface{}{
		"jti": "jti-no-exp",
		"iat": now.Unix(),
	}, secret)
	result, err := svc.RevokeJWT(context.Background(), noExp, secret, "")
	if err != nil {
		t.Fatalf("RevokeJWT(no exp) error = %v", err)
	}
	if result.ExpiresAt != nil {
		t.Fatalf("expected nil expiry, got %v", result.ExpiresAt)
	}
	if result.Reason != "" {
		t.Fatalf("expected empty reason, got %q", result.Reason)
	}
}

func TestRevokeUserTokensDBFailure(t *testing.T) {
	db := openScopeTestDB(t, &models.User{})
	if err := db.Migrator().DropTable(&models.User{}); err != nil {
		t.Fatalf("drop users: %v", err)
	}
	if _, err := RevokeUserTokens(context.Background(), db, 5, time.Now()); err == nil {
		t.Fatal("expected db failure on update")
	}

	db2 := openScopeTestDB(t, &models.User{})
	if err := db2.Migrator().DropColumn(&models.User{}, "token_version"); err != nil {
		t.Fatalf("drop column: %v", err)
	}
}

func TestRetentionCleanupBatchingAndErrors(t *testing.T) {
	db := openScopeTestDB(t, &models.RevokedToken{})
	now := time.Now().UTC()
	var rows []models.RevokedToken
	for i := 0; i < 5; i++ {
		rows = append(rows, models.RevokedToken{JTI: fmt.Sprintf("ret-%d", i), ExpiresAt: &[]time.Time{now.Add(-time.Duration(48-i) * time.Hour)}[0], RevokedAt: now})
	}
	if err := db.Create(&rows).Error; err != nil {
		t.Fatalf("seed: %v", err)
	}
	svc := NewGormRevokedTokenRetentionService(db, 2)
	deleted, err := svc.Cleanup(context.Background(), now)
	if err != nil {
		t.Fatalf("Cleanup() error = %v", err)
	}
	if deleted != 5 {
		t.Fatalf("deleted = %d want 5", deleted)
	}

	db2 := openScopeTestDB(t, &models.RevokedToken{})
	if err := db2.Migrator().DropTable(&models.RevokedToken{}); err != nil {
		t.Fatalf("drop: %v", err)
	}
	if _, err := NewGormRevokedTokenRetentionService(db2, 1).Cleanup(context.Background(), now); err == nil {
		t.Fatal("expected pluck error on missing table")
	}
}

func TestListRevokedTokensPageSizeClampAndFilters(t *testing.T) {
	db := openScopeTestDB(t, &models.RevokedToken{})
	now := time.Now().UTC()
	future := now.Add(time.Hour)
	uid := uint(77)
	if err := db.Create([]models.RevokedToken{
		{JTI: "q-1", UserID: uid, SessionID: "sess-1", TokenUse: "access", ExpiresAt: &future, RevokedAt: now},
		{JTI: "q-2", UserID: uid, SessionID: "sess-2", TokenUse: "refresh", RevokedAt: now},
	}).Error; err != nil {
		t.Fatalf("seed: %v", err)
	}
	svc := NewService(db, nil)
	items, total, err := svc.ListRevokedTokens(context.Background(), RevokedTokenListQuery{
		JTI:        "q-1",
		UserID:     &uid,
		SessionID:  "sess-1",
		TokenUse:   "access",
		ActiveOnly: true,
		Page:       1,
		PageSize:   500,
	})
	if err != nil {
		t.Fatalf("ListRevokedTokens() error = %v", err)
	}
	if total != 1 || len(items) != 1 || items[0].JTI != "q-1" {
		t.Fatalf("filtered items = %+v total=%d", items, total)
	}
}

func TestRetentionCleanupBranches(t *testing.T) {
	db := openScopeTestDB(t, &models.RevokedToken{})
	svc := NewGormRevokedTokenRetentionService(db, 5)
	deleted, err := svc.Cleanup(context.Background(), time.Now().UTC())
	if err != nil || deleted != 0 {
		t.Fatalf("empty cleanup = (%d, %v)", deleted, err)
	}

	// canceled ctx aborts after a full batch
	now := time.Now().UTC()
	past := now.Add(-time.Hour)
	if err := db.Create(&models.RevokedToken{JTI: "ctx-1", ExpiresAt: &past, RevokedAt: now}).Error; err != nil {
		t.Fatalf("seed: %v", err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if _, err := NewGormRevokedTokenRetentionService(db, 1).Cleanup(ctx, now); err == nil {
		t.Fatal("expected ctx error")
	}

	// delete failure propagates
	db2 := openScopeTestDB(t, &models.RevokedToken{})
	if err := db2.Create(&models.RevokedToken{JTI: "del-1", ExpiresAt: &past, RevokedAt: now}).Error; err != nil {
		t.Fatalf("seed: %v", err)
	}
	_ = db2.Callback().Delete().Replace("gorm:delete", func(tx *gorm.DB) {
		tx.Error = errors.New("delete failed")
	})
	if _, err := NewGormRevokedTokenRetentionService(db2, 5).Cleanup(context.Background(), now); err == nil {
		t.Fatal("expected delete error")
	}
}

func TestBatchRevokeTokensScopedFailures(t *testing.T) {
	db := openScopeTestDB(t, &models.User{}, &models.Agent{}, &models.Customer{})
	if err := db.Create(&models.User{ID: 101, Username: "u101", Email: "u101@example.com", Status: "active"}).Error; err != nil {
		t.Fatalf("seed user: %v", err)
	}
	if err := db.Create(&models.Agent{UserID: 101, TenantID: "tenant-z"}).Error; err != nil {
		t.Fatalf("seed agent: %v", err)
	}
	svc := NewService(db, nil)
	scopedCtx := platformauth.ContextWithScope(context.Background(), "tenant-z", "")

	if _, err := svc.BatchRevokeTokens(scopedCtx, []uint{101, 202}); err == nil {
		t.Fatal("expected scoped batch revoke missing user error")
	}

	// pluck failure inside scope resolution
	db2 := openScopeTestDB(t, &models.User{}, &models.Agent{}, &models.Customer{})
	if err := db2.Migrator().DropTable(&models.Customer{}); err != nil {
		t.Fatalf("drop customers: %v", err)
	}
	svc2 := NewService(db2, nil)
	if _, err := svc2.BatchRevokeTokens(scopedCtx, []uint{101}); err == nil {
		t.Fatal("expected pluck failure")
	}
	if _, err := svc2.GetUsers(scopedCtx, []uint{101}); err == nil {
		t.Fatal("expected GetUsers pluck failure")
	}
	if _, err := svc2.ListUserSessions(scopedCtx, 101); err == nil {
		t.Fatal("expected ListUserSessions pluck failure")
	}
}

func TestServiceRevokeSessionBlankIDAndDBErrors(t *testing.T) {
	db := openScopeTestDB(t, &models.UserAuthSession{})
	if err := db.AutoMigrate(&models.User{}); err != nil {
		t.Fatalf("migrate users: %v", err)
	}
	if err := db.Create(&models.User{ID: 111, Username: "u111", Email: "u111@example.com", Status: "active"}).Error; err != nil {
		t.Fatalf("seed user: %v", err)
	}
	svc := NewService(db, nil)

	if _, err := svc.RevokeSession(context.Background(), 111, ""); err == nil {
		t.Fatal("expected blank session id rejected")
	}

	if err := db.Migrator().DropTable(&models.UserAuthSession{}); err != nil {
		t.Fatalf("drop sessions: %v", err)
	}
	if _, err := svc.RevokeSession(context.Background(), 111, "any"); err == nil {
		t.Fatal("expected update failure on missing table")
	}
	if _, err := svc.ListUserSessions(context.Background(), 111); err == nil {
		t.Fatal("expected find failure on missing table")
	}
	if _, err := svc.RevokeAllSessions(context.Background(), 111, ""); err == nil {
		t.Fatal("expected find failure on missing table")
	}
}

func TestRevokeUserTokensLoadVersionFailure(t *testing.T) {
	db := openScopeTestDB(t, &models.User{})
	if err := db.Create(&models.User{ID: 121, Username: "u121", Email: "u121@example.com", Status: "active"}).Error; err != nil {
		t.Fatalf("seed user: %v", err)
	}
	// intercept the query callback so the follow-up First() fails
	_ = db.Callback().Query().Replace("gorm:query", func(tx *gorm.DB) {
		tx.Error = errors.New("load failed")
	})
	if _, err := RevokeUserTokens(context.Background(), db, 121, time.Now()); err == nil {
		t.Fatal("expected load token_version failure")
	}
}

func TestRevokeJWTDBFailures(t *testing.T) {
	db := openScopeTestDB(t, &models.RevokedToken{})
	now := time.Now().UTC()
	secret := "test-secret"
	token := createTestJWTForUserSecurity(t, map[string]interface{}{
		"jti": "jti-db-fail",
		"iat": now.Unix(),
		"exp": now.Add(time.Minute).Unix(),
	}, secret)

	if err := db.Migrator().DropTable(&models.RevokedToken{}); err != nil {
		t.Fatalf("drop: %v", err)
	}
	svc := NewService(db, nil)
	if _, err := svc.RevokeJWT(context.Background(), token, secret, ""); err == nil {
		t.Fatal("expected FirstOrCreate failure")
	}

	// reason update failure: table exists for create, updates callback errors
	db2 := openScopeTestDB(t, &models.RevokedToken{})
	_ = db2.Callback().Update().Replace("gorm:update", func(tx *gorm.DB) {
		tx.Error = errors.New("update failed")
	})
	svc2 := NewService(db2, nil)
	if _, err := svc2.RevokeJWT(context.Background(), token, secret, "reason"); err == nil {
		t.Fatal("expected reason update failure")
	}
}

func TestRevokeJWTScopedWithoutUserID(t *testing.T) {
	db := openScopeTestDB(t, &models.RevokedToken{})
	now := time.Now().UTC()
	secret := "test-secret"
	token := createTestJWTForUserSecurity(t, map[string]interface{}{
		"jti": "jti-scoped-no-user",
		"iat": now.Unix(),
		"exp": now.Add(time.Minute).Unix(),
	}, secret)
	svc := NewService(db, nil)
	scopedCtx := platformauth.ContextWithScope(context.Background(), "tenant-q", "")
	if _, err := svc.RevokeJWT(scopedCtx, token, secret, ""); err == nil {
		t.Fatal("expected scoped revoke without user id to fail")
	}
}
