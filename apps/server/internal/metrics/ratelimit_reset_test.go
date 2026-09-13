package metrics

import "testing"

// TestResetRateLimit 验证重置把总量与前缀计数都清零，且重置后可继续累加。
func TestResetRateLimit(t *testing.T) {
	ResetRateLimit()
	t.Cleanup(ResetRateLimit) // 不污染同包其他测试的计数

	IncRateLimitDrop("reset-a")
	IncRateLimitDrop("reset-a")
	IncRateLimitDrop("reset-b")

	total, by := RateLimitSnapshot()
	if total != 3 {
		t.Fatalf("total before reset = %d, want 3", total)
	}
	if by["reset-a"] != 2 || by["reset-b"] != 1 {
		t.Fatalf("byPrefix before reset = %v", by)
	}

	ResetRateLimit()

	total, by = RateLimitSnapshot()
	if total != 0 {
		t.Fatalf("total after reset = %d, want 0", total)
	}
	if len(by) != 0 {
		t.Fatalf("byPrefix after reset = %v, want empty", by)
	}

	// 重置后计数器仍可用
	IncRateLimitDrop("after-reset")
	total, by = RateLimitSnapshot()
	if total != 1 || by["after-reset"] != 1 {
		t.Fatalf("after re-increment total=%d by=%v", total, by)
	}
}
