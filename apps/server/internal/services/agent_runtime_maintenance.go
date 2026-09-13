package services

import (
	"context"
	"time"

	agentapp "servify/apps/server/internal/modules/agent/application"

	"github.com/sirupsen/logrus"
)

type agentRuntimeMaintenance struct {
	logger *logrus.Logger
	module *agentapp.Service
	// interval 供测试注入毫秒级 tick；零值取默认 1 分钟。
	interval time.Duration
}

func newAgentRuntimeMaintenance(logger *logrus.Logger, module *agentapp.Service) *agentRuntimeMaintenance {
	return &agentRuntimeMaintenance{
		logger: logger,
		module: module,
	}
}

func (m *agentRuntimeMaintenance) Start() {
	interval := m.interval
	if interval <= 0 {
		interval = time.Minute
	}
	ticker := time.NewTicker(interval)
	defer ticker.Stop()

	for range ticker.C {
		m.cleanupInactiveAgents(context.Background(), 5*time.Minute)
	}
}

func (m *agentRuntimeMaintenance) cleanupInactiveAgents(ctx context.Context, timeout time.Duration) {
	runtimes := m.module.GetOnlineAgents(ctx)
	for _, item := range runtimes {
		if item.LastActivity.IsZero() {
			continue
		}
		if time.Since(item.LastActivity) > timeout {
			m.logger.Warnf("Agent %d appears inactive, marking as away", item.UserID)
			_ = m.module.MarkAway(ctx, item.UserID)
		}
	}
}
