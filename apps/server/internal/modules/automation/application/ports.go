package application

import (
	"context"
	"time"
)

// TimerProcessor 是 automation timer worker 依赖的窄接口：
// worker 只需要「扫一批到期执行单并执行」，不感知触发器与工单细节。
type TimerProcessor interface {
	ProcessDueTimers(ctx context.Context, now time.Time) int
}
