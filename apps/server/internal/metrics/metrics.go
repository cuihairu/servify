package metrics

import (
	"sync"
	"sync/atomic"
)

// rateLimitStats holds counters for rate limit drops (HTTP 429).
// Kept simple/thread-safe for use from middlewares and exposition.
type rateLimitStats struct {
	total    uint64
	mu       sync.Mutex
	byPrefix map[string]uint64
}

var rl rateLimitStats

// IncRateLimitDrop increments drop counters for the given prefix.
// Use prefix "global" for global limiter rejections.
func IncRateLimitDrop(prefix string) {
	if prefix == "" {
		prefix = "global"
	}
	atomic.AddUint64(&rl.total, 1)
	rl.mu.Lock()
	if rl.byPrefix == nil {
		rl.byPrefix = make(map[string]uint64)
	}
	rl.byPrefix[prefix]++
	rl.mu.Unlock()
}

// RateLimitSnapshot returns a copy of the current counters.
func RateLimitSnapshot() (total uint64, by map[string]uint64) {
	total = atomic.LoadUint64(&rl.total)
	rl.mu.Lock()
	defer rl.mu.Unlock()
	by = make(map[string]uint64, len(rl.byPrefix))
	for k, v := range rl.byPrefix {
		by[k] = v
	}
	return total, by
}

// ResetRateLimit zeroes the rate limit drop counters.
// 进程级全局计数器没有按测试隔离的手段，供测试在开头重置以保证断言确定性。
func ResetRateLimit() {
	atomic.StoreUint64(&rl.total, 0)
	rl.mu.Lock()
	rl.byPrefix = nil
	rl.mu.Unlock()
}
