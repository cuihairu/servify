package application

// 端口级单测：stubRepo 字段即返回值，覆盖服务层全部分支（GORM 语义由 infra 侧
// db 型测试对账；本文件只验证服务逻辑、哨兵映射与错误文案）。文案断言与
// handlers 的字符串映射联动，改动须同步。

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"strings"
	"sync"
	"testing"
	"time"

	"servify/apps/server/internal/config"
	"servify/apps/server/internal/models"

	"github.com/pquerna/otp/totp"
	"golang.org/x/crypto/bcrypt"
)

// stubRepo 内存实现 Repository：字段即返回值，零值即零值返回。
type stubRepo struct {
	// users
	countByUsernameOrEmail   int64
	countByUsernameOrEmailFn func(ctx context.Context, username, email string) int64
	countUsersTotal          int64
	createUserErr            error
	createdUser              *models.User
	userByUsernameOrEmail    *models.User
	userByUsernameOrEmailErr error
	userByID                 *models.User
	userByIDErr              error
	userByEmailLower         *models.User
	userByEmailLowerErr      error
	getByEmailLowerFn        func(ctx context.Context, email string) (*models.User, error)
	updateUserLastLoginErr   error

	// 2FA 绑定/解绑
	enableTwoFactorErr  error
	disableTwoFactorErr error
	totpSecretSaved     string
	totpUserID          uint

	// user_auth_sessions
	session            *models.UserAuthSession
	sessionErr         error
	getAuthSessionFn   func(ctx context.Context, sessionID string, userID uint) (*models.UserAuthSession, error)
	createSessionErr   error
	createdSession     *models.UserAuthSession
	sessions           []models.UserAuthSession
	listSessionsErr    error
	revokeRows         int64
	revokeErr          error
	revokedSessionID   string
	revokeOtherRows    int64
	revokeOtherErr     error
	revokeActiveErr    error
	revokedActiveID    string
	rotateErr          error
	rotatedID          string
	rotatedUserID      uint
	rotatedFields      *AuthSessionRotation
	rotatedFingerprint string
	rotatedUA          string
	rotatedIP          string

	// user_recovery_codes
	countUnused         int64
	countUnusedErr      error
	useRecoveryRows     int64
	useRecoveryErr      error
	usedCodeHash        string
	deleteRecoveryErr   error
	deletedRecoveryUser uint
	replaceRecoveryErr  error
	replacedHashes      []string
}

var _ Repository = (*stubRepo)(nil)

func (r *stubRepo) CountUsersByUsernameOrEmail(ctx context.Context, username, email string) int64 {
	if r.countByUsernameOrEmailFn != nil {
		return r.countByUsernameOrEmailFn(ctx, username, email)
	}
	return r.countByUsernameOrEmail
}
func (r *stubRepo) CountUsers(ctx context.Context) int64 { return r.countUsersTotal }
func (r *stubRepo) CreateUser(ctx context.Context, user *models.User) error {
	r.createdUser = user
	return r.createUserErr
}
func (r *stubRepo) GetUserByUsernameOrEmail(ctx context.Context, identifier string) (*models.User, error) {
	return r.userByUsernameOrEmail, r.userByUsernameOrEmailErr
}
func (r *stubRepo) GetUserByID(ctx context.Context, id uint) (*models.User, error) {
	return r.userByID, r.userByIDErr
}
func (r *stubRepo) GetUserByEmailLower(ctx context.Context, email string) (*models.User, error) {
	if r.getByEmailLowerFn != nil {
		return r.getByEmailLowerFn(ctx, email)
	}
	return r.userByEmailLower, r.userByEmailLowerErr
}
func (r *stubRepo) UpdateUserLastLogin(ctx context.Context, user *models.User, now time.Time) error {
	return r.updateUserLastLoginErr
}
func (r *stubRepo) EnableUserTwoFactor(ctx context.Context, userID uint, secret string, now time.Time) error {
	r.totpUserID, r.totpSecretSaved = userID, secret
	return r.enableTwoFactorErr
}
func (r *stubRepo) DisableUserTwoFactor(ctx context.Context, userID uint, tokenValidAfter time.Time) error {
	return r.disableTwoFactorErr
}
func (r *stubRepo) CreateAuthSession(ctx context.Context, session *models.UserAuthSession) error {
	r.createdSession = session
	return r.createSessionErr
}
func (r *stubRepo) GetAuthSession(ctx context.Context, sessionID string, userID uint) (*models.UserAuthSession, error) {
	if r.getAuthSessionFn != nil {
		return r.getAuthSessionFn(ctx, sessionID, userID)
	}
	return r.session, r.sessionErr
}
func (r *stubRepo) ListAuthSessionsByUser(ctx context.Context, userID uint) ([]models.UserAuthSession, error) {
	return r.sessions, r.listSessionsErr
}
func (r *stubRepo) RevokeAuthSession(ctx context.Context, sessionID string, userID uint) (int64, error) {
	r.revokedSessionID = sessionID
	return r.revokeRows, r.revokeErr
}
func (r *stubRepo) RevokeOtherAuthSessions(ctx context.Context, userID uint, excludeSessionID string) (int64, error) {
	return r.revokeOtherRows, r.revokeOtherErr
}
func (r *stubRepo) RevokeActiveAuthSession(ctx context.Context, sessionID string, userID uint) error {
	r.revokedActiveID = sessionID
	return r.revokeActiveErr
}
func (r *stubRepo) RotateAuthSession(ctx context.Context, sessionID string, userID uint, fields AuthSessionRotation) error {
	r.rotatedID, r.rotatedUserID, r.rotatedFields = sessionID, userID, &fields
	r.rotatedFingerprint, r.rotatedUA, r.rotatedIP = fields.DeviceFingerprint, fields.UserAgent, fields.ClientIP
	return r.rotateErr
}
func (r *stubRepo) CountUnusedRecoveryCodes(ctx context.Context, userID uint) (int64, error) {
	return r.countUnused, r.countUnusedErr
}
func (r *stubRepo) UseRecoveryCode(ctx context.Context, userID uint, codeHash string) (int64, error) {
	r.usedCodeHash = codeHash
	return r.useRecoveryRows, r.useRecoveryErr
}
func (r *stubRepo) DeleteRecoveryCodesByUser(ctx context.Context, userID uint) error {
	r.deletedRecoveryUser = userID
	return r.deleteRecoveryErr
}
func (r *stubRepo) ReplaceRecoveryCodes(ctx context.Context, userID uint, hashes []string, createdAt time.Time) error {
	r.replacedHashes = hashes
	return r.replaceRecoveryErr
}

const testTOTPSecret = "JBSWY3DPEHPK3PXP"

func currentTOTP(t *testing.T, secret string) string {
	t.Helper()
	code, err := totp.GenerateCode(secret, time.Now())
	if err != nil {
		t.Fatalf("generate totp code: %v", err)
	}
	return code
}

func stubPassword(t *testing.T, pw string) string {
	t.Helper()
	hash, err := bcrypt.GenerateFromPassword([]byte(pw), bcrypt.MinCost)
	if err != nil {
		t.Fatalf("hash: %v", err)
	}
	return string(hash)
}

func activeUser() *models.User {
	return &models.User{ID: 7, Username: "stub-user", Email: "stub@x.com", Password: "x", Status: "active", Role: "customer"}
}

func activeSession() *models.UserAuthSession {
	return &models.UserAuthSession{ID: "sess-stub", UserID: 7, Status: "active", TokenVersion: 0}
}

