package application

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"regexp"
	"strings"
	"time"

	"servify/apps/server/internal/config"
	"servify/apps/server/internal/models"
	platformauth "servify/apps/server/internal/platform/auth"

	"github.com/pquerna/otp"
	"github.com/pquerna/otp/totp"
	"golang.org/x/crypto/bcrypt"
	"gorm.io/gorm"
)

// TOTP 两步验证子服务：绑定/解绑/恢复码 + 登录挑战步。
//
// 语义要点：
//   - Setup 只生成不落库，Enable 时客户端回传 secret 验证持有，免 pending 态
//   - 恢复码只存 sha256 哈希，核销是原子 UPDATE（used_at IS NULL 才置位），
//     并发下同一码不可能被使用两次
//   - 挑战 JWT token_use="2fa_challenge"，绑定发起方 ip/ua 指纹、短 TTL；
//     AuthMiddleware 拒绝非 access 的 token_use，挑战 token 换不到任何受保护资源
//   - OIDC 登录不触发挑战（IdP 的 MFA 承担第二因子）；OIDC 用户可在登录后自行启用
//   - cfg.Security.TwoFactor.Enabled 是 kill-switch：关闭时挑战步整体跳过
//     （已启用用户降级为单因子直登），setup/enable 端点拒绝，disable 不受限

const (
	challengeTokenUse = "2fa_challenge"
	recoveryCodeCount = 10
)

var (
	ErrTwoFactorDisabled       = errors.New("two-factor auth is disabled")
	ErrTwoFactorAlreadyEnabled = errors.New("two-factor auth is already enabled")
	ErrTwoFactorNotEnabled     = errors.New("two-factor auth is not enabled")
	ErrAuthInvalid2FACode      = errors.New("invalid two-factor code")
	ErrAuthInvalid2FAChallenge = errors.New("invalid two-factor challenge")
)

// challengeFingerprint 短哈希绑定挑战发起方的 ip/ua，防挑战 token 被换环境重放。
func challengeFingerprint(values ...string) string {
	h := sha256.Sum256([]byte(strings.Join(values, "\x00")))
	return hex.EncodeToString(h[:8])
}

// twoFactorCodeShape 接受 6 位 TOTP 码与归一化后的 8 位恢复码。
var twoFactorCodeShape = regexp.MustCompile(`^\d{6,8}$`)

// TwoFactorSetup 是绑定流程第一步的产物：secret 与 otpauth URI 由用户在
// 认证器 App 中录入/扫码；此时什么都不落库，验证持有后才写入。
type TwoFactorSetup struct {
	Secret     string `json:"secret"`
	OTPAuthURI string `json:"otpauth_uri"`
}

type TwoFactorVerifyInput struct {
	ChallengeToken string
	Code           string
}

// LoginOutcome 区分单因子直登与两步挑战：TwoFactorRequired 时 Result 为 nil，
// 调用方必须走 2FA verify 端点换取会话。
type LoginOutcome struct {
	Result            *AuthResult
	TwoFactorRequired bool
	ChallengeToken    string
	ExpiresIn         int
}

// SetupTwoFactor 生成绑定用 secret 与 otpauth URI（不落库）。
func (s *AuthService) SetupTwoFactor(ctx context.Context, userID uint) (*TwoFactorSetup, error) {
	if s == nil || s.db == nil {
		return nil, gorm.ErrInvalidDB
	}
	if err := s.requireTwoFactorEnabled(); err != nil {
		return nil, err
	}
	user, err := s.GetCurrentUser(ctx, userID)
	if err != nil {
		return nil, err
	}
	if user.TotpEnabled {
		return nil, ErrTwoFactorAlreadyEnabled
	}
	account := user.Email
	if strings.TrimSpace(account) == "" {
		account = user.Username
	}
	key, err := totp.Generate(totp.GenerateOpts{
		Issuer:      s.twoFactorIssuer(),
		AccountName: account,
		Period:      30,
		Digits:      otp.DigitsSix,
		Algorithm:   otp.AlgorithmSHA1,
	})
	if err != nil {
		return nil, err
	}
	return &TwoFactorSetup{Secret: key.Secret(), OTPAuthURI: key.URL()}, nil
}

