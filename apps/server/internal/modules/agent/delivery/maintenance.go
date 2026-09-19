package delivery

import (
	"context"
	"time"

	agentapp "servify/apps/server/internal/modules/agent/application"

	"github.com/sirupsen/logrus"
)

// RuntimeMaintenance 周期扫描在线坐席运行态，把超时无活动者标记为 away。
type RuntimeMaintenance struct {
	logger *logrus.Logger
	module *agentapp.Service
	// interval 供测试注入毫秒级 tick；零值取默认 1 分钟。
	interval time.Duration
}

// NewRuntimeMaintenance 构造运行态维护循环；logger 为 nil 时取默认。
func NewRuntimeMaintenance(logger *logrus.Logger, module *agentapp.Service) *RuntimeMaintenance {
	if logger == nil {
		logger = logrus.New()
	}
	return &RuntimeMaintenance{
		logger: logger,
		module: module,
	}
}

// Start 阻塞运行维护循环（由装配方 go 起），ctx 取消时退出。
func (m *RuntimeMaintenance) Start(ctx context.Context) {
	interval := m.interval
	if interval <= 0 {
		interval = time.Minute
	}
	ticker := time.NewTicker(interval)
	defer ticker.Stop()

	for {
		select {
		case <-ticker.C:
			m.CleanupInactiveAgents(ctx, 5*time.Minute)
		case <-ctx.Done():
			return
		}
	}
}

// CleanupInactiveAgents 把 LastActivity 超时的在线坐席标记为 away。
func (m *RuntimeMaintenance) CleanupInactiveAgents(ctx context.Context, timeout time.Duration) {
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
