package delivery

// 熔断器过早恢复修复（todo.md 审核发现行 224）测试：检索被跳过的成功请求
// 不得触发 circuitBreaker.OnSuccess。精确机制是半开态请求数上限（半开限
// 3 个试探请求）耗尽的第 4+ 请求：Allow=false → activeKnowledgeProvider
// 返回 nil → 检索被跳过 → 零命中 → 模型成功 → finalizeEnhanced 的
// OnSuccess 把半开态误转关闭、过早恢复访问（plain open 态 OnSuccess 是
// no-op，不构成过早恢复——真正的错误窗口只在半开态）。修复后 OnSuccess
// 仅在检索实际执行时触发。

import (
	"context"
	"testing"
	"time"

	kpmock "servify/apps/server/internal/platform/knowledgeprovider/mock"
	llm "servify/apps/server/internal/platform/llm"
	llmmock "servify/apps/server/internal/platform/llm/mock"
)

// openThenExhaustHalfOpen 把 svc 的熔断器确定性置入「半开、试探请求已耗尽」
// 状态：3 个半开试探都在途、Allow() 恒 false（activeKnowledgeProvider 返回
// nil → 检索必被跳过）。直接构造状态免去 ResetTimeout/转移时序歧义。
func openThenExhaustHalfOpen(svc *OrchestratedEnhancedAIService) {
	cb := NewCircuitBreakerWithConfig(&CircuitBreakerConfig{
		MaxFailures: 5, ResetTimeout: time.Millisecond, HalfOpenMaxReqs: 3,
	})
	cb.state = StateHalfOpenCB
	cb.halfOpenReqs = cb.config.HalfOpenMaxReqs
	svc.circuitBreaker = cb
	if svc.circuitBreaker.Allow() {
		panic("expected half-open probe limit exhausted")
	}
}

// TestOrchestratedAI_SkippedRetrievalDoesNotCloseBreaker 非流式：半开态检索
// 被跳过的请求成功走模型后，熔断器不得被误关闭（检索实际未执行）。
func TestOrchestratedAI_SkippedRetrievalDoesNotCloseBreaker(t *testing.T) {
	svc, llmProvider, _ := newOrchestratedForUnit(t)
	llmProvider.ChatResponse = llm.ChatResponse{Content: "model answer without retrieval"}
	openThenExhaustHalfOpen(svc)

	resp, err := svc.ProcessQueryEnhanced(context.Background(), "普通问题", "sess")
	if err != nil {
		t.Fatalf("expected success, got %v", err)
	}
	if resp == nil || resp.Content != "model answer without retrieval" {
		t.Fatalf("unexpected response: %+v", resp)
	}
	if svc.circuitBreaker.IsClosed() {
		t.Fatal("skipped-retrieval success must not close the breaker")
	}
}

// TestProcessQueryStreamSkippedRetrievalDoesNotCloseBreaker 流式：与上面同一
// 场景经 ProcessQueryStream 的终帧 finalizeEnhanced 路径，同样不得误关闭。
func TestProcessQueryStreamSkippedRetrievalDoesNotCloseBreaker(t *testing.T) {
	provider := &llmmock.Provider{StreamChunks: []llm.ChatChunk{{ContentDelta: "流式回答"}, {Done: true}}}
	svc := newStreamService(t, provider, &kpmock.Provider{})
	openThenExhaustHalfOpen(svc)

	stream, err := svc.ProcessQueryStream(context.Background(), "普通问题", "sess")
	if err != nil {
		t.Fatalf("ProcessQueryStream() error = %v", err)
	}
	events := drainAIStream(t, stream)
	if len(events) == 0 || !events[len(events)-1].Done {
		t.Fatalf("expected terminal done event, got %+v", events)
	}
	if svc.circuitBreaker.IsClosed() {
		t.Fatal("streaming skipped-retrieval success must not close the breaker")
	}
}
