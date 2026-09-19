package bootstrap

import (
	"fmt"
	"strings"

	"servify/apps/server/internal/config"
	"servify/apps/server/internal/platform/eventbus"

	"github.com/redis/go-redis/v9"
	"github.com/sirupsen/logrus"
)

const (
	eventBusProviderInMemory = "inmemory"
	eventBusProviderRedis    = "redis"
)

func BuildEventBus(cfg *config.Config, logger *logrus.Logger, redisClient *redis.Client) (eventbus.Bus, error) {
	if cfg == nil {
		cfg = config.GetDefaultConfig()
	}
	if logger == nil {
		logger = logrus.StandardLogger()
	}

	provider := strings.TrimSpace(strings.ToLower(cfg.EventBus.Provider))
	if provider == "" {
		provider = eventBusProviderInMemory
	}

	switch provider {
	case eventBusProviderInMemory:
		// P3-3：与 config.InsecureDefaults 的启动校验同口径的装配层兜底——
		// 绕过 LoadConfig 的调用方（GetDefaultConfig 直用）也拦在装配期。
		if strings.EqualFold(strings.TrimSpace(cfg.Server.Environment), "production") {
			return nil, fmt.Errorf("event bus provider %q is not allowed in production; configure event_bus.provider = 'redis' for durable events", cfg.EventBus.Provider)
		}
		return eventbus.NewInMemoryBusWithLogger(logger), nil
	case eventBusProviderRedis:
		if redisClient == nil {
			return nil, fmt.Errorf("redis client required for redis event bus provider")
		}
		logger.Info("using redis event bus provider")
		return eventbus.NewRedisBus(redisClient, logger), nil
	default:
		return nil, fmt.Errorf("unsupported event bus provider %q", cfg.EventBus.Provider)
	}
}