// EnableTwoFactor 验证 code 证明持有 secret 后写入绑定，并生成一批恢复码。
// 返回的明文恢复码只出现这一次；同时推进 TokenValidAfter 使既有 refresh token 失效。
func (s *AuthService) EnableTwoFactor(ctx context.Context, userID uint, secret, code string) ([]string, error) {
	if s == nil || s.db == nil {
		return nil, gorm.ErrInvalidDB
	}
	if err := s.requireTwoFactorEnabled(); err != nil {
		return nil, err
	}
	secret = normalizeTotpSecret(secret)
	if secret == "" {
		return nil, ErrInvalidAuthInput
	}
	if !s.verifyTwoFactorCode(secret, code) {
		return nil, ErrAuthInvalid2FACode
	}
	user, err := s.GetCurrentUser(ctx, userID)
	if err != nil {
		return nil, err
	}
	if user.TotpEnabled {
		return nil, ErrTwoFactorAlreadyEnabled
	}
	codes, hashes, err := generateRecoveryCodes(recoveryCodeCount)
	if err != nil {
		return nil, err
	}
	now := time.Now()
	if err := s.db.WithContext(ctx).Model(&models.User{}).Where("id = ?", userID).Updates(map[string]interface{}{
		"totp_secret":       secret,
		"totp_enabled":      true,
		"totp_enabled_at":   now,
		"token_valid_after": now,
	}).Error; err != nil {
		return nil, err
	}
	if err := s.replaceRecoveryCodes(ctx, userID, hashes); err != nil {
		return nil, err
	}
	return codes, nil
}

// DisableTwoFactor 解除绑定：本地密码用户必须验密码（OIDC 账号无本地密码则跳过），
// 再用当前 TOTP 码或一个未用恢复码证明是本人操作；解绑后删除全部恢复码并踢会话。
// 不受 kill-switch 限制——运营关闭开关后用户仍可解除绑定。
func (s *AuthService) DisableTwoFactor(ctx context.Context, userID uint, password, code string) error {
	if s == nil || s.db == nil {
		return gorm.ErrInvalidDB
	}
	user, err := s.GetCurrentUser(ctx, userID)
	if err != nil {
		return err
	}
	if !user.TotpEnabled {
		return ErrTwoFactorNotEnabled
	}
	if user.Password != "" {
		if err := bcrypt.CompareHashAndPassword([]byte(user.Password), []byte(password)); err != nil {
			return ErrAuthInvalidCredentials
		}
	}
	if !s.verifyTwoFactorCode(user.TotpSecret, code) && !s.consumeRecoveryCode(ctx, userID, code) {
		return ErrAuthInvalid2FACode
	}
	now := time.Now()
	if err := s.db.WithContext(ctx).Model(&models.User{}).Where("id = ?", userID).Updates(map[string]interface{}{
		"totp_secret":       "",
		"totp_enabled":      false,
		"totp_enabled_at":   nil,
		"token_valid_after": now,
	}).Error; err != nil {
		return err
	}
	return s.db.WithContext(ctx).Where("user_id = ?", userID).Delete(&models.UserRecoveryCode{}).Error
}

// RegenerateRecoveryCodes 以当前 TOTP 码为凭据重发一批恢复码（旧的作废）。
func (s *AuthService) RegenerateRecoveryCodes(ctx context.Context, userID uint, code string) ([]string, error) {
	if s == nil || s.db == nil {
		return nil, gorm.ErrInvalidDB
	}
	user, err := s.GetCurrentUser(ctx, userID)
	if err != nil {
		return nil, err
	}
	if !user.TotpEnabled {
		return nil, ErrTwoFactorNotEnabled
	}
	if !s.verifyTwoFactorCode(user.TotpSecret, code) {
		return nil, ErrAuthInvalid2FACode
	}
	codes, hashes, err := generateRecoveryCodes(recoveryCodeCount)
	if err != nil {
		return nil, err
	}
	if err := s.replaceRecoveryCodes(ctx, userID, hashes); err != nil {
		return nil, err
	}
	return codes, nil
}

