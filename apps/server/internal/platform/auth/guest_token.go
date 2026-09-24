// 访客 token（guest token，§10 #2 / 设计文档 D6）：宿主后端以 service
// 凭证调用 /api/v1/guest/session 换取的短期 HS256 JWT，payload 绑定
// session_id（"sid"）与过期时间；移动 SDK 握手 /api/v1/ws?access_token=
// 时由服务端校验签名、时效与会话绑定。签发与校验复用 jwt.secret（同一
// 信任域：本服务自签自验），required + 默认 secret 组合由
// config.InsecureDefaults 告警（production/staging 零容忍断启动）。
package auth

import (
	"crypto/hmac"
	"crypto/sha256"
	"encoding/base64"
	"encoding/json"
	"errors"
	"strings"
	"time"
)

// GuestTokenClaims 是访客 token 的最小声明面。
type GuestTokenClaims struct {
	SessionID string
	ExpiresAt int64
}

// SignGuestToken 签发绑定 sessionID 的访客 token（HS256）。ttl 为有效期，
// now 由调用方传入（测试注入）；返回 token 与过期 Unix 秒。
func SignGuestToken(secret, sessionID string, ttl time.Duration, now time.Time) (string, int64, error) {
	if strings.TrimSpace(secret) == "" {
		return "", 0, errors.New("guest token secret is required")
	}
	if strings.TrimSpace(sessionID) == "" {
		return "", 0, errors.New("guest token session_id is required")
	}
	if ttl <= 0 {
		return "", 0, errors.New("guest token ttl must be positive")
	}
	expiresAt := now.Add(ttl).Unix()
	payload := map[string]interface{}{
		"typ": "guest",
		"sid": sessionID,
		"iat": now.Unix(),
		"exp": expiresAt,
	}
	// 字面量/纯标量 payload 的 Marshal 可证不失败，契约式忽略（与
	// modules/auth/application createHS256JWT 同款）。
	headerJSON, _ := json.Marshal(map[string]string{"alg": "HS256", "typ": "JWT"})
	payloadJSON, _ := json.Marshal(payload)
	enc := base64.RawURLEncoding.EncodeToString
	signing := enc(headerJSON) + "." + enc(payloadJSON)
	mac := hmac.New(sha256.New, []byte(secret))
	mac.Write([]byte(signing))
	return signing + "." + enc(mac.Sum(nil)), expiresAt, nil
}

// ParseGuestToken 校验访客 token（签名 + alg + exp 等时间约束复用
// Validator）并提取会话绑定；typ 非 guest 或缺 sid 均拒绝。
func ParseGuestToken(secret, token string, now time.Time) (*GuestTokenClaims, error) {
	if strings.TrimSpace(secret) == "" {
		return nil, errors.New("guest token secret is required")
	}
	validator := Validator{Secret: secret, Now: func() time.Time { return now }}
	payload, err := validator.ValidateToken(token)
	if err != nil {
		return nil, err
	}
	if typ, _ := payload["typ"].(string); typ != "guest" {
		return nil, errors.New("token is not a guest token")
	}
	sessionID, _ := payload["sid"].(string)
	if strings.TrimSpace(sessionID) == "" {
		return nil, errors.New("guest token missing session binding")
	}
	expiresAt, _ := payload["exp"].(float64)
	if expiresAt <= 0 {
		return nil, errors.New("guest token missing expiry")
	}
	return &GuestTokenClaims{SessionID: sessionID, ExpiresAt: int64(expiresAt)}, nil
}
