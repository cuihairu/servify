package delivery

import (
	"sync"
	"time"
)

// CircuitBreakerState 熔断器状态
type CircuitBreakerState int

const (
	StateClosedCB   CircuitBreakerState = iota // 关闭状态（正常）
	StateOpenCB                                // 开启状态（熔断）
	StateHalfOpenCB                            // 半开状态（试探）
)

func (s CircuitBreakerState) String() string {
	switch s {
	case StateClosedCB:
		return "closed"
	case StateOpenCB:
		return "open"
	case StateHalfOpenCB:
		return "half-open"
	default:
		return "unknown"
	}
}

// CircuitBreakerConfig 熔断器配置
type CircuitBreakerConfig struct {
	MaxFailures     int           `yaml:"max_failures"`       // 最大失败次数
	ResetTimeout    time.Duration `yaml:"reset_timeout"`      // 重置超时时间
	HalfOpenMaxReqs int           `yaml:"half_open_max_reqs"` // 半开状态最大请求数
}

// DefaultCircuitBreakerConfig 默认熔断器配置
func DefaultCircuitBreakerConfig() *CircuitBreakerConfig {
	return &CircuitBreakerConfig{
		MaxFailures:     5,
		ResetTimeout:    60 * time.Second,
		HalfOpenMaxReqs: 3,
	}
}

// CircuitBreaker 熔断器实现
type CircuitBreaker struct {
	config       *CircuitBreakerConfig
	state        CircuitBreakerState
	failureCount int
	lastFailTime time.Time
	halfOpenReqs int
	mutex        sync.RWMutex
}

// NewCircuitBreaker 创建新的熔断器
func NewCircuitBreaker() *CircuitBreaker {
	return NewCircuitBreakerWithConfig(DefaultCircuitBreakerConfig())
}

// NewCircuitBreakerWithConfig 使用配置创建熔断器
func NewCircuitBreakerWithConfig(config *CircuitBreakerConfig) *CircuitBreaker {
	return &CircuitBreaker{
		config: config,
		state:  StateClosedCB,
	}
}

// Allow 检查是否允许请求通过
func (cb *CircuitBreaker) Allow() bool {
	cb.mutex.Lock()
	defer cb.mutex.Unlock()

	switch cb.state {
	case StateClosedCB:
		return true

	case StateOpenCB:
		// 检查是否可以转为半开状态
		if time.Since(cb.lastFailTime) > cb.config.ResetTimeout {
			cb.state = StateHalfOpenCB
			cb.halfOpenReqs = 0
			return true
		}
		return false

	case StateHalfOpenCB:
		// 半开状态下限制请求数量
		if cb.halfOpenReqs < cb.config.HalfOpenMaxReqs {
			cb.halfOpenReqs++
			return true
		}
		return false

	default:
		return false
	}
}

// OnSuccess 记录成功请求
func (cb *CircuitBreaker) OnSuccess() {
	cb.mutex.Lock()
	defer cb.mutex.Unlock()

	switch cb.state {
	case StateClosedCB:
		// 重置失败计数
		cb.failureCount = 0

	case StateHalfOpenCB:
		// 半开状态成功，转为关闭状态
		cb.state = StateClosedCB
		cb.failureCount = 0
		cb.halfOpenReqs = 0
	}
}

// OnFailure 记录失败请求
func (cb *CircuitBreaker) OnFailure() {
	cb.mutex.Lock()
	defer cb.mutex.Unlock()

	cb.failureCount++
	cb.lastFailTime = time.Now()

	switch cb.state {
	case StateClosedCB:
		// 检查是否达到失败阈值
		if cb.failureCount >= cb.config.MaxFailures {
			cb.state = StateOpenCB
		}

	case StateHalfOpenCB:
		// 半开状态失败，立即转为开启状态
		cb.state = StateOpenCB
		cb.halfOpenReqs = 0
	}
}

// State 获取当前状态
func (cb *CircuitBreaker) State() CircuitBreakerState {
	cb.mutex.RLock()
	defer cb.mutex.RUnlock()
	return cb.state
}

// FailureCount 获取失败计数
func (cb *CircuitBreaker) FailureCount() int {
	cb.mutex.RLock()
	defer cb.mutex.RUnlock()
	return cb.failureCount
}

// Reset 重置熔断器
func (cb *CircuitBreaker) Reset() {
	cb.mutex.Lock()
	defer cb.mutex.Unlock()

	cb.state = StateClosedCB
	cb.failureCount = 0
	cb.halfOpenReqs = 0
}

// Stats 获取熔断器统计信息
func (cb *CircuitBreaker) Stats() map[string]interface{} {
	cb.mutex.RLock()
	defer cb.mutex.RUnlock()

	return map[string]interface{}{
		"state":              cb.state.String(),
		"failure_count":      cb.failureCount,
		"last_fail_time":     cb.lastFailTime,
		"half_open_reqs":     cb.halfOpenReqs,
		"max_failures":       cb.config.MaxFailures,
		"reset_timeout":      cb.config.ResetTimeout,
		"half_open_max_reqs": cb.config.HalfOpenMaxReqs,
	}
}

// IsOpen 检查熔断器是否处于开启状态
func (cb *CircuitBreaker) IsOpen() bool {
	cb.mutex.RLock()
	defer cb.mutex.RUnlock()
	return cb.state == StateOpenCB
}

// IsClosed 检查熔断器是否处于关闭状态
func (cb *CircuitBreaker) IsClosed() bool {
	cb.mutex.RLock()
	defer cb.mutex.RUnlock()
	return cb.state == StateClosedCB
}

// IsHalfOpen 检查熔断器是否处于半开状态
func (cb *CircuitBreaker) IsHalfOpen() bool {
	cb.mutex.RLock()
	defer cb.mutex.RUnlock()
	return cb.state == StateHalfOpenCB
}
