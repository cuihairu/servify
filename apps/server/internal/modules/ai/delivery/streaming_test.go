package delivery

import (
	"context"
	"errors"
	"strings"
	"testing"

	kpmock "servify/apps/server/internal/platform/knowledgeprovider/mock"
	llm "servify/apps/server/internal/platform/llm"
	llmmock "servify/apps/server/internal/platform/llm/mock"
)

func newStreamService(t *testing.T, provider *llmmock.Provider, kp *kpmock.Provider) *OrchestratedEnhancedAIService {
	t.Helper()
	base := NewAIService("", "")
	base.InitializeKnowledgeBase()
	return NewOrchestratedEnhancedAIService(base, provider, kp, "", nil)
}

// drainAIStream 读取流式事件直至 channel 关闭。
func drainAIStream(t *testing.T, stream <-chan AIStreamEvent) []AIStreamEvent {
	t.Helper()
	var events []AIStreamEvent
	for evt := range stream {
		events = append(events, evt)
	}
	return events
}

// TestProcessQueryStreamStreamsAndFinalizes 流式首答主链路：增量原样透传，
// 终帧携带完整首答（内容、置信度、产生方式、置信门建议），与非流式
// ProcessQuery 的 WS 透出面等价。
func TestProcessQueryStreamStreamsAndFinalizes(t *testing.T) {
	provider := &llmmock.Provider{StreamChunks: []llm.ChatChunk{
		{ContentDelta: "已收到"},
		{ContentDelta: "您的反馈"},
		{Done: true},
	}}
	svc := newStreamService(t, provider, &kpmock.Provider{})
	svc = svc.WithRuntimeParams(AIRuntimeParams{HandoffEnabled: true, HandoffConfidenceThreshold: 0.99})

	stream, err := svc.ProcessQueryStream(context.Background(), "物流进度", "sess-stream-1")
	if err != nil {
		t.Fatalf("ProcessQueryStream() error = %v", err)
	}
	events := drainAIStream(t, stream)

	var deltas []string
	var final AIStreamEvent
	for _, evt := range events {
		if evt.Done {
			final = evt
		} else {
			deltas = append(deltas, evt.ContentDelta)
		}
	}
	if strings.Join(deltas, "") != "已收到您的反馈" {
		t.Fatalf("deltas = %q", deltas)
	}
	if !final.Done || final.Final == nil {
		t.Fatalf("terminal event = %+v, want Done with Final", final)
	}
	if final.Final.Content != "已收到您的反馈" {
		t.Fatalf("Final.Content = %q", final.Final.Content)
	}
	// 零命中直答 confidence=0.6 < 0.99 → 置信门元数据随终帧透出。
	if final.Final.Confidence != 0.6 {
		t.Fatalf("Confidence = %v", final.Final.Confidence)
	}
	if final.Final.NextAction != "handoff" || final.Final.HandoffReason != "low_confidence" {
		t.Fatalf("handoff meta = %+v", final.Final)
	}
	if final.Final.Strategy != "llm" {
		t.Fatalf("Strategy = %q, want llm (direct answer)", final.Final.Strategy)
	}
}

// TestProcessQueryStreamTransferShortCircuit 转人工关键词命中：单事件直达，
// 不发起任何模型调用。
func TestProcessQueryStreamTransferShortCircuit(t *testing.T) {
	provider := &llmmock.Provider{}
	svc := newStreamService(t, provider, &kpmock.Provider{})

	stream, err := svc.ProcessQueryStream(context.Background(), "我要转人工", "sess-stream-2")
	if err != nil {
		t.Fatalf("ProcessQueryStream() error = %v", err)
	}
	events := drainAIStream(t, stream)
	if len(events) != 1 || !events[0].Done || events[0].Final == nil {
		t.Fatalf("events = %+v, want single terminal", events)
	}
	if events[0].Final.Source != "system" || events[0].Final.Confidence != 1.0 {
		t.Fatalf("transfer final = %+v", events[0].Final)
	}
	if len(provider.RecordedRequests()) != 0 {
		t.Fatalf("model must not be called on transfer short-circuit, got %d requests", len(provider.RecordedRequests()))
	}
}

// TestProcessQueryStreamSyncError 同步期失败（策略/护栏/检索）：直接返回
// 错误，由调用方回退非流式路径。
func TestProcessQueryStreamSyncError(t *testing.T) {
	svc := newStreamService(t, &llmmock.Provider{}, &kpmock.Provider{SearchError: errors.New("retrieval down")})
	svc.SetKnowledgeProviderEnabled(true)

	stream, err := svc.ProcessQueryStream(context.Background(), "物流进度", "sess-stream-3")
	if err == nil || stream != nil {
		t.Fatalf("ProcessQueryStream() = (%v, %v), want (nil, error)", stream, err)
	}
}

// TestProcessQueryStreamEmptyStream llmProvider 未接线：HandleStream 的
// (nil, nil) 归一成错误，调用方回退非流式路径。
func TestProcessQueryStreamEmptyStream(t *testing.T) {
	base := NewAIService("", "")
	base.InitializeKnowledgeBase()
	svc := NewOrchestratedEnhancedAIService(base, nil, &kpmock.Provider{}, "", nil)

	stream, err := svc.ProcessQueryStream(context.Background(), "物流进度", "sess-stream-4")
	if err == nil || stream != nil {
		t.Fatalf("ProcessQueryStream() = (%v, %v), want (nil, error)", stream, err)
	}
}

// TestProcessQueryStreamMidStreamError 流启动后失败：终末 Done 事件
// Final 为空（已推送的增量不回滚、终帧不补发）。
func TestProcessQueryStreamMidStreamError(t *testing.T) {
	provider := &llmmock.Provider{StreamError: errors.New("upstream reset")}
	svc := newStreamService(t, provider, &kpmock.Provider{})

	stream, err := svc.ProcessQueryStream(context.Background(), "物流进度", "sess-stream-5")
	if err != nil {
		t.Fatalf("ProcessQueryStream() error = %v, want stream started", err)
	}
	events := drainAIStream(t, stream)
	if len(events) != 1 || !events[0].Done || events[0].Final != nil {
		t.Fatalf("events = %+v, want single terminal without Final", events)
	}
}

// TestProcessQueryStreamNilReceiver nil 接收者：显式报错不 panic。
func TestProcessQueryStreamNilReceiver(t *testing.T) {
	var svc *OrchestratedEnhancedAIService
	if _, err := svc.ProcessQueryStream(context.Background(), "q", "s"); err == nil {
		t.Fatal("nil receiver must return an error")
	}
}