// RecoveryCodesRemaining 返回尚未核销的恢复码数量（码本身不可回读）。
func (s *AuthService) RecoveryCodesRemaining(ctx context.Context, userID uint) (int64, error) {
	if s == nil || s.db == nil {
		return 0, gorm.ErrInvalidDB
	}
	var count int64
	err := s.db.WithContext(ctx).Model(&models.UserRecoveryCode{}).
		Where("user_id = ? AND used_at IS NULL", userID).Count(&count).Error
	return count, err
}

// VerifyTwoFactorLogin 完成挑战步：验证挑战 JWT（签名/用途/指纹）与第二因子
// （TOTP 码或恢复码），通过后创建会话并签发正式 token。
func (s *AuthService) VerifyTwoFactorLogin(ctx context.Context, req TwoFactorVerifyInput, meta AuthSessionMetadata) (*AuthResult, error) {
	if s == nil || s.db == nil {
		return nil, gorm.ErrInvalidDB
	}
	userID, err := s.validateChallengeToken(req.ChallengeToken, meta)
	if err != nil {
		return nil, err
	}
	user, err := s.GetCurrentUser(ctx, userID)
	if err != nil {
		return nil, ErrAuthInvalid2FAChallenge
	}
	if user.Status != "active" {
		return nil, ErrAuthUserDisabled
	}
	if !user.TotpEnabled {
		return nil, ErrAuthInvalid2FAChallenge
	}
	if !s.verifyTwoFactorCode(user.TotpSecret, req.Code) && !s.consumeRecoveryCode(ctx, user.ID, req.Code) {
		return nil, ErrAuthInvalid2FACode
	}
	session, err := s.createAuthSession(ctx, user.ID, meta)
	if err != nil {
		return nil, err
	}
	return s.buildAuthResult(ctx, user, session)
}

// createChallengeToken 签发短命挑战 JWT：不带 roles/session，且绑定发起方指纹。
func (s *AuthService) createChallengeToken(user *models.User, meta AuthSessionMetadata) (string, int, error) {
	ttl := s.challengeTTL()
	now := time.Now()
	token, err := hookCreateHS256JWT(map[string]interface{}{
		"iat":       now.Unix(),
		"sub":       user.ID,
		"jti":       newAuthTokenID(),
		"user_id":   user.ID,
		"exp":       now.Add(ttl).Unix(),
		"token_use": challengeTokenUse,
		"ip_fp":     challengeFingerprint(meta.ClientIP),
		"ua_fp":     challengeFingerprint(meta.UserAgent),
	}, s.config.JWT.Secret)
	if err != nil {
		return "", 0, err
	}
	return token, int(ttl.Seconds()), nil
}

func (s *AuthService) validateChallengeToken(token string, meta AuthSessionMetadata) (uint, error) {
	payload, err := (platformauth.Validator{Secret: s.config.JWT.Secret}).ValidateToken(strings.TrimSpace(token))
	if err != nil {
		return 0, ErrAuthInvalid2FAChallenge
	}
	if use, _ := payload["token_use"].(string); use != challengeTokenUse {
		return 0, ErrAuthInvalid2FAChallenge
	}
	if fp, _ := payload["ip_fp"].(string); fp != "" && fp != challengeFingerprint(meta.ClientIP) {
		return 0, ErrAuthInvalid2FAChallenge
	}
	if fp, _ := payload["ua_fp"].(string); fp != "" && fp != challengeFingerprint(meta.UserAgent) {
		return 0, ErrAuthInvalid2FAChallenge
	}
	userID, ok := authNumericClaim(payload, "user_id", "sub")
	if !ok || userID == 0 {
		return 0, ErrAuthInvalid2FAChallenge
	}
	return uint(userID), nil
}

// verifyTwoFactorCode 校验 6 位 TOTP 码（±1 个周期容差；窗口内重放 v1 不设防，
// 恢复码路径有原子核销兜底）。
func (s *AuthService) verifyTwoFactorCode(secret, code string) bool {
	secret = normalizeTotpSecret(secret)
	code = strings.TrimSpace(code)
	if secret == "" || !twoFactorCodeShape.MatchString(code) {
		return false
	}
	ok, err := totp.ValidateCustom(code, secret, time.Now(), totp.ValidateOpts{
		Period:    30,
		Skew:      1,
		Digits:    otp.DigitsSix,
		Algorithm: otp.AlgorithmSHA1,
	})
	return err == nil && ok
}

