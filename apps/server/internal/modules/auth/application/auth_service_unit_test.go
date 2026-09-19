package application

import (
	"context"
	"encoding/json"
	"strings"
	"testing"
	"time"

	"servify/apps/server/internal/models"

	"golang.org/x/crypto/bcrypt"
	"gorm.io/gorm"
)

func TestAuthService_Register(t *testing.T) {
	db := newAuthServiceTestDB(t)
	svc := NewService(db, testAuthConfig())
	ctx := context.Background()

	result, err := svc.Register(ctx, RegisterInput{
		Username: "newuser", Email: "new@x.com", Password: "pw123456", Name: "New",
	}, AuthSessionMetadata{DeviceFingerprint: strings.Repeat("f", 200), UserAgent: strings.Repeat("u", 600), ClientIP: "1.2.3.4"})
	if err != nil {
		t.Fatalf("Register: %v", err)
	}
	if result.SessionID == "" || result.Token == "" || result.RefreshToken == "" {
		t.Fatalf("unexpected result: %+v", result)
	}
	if result.User.Role != "customer" {
		t.Fatalf("expected customer default role, got %q", result.User.Role)
	}

	// duplicate username/email
	if _, err := svc.Register(ctx, RegisterInput{Username: "newuser", Email: "other@x.com", Password: "pw"}, AuthSessionMetadata{}); err != ErrAuthUserAlreadyExists {
		t.Fatalf("expected duplicate error, got %v", err)
	}
	if _, err := svc.Register(ctx, RegisterInput{Username: "other", Email: "new@x.com", Password: "pw"}, AuthSessionMetadata{}); err != ErrAuthUserAlreadyExists {
		t.Fatalf("expected duplicate email error, got %v", err)
	}

	// empty inputs
	if _, err := svc.Register(ctx, RegisterInput{Username: " ", Email: "e@x.com", Password: "p"}, AuthSessionMetadata{}); err != ErrInvalidAuthInput {
		t.Fatalf("expected invalid input, got %v", err)
	}
	if _, err := svc.Register(ctx, RegisterInput{Username: "u2", Email: "", Password: "p"}, AuthSessionMetadata{}); err != ErrInvalidAuthInput {
		t.Fatalf("expected invalid input, got %v", err)
	}
	if _, err := svc.Register(ctx, RegisterInput{Username: "u3", Email: "u3@x.com", Password: ""}, AuthSessionMetadata{}); err != ErrInvalidAuthInput {
		t.Fatalf("expected invalid input, got %v", err)
	}

	// subsequent admin request is downgraded to customer because users exist
	second, err := svc.Register(ctx, RegisterInput{
		Username: "second-admin", Email: "admin2@x.com", Password: "pw123456", Role: "admin",
	}, AuthSessionMetadata{})
	if err != nil {
		t.Fatalf("Register second admin: %v", err)
	}
	if second.User.Role != "customer" {
		t.Fatalf("expected downgrade to customer, got %q", second.User.Role)
	}

	// nil service / nil db guard
	var nilSvc *AuthService
	if _, err := nilSvc.Register(ctx, RegisterInput{}, AuthSessionMetadata{}); err != gorm.ErrInvalidDB {
		t.Fatalf("expected ErrInvalidDB, got %v", err)
	}
}

func TestAuthService_LoginValidation(t *testing.T) {
	db := newAuthServiceTestDB(t)
	svc := NewService(db, testAuthConfig())
	ctx := context.Background()

	hash, hashErr := bcrypt.GenerateFromPassword([]byte("password123"), bcrypt.MinCost)
	if hashErr != nil {
		t.Fatalf("hash: %v", hashErr)
	}
	hashed := string(hash)
	if err := db.Create(&models.User{
		ID: 31, Username: "active-u", Email: "active@x.com", Password: hashed, Status: "active",
	}).Error; err != nil {
		t.Fatalf("seed active: %v", err)
	}
	if err := db.Create(&models.User{
		ID: 32, Username: "banned-u", Email: "banned@x.com", Password: hashed, Status: "banned",
	}).Error; err != nil {
		t.Fatalf("seed banned: %v", err)
	}

	if _, err := svc.Login(ctx, LoginInput{Username: "ghost", Password: "x"}, AuthSessionMetadata{}); err != ErrAuthInvalidCredentials {
		t.Fatalf("expected invalid credentials, got %v", err)
	}
	if _, err := svc.Login(ctx, LoginInput{Username: "banned-u", Password: "password123"}, AuthSessionMetadata{}); err != ErrAuthUserDisabled {
		t.Fatalf("expected disabled error, got %v", err)
	}
	if _, err := svc.Login(ctx, LoginInput{Username: "active-u", Password: "wrong"}, AuthSessionMetadata{}); err != ErrAuthInvalidCredentials {
		t.Fatalf("expected bad password error, got %v", err)
	}
	// login by email
	if _, err := svc.Login(ctx, LoginInput{Username: "active@x.com", Password: "password123"}, AuthSessionMetadata{}); err != nil {
		t.Fatalf("login by email: %v", err)
	}

	var nilSvc *AuthService
	if _, err := nilSvc.Login(ctx, LoginInput{}, AuthSessionMetadata{}); err != gorm.ErrInvalidDB {
		t.Fatalf("expected ErrInvalidDB, got %v", err)
	}
}

