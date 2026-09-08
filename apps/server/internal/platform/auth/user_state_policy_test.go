package auth

import (
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"servify/apps/server/internal/models"

	"github.com/gin-gonic/gin"
	"github.com/glebarez/sqlite"
	"gorm.io/gorm"
)

func testAuthDB(t *testing.T) *gorm.DB {
	t.Helper()
	db, err := gorm.Open(sqlite.Open("file:auth_user_state_policy?mode=memory&cache=shared"), &gorm.Config{})
	if err != nil {
		t.Fatalf("open sqlite: %v", err)
	}
	if err := db.AutoMigrate(&models.User{}, &models.RevokedToken{}); err != nil {
		t.Fatalf("migrate user/revoked token: %v", err)
	}
	return db
}

func TestUserStateTokenPolicyRejectsInactiveUser(t *testing.T) {
	now := time.Unix(1_700_000_000, 0)
	db := testAuthDB(t)
	if err := db.Create(&models.User{
		ID:       7,
		Username: "u7",
		Email:    "u7@example.com",
		Status:   "inactive",
	}).Error; err != nil {
		t.Fatalf("seed user: %v", err)
	}

	secret := "test-secret"
	token := createTestHS256JWT(t, map[string]interface{}{
		"user_id": 7,
		"iat":     now.Unix(),
		"exp":     now.Add(10 * time.Minute).Unix(),
	}, secret)

	r := gin.New()
	r.Use(AuthMiddleware(MiddlewareConfig{
		Secret: secret,
		Now:    func() time.Time { return now },
		Policy: NewUserStateTokenPolicy(db),
	}))
	r.GET("/claims", func(c *gin.Context) { c.Status(http.StatusOK) })

	w := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/claims", nil)
	req.Header.Set("Authorization", "Bearer "+token)
	r.ServeHTTP(w, req)

	if w.Code != http.StatusUnauthorized {
		t.Fatalf("expected 401 got %d body=%s", w.Code, w.Body.String())
	}
}

func TestUserStateTokenPolicyRejectsTokenIssuedBeforeUserCutoff(t *testing.T) {
	now := time.Unix(1_700_000_000, 0)
	cutoff := now.Add(-30 * time.Minute)
	db := testAuthDB(t)
	if err := db.Create(&models.User{
		ID:              8,
		Username:        "u8",
		Email:           "u8@example.com",
		Status:          "active",
		TokenValidAfter: &cutoff,
	}).Error; err != nil {
		t.Fatalf("seed user: %v", err)
	}

	secret := "test-secret"
	token := createTestHS256JWT(t, map[string]interface{}{
		"user_id": 8,
		"iat":     now.Add(-2 * time.Hour).Unix(),
		"exp":     now.Add(10 * time.Minute).Unix(),
	}, secret)

	r := gin.New()
	r.Use(AuthMiddleware(MiddlewareConfig{
		Secret: secret,
		Now:    func() time.Time { return now },
		Policy: NewUserStateTokenPolicy(db),
	}))
	r.GET("/claims", func(c *gin.Context) { c.Status(http.StatusOK) })

	w := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/claims", nil)
	req.Header.Set("Authorization", "Bearer "+token)
	r.ServeHTTP(w, req)

	if w.Code != http.StatusUnauthorized {
		t.Fatalf("expected 401 got %d body=%s", w.Code, w.Body.String())
	}
}

func TestUserStateTokenPolicyRejectsStaleTokenVersion(t *testing.T) {
	now := time.Unix(1_700_000_000, 0)
	db := testAuthDB(t)
	if err := db.Create(&models.User{
		ID:           9,
		Username:     "u9",
		Email:        "u9@example.com",
		Status:       "active",
		TokenVersion: 2,
	}).Error; err != nil {
		t.Fatalf("seed user: %v", err)
	}

	secret := "test-secret"
	token := createTestHS256JWT(t, map[string]interface{}{
		"user_id":       9,
		"iat":           now.Unix(),
		"exp":           now.Add(10 * time.Minute).Unix(),
		"token_version": 1,
	}, secret)

	r := gin.New()
	r.Use(AuthMiddleware(MiddlewareConfig{
		Secret: secret,
		Now:    func() time.Time { return now },
		Policy: NewUserStateTokenPolicy(db),
	}))
	r.GET("/claims", func(c *gin.Context) { c.Status(http.StatusOK) })

	w := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/claims", nil)
	req.Header.Set("Authorization", "Bearer "+token)
	r.ServeHTTP(w, req)

	if w.Code != http.StatusUnauthorized {
		t.Fatalf("expected 401 got %d body=%s", w.Code, w.Body.String())
	}
}