func TestAuthService_RegisterStub(t *testing.T) {
	ctx := context.Background()

	var nilSvc *AuthService
	if _, err := nilSvc.Register(ctx, RegisterInput{}, AuthSessionMetadata{}); err != ErrAuthInvalidDB {
		t.Fatalf("nil service: %v", err)
	}
	if _, err := NewService(nil, testAuthConfig()).Register(ctx, RegisterInput{
		Username: "u", Email: "u@x.com", Password: "pw",
	}, AuthSessionMetadata{}); err != ErrAuthInvalidDB {
		t.Fatalf("nil repo: %v", err)
	}

	// 空白输入
	for _, input := range []RegisterInput{
		{Username: " ", Email: "e@x.com", Password: "p"},
		{Username: "u", Email: "", Password: "p"},
		{Username: "u", Email: "e@x.com", Password: ""},
	} {
		if _, err := NewService(&stubRepo{}, testAuthConfig()).Register(ctx, input, AuthSessionMetadata{}); err != ErrInvalidAuthInput {
			t.Fatalf("blank input %+v: %v", input, err)
		}
	}

	// 重复用户名/邮箱
	dup := &stubRepo{countByUsernameOrEmail: 1}
	if _, err := NewService(dup, testAuthConfig()).Register(ctx, RegisterInput{
		Username: "taken", Email: "taken@x.com", Password: "pw123456",
	}, AuthSessionMetadata{}); err != ErrAuthUserAlreadyExists {
		t.Fatalf("dup: %v", err)
	}

	// bcrypt 拒绝超长密码（>72 字节）
	if _, err := NewService(&stubRepo{}, testAuthConfig()).Register(ctx, RegisterInput{
		Username: "u", Email: "u@x.com", Password: strings.Repeat("x", 100),
	}, AuthSessionMetadata{}); err == nil {
		t.Fatal("expected bcrypt error for long password")
	}

	// 首个 admin 保留角色；有用户后降级 customer
	firstAdmin := &stubRepo{}
	result, err := NewService(firstAdmin, testAuthConfig()).Register(ctx, RegisterInput{
		Username: "root", Email: "root@x.com", Password: "pw123456", Role: "admin",
	}, AuthSessionMetadata{})
	if err != nil || result.User.Role != "admin" {
		t.Fatalf("first admin: %+v %v", result, err)
	}
	downgraded := &stubRepo{countUsersTotal: 3}
	if result, err := NewService(downgraded, testAuthConfig()).Register(ctx, RegisterInput{
		Username: "root2", Email: "root2@x.com", Password: "pw123456", Role: "admin",
	}, AuthSessionMetadata{}); err != nil || result.User.Role != "customer" {
		t.Fatalf("admin downgrade: %+v %v", result, err)
	}

	// CreateUser 唯一键冲突文案 → AlreadyExists；其余错误原样透传
	dupInsert := &stubRepo{createUserErr: errors.New("duplicate key value violates unique constraint")}
	if _, err := NewService(dupInsert, testAuthConfig()).Register(ctx, RegisterInput{
		Username: "u", Email: "u@x.com", Password: "pw123456",
	}, AuthSessionMetadata{}); err != ErrAuthUserAlreadyExists {
		t.Fatalf("dup insert: %v", err)
	}
	plainErr := &stubRepo{createUserErr: errBoom}
	if _, err := NewService(plainErr, testAuthConfig()).Register(ctx, RegisterInput{
		Username: "u", Email: "u@x.com", Password: "pw123456",
	}, AuthSessionMetadata{}); err != errBoom {
		t.Fatalf("plain insert error: %v", err)
	}

	// 会话创建失败
	sessErr := &stubRepo{createSessionErr: errBoom}
	if _, err := NewService(sessErr, testAuthConfig()).Register(ctx, RegisterInput{
		Username: "u", Email: "u@x.com", Password: "pw123456",
	}, AuthSessionMetadata{}); err != errBoom {
		t.Fatalf("session create error: %v", err)
	}

	// 成功路径：会话字段归一化 + last_login 错误被吞
	ok := &stubRepo{updateUserLastLoginErr: errBoom}
	result, err = NewService(ok, testAuthConfig()).Register(ctx, RegisterInput{
		Username: "ok", Email: "ok@x.com", Password: "pw123456",
	}, AuthSessionMetadata{DeviceFingerprint: strings.Repeat("f", 200), UserAgent: strings.Repeat("u", 600), ClientIP: " 1.2.3.4 "})
	if err != nil || result == nil {
		t.Fatalf("ok: %+v %v", result, err)
	}
	if result.User.Role != "customer" || result.Token == "" || result.RefreshToken == "" || result.SessionID == "" {
		t.Fatalf("unexpected result: %+v", result)
	}
	if ok.createdSession == nil || ok.createdSession.DeviceFingerprint != strings.Repeat("f", 128) ||
		ok.createdSession.UserAgent != strings.Repeat("u", 512) || ok.createdSession.ClientIP != "1.2.3.4" ||
		ok.createdSession.Status != "active" {
		t.Fatalf("session normalization: %+v", ok.createdSession)
	}

	// 签名失败（放在最后，setSeam 到测试函数结束才还原）
	calls := 0
	setSeam(t, &HookCreateHS256JWT, func(map[string]interface{}, string) (string, error) {
		calls++
		return "", errBoom
	})
	if _, err := NewService(&stubRepo{}, testAuthConfig()).Register(ctx, RegisterInput{
		Username: "u", Email: "u@x.com", Password: "pw123456",
	}, AuthSessionMetadata{}); err != errBoom {
		t.Fatalf("sign error: %v", err)
	}
	if calls == 0 {
		t.Fatal("expected signing attempts")
	}
}

func TestAuthService_LoginStub(t *testing.T) {
	ctx := context.Background()

	var nilSvc *AuthService
	if _, err := nilSvc.Login(ctx, LoginInput{}, AuthSessionMetadata{}); err != ErrAuthInvalidDB {
		t.Fatalf("nil service: %v", err)
	}
	if _, err := NewService(nil, testAuthConfig()).Login(ctx, LoginInput{Username: "u", Password: "p"}, AuthSessionMetadata{}); err != ErrAuthInvalidDB {
		t.Fatalf("nil repo: %v", err)
	}

	// 查无此人 → 统一 invalid credentials（不回显存在性）
	missing := &stubRepo{userByUsernameOrEmailErr: ErrAuthUserNotFound}
	if _, err := NewService(missing, testAuthConfig()).Login(ctx, LoginInput{Username: "ghost", Password: "x"}, AuthSessionMetadata{}); err != ErrAuthInvalidCredentials {
		t.Fatalf("missing user: %v", err)
	}
	// 其他查询错误原样透传
	lookupErr := &stubRepo{userByUsernameOrEmailErr: errBoom}
	if _, err := NewService(lookupErr, testAuthConfig()).Login(ctx, LoginInput{Username: "u", Password: "x"}, AuthSessionMetadata{}); err != errBoom {
		t.Fatalf("lookup error: %v", err)
	}

	disabled := &stubRepo{userByUsernameOrEmail: &models.User{ID: 1, Status: "banned", Password: "x"}}
	if _, err := NewService(disabled, testAuthConfig()).Login(ctx, LoginInput{Username: "u", Password: "x"}, AuthSessionMetadata{}); err != ErrAuthUserDisabled {
		t.Fatalf("disabled: %v", err)
	}

	badPw := &stubRepo{userByUsernameOrEmail: &models.User{ID: 1, Status: "active", Password: stubPassword(t, "pw123456")}}
	if _, err := NewService(badPw, testAuthConfig()).Login(ctx, LoginInput{Username: "u", Password: "wrong"}, AuthSessionMetadata{}); err != ErrAuthInvalidCredentials {
		t.Fatalf("bad password: %v", err)
	}

	// 直接登录成功（last_login 错误被吞、会话已建）
	ok := &stubRepo{
		userByUsernameOrEmail:  &models.User{ID: 7, Username: "u", Status: "active", Password: stubPassword(t, "pw123456"), Role: "customer"},
		updateUserLastLoginErr: errBoom,
	}
	outcome, err := NewService(ok, testAuthConfig()).Login(ctx, LoginInput{Username: "u", Password: "pw123456"}, AuthSessionMetadata{})
	if err != nil || outcome.TwoFactorRequired || outcome.Result == nil || outcome.Result.SessionID == "" {
		t.Fatalf("ok: %+v %v", outcome, err)
	}
	if ok.createdSession == nil {
		t.Fatal("expected session created")
	}

	// 会话创建失败
	sessErr := &stubRepo{
		userByUsernameOrEmail: &models.User{ID: 7, Status: "active", Password: stubPassword(t, "pw123456")},
		createSessionErr:      errBoom,
	}
	if _, err := NewService(sessErr, testAuthConfig()).Login(ctx, LoginInput{Username: "u", Password: "pw123456"}, AuthSessionMetadata{}); err != errBoom {
		t.Fatalf("session error: %v", err)
	}

	// 直登路径 buildAuthResult 失败原样透传（放在最后，seam 到函数结束还原）
	setSeam(t, &HookCreateHS256JWT, func(map[string]interface{}, string) (string, error) {
		return "", errBoom
	})
	directRepo := &stubRepo{
		userByUsernameOrEmail: &models.User{ID: 7, Status: "active", Password: stubPassword(t, "pw123456")},
	}
	if _, err := NewService(directRepo, twoFactorTestConfig(false)).Login(ctx, LoginInput{Username: "u", Password: "pw123456"}, AuthSessionMetadata{}); err != errBoom {
		t.Fatalf("buildAuthResult error: %v", err)
	}
}

