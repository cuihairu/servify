package delivery

import (
	"context"
	"errors"
	"testing"
	"time"

	"servify/apps/server/internal/models"
	"servify/apps/server/internal/platform/knowledgeprovider"
	kpmock "servify/apps/server/internal/platform/knowledgeprovider/mock"
	llm "servify/apps/server/internal/platform/llm"
	"servify/apps/server/internal/platform/llm/mock"
	llmmock "servify/apps/server/internal/platform/llm/mock"
)

// fakeHistoryLoader SessionHistoryLoader 的测试桩：按 DESC 序（最新在前）
// 返回预置消息，与 conversation 仓储的真实排序一致。
type fakeHistoryLoader struct {
	msgs      []models.Message
	err       error
	lastLimit int
	sessionID string
}

func (f *fakeHistoryLoader) ListRecentMessages(ctx context.Context, sessionID string, limit int) ([]models.Message, error) {
	f.lastLimit = limit
	f.sessionID = sessionID
	if f.err != nil {
		return nil, f.err
	}
	return f.msgs, nil
}

// userMessageContents 取最后一次 LLM 请求里 user 角色消息的内容序列，
// 作为多轮上下文是否正确进入 prompt 的观测点。
func userMessageContents(t *testing.T, provider *llmmock.Provider) []string {
	t.Helper()
	if len(provider.Requests) == 0 {
		t.Fatal("expected at least one llm chat request")
	}
	req := provider.Requests[len(provider.Requests)-1]
	out := make([]string, 0, len(req.Messages))
	for _, m := range req.Messages {
		if m.Role == "user" {
			out = append(out, m.Content)
		}
	}
	return out
}

func newMultiturnService(t *testing.T, loader SessionHistoryLoader) (*OrchestratedEnhancedAIService, *llmmock.Provider) {
	t.Helper()
	base := NewAIService("", "")
	base.InitializeKnowledgeBase()
	provider := &llmmock.Provider{ChatResponse: llm.ChatResponse{Content: "answer"}}
	svc := NewOrchestratedEnhancedAIService(base, provider, &kpmock.Provider{}, "", nil)
	if loader != nil {
		svc = svc.WithSessionHistory(loader)
	}
	return svc, provider
}

func TestSessionHistoryFeedsMultiTurnContext(t *testing.T) {
	// 仓储按 created_at DESC 返回：最新（当前问题）在最前。
	loader := &fakeHistoryLoader{msgs: []models.Message{
		{Sender: "customer", Content: "Servify 是什么", CreatedAt: time.Now()},
		{Sender: "agent", Content: "Servify 是智能客服平台", CreatedAt: time.Now().Add(-time.Minute)},
		{Sender: "customer", Content: "你好", CreatedAt: time.Now().Add(-2 * time.Minute)},
	}}
	svc, provider := newMultiturnService(t, svcLoader(loader))

	if _, err := svc.ProcessQueryEnhanced(context.Background(), "有什么功能", "sess-1"); err != nil {
		t.Fatalf("ProcessQueryEnhanced: %v", err)
	}
	// 历史按时间升序进 prompt（agent 消息转 assistant，不在 user 序列里），
	// 当前 query 恒为最后一条 user 消息（PromptBuilder 只在 Messages 为空
	// 时才自己追加 query）。
	want := []string{"你好", "Servify 是什么", "有什么功能"}
	got := userMessageContents(t, provider)
	if len(got) != len(want) {
		t.Fatalf("user messages = %v, want %v", got, want)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Fatalf("user messages = %v, want %v", got, want)
		}
	}
	if loader.sessionID != "sess-1" {
		t.Fatalf("expected session id forwarded, got %q", loader.sessionID)
	}
	// limit 取 turns+1：多出的一条用于识别已落库的当前消息。
	if loader.lastLimit != aiHistoryTurns+1 {
		t.Fatalf("expected history limit %d, got %d", aiHistoryTurns+1, loader.lastLimit)
	}
}

func TestSessionHistoryDedupesTrailingCurrentMessage(t *testing.T) {
	// WS 链路先落库客户消息再调 AI：最新一条与 query 相同，去重避免重复提问。
	loader := &fakeHistoryLoader{msgs: []models.Message{
		{Sender: "customer", Content: "Servify 是什么", CreatedAt: time.Now()},
		{Sender: "customer", Content: "你好", CreatedAt: time.Now().Add(-time.Minute)},
	}}
	svc, provider := newMultiturnService(t, svcLoader(loader))

	if _, err := svc.ProcessQueryEnhanced(context.Background(), "Servify 是什么", "sess-2"); err != nil {
		t.Fatalf("ProcessQueryEnhanced: %v", err)
	}
	got := userMessageContents(t, provider)
	want := []string{"你好", "Servify 是什么"}
	if len(got) != len(want) || got[0] != want[0] || got[1] != want[1] {
		t.Fatalf("user messages = %v, want %v", got, want)
	}
}