func TestUserStateTokenPolicyAllowsCurrentActiveUser(t *testing.T) {
	now := time.Unix(1_700_000_000, 0)
	cutoff := now.Add(-30 * time.Minute)
	db := testAuthDB(t)
	if err := db.Create(&models.User{
		ID:              10,
		Username:        "u10",
		Email:           "u10@example.com",
		Status:          "active",
		TokenValidAfter: &cutoff,
		TokenVersion:    2,
	}).Error; err != nil {
		t.Fatalf("seed user: %v", err)
	}

	secret := "test-secret"
	token := createTestHS256JWT(t, map[string]interface{}{
		"user_id":       10,
		"iat":           now.Unix(),
		"exp":           now.Add(10 * time.Minute).Unix(),
		"token_version": 2,
	}, secret)

	r := gin.New()
	r.Use(AuthMiddleware(MiddlewareConfig{
		Secret: secret,
		Now:    func() time.Time { return now },
		Policy: NewUserStateTokenPolicy(db),
	}))
	r.GET("/claims", func(c *gin.Context) { c.Status(http.StatusOK) })

	w := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/claims", nil)
	req.Header.Set("Authorization", "Bearer "+token)
	r.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("expected 200 got %d body=%s", w.Code, w.Body.String())
	}
}

func TestUserStateTokenPolicyRejectsRevokedSession(t *testing.T) {
	now := time.Unix(1_700_000_000, 0)
	db := testAuthDB(t)
	revokedAt := now.Add(-5 * time.Minute)
	if err := db.AutoMigrate(&models.UserAuthSession{}); err != nil {
		t.Fatalf("migrate auth session: %v", err)
	}
	if err := db.Create(&models.User{
		ID:       11,
		Username: "u11",
		Email:    "u11@example.com",
		Status:   "active",
	}).Error; err != nil {
		t.Fatalf("seed user: %v", err)
	}
	if err := db.Create(&models.UserAuthSession{
		ID:           "auth-session-1",
		UserID:       11,
		Status:       "revoked",
		TokenVersion: 2,
		RevokedAt:    &revokedAt,
	}).Error; err != nil {
		t.Fatalf("seed auth session: %v", err)
	}

	secret := "test-secret"
	token := createTestHS256JWT(t, map[string]interface{}{
		"user_id":               11,
		"session_id":            "auth-session-1",
		"session_token_version": 2,
		"iat":                   now.Unix(),
		"exp":                   now.Add(10 * time.Minute).Unix(),
	}, secret)

	r := gin.New()
	r.Use(AuthMiddleware(MiddlewareConfig{
		Secret: secret,
		Now:    func() time.Time { return now },
		Policy: NewUserStateTokenPolicy(db),
	}))
	r.GET("/claims", func(c *gin.Context) { c.Status(http.StatusOK) })

	w := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/claims", nil)
	req.Header.Set("Authorization", "Bearer "+token)
	r.ServeHTTP(w, req)

	if w.Code != http.StatusUnauthorized {
		t.Fatalf("expected 401 got %d body=%s", w.Code, w.Body.String())
	}
}

func TestRevokedTokenPolicyRejectsExplicitlyRevokedToken(t *testing.T) {
	now := time.Unix(1_700_000_000, 0)
	db := testAuthDB(t)
	exp := now.Add(10 * time.Minute)
	if err := db.Create(&models.RevokedToken{
		JTI:       "jti-123",
		UserID:    12,
		TokenUse:  "access",
		ExpiresAt: &exp,
		RevokedAt: now.Add(-1 * time.Minute),
	}).Error; err != nil {
		t.Fatalf("seed revoked token: %v", err)
	}

	secret := "test-secret"
	token := createTestHS256JWT(t, map[string]interface{}{
		"jti":     "jti-123",
		"user_id": 12,
		"iat":     now.Unix(),
		"exp":     exp.Unix(),
	}, secret)

	r := gin.New()
	r.Use(AuthMiddleware(MiddlewareConfig{
		Secret: secret,
		Now:    func() time.Time { return now },
		Policy: NewRevokedTokenPolicy(db),
	}))
	r.GET("/claims", func(c *gin.Context) { c.Status(http.StatusOK) })

	w := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/claims", nil)
	req.Header.Set("Authorization", "Bearer "+token)
	r.ServeHTTP(w, req)

	if w.Code != http.StatusUnauthorized {
		t.Fatalf("expected 401 got %d body=%s", w.Code, w.Body.String())
	}
}

