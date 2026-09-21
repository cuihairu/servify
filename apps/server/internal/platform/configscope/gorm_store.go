package configscope

import (
	"context"
	"strings"

	"servify/apps/server/internal/config"
	"servify/apps/server/internal/models"

	"gopkg.in/yaml.v3"
	"gorm.io/gorm"
)

type ScopedConfigDocument struct {
	TenantID    string                          `json:"tenant_id,omitempty"`
	WorkspaceID string                          `json:"workspace_id,omitempty"`
	Portal      *config.PortalConfig            `json:"portal,omitempty"`
	OpenAI      *config.OpenAIConfig            `json:"openai,omitempty"`
	Dify        *config.DifyConfig              `json:"dify,omitempty"`
	WeKnora     *config.WeKnoraConfig           `json:"weknora,omitempty"`
	RagFlow     *config.RagFlowConfig           `json:"ragflow,omitempty"`
	SessionRisk *config.SessionRiskPolicyConfig `json:"session_risk,omitempty"`
}

type GormConfigStore struct {
	db *gorm.DB
}

func NewGormConfigStore(db *gorm.DB) *GormConfigStore {
	if db == nil {
		return nil
	}
	return &GormConfigStore{db: db}
}

func (s *GormConfigStore) GetTenantConfig(ctx context.Context, tenantID string) (*ScopedConfigDocument, bool, error) {
	if s == nil || s.db == nil || strings.TrimSpace(tenantID) == "" {
		return nil, false, nil
	}
	var row models.TenantConfig
	if err := s.db.WithContext(ctx).Where("tenant_id = ?", strings.TrimSpace(tenantID)).First(&row).Error; err != nil {
		if err == gorm.ErrRecordNotFound {
			return nil, false, nil
		}
		return nil, false, err
	}
	doc, err := tenantRowToDocument(row)
	if err != nil {
		return nil, false, err
	}
	return doc, true, nil
}

func (s *GormConfigStore) UpsertTenantConfig(ctx context.Context, tenantID string, payload ScopedConfigDocument) (*ScopedConfigDocument, error) {
	if s == nil || s.db == nil || strings.TrimSpace(tenantID) == "" {
		return nil, nil
	}
	var row models.TenantConfig
	if err := s.db.WithContext(ctx).Where("tenant_id = ?", strings.TrimSpace(tenantID)).First(&row).Error; err != nil && err != gorm.ErrRecordNotFound {
		return nil, err
	}
	row.TenantID = strings.TrimSpace(tenantID)
	if payload.Portal != nil {
		encoded, err := encodeConfig(*payload.Portal)
		if err != nil {
			return nil, err
		}
		row.PortalJSON = encoded
	}
	if payload.OpenAI != nil {
		encoded, err := encodeConfig(*payload.OpenAI)
		if err != nil {
			return nil, err
		}
		row.OpenAIJSON = encoded
	}
	if payload.Dify != nil {
		encoded, err := encodeConfig(*payload.Dify)
		if err != nil {
			return nil, err
		}
		row.DifyJSON = encoded
	}
	if payload.WeKnora != nil {
		encoded, err := encodeConfig(*payload.WeKnora)
		if err != nil {
			return nil, err
		}
		row.WeKnoraJSON = encoded
	}
	if payload.RagFlow != nil {
		encoded, err := encodeConfig(*payload.RagFlow)
		if err != nil {
			return nil, err
		}
		row.RagFlowJSON = encoded
	}
	if payload.SessionRisk != nil {
		encoded, err := encodeConfig(*payload.SessionRisk)
		if err != nil {
			return nil, err
		}
		row.SessionRiskJSON = encoded
	}
	if err := s.db.WithContext(ctx).Save(&row).Error; err != nil {
		return nil, err
	}
	doc, err := tenantRowToDocument(row)
	if err != nil {
		return nil, err
	}
	return doc, nil
}

func (s *GormConfigStore) GetWorkspaceConfig(ctx context.Context, tenantID, workspaceID string) (*ScopedConfigDocument, bool, error) {
	if s == nil || s.db == nil || strings.TrimSpace(tenantID) == "" || strings.TrimSpace(workspaceID) == "" {
		return nil, false, nil
	}
	var row models.WorkspaceConfig
	if err := s.db.WithContext(ctx).Where("tenant_id = ? AND workspace_id = ?", strings.TrimSpace(tenantID), strings.TrimSpace(workspaceID)).First(&row).Error; err != nil {
		if err == gorm.ErrRecordNotFound {
			return nil, false, nil
		}
		return nil, false, err
	}
	doc, err := workspaceRowToDocument(row)
	if err != nil {
		return nil, false, err
	}
	return doc, true, nil
}

