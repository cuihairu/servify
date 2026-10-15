// quota_gate.go 租户级配额限流检查点（Phase 3，docs/design/tenant-quota-policy.md）：
// HandlerService 装饰器，装配层包在翻译门面外（REST / 实时自动翻译 / 历史
// 批量标注 / 语音通道统一过门）。租户从 ctx 认证信息提取（platformauth），
// 无租户（内部/未认证路径）静默跳过；计量存储为 Redis 固定窗（platform/
// quotameter，窗口锚点入键名），meter 故障 fail-open——保护面不该反过来
// 打死翻译主链路。
package delivery

import (
	"context"
	"errors"

	platformauth "servify/apps/server/internal/platform/auth"

	"github.com/sirupsen/logrus"
)

// QuotaUnit 计量单位（与 config translation.quota.unit 对齐）。
const (
	QuotaUnitRequests = "requests"
	QuotaUnitTokens   = "tokens"
)

// ParseQuotaUnit 校验计量单位字串（装配期 fail-fast 入口）。
func ParseQuotaUnit(s string) (string, error) {
	switch s {
	case QuotaUnitRequests, QuotaUnitTokens:
		return s, nil
	default:
		return "", errors.New("invalid quota unit (want requests|tokens)")
	}
}

// ErrTranslationQuotaExceeded 租户当前窗口配额耗尽（handler 映射 429）。
var ErrTranslationQuotaExceeded = errors.New("translation quota exceeded for tenant window")

// QuotaMeter 计量端口（quotameter.Meter 结构化满足；测试可 stub）。
type QuotaMeter interface {
	Usage(ctx context.Context, scope string) (int64, error)
	Add(ctx context.Context, scope string, amount int64) (int64, error)
}

// quotaGate 装饰任意 HandlerService：按 ctx 租户键控固定窗配额。
type quotaGate struct {
	next   HandlerService
	meter  QuotaMeter
	limit  int64
	unit   string
	logger *logrus.Logger
}

// 编译期确认实现端口。
var _ HandlerService = (*quotaGate)(nil)

// NewQuotaGate 装饰 next（unit/limit/window 由装配层校验后注入；logger
// 为 nil 时用标准 logger）。next 为 nil 时不装饰（装配层跳过配额面）。
func NewQuotaGate(next HandlerService, meter QuotaMeter, limit int64, unit string, logger *logrus.Logger) HandlerService {
	if next == nil || meter == nil {
		return next
	}
	if logger == nil {
		logger = logrus.StandardLogger()
	}
	return &quotaGate{next: next, meter: meter, limit: limit, unit: unit, logger: logger}
}

// Translate 过门语义：
//   - requests：先原子 +1 再比限（被拒调用也占窗——窗口耗尽后本就整窗
//     429，计数保守无害，且免 check-then-act 竞态）；
//   - tokens：先查已用量（≥limit 拒），成功后按 TokenUsage.TotalTokens
//     记账（provider 未回传 usage 则不记）。
//
// meter 出错一律 fail-open（放行 + Warn 日志）。
func (g *quotaGate) Translate(ctx context.Context, cmd TranslateCommand) (TranslateResult, error) {
	tenant := platformauth.TenantIDFromContext(ctx)
	if tenant == "" {
		return g.next.Translate(ctx, cmd)
	}
	if g.unit == QuotaUnitRequests {
		total, err := g.meter.Add(ctx, tenant, 1)
		if err != nil {
			g.logger.Warnf("translation quota meter failed (fail-open): %v", err)
		} else if total > g.limit {
			return TranslateResult{}, ErrTranslationQuotaExceeded
		}
		return g.next.Translate(ctx, cmd)
	}
	used, err := g.meter.Usage(ctx, tenant)
	if err != nil {
		g.logger.Warnf("translation quota meter failed (fail-open): %v", err)
		return g.next.Translate(ctx, cmd)
	}
	if used >= g.limit {
		return TranslateResult{}, ErrTranslationQuotaExceeded
	}
	res, err := g.next.Translate(ctx, cmd)
	if err != nil {
		return res, err
	}
	if res.TokenUsage != nil && res.TokenUsage.TotalTokens > 0 {
		if _, mErr := g.meter.Add(ctx, tenant, int64(res.TokenUsage.TotalTokens)); mErr != nil {
			g.logger.Warnf("translation quota meter record failed (fail-open): %v", mErr)
		}
	}
	return res, nil
}

// BatchTranslate 过门语义：requests 单位按一次调用 +1（计量的是 API 调用
// 次数而非段数）；tokens 单位仅事前检查——批量产出无 TokenUsage 面，事后
// 记账不可得（docs/design/tenant-quota-policy.md §3）。
func (g *quotaGate) BatchTranslate(ctx context.Context, cmd BatchTranslateCommand) (BatchTranslateResult, error) {
	tenant := platformauth.TenantIDFromContext(ctx)
	if tenant == "" {
		return g.next.BatchTranslate(ctx, cmd)
	}
	if g.unit == QuotaUnitRequests {
		total, err := g.meter.Add(ctx, tenant, 1)
		if err != nil {
			g.logger.Warnf("translation quota meter failed (fail-open): %v", err)
		} else if total > g.limit {
			return BatchTranslateResult{}, ErrTranslationQuotaExceeded
		}
		return g.next.BatchTranslate(ctx, cmd)
	}
	used, err := g.meter.Usage(ctx, tenant)
	if err != nil {
		g.logger.Warnf("translation quota meter failed (fail-open): %v", err)
		return g.next.BatchTranslate(ctx, cmd)
	}
	if used >= g.limit {
		return BatchTranslateResult{}, ErrTranslationQuotaExceeded
	}
	return g.next.BatchTranslate(ctx, cmd)
}