func TestUserStateTokenPolicyRejectsStaleSessionTokenVersion(t *testing.T) {
	now := time.Unix(1_700_000_000, 0)
	db := testAuthDB(t)
	if err := db.AutoMigrate(&models.UserAuthSession{}); err != nil {
		t.Fatalf("migrate auth session: %v", err)
	}
	if err := db.Create(&models.User{
		ID:       12,
		Username: "u12",
		Email:    "u12@example.com",
		Status:   "active",
	}).Error; err != nil {
		t.Fatalf("seed user: %v", err)
	}
	if err := db.Create(&models.UserAuthSession{
		ID:           "auth-session-2",
		UserID:       12,
		Status:       "active",
		TokenVersion: 3,
	}).Error; err != nil {
		t.Fatalf("seed auth session: %v", err)
	}

	secret := "test-secret"
	token := createTestHS256JWT(t, map[string]interface{}{
		"user_id":               12,
		"session_id":            "auth-session-2",
		"session_token_version": 2,
		"iat":                   now.Unix(),
		"exp":                   now.Add(10 * time.Minute).Unix(),
	}, secret)

	r := gin.New()
	r.Use(AuthMiddleware(MiddlewareConfig{
		Secret: secret,
		Now:    func() time.Time { return now },
		Policy: NewUserStateTokenPolicy(db),
	}))
	r.GET("/claims", func(c *gin.Context) { c.Status(http.StatusOK) })

	w := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/claims", nil)
	req.Header.Set("Authorization", "Bearer "+token)
	r.ServeHTTP(w, req)

	if w.Code != http.StatusUnauthorized {
		t.Fatalf("expected 401 got %d body=%s", w.Code, w.Body.String())
	}
}

func TestTokenPolicyNilDB(t *testing.T) {
	if NewUserStateTokenPolicy(nil) != nil {
		t.Fatal("expected nil user state policy")
	}
	if NewRevokedTokenPolicy(nil) != nil {
		t.Fatal("expected nil revoked token policy")
	}
}

func TestUserStateTokenPolicySkipsUserlessClaims(t *testing.T) {
	db := testAuthDB(t)
	policy := NewUserStateTokenPolicy(db)
	if err := policy(map[string]interface{}{}, Claims{}, time.Now()); err != nil {
		t.Fatalf("userless claims should pass, got %v", err)
	}
	if err := policy(map[string]interface{}{}, Claims{HasUserID: true, UserID: 0}, time.Now()); err != nil {
		t.Fatalf("zero user id should pass, got %v", err)
	}
}

