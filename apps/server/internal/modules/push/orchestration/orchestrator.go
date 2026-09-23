// Package orchestration 编排访客推送注册的写前校验与租户 scope 继承，
// 与 ticket 模块 orchestrator 同构（Prepare 两段式的前段）。
package orchestration

import (
	"context"
	"fmt"

	"gorm.io/gorm"

	"servify/apps/server/internal/models"
	pushcontract "servify/apps/server/internal/modules/push/contract"
)

// 支持的推送平台白名单：移动 SDK 双端各自上报固定值，未知值在写前拒绝
// （下发侧通道映射 ios=APNs / android=FCM，白名单保证映射穷举）。
var SupportedPlatforms = map[string]bool{
	"ios":     true,
	"android": true,
}

// PushOrchestrator 编排推送注册的写前准备（免认证访客 ctx，无认证 scope）。
type PushOrchestrator struct {
	db *gorm.DB
}

// NewPushOrchestrator 创建推送注册编排器。
func NewPushOrchestrator(db *gorm.DB) *PushOrchestrator {
	return &PushOrchestrator{db: db}
}

// PrepareRegisterPushToken 准备访客（免认证）推送注册（M3 移动 SDK 配套
// §10 #5）：会话必须已存在（SDK 握手 /api/v1/ws?session_id= 建连后服务端已
// 建 session 行），租户 scope 从 session 行原样继承（与 realtime 侧消息持久
// 化、访客工单创建同一口径）。platform 走白名单（未知值拒绝，通道映射因此
// 穷举）。token 幂等语义由落库层实现（同 session+platform 保活/换新）。
func (o *PushOrchestrator) PrepareRegisterPushToken(ctx context.Context, req *pushcontract.RegisterPushTokenRequest) (*models.PushToken, error) {
	if req == nil {
		return nil, fmt.Errorf("request required")
	}
	if !SupportedPlatforms[req.Platform] {
		return nil, fmt.Errorf("unsupported platform: %s", req.Platform)
	}
	var session models.Session
	if err := o.db.First(&session, "id = ?", req.SessionID).Error; err != nil {
		if err == gorm.ErrRecordNotFound {
			return nil, fmt.Errorf("session not found: %s", req.SessionID)
		}
		return nil, fmt.Errorf("session lookup failed: %w", err)
	}

	return &models.PushToken{
		SessionID:   req.SessionID,
		Platform:    req.Platform,
		Token:       req.Token,
		TenantID:    session.TenantID,
		WorkspaceID: session.WorkspaceID,
	}, nil
}