func TestAuthService_LoginTwoFactorChallengeStub(t *testing.T) {
	ctx := context.Background()
	enrolled := &models.User{ID: 7, Username: "u", Status: "active", TotpEnabled: true, Password: stubPassword(t, "pw123456")}
	repo := &stubRepo{userByUsernameOrEmail: enrolled}
	cfg := twoFactorTestConfig(true)

	outcome, err := NewService(repo, cfg).Login(ctx, LoginInput{Username: "u", Password: "pw123456"}, AuthSessionMetadata{ClientIP: "1.2.3.4", UserAgent: "ua"})
	if err != nil || !outcome.TwoFactorRequired || outcome.Result != nil || outcome.ChallengeToken == "" || outcome.ExpiresIn <= 0 {
		t.Fatalf("challenge: %+v %v", outcome, err)
	}
	if _, err := NewService(repo, cfg).Login(ctx, LoginInput{Username: "u", Password: "pw123456"}, AuthSessionMetadata{ClientIP: "1.2.3.4", UserAgent: "ua"}); err != nil {
		t.Fatalf("challenge ok: %v", err)
	}

	// 挑战 token 签名失败原样透传
	setSeam(t, &HookCreateHS256JWT, func(map[string]interface{}, string) (string, error) {
		return "", errBoom
	})
	if _, err := NewService(repo, cfg).Login(ctx, LoginInput{Username: "u", Password: "pw123456"}, AuthSessionMetadata{}); err != errBoom {
		t.Fatalf("sign error: %v", err)
	}
}

func TestAuthService_LoginRiskEnforcementStub(t *testing.T) {
	ctx := context.Background()
	meta := AuthSessionMetadata{ClientIP: "203.0.113.50"}
	credentials := LoginInput{Username: "u", Password: "pw123456"}

	mkSvc := func(enforcement string, totpEnabled bool) (*stubRepo, *AuthService) {
		user := &models.User{ID: 7, Username: "u", Status: "active", TotpEnabled: totpEnabled, Password: stubPassword(t, "pw123456")}
		repo := &stubRepo{userByUsernameOrEmail: user}
		cfg := twoFactorTestConfig(false)
		svc := NewService(repo, cfg).WithLoginRiskEnforcement(stubRiskIntel{label: "hosting"}, enforcement)
		return repo, svc
	}

	// block：高风险来源拒绝且不建会话
	repo, svc := mkSvc("block", false)
	if _, err := svc.Login(ctx, credentials, meta); err != ErrLoginBlockedByRisk {
		t.Fatalf("block: %v", err)
	}
	if repo.createdSession != nil {
		t.Fatalf("block must not create session: %+v", repo.createdSession)
	}

	// step_up + 未绑定 TOTP → 无第二因子可用，拒绝
	_, svc = mkSvc("step_up", false)
	if _, err := svc.Login(ctx, credentials, meta); err != ErrLoginBlockedByRisk {
		t.Fatalf("step_up without totp: %v", err)
	}

	// step_up + 已绑定 + kill-switch 关 → 强制挑战
	_, svc = mkSvc("step_up", true)
	outcome, err := svc.Login(ctx, credentials, meta)
	if err != nil || !outcome.TwoFactorRequired || outcome.ChallengeToken == "" || outcome.Result != nil {
		t.Fatalf("step_up forces challenge: %+v %v", outcome, err)
	}

	// step_up + 已绑定 + kill-switch 开 → 与常规挑战路径一致
	repo, svc = mkSvc("step_up", true)
	outcome, err = NewService(repo, twoFactorTestConfig(true)).
		WithLoginRiskEnforcement(stubRiskIntel{label: "hosting"}, "step_up").
		Login(ctx, credentials, meta)
	if err != nil || !outcome.TwoFactorRequired || outcome.ChallengeToken == "" {
		t.Fatalf("step_up with kill-switch on: %+v %v", outcome, err)
	}

	// 安全标签不拦截
	safe := &stubRepo{userByUsernameOrEmail: &models.User{ID: 7, Status: "active", Password: stubPassword(t, "pw123456")}}
	outcome, err = NewService(safe, twoFactorTestConfig(false)).
		WithLoginRiskEnforcement(stubRiskIntel{label: "private"}, "block").
		Login(ctx, credentials, meta)
	if err != nil || outcome.TwoFactorRequired || outcome.Result == nil {
		t.Fatalf("safe label: %+v %v", outcome, err)
	}

	// step_up 强制挑战 + 挑战签名失败：错误原样透传（放在最后，seam 到函数结束还原）
	stepUpRepo := &stubRepo{userByUsernameOrEmail: &models.User{ID: 7, Status: "active", TotpEnabled: true, Password: stubPassword(t, "pw123456")}}
	setSeam(t, &HookCreateHS256JWT, func(map[string]interface{}, string) (string, error) {
		return "", errBoom
	})
	if _, err := NewService(stepUpRepo, twoFactorTestConfig(false)).
		WithLoginRiskEnforcement(stubRiskIntel{label: "hosting"}, "step_up").
		Login(ctx, credentials, meta); err != errBoom {
		t.Fatalf("step_up challenge sign error: %v", err)
	}
}

func TestAuthService_GetCurrentUserStub(t *testing.T) {
	ctx := context.Background()

	var nilSvc *AuthService
	if _, err := nilSvc.GetCurrentUser(ctx, 1); err != ErrAuthInvalidDB {
		t.Fatalf("nil service: %v", err)
	}
	if _, err := NewService(nil, testAuthConfig()).GetCurrentUser(ctx, 1); err != ErrAuthInvalidDB {
		t.Fatalf("nil repo: %v", err)
	}

	notFound := &stubRepo{userByIDErr: ErrAuthUserNotFound}
	if _, err := NewService(notFound, testAuthConfig()).GetCurrentUser(ctx, 1); err != ErrAuthUserNotFound {
		t.Fatalf("not found: %v", err)
	}
	raw := &stubRepo{userByIDErr: errBoom}
	if _, err := NewService(raw, testAuthConfig()).GetCurrentUser(ctx, 1); err != errBoom {
		t.Fatalf("raw error: %v", err)
	}
	user, err := NewService(&stubRepo{userByID: activeUser()}, testAuthConfig()).GetCurrentUser(ctx, 7)
	if err != nil || user == nil || user.ID != 7 {
		t.Fatalf("ok: %+v %v", user, err)
	}
}

func TestAuthService_ListAndRevokeStub(t *testing.T) {
	ctx := context.Background()

	var nilSvc *AuthService
	if _, err := nilSvc.ListAuthSessions(ctx, 1); err != ErrAuthInvalidDB {
		t.Fatalf("nil service: %v", err)
	}
	if _, err := NewService(nil, testAuthConfig()).ListAuthSessions(ctx, 1); err != ErrAuthInvalidDB {
		t.Fatalf("nil repo: %v", err)
	}
	if _, err := NewService(&stubRepo{}, testAuthConfig()).ListAuthSessions(ctx, 0); err == nil || err.Error() != "user_id required" {
		t.Fatalf("zero user: %v", err)
	}
	if _, err := NewService(&stubRepo{listSessionsErr: errBoom}, testAuthConfig()).ListAuthSessions(ctx, 7); err != errBoom {
		t.Fatalf("list error: %v", err)
	}
	if sessions, err := NewService(&stubRepo{sessions: []models.UserAuthSession{*activeSession()}}, testAuthConfig()).ListAuthSessions(ctx, 7); err != nil || len(sessions) != 1 {
		t.Fatalf("ok: %+v %v", sessions, err)
	}

	// RevokeCurrentSession
	if _, err := nilSvc.RevokeCurrentSession(ctx, 1, "s"); err != ErrAuthInvalidDB {
		t.Fatalf("nil service: %v", err)
	}
	if _, err := NewService(&stubRepo{}, testAuthConfig()).RevokeCurrentSession(ctx, 0, "s"); err == nil || err.Error() != "user_id required" {
		t.Fatalf("zero user: %v", err)
	}
	if _, err := NewService(&stubRepo{}, testAuthConfig()).RevokeCurrentSession(ctx, 7, " "); err == nil || err.Error() != "session_id required" {
		t.Fatalf("blank session: %v", err)
	}
	if _, err := NewService(&stubRepo{revokeErr: errBoom}, testAuthConfig()).RevokeCurrentSession(ctx, 7, "s"); err != errBoom {
		t.Fatalf("revoke error: %v", err)
	}
	if _, err := NewService(&stubRepo{}, testAuthConfig()).RevokeCurrentSession(ctx, 7, "s"); err == nil || err.Error() != "session not found" {
		t.Fatalf("zero rows: %v", err)
	}
	if _, err := NewService(&stubRepo{revokeRows: 1, sessionErr: errBoom}, testAuthConfig()).RevokeCurrentSession(ctx, 7, "s"); err != errBoom {
		t.Fatalf("reload error: %v", err)
	}
	current, err := NewService(&stubRepo{revokeRows: 1, session: activeSession()}, testAuthConfig()).RevokeCurrentSession(ctx, 7, "sess-stub")
	if err != nil || current == nil || current.ID != "sess-stub" {
		t.Fatalf("ok: %+v %v", current, err)
	}

	// RevokeOtherSessions
	if _, err := nilSvc.RevokeOtherSessions(ctx, 1, "s"); err != ErrAuthInvalidDB {
		t.Fatalf("nil service: %v", err)
	}
	if _, err := NewService(&stubRepo{}, testAuthConfig()).RevokeOtherSessions(ctx, 0, "s"); err == nil || err.Error() != "user_id required" {
		t.Fatalf("zero user: %v", err)
	}
	if _, err := NewService(&stubRepo{}, testAuthConfig()).RevokeOtherSessions(ctx, 7, ""); err == nil || err.Error() != "current session_id required" {
		t.Fatalf("blank session: %v", err)
	}
	if _, err := NewService(&stubRepo{revokeOtherErr: errBoom}, testAuthConfig()).RevokeOtherSessions(ctx, 7, "s"); err != errBoom {
		t.Fatalf("revoke error: %v", err)
	}
	if count, err := NewService(&stubRepo{revokeOtherRows: 3}, testAuthConfig()).RevokeOtherSessions(ctx, 7, "s"); err != nil || count != 3 {
		t.Fatalf("ok: %d %v", count, err)
	}
}