func TestUserStateTokenPolicySessionBranches(t *testing.T) {
	now := time.Unix(1_700_000_000, 0)
	db := testAuthDB(t)
	if err := db.AutoMigrate(&models.UserAuthSession{}); err != nil {
		t.Fatalf("migrate session: %v", err)
	}
	if err := db.Create(&models.User{ID: 21, Username: "u21", Email: "u21@example.com", Status: "active"}).Error; err != nil {
		t.Fatalf("seed user: %v", err)
	}

	policy := NewUserStateTokenPolicy(db)

	// missing session
	err := policy(map[string]interface{}{"iat": float64(now.Unix())}, Claims{HasUserID: true, UserID: 21, SessionID: "missing"}, now)
	if err == nil || err.Error() != "token session no longer exists" {
		t.Fatalf("missing session = %v", err)
	}

	// inactive session
	session := models.UserAuthSession{ID: "sess-21", UserID: 21, Status: "revoked"}
	if err := db.Create(&session).Error; err != nil {
		t.Fatalf("seed session: %v", err)
	}
	err = policy(map[string]interface{}{"iat": float64(now.Unix())}, Claims{HasUserID: true, UserID: 21, SessionID: session.ID}, now)
	if err == nil || err.Error() != "token session is not active" {
		t.Fatalf("inactive session = %v", err)
	}

	// active session without token version requirement
	db.Model(&session).Update("status", "active")
	err = policy(map[string]interface{}{"iat": float64(now.Unix())}, Claims{HasUserID: true, UserID: 21, SessionID: session.ID}, now)
	if err != nil {
		t.Fatalf("active session = %v", err)
	}

	// stale session token version
	db.Model(&session).Update("token_version", 5)
	err = policy(map[string]interface{}{"iat": float64(now.Unix()), "stv": float64(1)}, Claims{HasUserID: true, UserID: 21, SessionID: session.ID}, now)
	if err == nil || err.Error() != "token has been revoked by session policy" {
		t.Fatalf("stale session version = %v", err)
	}
	err = policy(map[string]interface{}{"iat": float64(now.Unix())}, Claims{HasUserID: true, UserID: 21, SessionID: session.ID}, now)
	if err == nil || err.Error() != "token missing session_token_version required by session policy" {
		t.Fatalf("missing session version = %v", err)
	}
	err = policy(map[string]interface{}{"iat": float64(now.Unix()), "stv": float64(7)}, Claims{HasUserID: true, UserID: 21, SessionID: session.ID}, now)
	if err != nil {
		t.Fatalf("fresh session version = %v", err)
	}
}

func TestUserStateTokenPolicyUserBranches(t *testing.T) {
	now := time.Unix(1_700_000_000, 0)
	db := testAuthDB(t)

	policy := NewUserStateTokenPolicy(db)

	// db error while loading user
	err := policy(map[string]interface{}{}, Claims{HasUserID: true, UserID: 42}, now)
	if err == nil || err.Error() != "token user no longer exists" {
		t.Fatalf("missing user = %v", err)
	}

	if err := db.Migrator().DropTable(&models.User{}); err != nil {
		t.Fatalf("drop user table: %v", err)
	}
	err = policy(map[string]interface{}{}, Claims{HasUserID: true, UserID: 42}, now)
	if err == nil || err.Error() != "failed to evaluate token state" {
		t.Fatalf("user db failure = %v", err)
	}
}

func TestRevokedTokenPolicyBranches(t *testing.T) {
	now := time.Unix(1_700_000_000, 0)
	db := testAuthDB(t)
	policy := NewRevokedTokenPolicy(db)

	if err := policy(map[string]interface{}{}, Claims{}, now); err != nil {
		t.Fatalf("token without id should pass, got %v", err)
	}
	if err := policy(map[string]interface{}{}, Claims{TokenID: "unknown"}, now); err != nil {
		t.Fatalf("unrevoked token should pass, got %v", err)
	}

	past := now.Add(-24 * time.Hour)
	revoked := models.RevokedToken{JTI: "revoked-1", ExpiresAt: &past, RevokedAt: past}
	if err := db.Create(&revoked).Error; err != nil {
		t.Fatalf("seed revoked token: %v", err)
	}
	if err := policy(map[string]interface{}{}, Claims{TokenID: "revoked-1"}, now); err != nil {
		t.Fatalf("expired revoked token (expires_at <= now) should pass, got %v", err)
	}

	future := now.Add(time.Hour)
	active := models.RevokedToken{JTI: "revoked-2", ExpiresAt: &future}
	if err := db.Create(&active).Error; err != nil {
		t.Fatalf("seed active revoked token: %v", err)
	}
	err := policy(map[string]interface{}{}, Claims{TokenID: "revoked-2"}, now)
	if err == nil || err.Error() != "token has been explicitly revoked" {
		t.Fatalf("revoked token = %v", err)
	}

	if err := db.Migrator().DropTable(&models.RevokedToken{}); err != nil {
		t.Fatalf("drop table: %v", err)
	}
	err = policy(map[string]interface{}{}, Claims{TokenID: "any"}, now)
	if err == nil || err.Error() != "failed to evaluate revoked token state" {
		t.Fatalf("revoked db failure = %v", err)
	}
}