func TestAuthService_GetCurrentUserAndGuards(t *testing.T) {
	db := newAuthServiceTestDB(t)
	svc := NewService(db, testAuthConfig())
	ctx := context.Background()

	if err := db.Create(&models.User{ID: 41, Username: "u41", Email: "u41@x.com", Password: "x", Status: "active"}).Error; err != nil {
		t.Fatalf("seed: %v", err)
	}
	user, err := svc.GetCurrentUser(ctx, 41)
	if err != nil || user.ID != 41 {
		t.Fatalf("GetCurrentUser: %v %+v", err, user)
	}
	if _, err := svc.GetCurrentUser(ctx, 999); err == nil {
		t.Fatal("expected not found error")
	}
	var nilSvc *AuthService
	if _, err := nilSvc.GetCurrentUser(ctx, 1); err != gorm.ErrInvalidDB {
		t.Fatalf("expected ErrInvalidDB, got %v", err)
	}

	if _, err := svc.ListAuthSessions(ctx, 0); err == nil {
		t.Fatal("expected user_id required error")
	}
	if _, err := nilSvc.ListAuthSessions(ctx, 1); err != gorm.ErrInvalidDB {
		t.Fatalf("expected ErrInvalidDB, got %v", err)
	}
	if _, err := nilSvc.RevokeCurrentSession(ctx, 1, "s"); err != gorm.ErrInvalidDB {
		t.Fatalf("expected ErrInvalidDB, got %v", err)
	}
	if _, err := nilSvc.RevokeOtherSessions(ctx, 1, "s"); err != gorm.ErrInvalidDB {
		t.Fatalf("expected ErrInvalidDB, got %v", err)
	}
	if _, err := nilSvc.RefreshToken(ctx, "x", AuthSessionMetadata{}); err != gorm.ErrInvalidDB {
		t.Fatalf("expected ErrInvalidDB, got %v", err)
	}
}

func TestAuthService_RevokeValidation(t *testing.T) {
	db := newAuthServiceTestDB(t)
	svc := NewService(db, testAuthConfig())
	ctx := context.Background()

	if _, err := svc.RevokeCurrentSession(ctx, 0, "s"); err == nil {
		t.Fatal("expected user_id required error")
	}
	if _, err := svc.RevokeCurrentSession(ctx, 1, " "); err == nil {
		t.Fatal("expected session_id required error")
	}
	if _, err := svc.RevokeCurrentSession(ctx, 1, "ghost"); err == nil {
		t.Fatal("expected session not found error")
	}

	if _, err := svc.RevokeOtherSessions(ctx, 0, "s"); err == nil {
		t.Fatal("expected user_id required error")
	}
	if _, err := svc.RevokeOtherSessions(ctx, 1, ""); err == nil {
		t.Fatal("expected current session required error")
	}
	count, err := svc.RevokeOtherSessions(ctx, 1, "s")
	if err != nil || count != 0 {
		t.Fatalf("RevokeOtherSessions noop: %v %d", err, count)
	}
}

