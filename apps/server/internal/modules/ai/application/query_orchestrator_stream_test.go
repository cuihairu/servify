package application

import (
	"context"
	"errors"
	"strings"
	"testing"

	"servify/apps/server/internal/platform/knowledgeprovider"
	mockkp "servify/apps/server/internal/platform/knowledgeprovider/mock"
	"servify/apps/server/internal/platform/llm"
	mockllm "servify/apps/server/internal/platform/llm/mock"
)

// drainStream 读取流式事件直至 channel 关闭。
func drainStream(t *testing.T, stream <-chan StreamEvent) []StreamEvent {
	t.Helper()
	var events []StreamEvent
	for evt := range stream {
		events = append(events, evt)
	}
	return events
}

// TestHandleStreamStreamsDeltasAndFinalizes 无工具路径：内容分片即产即发，
// 终末 Done 携带完整结果——内容为分片拼接、引用来源随检索命中透出、
// provider 缺省回落 "llm"。
func TestHandleStreamStreamsDeltasAndFinalizes(t *testing.T) {
	hits := []knowledgeprovider.KnowledgeHit{{DocumentID: "doc-1", Title: "退款政策", Content: "7 天无理由"}}
	provider := &mockllm.Provider{StreamChunks: []llm.ChatChunk{
		{ContentDelta: "你好"},
		{ContentDelta: "，已为您查询。"},
		{Done: true},
	}}
	orchestrator := NewQueryOrchestrator(provider, &mockkp.Provider{Hits: hits})

	stream, err := orchestrator.HandleStream(context.Background(), AIRequest{
		Query:          "退款政策是什么",
		ConversationID: "s-stream-1",
		RetrievalPolicy: RetrievalPolicy{
			Enabled: true,
			TopK:    5,
		},
	})
	if err != nil {
		t.Fatalf("HandleStream() error = %v", err)
	}
	events := drainStream(t, stream)
	if len(events) != 3 {
		t.Fatalf("events = %d, want 3: %+v", len(events), events)
	}
	if events[0].ContentDelta != "你好" || events[1].ContentDelta != "，已为您查询。" {
		t.Fatalf("deltas wrong: %+v", events[:2])
	}
	final := events[2]
	if !final.Done || final.Err != nil || final.Response == nil {
		t.Fatalf("terminal event wrong: %+v", final)
	}
	if final.Response.Content != "你好，已为您查询。" {
		t.Fatalf("content = %q", final.Response.Content)
	}
	if len(final.Response.Sources) != 1 || final.Response.Sources[0].DocumentID != "doc-1" {
		t.Fatalf("sources = %+v", final.Response.Sources)
	}
	if final.Response.Provider != "llm" {
		t.Fatalf("provider = %q, want default llm", final.Response.Provider)
	}
	if final.Response.Truncated {
		t.Fatal("short content must not be truncated")
	}
}

