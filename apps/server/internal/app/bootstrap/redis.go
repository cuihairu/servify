package bootstrap

import (
	"context"
	"fmt"
	"strings"
	"time"

	"servify/apps/server/internal/config"

	"github.com/redis/go-redis/v9"
)

// OpenRedis initializes the shared Redis client when runtime features require it.
func OpenRedis(cfg *config.Config) (*redis.Client, error) {
	// redisRequired 对 nil cfg 返回 false，走到这里 cfg 必非 nil，
	// 无需默认值兜底分支。
	if !redisRequired(cfg) {
		return nil, nil
	}

	addr := fmt.Sprintf("%s:%d", cfg.Redis.Host, cfg.Redis.Port)
	client := redis.NewClient(&redis.Options{
		Addr:         addr,
		Password:     cfg.Redis.Password,
		DB:           cfg.Redis.DB,
		PoolSize:     cfg.Redis.PoolSize,
		MinIdleConns: cfg.Redis.MinIdleConns,
	})

	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()
	if err := client.Ping(ctx).Err(); err != nil {
		_ = client.Close()
		return nil, fmt.Errorf("connect redis %s: %w", addr, err)
	}
	return client, nil
}

func redisRequired(cfg *config.Config) bool {
	if cfg == nil {
		return false
	}
	return strings.EqualFold(strings.TrimSpace(cfg.EventBus.Provider), eventBusProviderRedis)
}