func TestAuthService_RefreshToken_ErrorPaths(t *testing.T) {
	db := newAuthServiceTestDB(t)
	cfg := testAuthConfig()
	svc := NewService(db, cfg)
	ctx := context.Background()

	secret := cfg.JWT.Secret
	makeToken := func(claims map[string]interface{}) string {
		tok, err := createHS256JWT(claims, secret)
		if err != nil {
			t.Fatalf("create token: %v", err)
		}
		return tok
	}

	if _, err := svc.RefreshToken(ctx, "  ", AuthSessionMetadata{}); err != ErrAuthInvalidRefreshToken {
		t.Fatalf("expected invalid refresh token, got %v", err)
	}
	if _, err := svc.RefreshToken(ctx, "garbage.token.sig", AuthSessionMetadata{}); err != ErrAuthInvalidRefreshToken {
		t.Fatalf("expected invalid jwt error, got %v", err)
	}

	// access token instead of refresh
	access := makeToken(map[string]interface{}{"token_use": "access", "user_id": float64(1), "session_id": "s", "session_token_version": float64(0), "token_version": float64(0), "iat": float64(time.Now().Unix())})
	if _, err := svc.RefreshToken(ctx, access, AuthSessionMetadata{}); err != ErrAuthInvalidRefreshToken {
		t.Fatalf("expected token_use error, got %v", err)
	}

	// missing user id
	noUser := makeToken(map[string]interface{}{"token_use": "refresh", "session_id": "s", "session_token_version": float64(0)})
	if _, err := svc.RefreshToken(ctx, noUser, AuthSessionMetadata{}); err != ErrAuthInvalidRefreshToken {
		t.Fatalf("expected user id error, got %v", err)
	}
	zeroUser := makeToken(map[string]interface{}{"token_use": "refresh", "user_id": float64(0), "session_id": "s", "session_token_version": float64(0)})
	if _, err := svc.RefreshToken(ctx, zeroUser, AuthSessionMetadata{}); err != ErrAuthInvalidRefreshToken {
		t.Fatalf("expected zero user error, got %v", err)
	}

	// missing session id
	noSession := makeToken(map[string]interface{}{"token_use": "refresh", "user_id": float64(51), "session_token_version": float64(0)})
	if _, err := svc.RefreshToken(ctx, noSession, AuthSessionMetadata{}); err != ErrAuthInvalidRefreshToken {
		t.Fatalf("expected session id error, got %v", err)
	}
	blankSession := makeToken(map[string]interface{}{"token_use": "refresh", "user_id": float64(51), "session_id": "  ", "session_token_version": float64(0)})
	if _, err := svc.RefreshToken(ctx, blankSession, AuthSessionMetadata{}); err != ErrAuthInvalidRefreshToken {
		t.Fatalf("expected blank session error, got %v", err)
	}

	// missing session token version
	noVersion := makeToken(map[string]interface{}{"token_use": "refresh", "user_id": float64(51), "session_id": "s"})
	if _, err := svc.RefreshToken(ctx, noVersion, AuthSessionMetadata{}); err != ErrAuthInvalidRefreshToken {
		t.Fatalf("expected session version error, got %v", err)
	}

	base := map[string]interface{}{
		"token_use": "refresh", "user_id": float64(51), "session_id": "sess-51",
		"session_token_version": float64(0), "token_version": float64(2),
		"iat": float64(time.Now().Unix()),
	}

	// unknown user
	if _, err := svc.RefreshToken(ctx, makeToken(base), AuthSessionMetadata{}); err != ErrAuthInvalidRefreshToken {
		t.Fatalf("expected unknown user error, got %v", err)
	}

	// seed user + session scenarios
	if err := db.Create(&models.User{
		ID: 51, Username: "u51", Email: "u51@x.com", Password: "x", Status: "active", TokenVersion: 2,
	}).Error; err != nil {
		t.Fatalf("seed user: %v", err)
	}
	// stale token version
	stale := cloneClaims(base)
	stale["token_version"] = float64(1)
	if _, err := svc.RefreshToken(ctx, makeToken(stale), AuthSessionMetadata{}); err != ErrAuthInvalidRefreshToken {
		t.Fatalf("expected stale token error, got %v", err)
	}

	// token issued before TokenValidAfter
	past := time.Now().Add(-time.Hour)
	if err := db.Model(&models.User{}).Where("id = ?", 51).Update("token_valid_after", time.Now().Add(time.Minute)).Error; err != nil {
		t.Fatalf("set token_valid_after: %v", err)
	}
	if _, err := svc.RefreshToken(ctx, makeToken(cloneClaims(base)), AuthSessionMetadata{}); err != ErrAuthInvalidRefreshToken {
		t.Fatalf("expected token_valid_after error, got %v", err)
	}
	if err := db.Model(&models.User{}).Where("id = ?", 51).Update("token_valid_after", nil).Error; err != nil {
		t.Fatalf("clear token_valid_after: %v", err)
	}
	_ = past

	// missing session row
	if _, err := svc.RefreshToken(ctx, makeToken(cloneClaims(base)), AuthSessionMetadata{}); err != ErrAuthInvalidRefreshToken {
		t.Fatalf("expected missing session error, got %v", err)
	}

	// revoked session
	if err := db.Create(&models.UserAuthSession{ID: "sess-51", UserID: 51, Status: "revoked", TokenVersion: 0}).Error; err != nil {
		t.Fatalf("seed session: %v", err)
	}
	if _, err := svc.RefreshToken(ctx, makeToken(cloneClaims(base)), AuthSessionMetadata{}); err != ErrAuthInvalidRefreshToken {
		t.Fatalf("expected revoked session error, got %v", err)
	}

	// session token version mismatch
	if err := db.Model(&models.UserAuthSession{}).Where("id = ?", "sess-51").Updates(map[string]interface{}{"status": "active", "token_version": 5}).Error; err != nil {
		t.Fatalf("reset session: %v", err)
	}
	if _, err := svc.RefreshToken(ctx, makeToken(cloneClaims(base)), AuthSessionMetadata{}); err != ErrAuthInvalidRefreshToken {
		t.Fatalf("expected version mismatch error, got %v", err)
	}

	// happy path rotation
	if err := db.Model(&models.UserAuthSession{}).Where("id = ?", "sess-51").Update("token_version", 0).Error; err != nil {
		t.Fatalf("reset version: %v", err)
	}
	result, err := svc.RefreshToken(ctx, makeToken(cloneClaims(base)), AuthSessionMetadata{})
	if err != nil {
		t.Fatalf("RefreshToken: %v", err)
	}
	if result.SessionID != "sess-51" {
		t.Fatalf("unexpected session: %q", result.SessionID)
	}

	// disabled user
	if err := db.Model(&models.User{}).Where("id = ?", 51).Update("status", "disabled").Error; err != nil {
		t.Fatalf("disable user: %v", err)
	}
	if _, err := svc.RefreshToken(ctx, makeToken(cloneClaims(base)), AuthSessionMetadata{}); err != ErrAuthUserDisabled {
		t.Fatalf("expected disabled error, got %v", err)
	}
}

