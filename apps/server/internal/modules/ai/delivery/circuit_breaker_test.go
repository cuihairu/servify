package delivery

import (
	"testing"
	"time"
)

func TestCircuitBreaker_Transitions(t *testing.T) {
	cfg := &CircuitBreakerConfig{MaxFailures: 1, ResetTimeout: 1 * time.Millisecond, HalfOpenMaxReqs: 1}
	cb := NewCircuitBreakerWithConfig(cfg)

	if !cb.IsClosed() {
		t.Fatalf("new breaker should be closed")
	}
	cb.OnFailure()
	if !cb.IsOpen() {
		t.Fatalf("breaker should open after reaching max failures")
	}

	// After reset timeout, Allow should move to half-open and allow one request
	time.Sleep(2 * time.Millisecond)
	if !cb.Allow() {
		t.Fatalf("expected allow in half-open")
	}
	if !cb.IsHalfOpen() {
		t.Fatalf("state should be half-open after allow")
	}

	// success in half-open -> closed
	cb.OnSuccess()
	if !cb.IsClosed() {
		t.Fatalf("expected closed after success in half-open")
	}
}

func TestCircuitBreakerState_String(t *testing.T) {
	tests := []struct {
		name     string
		state    CircuitBreakerState
		expected string
	}{
		{"closed", StateClosedCB, "closed"},
		{"open", StateOpenCB, "open"},
		{"half-open", StateHalfOpenCB, "half-open"},
		{"unknown", CircuitBreakerState(99), "unknown"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := tt.state.String(); got != tt.expected {
				t.Errorf("String() = %v, want %v", got, tt.expected)
			}
		})
	}
}

func TestDefaultCircuitBreakerConfig(t *testing.T) {
	cfg := DefaultCircuitBreakerConfig()

	if cfg.MaxFailures != 5 {
		t.Errorf("expected MaxFailures 5, got %d", cfg.MaxFailures)
	}
	if cfg.ResetTimeout != 60*time.Second {
		t.Errorf("expected ResetTimeout 60s, got %v", cfg.ResetTimeout)
	}
	if cfg.HalfOpenMaxReqs != 3 {
		t.Errorf("expected HalfOpenMaxReqs 3, got %d", cfg.HalfOpenMaxReqs)
	}
}

func TestNewCircuitBreaker(t *testing.T) {
	cb := NewCircuitBreaker()

	if cb == nil {
		t.Fatal("expected non-nil circuit breaker")
	}

	if cb.State() != StateClosedCB {
		t.Errorf("expected initial state to be closed, got %v", cb.State())
	}

	if cb.FailureCount() != 0 {
		t.Errorf("expected initial failure count 0, got %d", cb.FailureCount())
	}
}

func TestCircuitBreaker_Allow_ClosedState(t *testing.T) {
	cb := NewCircuitBreaker()

	if !cb.Allow() {
		t.Error("expected Allow() to return true in closed state")
	}
}

func TestCircuitBreaker_Allow_OpenState(t *testing.T) {
	cb := NewCircuitBreakerWithConfig(&CircuitBreakerConfig{
		MaxFailures:  2,
		ResetTimeout: 100 * time.Millisecond,
	})

	// 触发熔断器打开
	cb.OnFailure()
	cb.OnFailure()

	if cb.State() != StateOpenCB {
		t.Errorf("expected state to be open, got %v", cb.State())
	}

	// 在开启状态下应该拒绝请求
	if cb.Allow() {
		t.Error("expected Allow() to return false in open state")
	}
}

func TestCircuitBreaker_OnSuccess_ResetsFailureCount(t *testing.T) {
	cb := NewCircuitBreaker()

	// 记录一些失败
	cb.OnFailure()
	cb.OnFailure()

	if cb.FailureCount() != 2 {
		t.Errorf("expected failure count 2, got %d", cb.FailureCount())
	}

	// 成功应该重置失败计数
	cb.OnSuccess()

	if cb.FailureCount() != 0 {
		t.Errorf("expected failure count to be reset to 0, got %d", cb.FailureCount())
	}
}

func TestCircuitBreaker_Reset(t *testing.T) {
	cb := NewCircuitBreaker()

	// 设置一些状态
	cb.OnFailure()
	cb.OnFailure()

	if cb.FailureCount() != 2 {
		t.Errorf("expected failure count 2, got %d", cb.FailureCount())
	}

	// 重置
	cb.Reset()

	if cb.State() != StateClosedCB {
		t.Errorf("expected state to be closed after reset, got %v", cb.State())
	}

	if cb.FailureCount() != 0 {
		t.Errorf("expected failure count to be 0 after reset, got %d", cb.FailureCount())
	}
}

func TestCircuitBreaker_Stats(t *testing.T) {
	cfg := &CircuitBreakerConfig{
		MaxFailures:     5,
		ResetTimeout:    60 * time.Second,
		HalfOpenMaxReqs: 3,
	}

	cb := NewCircuitBreakerWithConfig(cfg)
	cb.OnFailure()

	stats := cb.Stats()

	if stats["state"] != "closed" {
		t.Errorf("expected state 'closed', got %v", stats["state"])
	}

	if stats["failure_count"] != 1 {
		t.Errorf("expected failure_count 1, got %v", stats["failure_count"])
	}

	if stats["max_failures"] != 5 {
		t.Errorf("expected max_failures 5, got %v", stats["max_failures"])
	}

	if stats["reset_timeout"] != 60*time.Second {
		t.Errorf("expected reset_timeout 60s, got %v", stats["reset_timeout"])
	}

	if stats["half_open_max_reqs"] != 3 {
		t.Errorf("expected half_open_max_reqs 3, got %v", stats["half_open_max_reqs"])
	}
}

func TestCircuitBreaker_HalfOpenFailure(t *testing.T) {
	cb := NewCircuitBreakerWithConfig(&CircuitBreakerConfig{
		MaxFailures:  1,
		ResetTimeout: 50 * time.Millisecond,
	})

	cb.OnFailure()

	// 进入半开状态
	time.Sleep(60 * time.Millisecond)
	cb.Allow()

	// 半开状态失败应该转回开启状态
	cb.OnFailure()

	if cb.State() != StateOpenCB {
		t.Errorf("expected state to be open after failure in half-open, got %v", cb.State())
	}
}

func TestCircuitBreaker_FailureThreshold(t *testing.T) {
	cb := NewCircuitBreakerWithConfig(&CircuitBreakerConfig{
		MaxFailures: 3,
	})

	// 未达到失败阈值
	cb.OnFailure()
	cb.OnFailure()

	if cb.State() != StateClosedCB {
		t.Errorf("expected state to remain closed below threshold, got %v", cb.State())
	}

	// 达到失败阈值
	cb.OnFailure()

	if cb.State() != StateOpenCB {
		t.Errorf("expected state to be open after reaching threshold, got %v", cb.State())
	}
}
