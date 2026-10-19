// translation_quota.go 租户级翻译配额门面装配（Phase 3，
// docs/design/tenant-quota-policy.md）：enabled=false 零行为（返回原门面）；
// enabled=true 在翻译门面外再包一层 QuotaGate（Redis 固定窗计量，meter
// 故障 fail-open），unit/window/limit 与 redis 可用性在此 fail-fast 校验
// （config.Validate 无枚举校验先例，装配期把关）。限额按 ctx 租户经
// configscope 解析（system 默认 + 租户/工作区覆盖，db 为 nil 时只用系统值）。
package server

import (
	"errors"
	"fmt"

	"servify/apps/server/internal/config"
	translationdelivery "servify/apps/server/internal/modules/translation/delivery"
	"servify/apps/server/internal/platform/configscope"
	"servify/apps/server/internal/platform/quotameter"

	"github.com/redis/go-redis/v9"
	"github.com/sirupsen/logrus"
	"gorm.io/gorm"
)

// buildTranslationQuotaGate 按配置构造配额门面：quota 关闭时原样返回 next；
// next 为 nil 时同样原样返回（未装配翻译的部署形态）。返回错误仅发生在
// enabled=true 且配置非法 / redis 缺失时（启动失败口径）。
func buildTranslationQuotaGate(cfg *config.Config, db *gorm.DB, redisClient *redis.Client, next translationdelivery.HandlerService, logger *logrus.Logger) (translationdelivery.HandlerService, error) {
	if cfg == nil || !cfg.Translation.Quota.Enabled || next == nil {
		return next, nil
	}
	q := cfg.Translation.Quota
	unit, err := translationdelivery.ParseQuotaUnit(q.Unit)
	if err != nil {
		return nil, fmt.Errorf("translation.quota.unit: %w", err)
	}
	window, err := quotameter.ParseWindow(q.Window)
	if err != nil {
		return nil, fmt.Errorf("translation.quota.window: %w", err)
	}
	if q.Limit <= 0 {
		return nil, fmt.Errorf("translation.quota.limit must be positive, got %d", q.Limit)
	}
	if redisClient == nil {
		return nil, errors.New("translation.quota.enabled requires redis (config event_bus.provider=redis)")
	}
	// window 已在上方经 ParseWindow 校验、redis 非空已检查，quotameter.New 的
	// 两个失败条件（非法 window / nil client）均已前置排除，此处不再重复判错。
	meter, _ := quotameter.New(redisClient, "translation", window)
	return translationdelivery.NewQuotaGateWithLimitSource(next, meter, q.Limit, unit, newTranslationQuotaLimitSource(cfg, db), logger), nil
}

// newTranslationQuotaLimitSource 构造每请求限额解析器（租户/工作区覆盖层，
// 租户与工作区身份取自 ctx）。db 为 nil 时返回 nil——部署形态无主库，
// 配额只按系统默认限额把关。
func newTranslationQuotaLimitSource(cfg *config.Config, db *gorm.DB) translationdelivery.QuotaLimitSource {
	if db == nil {
		return nil
	}
	resolver := configscope.NewResolver(cfg,
		configscope.WithTenantTranslationQuotaProvider(configscope.NewGormTenantConfigProvider(db)),
		configscope.WithWorkspaceTranslationQuotaProvider(configscope.NewGormWorkspaceConfigProvider(db)),
	)
	return resolver.ResolveTranslationQuotaLimit
}