func refreshClaims(userID int64, sessionID string, stv, tv float64) map[string]interface{} {
	return map[string]interface{}{
		"token_use": "refresh", "user_id": float64(userID), "session_id": sessionID,
		"session_token_version": stv, "token_version": tv, "iat": float64(time.Now().Unix()),
		"exp": float64(time.Now().Add(time.Hour).Unix()),
	}
}

func TestAuthService_RefreshTokenStub(t *testing.T) {
	ctx := context.Background()
	cfg := testAuthConfig()
	secret := cfg.JWT.Secret
	mkToken := func(claims map[string]interface{}) string {
		tok, err := createHS256JWT(claims, secret)
		if err != nil {
			t.Fatalf("mint token: %v", err)
		}
		return tok
	}

	var nilSvc *AuthService
	if _, err := nilSvc.RefreshToken(ctx, "x", AuthSessionMetadata{}); err != ErrAuthInvalidDB {
		t.Fatalf("nil service: %v", err)
	}
	if _, err := NewService(nil, cfg).RefreshToken(ctx, "x", AuthSessionMetadata{}); err != ErrAuthInvalidDB {
		t.Fatalf("nil repo: %v", err)
	}
	if _, err := NewService(&stubRepo{}, cfg).RefreshToken(ctx, "  ", AuthSessionMetadata{}); err != ErrAuthInvalidRefreshToken {
		t.Fatalf("blank token: %v", err)
	}
	if _, err := NewService(&stubRepo{}, cfg).RefreshToken(ctx, "garbage.token.sig", AuthSessionMetadata{}); err != ErrAuthInvalidRefreshToken {
		t.Fatalf("invalid jwt: %v", err)
	}

	// claim 级拒绝
	cases := []struct {
		name   string
		claims map[string]interface{}
	}{
		{"access token use", map[string]interface{}{"token_use": "access", "user_id": float64(7), "session_id": "s", "session_token_version": float64(0), "iat": float64(time.Now().Unix())}},
		{"missing user id", map[string]interface{}{"token_use": "refresh", "session_id": "s", "session_token_version": float64(0)}},
		{"zero user id", map[string]interface{}{"token_use": "refresh", "user_id": float64(0), "session_id": "s", "session_token_version": float64(0)}},
		{"missing session id", map[string]interface{}{"token_use": "refresh", "user_id": float64(7), "session_token_version": float64(0)}},
		{"blank session id", map[string]interface{}{"token_use": "refresh", "user_id": float64(7), "session_id": "  ", "session_token_version": float64(0)}},
		{"missing session version", map[string]interface{}{"token_use": "refresh", "user_id": float64(7), "session_id": "s"}},
	}
	for _, tc := range cases {
		if _, err := NewService(&stubRepo{}, cfg).RefreshToken(ctx, mkToken(tc.claims), AuthSessionMetadata{}); err != ErrAuthInvalidRefreshToken {
			t.Fatalf("%s: %v", tc.name, err)
		}
	}

	// 用户级拒绝
	unknownUser := &stubRepo{userByIDErr: ErrAuthUserNotFound}
	if _, err := NewService(unknownUser, cfg).RefreshToken(ctx, mkToken(refreshClaims(7, "s", 0, 0)), AuthSessionMetadata{}); err != ErrAuthInvalidRefreshToken {
		t.Fatalf("unknown user: %v", err)
	}
	disabled := &stubRepo{userByID: &models.User{ID: 7, Status: "banned"}}
	if _, err := NewService(disabled, cfg).RefreshToken(ctx, mkToken(refreshClaims(7, "s", 0, 0)), AuthSessionMetadata{}); err != ErrAuthUserDisabled {
		t.Fatalf("disabled: %v", err)
	}
	stale := &stubRepo{userByID: &models.User{ID: 7, Status: "active", TokenVersion: 2}}
	if _, err := NewService(stale, cfg).RefreshToken(ctx, mkToken(refreshClaims(7, "s", 0, 1)), AuthSessionMetadata{}); err != ErrAuthInvalidRefreshToken {
		t.Fatalf("stale token version: %v", err)
	}
	validAfter := &stubRepo{userByID: &models.User{ID: 7, Status: "active", TokenValidAfter: ptrTime(time.Now().Add(time.Minute))}}
	if _, err := NewService(validAfter, cfg).RefreshToken(ctx, mkToken(refreshClaims(7, "s", 0, 0)), AuthSessionMetadata{}); err != ErrAuthInvalidRefreshToken {
		t.Fatalf("token_valid_after: %v", err)
	}

	// 会话级拒绝
	user := activeUser()
	getErr := &stubRepo{userByID: user, sessionErr: errBoom}
	if _, err := NewService(getErr, cfg).RefreshToken(ctx, mkToken(refreshClaims(7, "s", 0, 0)), AuthSessionMetadata{}); err != ErrAuthInvalidRefreshToken {
		t.Fatalf("session load error: %v", err)
	}
	revoked := &stubRepo{userByID: user, session: &models.UserAuthSession{ID: "s", UserID: 7, Status: "revoked"}}
	if _, err := NewService(revoked, cfg).RefreshToken(ctx, mkToken(refreshClaims(7, "s", 0, 0)), AuthSessionMetadata{}); err != ErrAuthInvalidRefreshToken {
		t.Fatalf("revoked session: %v", err)
	}
	future := &stubRepo{userByID: user, session: activeSession()}
	forged, err := createHS256JWT(func() map[string]interface{} {
		c := refreshClaims(7, "sess-stub", 99, 0)
		return c
	}(), secret)
	if err != nil {
		t.Fatalf("forge: %v", err)
	}
	if _, err := NewService(future, cfg).RefreshToken(ctx, forged, AuthSessionMetadata{}); err != ErrAuthInvalidRefreshToken {
		t.Fatalf("future version: %v", err)
	}
	if future.session.Status != "active" {
		t.Fatalf("future version must not revoke, got %+v", future.session)
	}

	// 版本失配 + revoke_family：吊销整个家族
	revokeRepo := &stubRepo{userByID: user, session: &models.UserAuthSession{ID: "sess-stub", UserID: 7, Status: "active", TokenVersion: 3}}
	revokeRepo.getAuthSessionFn = func(ctx context.Context, sessionID string, userID uint) (*models.UserAuthSession, error) {
		return &models.UserAuthSession{ID: sessionID, UserID: userID, Status: "active", TokenVersion: 3}, nil
	}
	revokeSvc := NewService(revokeRepo, cfg).WithRefreshReusePolicy("revoke_family")
	if _, err := revokeSvc.RefreshToken(ctx, mkToken(refreshClaims(7, "sess-stub", 1, 0)), AuthSessionMetadata{}); err != ErrAuthInvalidRefreshToken {
		t.Fatalf("reuse with revocation: %v", err)
	}
	if revokeRepo.revokedActiveID != "sess-stub" {
		t.Fatalf("expected family revocation, got %q", revokeRepo.revokedActiveID)
	}

	// 家族吊销的 UPDATE 失败：fail-closed（仍拒绝，不静默放过）
	revokeFail := &stubRepo{userByID: user, revokeActiveErr: errBoom}
	revokeFail.getAuthSessionFn = func(ctx context.Context, sessionID string, userID uint) (*models.UserAuthSession, error) {
		return &models.UserAuthSession{ID: sessionID, UserID: userID, Status: "active", TokenVersion: 3}, nil
	}
	revokeFailSvc := NewService(revokeFail, cfg).WithRefreshReusePolicy("revoke_family")
	if _, err := revokeFailSvc.RefreshToken(ctx, mkToken(refreshClaims(7, "sess-stub", 1, 0)), AuthSessionMetadata{}); err != ErrAuthInvalidRefreshToken {
		t.Fatalf("revocation failure: %v", err)
	}

	// 版本失配但未开启 revoke_family：拒绝但不吊销
	noRevoke := &stubRepo{userByID: user}
	noRevoke.getAuthSessionFn = func(ctx context.Context, sessionID string, userID uint) (*models.UserAuthSession, error) {
		return &models.UserAuthSession{ID: sessionID, UserID: userID, Status: "active", TokenVersion: 3}, nil
	}
	noRevokeSvc := NewService(noRevoke, cfg).WithRefreshReusePolicy("off")
	if _, err := noRevokeSvc.RefreshToken(ctx, mkToken(refreshClaims(7, "sess-stub", 1, 0)), AuthSessionMetadata{}); err != ErrAuthInvalidRefreshToken {
		t.Fatalf("mismatch without revocation: %v", err)
	}
	if noRevoke.revokedActiveID != "" {
		t.Fatalf("off 档位不得吊销会话, got %q", noRevoke.revokedActiveID)
	}

	// 轮换 UPDATE 失败
	rotateErr := &stubRepo{userByID: user, session: activeSession(), rotateErr: errBoom}
	if _, err := NewService(rotateErr, cfg).RefreshToken(ctx, mkToken(refreshClaims(7, "sess-stub", 0, 0)), AuthSessionMetadata{}); err != ErrAuthInvalidRefreshToken {
		t.Fatalf("rotate error: %v", err)
	}

	// 轮换后重载失败
	calls := 0
	reloadErr := &stubRepo{userByID: user}
	reloadErr.getAuthSessionFn = func(ctx context.Context, sessionID string, userID uint) (*models.UserAuthSession, error) {
		calls++
		if calls == 2 {
			return nil, errBoom
		}
		return activeSession(), nil
	}
	if _, err := NewService(reloadErr, cfg).RefreshToken(ctx, mkToken(refreshClaims(7, "sess-stub", 0, 0)), AuthSessionMetadata{}); err != ErrAuthInvalidRefreshToken {
		t.Fatalf("reload error: %v", err)
	}

	// 成功轮换：字段归一化 + last_login 错误被吞
	ok := &stubRepo{userByID: user, session: activeSession(), updateUserLastLoginErr: errBoom}
	result, err := NewService(ok, cfg).RefreshToken(ctx, mkToken(refreshClaims(7, "sess-stub", 0, 0)), AuthSessionMetadata{
		DeviceFingerprint: strings.Repeat("f", 200), UserAgent: strings.Repeat("u", 600), ClientIP: " 9.9.9.9 ",
	})
	if err != nil || result == nil || result.SessionID != "sess-stub" {
		t.Fatalf("ok: %+v %v", result, err)
	}
	if ok.rotatedID != "sess-stub" || ok.rotatedUserID != 7 || ok.rotatedFields == nil {
		t.Fatalf("rotate capture: %q %d %+v", ok.rotatedID, ok.rotatedUserID, ok.rotatedFields)
	}
	if ok.rotatedFingerprint != strings.Repeat("f", 128) || ok.rotatedUA != strings.Repeat("u", 512) || ok.rotatedIP != "9.9.9.9" {
		t.Fatalf("rotate normalization: %+v", ok.rotatedFields)
	}
	if ok.rotatedFields.Now.IsZero() {
		t.Fatal("expected rotation timestamp")
	}
}