// consumeRecoveryCode 原子核销恢复码：归一化（小写、去连字符）后比对哈希，
// 只有未用过的码才置 used_at。
func (s *AuthService) consumeRecoveryCode(ctx context.Context, userID uint, code string) bool {
	normalized := normalizeRecoveryCode(code)
	if normalized == "" {
		return false
	}
	sum := sha256.Sum256([]byte(normalized))
	result := s.db.WithContext(ctx).Model(&models.UserRecoveryCode{}).
		Where("user_id = ? AND code_hash = ? AND used_at IS NULL", userID, hex.EncodeToString(sum[:])).
		Update("used_at", time.Now())
	return result.Error == nil && result.RowsAffected > 0
}

func (s *AuthService) replaceRecoveryCodes(ctx context.Context, userID uint, hashes []string) error {
	return s.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		if err := tx.Where("user_id = ?", userID).Delete(&models.UserRecoveryCode{}).Error; err != nil {
			return err
		}
		now := time.Now()
		rows := make([]models.UserRecoveryCode, 0, len(hashes))
		for _, h := range hashes {
			rows = append(rows, models.UserRecoveryCode{UserID: userID, CodeHash: h, CreatedAt: now})
		}
		return tx.Create(&rows).Error
	})
}

func (s *AuthService) requireTwoFactorEnabled() error {
	if s.config == nil || !s.config.Security.TwoFactor.Enabled {
		return ErrTwoFactorDisabled
	}
	return nil
}

func (s *AuthService) twoFactorIssuer() string {
	if s.config != nil && strings.TrimSpace(s.config.Security.TwoFactor.Issuer) != "" {
		return strings.TrimSpace(s.config.Security.TwoFactor.Issuer)
	}
	return "Servify"
}

func (s *AuthService) challengeTTL() time.Duration {
	if s.config != nil && s.config.Security.TwoFactor.ChallengeTTL > 0 {
		return s.config.Security.TwoFactor.ChallengeTTL
	}
	return 5 * time.Minute
}

// twoFactorChallengeEnabled 决定登录是否进入挑战步：kill-switch 开且用户已绑定。
// 开关关闭时已启用用户降级为单因子直登（密码仍必验）。
func (s *AuthService) twoFactorChallengeEnabled(cfg *config.Config, user *models.User) bool {
	return cfg != nil && cfg.Security.TwoFactor.Enabled && user.TotpEnabled
}

// normalizeTotpSecret 归一化 base32 secret：去空格、去连字符、大写。
func normalizeTotpSecret(secret string) string {
	secret = strings.ToUpper(strings.TrimSpace(secret))
	secret = strings.ReplaceAll(secret, " ", "")
	secret = strings.ReplaceAll(secret, "-", "")
	return secret
}

// normalizeRecoveryCode 归一化恢复码：小写、去连字符与空格，必须是 8 位 hex；
// 哈希一律按该归一化形态计算。
func normalizeRecoveryCode(code string) string {
	code = strings.ToLower(strings.TrimSpace(code))
	code = strings.ReplaceAll(code, "-", "")
	code = strings.ReplaceAll(code, " ", "")
	if len(code) != 8 {
		return ""
	}
	for _, r := range code {
		if !((r >= '0' && r <= '9') || (r >= 'a' && r <= 'f')) {
			return ""
		}
	}
	return code
}

// generateRecoveryCodes 生成 `xxxx-xxxx` 展示形式的一次性恢复码；
// 返回明文（仅此一次，服务端不留底）与 sha256 哈希（落库）。
func generateRecoveryCodes(n int) ([]string, []string, error) {
	codes := make([]string, 0, n)
	hashes := make([]string, 0, n)
	for i := 0; i < n; i++ {
		var buf [4]byte
		if _, err := hookRandRead(buf[:]); err != nil {
			return nil, nil, fmt.Errorf("generate recovery code: %w", err)
		}
		raw := hex.EncodeToString(buf[:])
		codes = append(codes, raw[:4]+"-"+raw[4:])
		sum := sha256.Sum256([]byte(raw))
		hashes = append(hashes, hex.EncodeToString(sum[:]))
	}
	return codes, hashes, nil
}
