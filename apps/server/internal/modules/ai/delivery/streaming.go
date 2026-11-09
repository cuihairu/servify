package delivery

import (
	"context"
	"errors"
	"fmt"
	"time"

	aimodule "servify/apps/server/internal/modules/ai/application"
)

// AIStreamEvent 流式首答事件：ContentDelta 为增量文本；终末事件恒为
// Done=true，Final 携带与 ProcessQuery 等价的完整首答（引用来源、置信度、
// 产生方式、转人工建议）。流启动后失败时 Final 为 nil——已推送的增量不
// 回滚、终帧不补发，调用方按无响应处理（与 ProcessQuery 失败路径一致）。
type AIStreamEvent struct {
	ContentDelta string
	Done         bool
	Final        *AIResponse
}

// RuntimeQueryStreamer 流式首答可选能力。不并入 RuntimeService：调用方
// （WS hub、scoped 包装器）按能力类型断言协商，无流式能力时回退单发路径。
type RuntimeQueryStreamer interface {
	ProcessQueryStream(ctx context.Context, query string, sessionID string) (<-chan AIStreamEvent, error)
}

// ProcessQueryStream 流式首答：与 ProcessQueryEnhanced 同一条流水线
// （转人工关键词短路、检索增强、多轮上下文、置信门），模型输出经
// ChatStream 增量推送。转人工命中时单事件直达（不碰模型）；同步期失败
// （策略/护栏/检索）直接返回错误，由调用方回退非流式路径；流启动后的
// 失败经终末 Done 事件（Final=nil）透出。
func (s *OrchestratedEnhancedAIService) ProcessQueryStream(ctx context.Context, query string, sessionID string) (<-chan AIStreamEvent, error) {
	if s == nil {
		return nil, fmt.Errorf("ai streaming unavailable (service not assembled)")
	}
	start := time.Now()
	s.metrics.QueryCount++
	if s.ShouldTransferToHuman(query, nil) {
		transfer := s.transferShortCircuit(start)
		out := make(chan AIStreamEvent, 1)
		out <- AIStreamEvent{Done: true, Final: transfer.AIResponse}
		close(out)
		return out, nil
	}

	// 检索是否实际执行，在编排前定死（同 ProcessQueryEnhanced）：半开态
	// 试探耗尽时检索被跳过，流式成功的终帧不得触发 OnSuccess 误关熔断器。
	retrievalActive := s.activeKnowledgeProvider() != nil
	stream, err := s.activeOrchestrator().HandleStream(ctx, s.buildAIRequest(ctx, query, sessionID))
	if err != nil {
		// 知识服务故障与零命中分开：有配置的非直答策略时返回可审计固定
		// 响应，避免调用方把故障流回退成模型直答。
		var knowledgeErr *aimodule.KnowledgeUnavailableError
		if errors.As(err, &knowledgeErr) {
			action := normalizeNoEvidenceAction(s.runtimeParams.NoEvidence.ZeroHitAction)
			if action != NoEvidenceDirect {
				if s.knowledgeProviderEnabled {
					s.circuitBreaker.OnFailure()
				}
				final := &AIResponse{
					Confidence: 0.6,
					Source:     "system",
				}
				s.applyNoEvidenceAction(ctx, sessionID, final, action, "knowledge_unavailable")
				final.Strategy = string(action)
				s.promMetrics.RecordAIRequest(s.aiProviderLabel(), "", "success", string(action), time.Since(start).Seconds())
				out := make(chan AIStreamEvent, 1)
				out <- AIStreamEvent{Done: true, Final: final}
				close(out)
				return out, nil
			}
		}
		return nil, err
	}
	if stream == nil {
		// HandleStream 契约外仍可能 (nil, nil)（如 llmProvider 未接线）：
		// 归一成错误，调用方回退非流式路径。
		return nil, fmt.Errorf("ai orchestrator returned empty stream (session_id=%s)", sessionID)
	}

	out := make(chan AIStreamEvent)
	go func() {
		defer close(out)
		for evt := range stream {
			if evt.Done {
				if evt.Response != nil {
					enhanced := s.finalizeEnhanced(ctx, sessionID, evt.Response, retrievalActive)
					// 与 ProcessQuery 同规：把 enhanced 附加输出（引用来源、
					// 产生方式）带回 AIResponse，WS ai-response 终帧据此透出。
					final := enhanced.AIResponse
					final.Sources = enhanced.Sources
					final.Strategy = enhanced.Strategy
					out <- AIStreamEvent{Done: true, Final: final}
				} else {
					out <- AIStreamEvent{Done: true}
				}
				continue
			}
			if evt.ContentDelta != "" {
				out <- AIStreamEvent{ContentDelta: evt.ContentDelta}
			}
		}
	}()
	return out, nil
}
