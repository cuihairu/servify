package application

import (
	"sync"
	"time"
)

// Metrics captures high-level orchestrator metrics.
type Metrics struct {
	mu                  sync.RWMutex
	queryCount          int64
	successCount        int64
	fallbackCount       int64
	errorCount          int64
	policyRejectCount   int64
	providerUsageCount  map[string]int64
	providerErrorCount  map[string]int64
	lastLatency         time.Duration
	lastTokenUsage      int
	lastErrorCategory   string
	toolCallCount       int64
	toolErrorCount      map[string]int64
	toolCallCountByTool map[string]int64
}

func NewMetrics() *Metrics {
	return &Metrics{
		providerUsageCount:  make(map[string]int64),
		providerErrorCount:  make(map[string]int64),
		toolErrorCount:      make(map[string]int64),
		toolCallCountByTool: make(map[string]int64),
	}
}

func (m *Metrics) RecordQuery() {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.queryCount++
}

func (m *Metrics) RecordSuccess(provider string, latency time.Duration, totalTokens int) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.successCount++
	m.lastLatency = latency
	m.lastTokenUsage = totalTokens
	if provider != "" {
		m.providerUsageCount[provider]++
	}
}

func (m *Metrics) RecordFallback() {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.fallbackCount++
}

func (m *Metrics) RecordError(provider string, category string) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.errorCount++
	m.lastErrorCategory = category
	if provider != "" {
		m.providerErrorCount[provider]++
	}
}

func (m *Metrics) RecordPolicyRejection(reason string) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.policyRejectCount++
	m.lastErrorCategory = reason
}

func (m *Metrics) RecordToolCall(toolName string) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.toolCallCount++
	m.toolCallCountByTool[toolName]++
}

func (m *Metrics) RecordToolError(toolName string) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.toolErrorCount[toolName]++
}

type MetricsSnapshot struct {
	QueryCount          int64            `json:"query_count"`
	SuccessCount        int64            `json:"success_count"`
	FallbackCount       int64            `json:"fallback_count"`
	ErrorCount          int64            `json:"error_count"`
	PolicyRejectCount   int64            `json:"policy_reject_count"`
	ProviderUsageCount  map[string]int64 `json:"provider_usage_count"`
	ProviderErrorCount  map[string]int64 `json:"provider_error_count"`
	LastLatency         time.Duration    `json:"last_latency"`
	LastTokenUsage      int              `json:"last_token_usage"`
	LastErrorCategory   string           `json:"last_error_category,omitempty"`
	ToolCallCount       int64            `json:"tool_call_count"`
	ToolErrorCount      map[string]int64 `json:"tool_error_count,omitempty"`
	ToolCallCountByTool map[string]int64 `json:"tool_call_count_by_tool,omitempty"`
}

func (m *Metrics) Snapshot() MetricsSnapshot {
	m.mu.RLock()
	defer m.mu.RUnlock()
	out := MetricsSnapshot{
		QueryCount:          m.queryCount,
		SuccessCount:        m.successCount,
		FallbackCount:       m.fallbackCount,
		ErrorCount:          m.errorCount,
		PolicyRejectCount:   m.policyRejectCount,
		ProviderUsageCount:  make(map[string]int64, len(m.providerUsageCount)),
		ProviderErrorCount:  make(map[string]int64, len(m.providerErrorCount)),
		LastLatency:         m.lastLatency,
		LastTokenUsage:      m.lastTokenUsage,
		LastErrorCategory:   m.lastErrorCategory,
		ToolCallCount:       m.toolCallCount,
		ToolErrorCount:      make(map[string]int64, len(m.toolErrorCount)),
		ToolCallCountByTool: make(map[string]int64, len(m.toolCallCountByTool)),
	}
	for k, v := range m.providerUsageCount {
		out.ProviderUsageCount[k] = v
	}
	for k, v := range m.providerErrorCount {
		out.ProviderErrorCount[k] = v
	}
	for k, v := range m.toolErrorCount {
		out.ToolErrorCount[k] = v
	}
	for k, v := range m.toolCallCountByTool {
		out.ToolCallCountByTool[k] = v
	}
	return out
}
