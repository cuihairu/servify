package realtime

import (
	"context"
	"sync/atomic"
	"testing"
	"time"

	aidelivery "servify/apps/server/internal/modules/ai/delivery"
	baseweknora "servify/apps/server/pkg/weknora"
)

// streamingAI 在 unitAIService 之上叠加流式可选能力（ProcessQueryStream）。
type streamingAI struct {
	unitAIService
	streamErr   error
	returnNilCh bool
	stream      <-chan aidelivery.AIStreamEvent
	streamCalls atomic.Int64
}

func (a *streamingAI) ProcessQueryStream(ctx context.Context, query string, sessionID string) (<-chan aidelivery.AIStreamEvent, error) {
	a.streamCalls.Add(1)
	if a.streamErr != nil {
		return nil, a.streamErr
	}
	if a.returnNilCh {
		return nil, nil
	}
	return a.stream, nil
}

// eventsChannel 把预置事件装入已关闭的 channel。
func eventsChannel(events ...aidelivery.AIStreamEvent) <-chan aidelivery.AIStreamEvent {
	ch := make(chan aidelivery.AIStreamEvent, len(events))
	for _, evt := range events {
		ch <- evt
	}
	close(ch)
	return ch
}

// expectDelta 断言下一帧是 ai-response-delta 并校验字段。
func expectDelta(t *testing.T, ch chan WebSocketMessage, wantDelta string, wantDone bool) {
	t.Helper()
	msg := waitForMessage(t, ch)
	if msg.Type != "ai-response-delta" {
		t.Fatalf("frame type = %s, want ai-response-delta: %+v", msg.Type, msg)
	}
	data, ok := msg.Data.(map[string]interface{})
	if !ok {
		t.Fatalf("delta data type = %T: %+v", msg.Data, msg)
	}
	if data["content_delta"] != wantDelta {
		t.Fatalf("content_delta = %v, want %q", data["content_delta"], wantDelta)
	}
	if data["done"] != wantDone {
		t.Fatalf("done = %v, want %v", data["done"], wantDone)
	}
}

// TestWebSocket_AIStreamFramesDeltasThenFullResponse 流式首答主链路：
// 增量帧即产即发（done=false）→ 终末 delta（done=true）→ 完整 ai-response
// 终帧（含引用来源与产生方式），全程不回退单发。
func TestWebSocket_AIStreamFramesDeltasThenFullResponse(t *testing.T) {
	hub := NewWebSocketHub()
	go hub.Run()
	ai := &streamingAI{stream: eventsChannel(
		aidelivery.AIStreamEvent{ContentDelta: "你"},
		aidelivery.AIStreamEvent{ContentDelta: "好"},
		aidelivery.AIStreamEvent{Done: true, Final: &aidelivery.AIResponse{
			Content:    "你好",
			Confidence: 0.8,
			Source:     "ai",
			Strategy:   "llm",
			Sources:    []baseweknora.SearchResult{{DocumentID: "doc-1", Title: "政策"}},
		}},
	)}
	hub.SetAIService(ai)
	c := &WebSocketClient{ID: "c", SessionID: "s", Send: make(chan WebSocketMessage, 8), Hub: hub}
	hub.register <- c
	time.Sleep(20 * time.Millisecond)

	c.processMessageWithAI(WebSocketMessage{Type: "text-message", Data: map[string]interface{}{"content": "hi"}})

	expectDelta(t, c.Send, "你", false)
	expectDelta(t, c.Send, "好", false)
	expectDelta(t, c.Send, "", true)

	full := waitForMessage(t, c.Send)
	if full.Type != "ai-response" {
		t.Fatalf("frame type = %s, want ai-response: %+v", full.Type, full)
	}
	data := full.Data.(map[string]interface{})
	if data["content"] != "你好" || data["strategy"] != "llm" {
		t.Fatalf("full response data = %+v", data)
	}
	if _, ok := data["sources"]; !ok {
		t.Fatalf("sources missing: %+v", data)
	}
	if ai.processCalls.Load() != 0 {
		t.Fatal("streaming success must not fall back to ProcessQuery")
	}
}