func TestSessionHistoryCapsAtTurnLimit(t *testing.T) {
	// 10 条历史（DESC）→ 截取最近 8 条，最旧的 2 条被裁掉。
	msgs := make([]models.Message, 0, 10)
	for i := 0; i < 10; i++ {
		msgs = append(msgs, models.Message{
			Sender:    "customer",
			Content:   "历史消息" + string(rune('0'+i)),
			CreatedAt: time.Now().Add(-time.Duration(i) * time.Minute),
		})
	}
	loader := &fakeHistoryLoader{msgs: msgs}
	svc, provider := newMultiturnService(t, svcLoader(loader))

	if _, err := svc.ProcessQueryEnhanced(context.Background(), "当前问题", "sess-3"); err != nil {
		t.Fatalf("ProcessQueryEnhanced: %v", err)
	}
	got := userMessageContents(t, provider)
	if len(got) != aiHistoryTurns+1 {
		t.Fatalf("expected %d user messages (capped history + query), got %d: %v", aiHistoryTurns+1, len(got), got)
	}
	if got[0] == "历史消息9" {
		t.Fatal("oldest messages should be trimmed first")
	}
}

func TestSessionHistorySkipsSystemAndEmptyMessages(t *testing.T) {
	loader := &fakeHistoryLoader{msgs: []models.Message{
		{Sender: "system", Content: "会话已创建", CreatedAt: time.Now()},
		{Sender: "agent", Content: "", CreatedAt: time.Now().Add(-time.Second)},
		{Sender: "customer", Content: "你好", CreatedAt: time.Now().Add(-time.Minute)},
	}}
	svc, provider := newMultiturnService(t, svcLoader(loader))

	if _, err := svc.ProcessQueryEnhanced(context.Background(), "在吗", "sess-4"); err != nil {
		t.Fatalf("ProcessQueryEnhanced: %v", err)
	}
	got := userMessageContents(t, provider)
	want := []string{"你好", "在吗"}
	if len(got) != len(want) || got[0] != want[0] || got[1] != want[1] {
		t.Fatalf("user messages = %v, want %v", got, want)
	}
}

func TestSessionHistoryErrorFallsBackToSingleTurn(t *testing.T) {
	loader := &fakeHistoryLoader{err: errors.New("db down")}
	svc, provider := newMultiturnService(t, svcLoader(loader))

	if _, err := svc.ProcessQueryEnhanced(context.Background(), "普通问题", "sess-5"); err != nil {
		t.Fatalf("history load failure must not fail the query: %v", err)
	}
	// 降级为单轮：Messages 为空 → PromptBuilder 自己追加 query，
	// user 消息只有当前问题一条。
	got := userMessageContents(t, provider)
	if len(got) != 1 || got[0] != "普通问题" {
		t.Fatalf("expected single-turn fallback, got %v", got)
	}
}

func TestSessionHistoryNotInjectedWithoutLoaderOrSession(t *testing.T) {
	// nil receiver：与 WithRuntimeParams 同款 nil 安全，不得 panic。
	var nilSvc *OrchestratedEnhancedAIService
	nilSvc.WithSessionHistory(&fakeHistoryLoader{})

	// 无 loader：零值 = 历史单轮行为。
	svc, provider := newMultiturnService(t, nil)
	if _, err := svc.ProcessQueryEnhanced(context.Background(), "问题A", "sess-6"); err != nil {
		t.Fatalf("ProcessQueryEnhanced: %v", err)
	}
	if got := userMessageContents(t, provider); len(got) != 1 || got[0] != "问题A" {
		t.Fatalf("expected legacy single-turn, got %v", got)
	}

	// 有 loader 但 sessionID 为空（REST 匿名查询）：同样单轮。
	loader := &fakeHistoryLoader{msgs: []models.Message{
		{Sender: "customer", Content: "旧消息", CreatedAt: time.Now()},
	}}
	svc2, provider2 := newMultiturnService(t, svcLoader(loader))
	if _, err := svc2.ProcessQueryEnhanced(context.Background(), "问题B", "  "); err != nil {
		t.Fatalf("ProcessQueryEnhanced: %v", err)
	}
	if loader.sessionID != "" {
		t.Fatal("empty session id must not hit the loader")
	}
	if got := userMessageContents(t, provider2); len(got) != 1 || got[0] != "问题B" {
		t.Fatalf("expected single-turn for empty session, got %v", got)
	}
}

func svcLoader(loader *fakeHistoryLoader) SessionHistoryLoader { return loader }