func (s *GormConfigStore) UpsertWorkspaceConfig(ctx context.Context, tenantID, workspaceID string, payload ScopedConfigDocument) (*ScopedConfigDocument, error) {
	if s == nil || s.db == nil || strings.TrimSpace(tenantID) == "" || strings.TrimSpace(workspaceID) == "" {
		return nil, nil
	}
	var row models.WorkspaceConfig
	if err := s.db.WithContext(ctx).Where("tenant_id = ? AND workspace_id = ?", strings.TrimSpace(tenantID), strings.TrimSpace(workspaceID)).First(&row).Error; err != nil && err != gorm.ErrRecordNotFound {
		return nil, err
	}
	row.TenantID = strings.TrimSpace(tenantID)
	row.WorkspaceID = strings.TrimSpace(workspaceID)
	if payload.Portal != nil {
		encoded, err := encodeConfig(*payload.Portal)
		if err != nil {
			return nil, err
		}
		row.PortalJSON = encoded
	}
	if payload.OpenAI != nil {
		encoded, err := encodeConfig(*payload.OpenAI)
		if err != nil {
			return nil, err
		}
		row.OpenAIJSON = encoded
	}
	if payload.Dify != nil {
		encoded, err := encodeConfig(*payload.Dify)
		if err != nil {
			return nil, err
		}
		row.DifyJSON = encoded
	}
	if payload.WeKnora != nil {
		encoded, err := encodeConfig(*payload.WeKnora)
		if err != nil {
			return nil, err
		}
		row.WeKnoraJSON = encoded
	}
	if payload.RagFlow != nil {
		encoded, err := encodeConfig(*payload.RagFlow)
		if err != nil {
			return nil, err
		}
		row.RagFlowJSON = encoded
	}
	if payload.SessionRisk != nil {
		encoded, err := encodeConfig(*payload.SessionRisk)
		if err != nil {
			return nil, err
		}
		row.SessionRiskJSON = encoded
	}
	if err := s.db.WithContext(ctx).Save(&row).Error; err != nil {
		return nil, err
	}
	doc, err := workspaceRowToDocument(row)
	if err != nil {
		return nil, err
	}
	return doc, nil
}

func tenantRowToDocument(row models.TenantConfig) (*ScopedConfigDocument, error) {
	doc := &ScopedConfigDocument{TenantID: row.TenantID}
	if cfg, ok, err := decodeConfig[config.PortalConfig](row.PortalJSON); err != nil {
		return nil, err
	} else if ok {
		doc.Portal = &cfg
	}
	if cfg, ok, err := decodeConfig[config.OpenAIConfig](row.OpenAIJSON); err != nil {
		return nil, err
	} else if ok {
		doc.OpenAI = &cfg
	}
	if cfg, ok, err := decodeConfig[config.DifyConfig](row.DifyJSON); err != nil {
		return nil, err
	} else if ok {
		doc.Dify = &cfg
	}
	if cfg, ok, err := decodeConfig[config.WeKnoraConfig](row.WeKnoraJSON); err != nil {
		return nil, err
	} else if ok {
		doc.WeKnora = &cfg
	}
	if cfg, ok, err := decodeConfig[config.RagFlowConfig](row.RagFlowJSON); err != nil {
		return nil, err
	} else if ok {
		doc.RagFlow = &cfg
	}
	if cfg, ok, err := decodeConfig[config.SessionRiskPolicyConfig](row.SessionRiskJSON); err != nil {
		return nil, err
	} else if ok {
		doc.SessionRisk = &cfg
	}
	return doc, nil
}

func workspaceRowToDocument(row models.WorkspaceConfig) (*ScopedConfigDocument, error) {
	doc := &ScopedConfigDocument{TenantID: row.TenantID, WorkspaceID: row.WorkspaceID}
	if cfg, ok, err := decodeConfig[config.PortalConfig](row.PortalJSON); err != nil {
		return nil, err
	} else if ok {
		doc.Portal = &cfg
	}
	if cfg, ok, err := decodeConfig[config.OpenAIConfig](row.OpenAIJSON); err != nil {
		return nil, err
	} else if ok {
		doc.OpenAI = &cfg
	}
	if cfg, ok, err := decodeConfig[config.DifyConfig](row.DifyJSON); err != nil {
		return nil, err
	} else if ok {
		doc.Dify = &cfg
	}
	if cfg, ok, err := decodeConfig[config.WeKnoraConfig](row.WeKnoraJSON); err != nil {
		return nil, err
	} else if ok {
		doc.WeKnora = &cfg
	}
	if cfg, ok, err := decodeConfig[config.RagFlowConfig](row.RagFlowJSON); err != nil {
		return nil, err
	} else if ok {
		doc.RagFlow = &cfg
	}
	if cfg, ok, err := decodeConfig[config.SessionRiskPolicyConfig](row.SessionRiskJSON); err != nil {
		return nil, err
	} else if ok {
		doc.SessionRisk = &cfg
	}
	return doc, nil
}

// yamlMarshal 是包级 seam（默认 yaml.Marshal）：五个 Scoped 配置结构体只含
// 基础类型字段，且 yaml.v3 对不可序列化类型直接 panic 而不返回 error，
// 错误分支生产不可达；测试注入失败以保持各 Upsert 路径的防御性传播。
var yamlMarshal = yaml.Marshal

func encodeConfig[T any](value T) (string, error) {
	body, err := yamlMarshal(value)
	if err != nil {
		return "", err
	}
	return string(body), nil
}