func ptrTime(v time.Time) *time.Time { return &v }

func TestAuthService_BuildAuthResultStub(t *testing.T) {
	ctx := context.Background()
	svc := NewService(&stubRepo{}, testAuthConfig())

	if _, err := svc.buildAuthResult(ctx, nil, activeSession()); err == nil || err.Error() != "user is required" {
		t.Fatalf("nil user: %v", err)
	}
	if _, err := svc.buildAuthResult(ctx, activeUser(), nil); err == nil || err.Error() != "auth session is required" {
		t.Fatalf("nil session: %v", err)
	}

	// access 签名失败
	calls := 0
	setSeam(t, &HookCreateHS256JWT, func(payload map[string]interface{}, secret string) (string, error) {
		calls++
		if calls == 1 {
			return "", errBoom
		}
		return createHS256JWT(payload, secret)
	})
	if _, err := svc.buildAuthResult(ctx, activeUser(), activeSession()); err != errBoom {
		t.Fatalf("access sign error: %v", err)
	}

	// refresh 签名失败
	calls = 0
	setSeam(t, &HookCreateHS256JWT, func(payload map[string]interface{}, secret string) (string, error) {
		calls++
		if calls == 2 {
			return "", errBoom
		}
		return createHS256JWT(payload, secret)
	})
	if _, err := svc.buildAuthResult(ctx, activeUser(), activeSession()); err != errBoom {
		t.Fatalf("refresh sign error: %v", err)
	}

	// 成功：token_use 各就各位
	setSeam(t, &HookCreateHS256JWT, func(payload map[string]interface{}, secret string) (string, error) {
		return createHS256JWT(payload, secret)
	})
	result, err := svc.buildAuthResult(ctx, &models.User{ID: 7, Role: "agent", TokenVersion: 2}, activeSession())
	if err != nil {
		t.Fatalf("ok: %v", err)
	}
	if result.ExpiresIn != 3600 || result.RefreshExpiresIn != int((24*time.Hour).Seconds()) {
		t.Fatalf("expiries: %+v", result)
	}
	if result.SessionID != "sess-stub" || result.User.ID != 7 {
		t.Fatalf("result: %+v", result)
	}

	// refresh TTL 缺省时回退 7 天
	cfg := testAuthConfig()
	cfg.JWT.RefreshExpiresIn = 0
	svc2 := NewService(&stubRepo{}, cfg)
	if got := svc2.refreshExpiresIn(); got != 7*24*time.Hour {
		t.Fatalf("default refresh ttl = %v", got)
	}
}

func TestSetupTwoFactorStub(t *testing.T) {
	ctx := context.Background()

	var nilSvc *AuthService
	if _, err := nilSvc.SetupTwoFactor(ctx, 1); err != ErrAuthInvalidDB {
		t.Fatalf("nil service: %v", err)
	}
	if _, err := NewService(nil, twoFactorTestConfig(true)).SetupTwoFactor(ctx, 1); err != ErrAuthInvalidDB {
		t.Fatalf("nil repo: %v", err)
	}
	if _, err := NewService(&stubRepo{}, twoFactorTestConfig(false)).SetupTwoFactor(ctx, 1); err != ErrTwoFactorDisabled {
		t.Fatalf("kill-switch: %v", err)
	}
	if _, err := NewService(&stubRepo{userByIDErr: errBoom}, twoFactorTestConfig(true)).SetupTwoFactor(ctx, 1); err != errBoom {
		t.Fatalf("user error: %v", err)
	}
	if _, err := NewService(&stubRepo{userByID: &models.User{ID: 1, TotpEnabled: true}}, twoFactorTestConfig(true)).SetupTwoFactor(ctx, 1); err != ErrTwoFactorAlreadyEnabled {
		t.Fatalf("already enabled: %v", err)
	}
	// email/username 均为空 → totp.Generate 报错
	if _, err := NewService(&stubRepo{userByID: &models.User{ID: 1, Status: "active"}}, twoFactorTestConfig(true)).SetupTwoFactor(ctx, 1); err == nil {
		t.Fatal("expected generate error for empty account")
	}

	// email 账号
	setup, err := NewService(&stubRepo{userByID: activeUser()}, twoFactorTestConfig(true)).SetupTwoFactor(ctx, 7)
	if err != nil || setup == nil || setup.Secret == "" || !strings.Contains(setup.OTPAuthURI, "stub@x.com") {
		t.Fatalf("email account: %+v %v", setup, err)
	}
	// username 回退
	fallback, err := NewService(&stubRepo{userByID: &models.User{ID: 2, Username: "uname", Status: "active"}}, twoFactorTestConfig(true)).SetupTwoFactor(ctx, 2)
	if err != nil || fallback == nil || !strings.Contains(fallback.OTPAuthURI, "uname") {
		t.Fatalf("username fallback: %+v %v", fallback, err)
	}
}