func TestHandoffSuggestedOnLowConfidence(t *testing.T) {
	svc, _ := newMultiturnService(t, nil)
	// 默认阈值语义：零命中直答 confidence=0.6 < 0.65 → 建议转人工。
	svc = svc.WithRuntimeParams(AIRuntimeParams{HandoffEnabled: true, HandoffConfidenceThreshold: 0.65})

	resp, err := svc.ProcessQueryEnhanced(context.Background(), "冷门问题", "sess-7")
	if err != nil {
		t.Fatalf("ProcessQueryEnhanced: %v", err)
	}
	if resp.NextAction != "handoff" || resp.HandoffReason != "low_confidence" {
		t.Fatalf("expected handoff suggestion, got next_action=%q reason=%q", resp.NextAction, resp.HandoffReason)
	}
	if resp.Strategy != "llm" {
		t.Fatalf("expected llm strategy, got %q", resp.Strategy)
	}

	// 有命中（confidence=0.91 ≥ 0.65）：正常作答，无建议字段。
	// 检索 provider 必须在构造期传入——orchestrator 持有自己的引用。
	base2 := NewAIService("", "")
	base2.InitializeKnowledgeBase()
	svc2 := NewOrchestratedEnhancedAIService(
		base2,
		&llmmock.Provider{ChatResponse: llm.ChatResponse{Content: "answer"}},
		&kpmock.Provider{Hits: []knowledgeprovider.KnowledgeHit{
			{DocumentID: "doc-1", Title: "Billing", Content: "Billing details", Score: 0.91},
		}},
		"", nil,
	).WithRuntimeParams(AIRuntimeParams{HandoffEnabled: true, HandoffConfidenceThreshold: 0.65})
	resp2, err := svc2.ProcessQueryEnhanced(context.Background(), "billing", "sess-8")
	if err != nil {
		t.Fatalf("ProcessQueryEnhanced: %v", err)
	}
	if resp2.NextAction != "" || resp2.HandoffReason != "" {
		t.Fatalf("confident answer must not suggest handoff, got %+v", resp2.AIResponse)
	}
}

func TestHandoffDisabledByDefault(t *testing.T) {
	svc, _ := newMultiturnService(t, nil)
	// 零值 AIRuntimeParams（历史行为）：不开置信门，不产出建议字段。
	resp, err := svc.ProcessQueryEnhanced(context.Background(), "冷门问题", "sess-9")
	if err != nil {
		t.Fatalf("ProcessQueryEnhanced: %v", err)
	}
	if resp.NextAction != "" || resp.HandoffReason != "" {
		t.Fatalf("handoff must stay off by default, got %+v", resp.AIResponse)
	}
}

func TestProcessQueryCarriesEnhancedExtras(t *testing.T) {
	base := NewAIService("", "")
	base.InitializeKnowledgeBase()
	llmProvider := &mock.Provider{ChatResponse: llm.ChatResponse{Content: "answer"}}
	svc := NewOrchestratedEnhancedAIService(base, llmProvider, &kpmock.Provider{
		Hits: []knowledgeprovider.KnowledgeHit{
			{DocumentID: "doc-1", Title: "Billing", Content: "Billing details", Score: 0.91},
		},
	}, "weknora", nil)

	// WS / REST 普通查询路径：引用来源与产生方式带回 legacy AIResponse。
	resp, err := svc.ProcessQuery(context.Background(), "billing", "sess-10")
	if err != nil {
		t.Fatalf("ProcessQuery: %v", err)
	}
	if len(resp.Sources) != 1 {
		t.Fatalf("expected sources on legacy response, got %+v", resp)
	}
	if resp.Strategy != "weknora" {
		t.Fatalf("expected strategy passthrough, got %q", resp.Strategy)
	}
	if resp.NextAction != "" {
		t.Fatalf("confident answer must not set next_action, got %q", resp.NextAction)
	}
}

func TestNilOrchestratorResultFallsBack(t *testing.T) {
	// llmProvider 未接线时 Handle 返回 (nil, nil)：归一成错误走兜底，
	// 而不是对 result.Content 解引用 panic。
	base := NewAIService("", "")
	base.InitializeKnowledgeBase()
	svc := NewOrchestratedEnhancedAIService(base, nil, nil, "", nil)

	resp, err := svc.ProcessQueryEnhanced(context.Background(), "任意问题", "sess-11")
	if err != nil {
		t.Fatalf("expected fallback service, got error: %v", err)
	}
	if resp == nil || resp.Content == "" {
		t.Fatalf("expected fallback content, got %+v", resp)
	}
	if resp.Strategy != "fallback" {
		t.Fatalf("expected fallback strategy, got %q", resp.Strategy)
	}
}
