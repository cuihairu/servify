package services

import (
	"testing"
	"time"
)

func TestCircuitBreaker_AllowHalfOpenLimitAndUnknownState(t *testing.T) {
	cb := NewCircuitBreakerWithConfig(&CircuitBreakerConfig{
		MaxFailures: 1, ResetTimeout: time.Millisecond, HalfOpenMaxReqs: 1,
	})
	cb.OnFailure()
	time.Sleep(2 * time.Millisecond)

	if !cb.Allow() { // transitions to half-open and allows first request
		t.Fatal("expected first half-open request allowed")
	}
	if !cb.Allow() { // consumes the single half-open request budget
		t.Fatal("expected budget request allowed")
	}
	if cb.Allow() { // half-open request budget exhausted
		t.Fatal("expected third half-open request rejected")
	}

	// force an unknown state to hit the default branch
	unknown := NewCircuitBreakerWithConfig(&CircuitBreakerConfig{MaxFailures: 1})
	unknown.state = CircuitBreakerState(42)
	if unknown.Allow() {
		t.Fatal("expected unknown state to reject")
	}
}

func TestCircuitBreaker_IsClosedIsOpen(t *testing.T) {
	cb := NewCircuitBreaker()
	if !cb.IsClosed() || cb.IsOpen() {
		t.Fatal("fresh breaker should be closed")
	}
	cb.OnFailure()
	if !cb.IsClosed() || cb.IsOpen() {
		t.Fatal("single failure below threshold should keep breaker closed")
	}

	strict := NewCircuitBreakerWithConfig(&CircuitBreakerConfig{MaxFailures: 1})
	strict.OnFailure()
	if strict.IsClosed() || !strict.IsOpen() {
		t.Fatal("breaker at threshold should be open")
	}
}