func TestEnableTwoFactorStub(t *testing.T) {
	ctx := context.Background()

	var nilSvc *AuthService
	if _, err := nilSvc.EnableTwoFactor(ctx, 1, "s", "123456"); err != ErrAuthInvalidDB {
		t.Fatalf("nil service: %v", err)
	}
	if _, err := NewService(nil, twoFactorTestConfig(true)).EnableTwoFactor(ctx, 1, "s", "123456"); err != ErrAuthInvalidDB {
		t.Fatalf("nil repo: %v", err)
	}
	if _, err := NewService(&stubRepo{}, twoFactorTestConfig(false)).EnableTwoFactor(ctx, 1, "s", "123456"); err != ErrTwoFactorDisabled {
		t.Fatalf("kill-switch: %v", err)
	}
	if _, err := NewService(&stubRepo{}, twoFactorTestConfig(true)).EnableTwoFactor(ctx, 1, "   ", "123456"); err != ErrInvalidAuthInput {
		t.Fatalf("blank secret: %v", err)
	}
	if _, err := NewService(&stubRepo{}, twoFactorTestConfig(true)).EnableTwoFactor(ctx, 1, testTOTPSecret, "000000"); err != ErrAuthInvalid2FACode {
		t.Fatalf("bad code: %v", err)
	}
	if _, err := NewService(&stubRepo{userByIDErr: errBoom}, twoFactorTestConfig(true)).EnableTwoFactor(ctx, 1, testTOTPSecret, currentTOTP(t, testTOTPSecret)); err != errBoom {
		t.Fatalf("user error: %v", err)
	}
	if _, err := NewService(&stubRepo{userByID: &models.User{ID: 1, TotpEnabled: true}}, twoFactorTestConfig(true)).EnableTwoFactor(ctx, 1, testTOTPSecret, currentTOTP(t, testTOTPSecret)); err != ErrTwoFactorAlreadyEnabled {
		t.Fatalf("already enabled: %v", err)
	}

	code := currentTOTP(t, testTOTPSecret)

	// 绑定写入失败
	bindErr := &stubRepo{userByID: activeUser(), enableTwoFactorErr: errBoom}
	if _, err := NewService(bindErr, twoFactorTestConfig(true)).EnableTwoFactor(ctx, 7, testTOTPSecret, code); err != errBoom {
		t.Fatalf("enable error: %v", err)
	}

	// 恢复码落库失败
	rcErr := &stubRepo{userByID: activeUser(), replaceRecoveryErr: errBoom}
	if _, err := NewService(rcErr, twoFactorTestConfig(true)).EnableTwoFactor(ctx, 7, testTOTPSecret, code); err != errBoom {
		t.Fatalf("replace error: %v", err)
	}

	// 成功：secret 绑定 + 一批恢复码
	ok := &stubRepo{userByID: activeUser()}
	codes, err := NewService(ok, twoFactorTestConfig(true)).EnableTwoFactor(ctx, 7, testTOTPSecret, code)
	if err != nil || len(codes) != 10 {
		t.Fatalf("ok: %d codes, %v", len(codes), err)
	}
	if ok.totpUserID != 7 || ok.totpSecretSaved != testTOTPSecret || len(ok.replacedHashes) != 10 {
		t.Fatalf("captures: %d %q %d", ok.totpUserID, ok.totpSecretSaved, len(ok.replacedHashes))
	}

	// 恢复码生成失败 → 整体回滚（不写绑定、不落恢复码；放在最后，
	// failingRand 到测试函数结束才还原）
	calls := 0
	failingRand(t, []bool{true}, &calls)
	rollback := &stubRepo{userByID: activeUser()}
	if _, err := NewService(rollback, twoFactorTestConfig(true)).EnableTwoFactor(ctx, 7, testTOTPSecret, code); err == nil || !strings.Contains(err.Error(), "generate recovery code") {
		t.Fatalf("recovery code rand error: %v", err)
	}
	if rollback.totpSecretSaved != "" || rollback.replacedHashes != nil {
		t.Fatalf("binding must be rolled back: %q %v", rollback.totpSecretSaved, rollback.replacedHashes)
	}
}

func TestDisableTwoFactorStub(t *testing.T) {
	ctx := context.Background()

	var nilSvc *AuthService
	if err := nilSvc.DisableTwoFactor(ctx, 1, "p", "123456"); err != ErrAuthInvalidDB {
		t.Fatalf("nil service: %v", err)
	}
	if err := NewService(nil, testAuthConfig()).DisableTwoFactor(ctx, 1, "p", "123456"); err != ErrAuthInvalidDB {
		t.Fatalf("nil repo: %v", err)
	}
	if err := NewService(&stubRepo{userByIDErr: errBoom}, testAuthConfig()).DisableTwoFactor(ctx, 1, "p", "123456"); err != errBoom {
		t.Fatalf("user error: %v", err)
	}
	if err := NewService(&stubRepo{userByID: activeUser()}, testAuthConfig()).DisableTwoFactor(ctx, 1, "p", "123456"); err != ErrTwoFactorNotEnabled {
		t.Fatalf("not enabled: %v", err)
	}

	enrolled := &models.User{ID: 7, Username: "u", Status: "active", TotpEnabled: true, TotpSecret: testTOTPSecret, Password: stubPassword(t, "pw123456")}
	code := currentTOTP(t, testTOTPSecret)

	if err := NewService(&stubRepo{userByID: enrolled}, testAuthConfig()).DisableTwoFactor(ctx, 7, "wrong", code); err != ErrAuthInvalidCredentials {
		t.Fatalf("wrong password: %v", err)
	}
	// OIDC 账号无本地密码 → 跳过密码校验
	oidcUser := &models.User{ID: 7, Status: "active", TotpEnabled: true, TotpSecret: testTOTPSecret}
	if err := NewService(&stubRepo{userByID: oidcUser}, testAuthConfig()).DisableTwoFactor(ctx, 7, "", "000000"); err != ErrAuthInvalid2FACode {
		t.Fatalf("bad code without recovery: %v", err)
	}
	if err := NewService(&stubRepo{userByID: enrolled, disableTwoFactorErr: errBoom}, testAuthConfig()).DisableTwoFactor(ctx, 7, "pw123456", code); err != errBoom {
		t.Fatalf("unbind error: %v", err)
	}
	if err := NewService(&stubRepo{userByID: enrolled, deleteRecoveryErr: errBoom}, testAuthConfig()).DisableTwoFactor(ctx, 7, "pw123456", code); err != errBoom {
		t.Fatalf("delete recovery error: %v", err)
	}
	// 成功（TOTP 码）
	if err := NewService(&stubRepo{userByID: enrolled}, testAuthConfig()).DisableTwoFactor(ctx, 7, "pw123456", code); err != nil {
		t.Fatalf("ok: %v", err)
	}
	// 成功（恢复码路径：TOTP 码错误但恢复码核销成功）
	recovery := &stubRepo{userByID: enrolled, useRecoveryRows: 1}
	if err := NewService(recovery, testAuthConfig()).DisableTwoFactor(ctx, 7, "pw123456", "ab12-cd34"); err != nil {
		t.Fatalf("recovery path: %v", err)
	}
	sum := sha256.Sum256([]byte("ab12cd34"))
	if recovery.usedCodeHash != hex.EncodeToString(sum[:]) {
		t.Fatalf("recovery hash: %q", recovery.usedCodeHash)
	}
	if recovery.deletedRecoveryUser != 7 {
		t.Fatalf("expected recovery codes deleted, got %d", recovery.deletedRecoveryUser)
	}
}

