package metrics

import "testing"

// TestBusinessMetricsNilReceiverNoop 验证未装配（nil）时所有打点方法
// 静默降级——调用方（如 AI 编排服务测试）不需要显式判空。
func TestBusinessMetricsNilReceiverNoop(t *testing.T) {
	var m *BusinessMetrics

	m.RecordConversationCreated("tenant-1", "web")
	m.RecordTicketCreated("tenant-1", "high")
	m.RecordTicketResolved("tenant-1", "agent")
	m.RecordRoutingDecision("tenant-1", "round_robin", "success")
	m.RecordAIRequest("weknora", "", "success", "primary", 0.5)
	m.RecordAILLMTokens("weknora", "input", 10)
}