// TestWebSocket_AIStreamStartErrorFallsBackToProcessQuery 流启动即失败：
// 无增量帧，整体回退非流式单发路径。
func TestWebSocket_AIStreamStartErrorFallsBackToProcessQuery(t *testing.T) {
	hub := NewWebSocketHub()
	go hub.Run()
	ai := &streamingAI{streamErr: context.DeadlineExceeded}
	hub.SetAIService(ai)
	c := &WebSocketClient{ID: "c", SessionID: "s", Send: make(chan WebSocketMessage, 4), Hub: hub}
	hub.register <- c
	time.Sleep(20 * time.Millisecond)

	c.processMessageWithAI(WebSocketMessage{Type: "text-message", Data: map[string]interface{}{"content": "hi"}})

	full := waitForMessage(t, c.Send)
	if full.Type != "ai-response" {
		t.Fatalf("frame type = %s, want ai-response from fallback: %+v", full.Type, full)
	}
	if ai.processCalls.Load() != 1 {
		t.Fatalf("ProcessQuery calls = %d, want 1", ai.processCalls.Load())
	}
	if got := len(c.Send); got != 0 {
		t.Fatalf("extra frames = %d, want 0", got)
	}
}

// TestWebSocket_AIStreamNilChannelFallsBack (nil, nil) 流：归一为启动失败，
// 回退非流式单发路径。
func TestWebSocket_AIStreamNilChannelFallsBack(t *testing.T) {
	hub := NewWebSocketHub()
	go hub.Run()
	ai := &streamingAI{returnNilCh: true}
	hub.SetAIService(ai)
	c := &WebSocketClient{ID: "c", SessionID: "s", Send: make(chan WebSocketMessage, 4), Hub: hub}
	hub.register <- c
	time.Sleep(20 * time.Millisecond)

	c.processMessageWithAI(WebSocketMessage{Type: "text-message", Data: map[string]interface{}{"content": "hi"}})

	full := waitForMessage(t, c.Send)
	if full.Type != "ai-response" {
		t.Fatalf("frame type = %s, want ai-response from fallback: %+v", full.Type, full)
	}
	if ai.processCalls.Load() != 1 {
		t.Fatalf("ProcessQuery calls = %d, want 1", ai.processCalls.Load())
	}
}

// TestWebSocket_AIStreamMidStreamFailureNoFinal 增量已推送后流失败：
// 补终末 delta（done=true）但不再发 ai-response，也不回退单发（避免文本重复）。
func TestWebSocket_AIStreamMidStreamFailureNoFinal(t *testing.T) {
	hub := NewWebSocketHub()
	go hub.Run()
	ai := &streamingAI{stream: eventsChannel(
		aidelivery.AIStreamEvent{ContentDelta: "部分"},
		aidelivery.AIStreamEvent{Done: true},
	)}
	hub.SetAIService(ai)
	c := &WebSocketClient{ID: "c", SessionID: "s", Send: make(chan WebSocketMessage, 4), Hub: hub}
	hub.register <- c
	time.Sleep(20 * time.Millisecond)

	c.processMessageWithAI(WebSocketMessage{Type: "text-message", Data: map[string]interface{}{"content": "hi"}})

	expectDelta(t, c.Send, "部分", false)
	expectDelta(t, c.Send, "", true)
	time.Sleep(50 * time.Millisecond)
	if got := len(c.Send); got != 0 {
		t.Fatalf("extra frames after stream failure = %d, want 0", got)
	}
	if ai.processCalls.Load() != 0 {
		t.Fatal("mid-stream failure must not fall back (would duplicate streamed text)")
	}
}

// TestWebSocket_AIStreamNoStreamerCapabilityUsesSingleShot 无流式能力的服务：
// 与既有单发路径行为一致（可选接口协商）。
func TestWebSocket_AIStreamNoStreamerCapabilityUsesSingleShot(t *testing.T) {
	hub := NewWebSocketHub()
	go hub.Run()
	ai := &unitAIService{}
	hub.SetAIService(ai)
	c := &WebSocketClient{ID: "c", SessionID: "s", Send: make(chan WebSocketMessage, 4), Hub: hub}
	hub.register <- c
	time.Sleep(20 * time.Millisecond)

	c.processMessageWithAI(WebSocketMessage{Type: "text-message", Data: map[string]interface{}{"content": "hi"}})

	full := waitForMessage(t, c.Send)
	if full.Type != "ai-response" {
		t.Fatalf("frame type = %s, want ai-response: %+v", full.Type, full)
	}
	if ai.processCalls.Load() != 1 {
		t.Fatalf("ProcessQuery calls = %d, want 1", ai.processCalls.Load())
	}
}
