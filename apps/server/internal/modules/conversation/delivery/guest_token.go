// 访客 token 签发与 WS 握手校验（M3 移动 SDK 配套 §10 #2 / 设计文档 D6）：
// 宿主后端持 service API key 调 /api/v1/guest/session 换取绑定 session_id
// 的短期 HS256 token，SDK 握手 /api/v1/ws?access_token= 时由 hub 校验。
// 签发与校验复用 jwt.secret（同一信任域：本服务自签自验），签发不要求
// session 行已存在——行在首条消息持久化时才建，guest token 绑定的是
// 客户端 session_id 字符串本身。
package delivery

import (
	"errors"
	"strings"
	"time"

	platformauth "servify/apps/server/internal/platform/auth"
)

// DefaultGuestTokenTTL 是访客 token 的默认有效期；配置 ttl 非正值归一到它。
const DefaultGuestTokenTTL = 24 * time.Hour

// GuestTokenIssuer 是访客 token 签发的窄契约（handlers 层依赖此接口）。
type GuestTokenIssuer interface {
	IssueGuestToken(sessionID string) (token string, expiresAt int64, err error)
}

// GuestTokenService 是签发的生产实现：无状态（secret + ttl 足够），不落库。
type GuestTokenService struct {
	secret string
	ttl    time.Duration
}

// NewGuestTokenService 创建签发服务。secret 空白直接拒绝（装配层兜底：
// production/staging 的 config gate 已拦默认 secret，这里防绕过 config
// 直建 Runtime 的路径）；ttl 非正值归一到 DefaultGuestTokenTTL。
func NewGuestTokenService(secret string, ttl time.Duration) (*GuestTokenService, error) {
	if strings.TrimSpace(secret) == "" {
		return nil, errors.New("guest token issuer requires a non-empty jwt secret")
	}
	if ttl <= 0 {
		ttl = DefaultGuestTokenTTL
	}
	return &GuestTokenService{secret: secret, ttl: ttl}, nil
}

// IssueGuestToken 签发绑定 sessionID 的访客 token，返回 token 与过期 Unix 秒。
func (s *GuestTokenService) IssueGuestToken(sessionID string) (string, int64, error) {
	return platformauth.SignGuestToken(s.secret, sessionID, s.ttl, time.Now())
}

// NewGuestTokenValidator 返回 WS 握手校验闭包（hub.SetTokenValidator 注入，
// 签名 func(sessionID, accessToken string) error）：验签 + 时效 + 会话绑定
// 三层——token 合法但绑定其他 session 同样拒绝，防止拿 A 会话 token 握 B 会话。
func NewGuestTokenValidator(secret string) func(sessionID, accessToken string) error {
	return func(sessionID, accessToken string) error {
		claims, err := platformauth.ParseGuestToken(secret, accessToken, time.Now())
		if err != nil {
			return err
		}
		if claims.SessionID != sessionID {
			return errors.New("guest token is bound to a different session")
		}
		return nil
	}
}