func TestRegenerateRecoveryCodesStub(t *testing.T) {
	ctx := context.Background()

	var nilSvc *AuthService
	if _, err := nilSvc.RegenerateRecoveryCodes(ctx, 1, "123456"); err != ErrAuthInvalidDB {
		t.Fatalf("nil service: %v", err)
	}
	if _, err := NewService(nil, testAuthConfig()).RegenerateRecoveryCodes(ctx, 1, "123456"); err != ErrAuthInvalidDB {
		t.Fatalf("nil repo: %v", err)
	}
	if _, err := NewService(&stubRepo{userByIDErr: errBoom}, testAuthConfig()).RegenerateRecoveryCodes(ctx, 1, "123456"); err != errBoom {
		t.Fatalf("user error: %v", err)
	}
	if _, err := NewService(&stubRepo{userByID: activeUser()}, testAuthConfig()).RegenerateRecoveryCodes(ctx, 1, "123456"); err != ErrTwoFactorNotEnabled {
		t.Fatalf("not enabled: %v", err)
	}
	if _, err := NewService(&stubRepo{userByID: enrolledUser()}, testAuthConfig()).RegenerateRecoveryCodes(ctx, 1, "000000"); err != ErrAuthInvalid2FACode {
		t.Fatalf("bad code: %v", err)
	}

	code := currentTOTP(t, testTOTPSecret)

	if _, err := NewService(&stubRepo{userByID: enrolledUser(), replaceRecoveryErr: errBoom}, testAuthConfig()).RegenerateRecoveryCodes(ctx, 7, code); err != errBoom {
		t.Fatalf("replace error: %v", err)
	}
	fresh, err := NewService(&stubRepo{userByID: enrolledUser()}, testAuthConfig()).RegenerateRecoveryCodes(ctx, 7, code)
	if err != nil || len(fresh) != 10 {
		t.Fatalf("ok: %d codes, %v", len(fresh), err)
	}

	// 恢复码生成失败（放在最后，failingRand 到测试函数结束才还原）
	calls := 0
	failingRand(t, []bool{true}, &calls)
	if _, err := NewService(&stubRepo{userByID: enrolledUser()}, testAuthConfig()).RegenerateRecoveryCodes(ctx, 7, code); err == nil || !strings.Contains(err.Error(), "generate recovery code") {
		t.Fatalf("rand error: %v", err)
	}
}

func TestRecoveryCodesRemainingStub(t *testing.T) {
	ctx := context.Background()

	var nilSvc *AuthService
	if _, err := nilSvc.RecoveryCodesRemaining(ctx, 1); err != ErrAuthInvalidDB {
		t.Fatalf("nil service: %v", err)
	}
	if _, err := NewService(nil, testAuthConfig()).RecoveryCodesRemaining(ctx, 1); err != ErrAuthInvalidDB {
		t.Fatalf("nil repo: %v", err)
	}
	if _, err := NewService(&stubRepo{countUnusedErr: errBoom}, testAuthConfig()).RecoveryCodesRemaining(ctx, 7); err != errBoom {
		t.Fatalf("error: %v", err)
	}
	if remaining, err := NewService(&stubRepo{countUnused: 4}, testAuthConfig()).RecoveryCodesRemaining(ctx, 7); err != nil || remaining != 4 {
		t.Fatalf("ok: %d %v", remaining, err)
	}
}

func TestVerifyTwoFactorLoginStub(t *testing.T) {
	ctx := context.Background()
	meta := AuthSessionMetadata{ClientIP: "1.2.3.4", UserAgent: "ua-verify"}
	cfg := twoFactorTestConfig(true)

	var nilSvc *AuthService
	if _, err := nilSvc.VerifyTwoFactorLogin(ctx, TwoFactorVerifyInput{}, meta); err != ErrAuthInvalidDB {
		t.Fatalf("nil service: %v", err)
	}
	if _, err := NewService(nil, cfg).VerifyTwoFactorLogin(ctx, TwoFactorVerifyInput{}, meta); err != ErrAuthInvalidDB {
		t.Fatalf("nil repo: %v", err)
	}

	svc := NewService(&stubRepo{}, cfg)
	mkChallenge := func(t *testing.T, svc *AuthService, user *models.User, m AuthSessionMetadata) string {
		t.Helper()
		token, _, err := svc.createChallengeToken(user, m)
		if err != nil {
			t.Fatalf("createChallengeToken: %v", err)
		}
		return token
	}

	// 非法 token / 错误用途 / 指纹不符 / user_id 为 0
	if _, err := svc.VerifyTwoFactorLogin(ctx, TwoFactorVerifyInput{ChallengeToken: "garbage"}, meta); err != ErrAuthInvalid2FAChallenge {
		t.Fatalf("garbage: %v", err)
	}
	accessTok, err := createHS256JWT(map[string]interface{}{
		"user_id": 7, "token_use": "access", "exp": time.Now().Add(time.Hour).Unix(),
	}, cfg.JWT.Secret)
	if err != nil {
		t.Fatalf("craft access token: %v", err)
	}
	if _, err := svc.VerifyTwoFactorLogin(ctx, TwoFactorVerifyInput{ChallengeToken: accessTok}, meta); err != ErrAuthInvalid2FAChallenge {
		t.Fatalf("access token: %v", err)
	}
	bound := mkChallenge(t, svc, activeUser(), meta)
	if _, err := svc.VerifyTwoFactorLogin(ctx, TwoFactorVerifyInput{ChallengeToken: bound}, AuthSessionMetadata{ClientIP: "9.9.9.9", UserAgent: "ua-verify"}); err != ErrAuthInvalid2FAChallenge {
		t.Fatalf("ip fingerprint mismatch: %v", err)
	}
	if _, err := svc.VerifyTwoFactorLogin(ctx, TwoFactorVerifyInput{ChallengeToken: bound}, AuthSessionMetadata{ClientIP: "1.2.3.4", UserAgent: "other-ua"}); err != ErrAuthInvalid2FAChallenge {
		t.Fatalf("ua fingerprint mismatch: %v", err)
	}
	unknown := &stubRepo{userByIDErr: ErrAuthUserNotFound}
	if _, err := NewService(unknown, cfg).VerifyTwoFactorLogin(ctx, TwoFactorVerifyInput{ChallengeToken: bound}, meta); err != ErrAuthInvalid2FAChallenge {
		t.Fatalf("unknown user: %v", err)
	}

	// 用户级拒绝
	if _, err := NewService(&stubRepo{userByIDErr: errBoom}, cfg).VerifyTwoFactorLogin(ctx, TwoFactorVerifyInput{ChallengeToken: bound}, meta); err != ErrAuthInvalid2FAChallenge {
		t.Fatalf("user error folds to challenge: %v", err)
	}
	if _, err := NewService(&stubRepo{userByID: &models.User{ID: 7, Status: "banned"}}, cfg).VerifyTwoFactorLogin(ctx, TwoFactorVerifyInput{ChallengeToken: bound}, meta); err != ErrAuthUserDisabled {
		t.Fatalf("disabled: %v", err)
	}
	// 未绑定 TOTP 的用户拿不到会话（挑战 token 换不来任何资源）
	if _, err := NewService(&stubRepo{userByID: activeUser()}, cfg).VerifyTwoFactorLogin(ctx, TwoFactorVerifyInput{ChallengeToken: bound}, meta); err != ErrAuthInvalid2FAChallenge {
		t.Fatalf("not enrolled: %v", err)
	}
	enrolled := &stubRepo{userByID: enrolledUser()}
	if _, err := NewService(enrolled, cfg).VerifyTwoFactorLogin(ctx, TwoFactorVerifyInput{ChallengeToken: bound, Code: "000000"}, meta); err != ErrAuthInvalid2FACode {
		t.Fatalf("bad code: %v", err)
	}

	// 会话创建 / 结果构建失败
	enrolled.createSessionErr = errBoom
	if _, err := NewService(enrolled, cfg).VerifyTwoFactorLogin(ctx, TwoFactorVerifyInput{ChallengeToken: bound, Code: currentTOTP(t, testTOTPSecret)}, meta); err != errBoom {
		t.Fatalf("session create error: %v", err)
	}

	// 成功（TOTP 码）
	okSvc := NewService(&stubRepo{userByID: enrolledUser()}, cfg)
	challenge := mkChallenge(t, okSvc, enrolledUser(), meta)
	result, err := okSvc.VerifyTwoFactorLogin(ctx, TwoFactorVerifyInput{ChallengeToken: challenge, Code: currentTOTP(t, testTOTPSecret)}, meta)
	if err != nil || result == nil || result.SessionID == "" {
		t.Fatalf("ok: %+v %v", result, err)
	}

	// 成功（恢复码路径）
	recovery := &stubRepo{userByID: enrolledUser(), useRecoveryRows: 1}
	recoverySvc := NewService(recovery, cfg)
	recoveryChallenge := mkChallenge(t, recoverySvc, enrolledUser(), meta)
	if _, err := recoverySvc.VerifyTwoFactorLogin(ctx, TwoFactorVerifyInput{ChallengeToken: recoveryChallenge, Code: "ab12-cd34"}, meta); err != nil {
		t.Fatalf("recovery ok: %v", err)
	}
}

