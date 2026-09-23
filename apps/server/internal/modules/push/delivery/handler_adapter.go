// Package delivery 桥接 HTTP handlers 与推送注册编排器（免认证访客通道
// 唯一写入口，窄契约独立于管理面膨胀面，与 ticket VisitorTicketService 同构）。
package delivery

import (
	"context"
	"time"

	"gorm.io/gorm"

	"servify/apps/server/internal/models"
	pushcontract "servify/apps/server/internal/modules/push/contract"
	pushorchestration "servify/apps/server/internal/modules/push/orchestration"
)

// PushRegistrationService 是访客推送注册的窄契约（M3 移动 SDK 配套 §10 #5）：
// 免认证 REST 通道唯一可用的推送注册写入口。
type PushRegistrationService interface {
	RegisterPushToken(ctx context.Context, req *pushcontract.RegisterPushTokenRequest) (*pushcontract.PushTokenRegistration, error)
}

// PushRegistrationAdapter 桥接 handler 与编排器并落库：同 (session_id,
// platform) 幂等——重复注册保活（更新 token 与 updated_at）、换 token 更新，
// SDK 侧 provider 每次 connect 均可重报而无需去重。scope 已由编排器从
// session 继承落在行上；并发注册不追求跨实例幂等（访客单客户端场景，
// unique 索引窗口内冲突按重读更新兜底）。
type PushRegistrationAdapter struct {
	orchestrator *pushorchestration.PushOrchestrator
	db           *gorm.DB
}

// NewPushRegistrationAdapter 创建推送注册适配器。
func NewPushRegistrationAdapter(db *gorm.DB) *PushRegistrationAdapter {
	return &PushRegistrationAdapter{
		orchestrator: pushorchestration.NewPushOrchestrator(db),
		db:           db,
	}
}

// RegisterPushToken 注册（或保活）推送 token 并返回摘要（不含 token 全文）。
func (a *PushRegistrationAdapter) RegisterPushToken(ctx context.Context, req *pushcontract.RegisterPushTokenRequest) (*pushcontract.PushTokenRegistration, error) {
	prepared, err := a.orchestrator.PrepareRegisterPushToken(ctx, req)
	if err != nil {
		return nil, err
	}

	var existing models.PushToken
	err = a.db.WithContext(ctx).
		Where("session_id = ? AND platform = ?", prepared.SessionID, prepared.Platform).
		First(&existing).Error
	switch {
	case err == nil:
		// 保活/换 token：token 与 updated_at 始终刷新；摘要沿用已存在的行 id
		// （map 更新不走 GORM 结构体回填，也不用 pg 方言的 Returning——
		// sqlite 测试面与 pg 生产面同语义）。
		now := time.Now()
		result := a.db.WithContext(ctx).Model(&models.PushToken{}).
			Where("session_id = ? AND platform = ?", prepared.SessionID, prepared.Platform).
			Updates(map[string]interface{}{
				"token":      prepared.Token,
				"updated_at": now,
			})
		if result.Error != nil {
			return nil, result.Error
		}
		return &pushcontract.PushTokenRegistration{
			ID:        existing.ID,
			SessionID: prepared.SessionID,
			Platform:  prepared.Platform,
			UpdatedAt: now,
		}, nil
	case err == gorm.ErrRecordNotFound:
		if err := a.db.WithContext(ctx).Create(prepared).Error; err != nil {
			return nil, err
		}
		return registrationOf(prepared), nil
	default:
		return nil, err
	}
}

// registrationOf 摘要投影：id/session/platform/updated_at，token 不出服务边界。
func registrationOf(m *models.PushToken) *pushcontract.PushTokenRegistration {
	return &pushcontract.PushTokenRegistration{
		ID:        m.ID,
		SessionID: m.SessionID,
		Platform:  m.Platform,
		UpdatedAt: m.UpdatedAt,
	}
}
