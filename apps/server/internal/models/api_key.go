package models

import "time"

// APIKey 开放平台服务密钥（service principal 凭据）。
// 明文只在签发响应中出现一次；库内只存 sha256 哈希，prefix 用于列表识别。
type APIKey struct {
	ID          uint       `gorm:"primaryKey" json:"id"`
	Name        string     `gorm:"not null" json:"name"`
	Prefix      string     `gorm:"size:16;not null;index" json:"prefix"`
	KeyHash     string     `gorm:"size:64;not null;uniqueIndex" json:"-"`
	TenantID    string     `gorm:"index" json:"tenant_id"`
	WorkspaceID string     `json:"workspace_id"`
	Scopes      string     `json:"scopes"` // 逗号分隔权限，空 = 默认只读面
	CreatedBy   string     `json:"created_by"`
	LastUsedAt  *time.Time `json:"last_used_at"`
	ExpiresAt   *time.Time `json:"expires_at"`
	RevokedAt   *time.Time `json:"revoked_at"`
	CreatedAt   time.Time  `json:"created_at"`
	UpdatedAt   time.Time  `json:"updated_at"`
}