var enrolledPassword = sync.OnceValue(func() string {
	hash, err := bcrypt.GenerateFromPassword([]byte("pw123456"), bcrypt.MinCost)
	if err != nil {
		panic(err)
	}
	return string(hash)
})

func enrolledUser() *models.User {
	return &models.User{ID: 7, Username: "totp-user", Email: "totp@x.com", Status: "active", TotpEnabled: true, TotpSecret: testTOTPSecret, Password: enrolledPassword()}
}

func TestConsumeRecoveryCodeStub(t *testing.T) {
	ctx := context.Background()
	svc := NewService(&stubRepo{}, testAuthConfig())

	if svc.consumeRecoveryCode(ctx, 7, "   ") {
		t.Fatal("blank code must fail")
	}
	if svc.consumeRecoveryCode(ctx, 7, "zz12cd34") {
		t.Fatal("non-hex code must fail")
	}
	errRepo := &stubRepo{useRecoveryErr: errBoom}
	if NewService(errRepo, testAuthConfig()).consumeRecoveryCode(ctx, 7, "ab12-cd34") {
		t.Fatal("query error must fail")
	}
	zeroRows := &stubRepo{}
	if NewService(zeroRows, testAuthConfig()).consumeRecoveryCode(ctx, 7, "ab12-cd34") {
		t.Fatal("zero rows must fail")
	}
	hit := &stubRepo{useRecoveryRows: 1}
	if !NewService(hit, testAuthConfig()).consumeRecoveryCode(ctx, 7, "ab12-cd34") {
		t.Fatal("single row must succeed")
	}
	sum := sha256.Sum256([]byte("ab12cd34"))
	if hit.usedCodeHash != hex.EncodeToString(sum[:]) {
		t.Fatalf("normalized hash: %q", hit.usedCodeHash)
	}
}

func TestAuthService_LoginWithOIDCStub(t *testing.T) {
	ctx := context.Background()
	meta := AuthSessionMetadata{ClientIP: "1.2.3.4", UserAgent: "ua-oidc"}
	identity := OIDCIdentity{Subject: "sub-1", Email: "Ada@Example.com", EmailVerified: true, Name: " Ada Lovelace "}

	var nilSvc *AuthService
	if _, err := nilSvc.LoginWithOIDC(ctx, identity, meta); err != ErrAuthInvalidDB {
		t.Fatalf("nil service: %v", err)
	}
	if _, err := NewService(nil, oidcTestConfig(nil)).LoginWithOIDC(ctx, identity, meta); err != ErrAuthInvalidDB {
		t.Fatalf("nil repo: %v", err)
	}

	cfg := oidcTestConfig(nil)

	// 邮箱缺省 / 未验证 → 统一 unverified
	if _, err := NewService(&stubRepo{}, cfg).LoginWithOIDC(ctx, OIDCIdentity{Subject: "s", Email: "  "}, meta); err != ErrOIDCUnverifiedEmail {
		t.Fatalf("blank email: %v", err)
	}
	if _, err := NewService(&stubRepo{}, cfg).LoginWithOIDC(ctx, OIDCIdentity{Subject: "s", Email: "a@x.com", EmailVerified: false}, meta); err != ErrOIDCUnverifiedEmail {
		t.Fatalf("unverified email: %v", err)
	}
	// subject 缺省
	if _, err := NewService(&stubRepo{}, cfg).LoginWithOIDC(ctx, OIDCIdentity{Email: "a@x.com", EmailVerified: true}, meta); err != ErrInvalidAuthInput {
		t.Fatalf("blank subject: %v", err)
	}
	// 域白名单拒绝
	domainCfg := oidcTestConfig(func(c *config.OIDCConfig) { c.AllowedDomains = []string{"allowed.com"} })
	if _, err := NewService(&stubRepo{}, domainCfg).LoginWithOIDC(ctx, identity, meta); err != ErrOIDCDomainNotAllowed {
		t.Fatalf("domain not allowed: %v", err)
	}

	// 既有用户：直接建会话（邮箱大小写归一化后命中）
	existing := &stubRepo{userByEmailLower: &models.User{ID: 9, Username: "ada", Email: "ada@example.com", Status: "active", Role: "agent"}}
	result, err := NewService(existing, cfg).LoginWithOIDC(ctx, identity, meta)
	if err != nil || result == nil || result.SessionID == "" || result.User.ID != 9 {
		t.Fatalf("existing user: %+v %v", result, err)
	}
	// 既有用户被封禁
	banned := &stubRepo{userByEmailLower: &models.User{ID: 9, Status: " banned "}}
	if _, err := NewService(banned, cfg).LoginWithOIDC(ctx, identity, meta); err != ErrAuthUserDisabled {
		t.Fatalf("banned: %v", err)
	}
	// 既有用户查询出错原样透传
	lookupErr := &stubRepo{userByEmailLowerErr: errBoom}
	if _, err := NewService(lookupErr, cfg).LoginWithOIDC(ctx, identity, meta); err != errBoom {
		t.Fatalf("lookup error: %v", err)
	}

	// 新身份 + 未开自动建号
	if _, err := NewService(&stubRepo{userByEmailLowerErr: ErrAuthUserNotFound}, cfg).LoginWithOIDC(ctx, identity, meta); err != ErrOIDCAutoProvisionDisabled {
		t.Fatalf("auto provision disabled: %v", err)
	}

	// 新身份 + 自动建号成功：角色走映射、用户名带随机后缀、密码为 64 位 hex 随机值
	autoCfg := oidcTestConfig(func(c *config.OIDCConfig) { c.AutoProvision = true })
	idpAdmin := OIDCIdentity{Subject: "sub-2", Email: "new@x.com", EmailVerified: true, Roles: []string{"idp-admins"}}
	provisioned := &stubRepo{userByEmailLowerErr: ErrAuthUserNotFound}
	result, err = NewService(provisioned, autoCfg).LoginWithOIDC(ctx, idpAdmin, meta)
	if err != nil || result == nil || result.SessionID == "" {
		t.Fatalf("provisioned: %+v %v", result, err)
	}
	if u := provisioned.createdUser; u == nil || !strings.HasPrefix(u.Username, "new_") ||
		u.Email != "new@x.com" || u.Role != "admin" || len(u.Password) != 64 {
		t.Fatalf("provisioned user: %+v", provisioned.createdUser)
	}

	// 建号撞唯一键 → 回落查询既有用户
	dup := &stubRepo{createUserErr: errors.New("duplicate key value")}
	dupCalls := 0
	dup.getByEmailLowerFn = func(ctx context.Context, email string) (*models.User, error) {
		dupCalls++
		if dupCalls == 1 {
			return nil, ErrAuthUserNotFound
		}
		return &models.User{ID: 11, Username: "existing", Status: "active"}, nil
	}
	result, err = NewService(dup, autoCfg).LoginWithOIDC(ctx, identity, meta)
	if err != nil || result == nil || result.User.ID != 11 {
		t.Fatalf("dup fallback: %+v %v", result, err)
	}
	// 回落查询也失败 → 保留原始唯一键错误（lookup 错误被丢弃）
	dupFail := &stubRepo{createUserErr: errors.New("duplicate key value")}
	dupFailCalls := 0
	dupFail.getByEmailLowerFn = func(ctx context.Context, email string) (*models.User, error) {
		dupFailCalls++
		if dupFailCalls == 1 {
			return nil, ErrAuthUserNotFound
		}
		return nil, errBoom
	}
	if _, err := NewService(dupFail, autoCfg).LoginWithOIDC(ctx, identity, meta); err == nil || !strings.Contains(err.Error(), "duplicate") {
		t.Fatalf("dup fallback failure: %v", err)
	}
	// 非唯一键错误原样透传
	plainErr := &stubRepo{userByEmailLowerErr: ErrAuthUserNotFound, createUserErr: errBoom}
	if _, err := NewService(plainErr, autoCfg).LoginWithOIDC(ctx, identity, meta); err != errBoom {
		t.Fatalf("plain insert error: %v", err)
	}

	// 建号成功但会话创建失败
	sessErr := &stubRepo{userByEmailLowerErr: ErrAuthUserNotFound, createSessionErr: errBoom}
	if _, err := NewService(sessErr, autoCfg).LoginWithOIDC(ctx, identity, meta); err != errBoom {
		t.Fatalf("session error: %v", err)
	}
}