// TestHandleStreamAgentLoopToolThenAnswer 流式 agent 循环：分片中的完整
// 工具调用触发执行进入下一步；最终内容为全部增量拼接（与客户端增量渲染
// 一致，完成帧替换是 no-op）。
func TestHandleStreamAgentLoopToolThenAnswer(t *testing.T) {
	provider := &mockllm.Provider{
		StreamQueue: [][]llm.ChatChunk{
			{
				{ContentDelta: "让我查一下"},
				{ToolCall: &llm.ToolCall{ID: "call-1", Name: "test_tool", Arguments: map[string]interface{}{"input": "hello"}}},
				{Done: true},
			},
			{
				{ContentDelta: "查到了：已完成。"},
				{Done: true},
			},
		},
	}
	registry := NewToolRegistry()
	registry.Register(&stubTool{
		name:   "test_tool",
		desc:   "A test tool",
		schema: map[string]interface{}{"type": "object"},
		result: map[string]interface{}{"status": "done"},
	})
	orchestrator := NewQueryOrchestrator(provider, nil)
	orchestrator.SetToolExecutor(NewToolExecutor(registry, nil))

	stream, err := orchestrator.HandleStream(context.Background(), AIRequest{
		Query: "run test tool",
		ToolPolicy: ToolPolicy{
			Enabled:  true,
			MaxSteps: 5,
		},
	})
	if err != nil {
		t.Fatalf("HandleStream() error = %v", err)
	}
	events := drainStream(t, stream)

	var deltas []string
	var final StreamEvent
	for _, evt := range events {
		if evt.Done {
			final = evt
		} else {
			deltas = append(deltas, evt.ContentDelta)
		}
	}
	if len(deltas) != 2 || strings.Join(deltas, "") != "让我查一下查到了：已完成。" {
		t.Fatalf("deltas = %q", deltas)
	}
	if final.Err != nil || final.Response == nil {
		t.Fatalf("terminal event wrong: %+v", final)
	}
	if final.Response.Content != "让我查一下查到了：已完成。" {
		t.Fatalf("final content = %q, want accumulated deltas", final.Response.Content)
	}

	// 第二步请求携带工具反馈：assistant（含工具调用）+ tool 结果。
	reqs := provider.RecordedRequests()
	if len(reqs) != 2 {
		t.Fatalf("chat stream calls = %d, want 2", len(reqs))
	}
	msgs := reqs[1].Messages
	if len(msgs) < 3 {
		t.Fatalf("second step messages = %d, want >= 3: %+v", len(msgs), msgs)
	}
	assistant := msgs[len(msgs)-2]
	toolMsg := msgs[len(msgs)-1]
	if assistant.Role != "assistant" || len(assistant.ToolCalls) != 1 || assistant.ToolCalls[0].ID != "call-1" {
		t.Fatalf("assistant feedback message = %+v", assistant)
	}
	if toolMsg.Role != "tool" || toolMsg.ToolCallID != "call-1" || !strings.Contains(toolMsg.Content, "status") {
		t.Fatalf("tool result message = %+v", toolMsg)
	}

	if got := orchestrator.Metrics().ToolCallCount; got != 1 {
		t.Fatalf("tool call metric = %d, want 1", got)
	}
}

// TestHandleStreamChatStreamError ChatStream 启动即失败：同步期无事件、
// 返回错误前不做任何模型交互；错误经终末 Done（Err 非空、Response 空）透出。
func TestHandleStreamChatStreamError(t *testing.T) {
	provider := &mockllm.Provider{StreamError: errors.New("upstream 500")}
	orchestrator := NewQueryOrchestrator(provider, nil)

	stream, err := orchestrator.HandleStream(context.Background(), AIRequest{Query: "你好"})
	if err != nil {
		t.Fatalf("HandleStream() error = %v, want stream started", err)
	}
	events := drainStream(t, stream)
	if len(events) != 1 || !events[0].Done || events[0].Err == nil || events[0].Response != nil {
		t.Fatalf("events = %+v, want single terminal error event", events)
	}
}

// TestHandleStreamStepErrorQueue 多步循环中途失败：错误队列按次消费
// （首步放行工具调用、次步报错），中途失败以终末 Done 事件透出。
func TestHandleStreamStepErrorQueue(t *testing.T) {
	provider := &mockllm.Provider{
		StreamQueue: [][]llm.ChatChunk{
			{
				{ContentDelta: "部分"},
				{ToolCall: &llm.ToolCall{ID: "call-1", Name: "test_tool"}},
				{Done: true},
			},
			nil,
		},
		StreamErrorQueue: []error{nil, errors.New("step-2 down")},
	}
	registry := NewToolRegistry()
	registry.Register(&stubTool{name: "test_tool", desc: "tool", result: map[string]interface{}{"ok": true}})
	orchestrator := NewQueryOrchestrator(provider, nil)
	orchestrator.SetToolExecutor(NewToolExecutor(registry, nil))

	stream, err := orchestrator.HandleStream(context.Background(), AIRequest{
		Query: "你好",
		ToolPolicy: ToolPolicy{
			Enabled:  true,
			MaxSteps: 3,
		},
	})
	if err != nil {
		t.Fatalf("HandleStream() error = %v", err)
	}
	events := drainStream(t, stream)
	var deltas, terminals []StreamEvent
	for _, evt := range events {
		if evt.Done {
			terminals = append(terminals, evt)
		} else {
			deltas = append(deltas, evt)
		}
	}
	if len(deltas) != 1 || deltas[0].ContentDelta != "部分" {
		t.Fatalf("deltas = %+v", deltas)
	}
	if len(terminals) != 1 || terminals[0].Err == nil || terminals[0].Response != nil {
		t.Fatalf("terminals = %+v, want single error terminal", terminals)
	}
}

