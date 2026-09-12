package auth

import (
	"context"
	"crypto/rand"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"strings"
	"time"
)

// API Key 只读开放面的默认权限集。
const (
	PermissionTicketsRead       = "tickets.read"
	PermissionConversationsRead = "conversations.read"
)

// APIKeyDefaultPermissions 未指定 scopes 时授予的默认只读面。
var APIKeyDefaultPermissions = []string{PermissionTicketsRead, PermissionConversationsRead}

// APIKeyPrefix 明文密钥前缀，用于快速识别与展示。
const APIKeyPrefix = "sv_"

var (
	// ErrAPIKeyInvalid 密钥不存在、格式错误、已吊销或已过期（统一拒绝，不区分原因）。
	ErrAPIKeyInvalid = errors.New("invalid api key")
)

// APIKeyRecord 认证所需的 API Key 最小投影。
type APIKeyRecord struct {
	ID          uint
	TenantID    string
	WorkspaceID string
	Scopes      []string
	ExpiresAt   *time.Time
	RevokedAt   *time.Time
}

// APIKeyResolver 按明文密钥解析凭据（实现在 DB 侧，platform/auth 保持无gorm依赖）。
type APIKeyResolver interface {
	ResolveAPIKey(ctx context.Context, plaintext string) (*APIKeyRecord, error)
}

// GenerateAPIKey 生成新明文密钥 "sv_" + 40 hex 字符。
// 返回 (明文, prefix, hash)；明文仅此一次可见。
func GenerateAPIKey() (plaintext, prefix, hash string, err error) {
	raw := make([]byte, 20)
	if _, err = rand.Read(raw); err != nil {
		return "", "", "", err
	}
	plaintext = APIKeyPrefix + hex.EncodeToString(raw)
	prefix, hash = APIKeyPrefix+hex.EncodeToString(raw)[:8], APIKeyHash(plaintext)
	return plaintext, prefix, hash, nil
}

// APIKeyHash sha256 hex（高熵随机密钥，抗穷举，无需慢哈希/盐）。
func APIKeyHash(plaintext string) string {
	sum := sha256.Sum256([]byte(plaintext))
	return hex.EncodeToString(sum[:])
}

// APIKeyScopesOrDefault 解析 scopes 字段；空 = 默认只读面。
func APIKeyScopesOrDefault(scopes []string) []string {
	cleaned := make([]string, 0, len(scopes))
	for _, s := range scopes {
		if s = strings.TrimSpace(s); s != "" {
			cleaned = append(cleaned, s)
		}
	}
	if len(cleaned) == 0 {
		return APIKeyDefaultPermissions
	}
	return cleaned
}