func cloneClaims(m map[string]interface{}) map[string]interface{} {
	out := make(map[string]interface{}, len(m))
	for k, v := range m {
		out[k] = v
	}
	return out
}

func TestAuthService_BuildersAndHelpers(t *testing.T) {
	svc := &AuthService{}
	if _, err := svc.buildAuthResult(context.Background(), nil, nil); err == nil {
		t.Fatal("expected nil user error")
	}
	if _, err := svc.buildAuthResult(context.Background(), &models.User{}, nil); err == nil {
		t.Fatal("expected nil session error")
	}
	if svc.refreshExpiresIn() != 7*24*time.Hour {
		t.Fatal("expected default refresh expiry")
	}

	// json.Number claim parsing
	num := json.Number("42")
	if v, ok := authNumericClaim(map[string]interface{}{"n": num}, "n"); !ok || v != 42 {
		t.Fatalf("json.Number claim: %v %v", v, ok)
	}
	badNum := json.Number("oops")
	if _, ok := authNumericClaim(map[string]interface{}{"n": badNum}, "n"); ok {
		t.Fatal("expected parse failure to continue search")
	}
	if v, ok := authNumericClaim(map[string]interface{}{"f": 1.5}, "f"); !ok || v != 1 {
		t.Fatalf("float claim: %v %v", v, ok)
	}
	if _, ok := authNumericClaim(map[string]interface{}{"x": "str"}, "x"); ok {
		t.Fatal("string claim should not parse")
	}
	if _, ok := authNumericClaim(map[string]interface{}{"x": nil}, "x"); ok {
		t.Fatal("nil claim should not parse")
	}

	if v, ok := authStringClaim(map[string]interface{}{"s": " val "}, "s"); !ok || v != "val" {
		t.Fatalf("string claim: %q %v", v, ok)
	}
	if _, ok := authStringClaim(map[string]interface{}{"s": "  "}, "s"); ok {
		t.Fatal("blank claim should not parse")
	}
	if _, ok := authStringClaim(map[string]interface{}{"s": 42}, "s"); ok {
		t.Fatal("non-string claim should not parse")
	}
	if _, ok := authStringClaim(map[string]interface{}{"s": nil}, "s"); ok {
		t.Fatal("nil claim should not parse")
	}

	if got := normalizeAuthSessionField("  padded  ", 6); got != "padded" {
		t.Fatalf("normalize: %q", got)
	}
	if got := normalizeAuthSessionField("longvalue", 4); got != "long" {
		t.Fatalf("truncate: %q", got)
	}

	if id := newAuthSessionID(); !strings.HasPrefix(id, "auth_") {
		t.Fatalf("session id: %q", id)
	}
	if id := newAuthTokenID(); !strings.HasPrefix(id, "jti_") {
		t.Fatalf("token id: %q", id)
	}

	token, err := createHS256JWT(map[string]interface{}{"k": "v"}, "secret")
	if err != nil || strings.Count(token, ".") != 2 {
		t.Fatalf("createHS256JWT: %v %q", err, token)
	}
}

func TestAuthService_RegisterFirstAdmin(t *testing.T) {
	db := newServicesTestDB(t, &models.User{}, &models.UserAuthSession{})
	svc := NewService(db, testAuthConfig())

	adminResult, err := svc.Register(context.Background(), RegisterInput{
		Username: "root", Email: "root@x.com", Password: "pw123456", Role: "admin",
	}, AuthSessionMetadata{})
	if err != nil {
		t.Fatalf("Register admin: %v", err)
	}
	if adminResult.User.Role != "admin" {
		t.Fatalf("expected admin for empty table, got %q", adminResult.User.Role)
	}
}