// TestHandleStreamMaxStepsReached 步数耗尽：以已流出的内容收尾（Done 带
// Response），不再发起下一步调用。
func TestHandleStreamMaxStepsReached(t *testing.T) {
	provider := &mockllm.Provider{StreamChunks: []llm.ChatChunk{
		{ToolCall: &llm.ToolCall{ID: "call-1", Name: "test_tool"}},
		{Done: true},
	}}
	registry := NewToolRegistry()
	registry.Register(&stubTool{name: "test_tool", desc: "tool", result: map[string]interface{}{"ok": true}})
	orchestrator := NewQueryOrchestrator(provider, nil)
	orchestrator.SetToolExecutor(NewToolExecutor(registry, nil))

	stream, err := orchestrator.HandleStream(context.Background(), AIRequest{
		Query: "loop",
		ToolPolicy: ToolPolicy{
			Enabled:  true,
			MaxSteps: 1,
		},
	})
	if err != nil {
		t.Fatalf("HandleStream() error = %v", err)
	}
	events := drainStream(t, stream)
	var terminals []StreamEvent
	for _, evt := range events {
		if evt.Done {
			terminals = append(terminals, evt)
		}
	}
	if len(terminals) != 1 || terminals[0].Response == nil || terminals[0].Err != nil {
		t.Fatalf("terminals = %+v, want single success terminal", terminals)
	}
	if len(provider.RecordedRequests()) != 1 {
		t.Fatalf("chat stream calls = %d, want 1 (max steps)", len(provider.RecordedRequests()))
	}
}

// TestHandleStreamNilProvider llmProvider 未接线：与 Handle 同契约为
// (nil, nil)，由调用方归一成错误回退非流式路径。
func TestHandleStreamNilProvider(t *testing.T) {
	orchestrator := NewQueryOrchestrator(nil, nil)
	stream, err := orchestrator.HandleStream(context.Background(), AIRequest{Query: "你好"})
	if err != nil || stream != nil {
		t.Fatalf("HandleStream() = (%v, %v), want (nil, nil)", stream, err)
	}
}

// TestHandleStreamPolicyRejection 策略钩子拒绝：同步返回错误、不产出事件
// channel（prepare 共享前置流水线的 HandleStream 侧覆盖）。
func TestHandleStreamPolicyRejection(t *testing.T) {
	orchestrator := NewQueryOrchestrator(&mockllm.Provider{}, nil)
	orchestrator.SetPolicyHooks(stubPolicyHook{decision: PolicyDecision{Allowed: false, Reason: "denied"}})

	stream, err := orchestrator.HandleStream(context.Background(), AIRequest{Query: "你好"})
	if err == nil || stream != nil {
		t.Fatalf("HandleStream() = (%v, %v), want (nil, error)", stream, err)
	}
}

// manualStreamProvider 以外部 channel 手动供给分片（放弃场景测试用），
// 其余方法借用内嵌 mock。
type manualStreamProvider struct {
	mockllm.Provider
	ch chan llm.ChatChunk
}

func (p *manualStreamProvider) ChatStream(ctx context.Context, req llm.ChatRequest) (<-chan llm.ChatChunk, error) {
	return p.ch, nil
}

// TestHandleStreamAbandonsOnContextCancel 调用方取消 ctx：事件发射侧
// select 走 ctx.Done 分支放弃，channel 关闭且不产出任何事件。
func TestHandleStreamAbandonsOnContextCancel(t *testing.T) {
	ch := make(chan llm.ChatChunk)
	provider := &manualStreamProvider{ch: ch}
	orchestrator := NewQueryOrchestrator(provider, nil)

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	stream, err := orchestrator.HandleStream(ctx, AIRequest{Query: "你好"})
	if err != nil {
		t.Fatalf("HandleStream() error = %v", err)
	}

	// 放一个内容分片：goroutine 消费后阻塞在事件发射（无读者）。
	ch <- llm.ChatChunk{ContentDelta: "x"}
	// 取消 ctx：发射 select 应走 ctx.Done 分支并整体放弃。
	cancel()
	for evt := range stream {
		t.Fatalf("no event should be delivered after cancel, got %+v", evt)
	}
}
