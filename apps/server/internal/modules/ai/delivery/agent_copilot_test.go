package delivery

import (
	"context"
	"errors"
	"strings"
	"testing"

	"servify/apps/server/internal/models"
	"servify/apps/server/internal/platform/llm"
	llmmock "servify/apps/server/internal/platform/llm/mock"
)

// --- 测试脚手架：复用多轮上下文测试的 fakeHistoryLoader 形状 ---

func newCopilotService(provider *llmmock.Provider, loader SessionHistoryLoader) *AgentCopilotService {
	svc := NewAgentCopilotService(provider, AIRuntimeParams{Model: "test-model", Temperature: 0.3, MaxTokens: 256, TimeoutMs: 1500})
	return svc.WithSessionHistory(loader)
}

func descMessages(items ...[2]string) []models.Message {
	// 入参按时间降序（最新在前），与 ListRecentMessages 契约一致。
	out := make([]models.Message, 0, len(items))
	for _, it := range items {
		out = append(out, models.Message{Sender: it[0], Content: it[1]})
	}
	return out
}

func TestCopilotSuggestReplyFeedsHistory(t *testing.T) {
	provider := &llmmock.Provider{ChatResponse: llm.ChatResponse{Content: "  好的，已为您登记，稍后回复。  "}}
	loader := &fakeHistoryLoader{msgs: descMessages(
		[2]string{"customer", "多久能到？"},
		[2]string{"agent", "您好，正在为您核实"},
		[2]string{"customer", "我的订单还没发货"},
	)}
	svc := newCopilotService(provider, loader)

	resp, err := svc.Copilot(context.Background(), CopilotRequest{Action: CopilotSuggestReply, SessionID: "s-1"})
	if err != nil {
		t.Fatalf("Copilot() error = %v", err)
	}
	if resp.Action != CopilotSuggestReply || resp.SessionID != "s-1" {
		t.Fatalf("unexpected meta: %+v", resp)
	}
	if resp.Text != "好的，已为您登记，稍后回复。" {
		t.Fatalf("Text = %q, want trimmed suggestion", resp.Text)
	}
	if loader.lastLimit != aiHistoryTurns {
		t.Fatalf("history limit = %d, want %d", loader.lastLimit, aiHistoryTurns)
	}
	reqs := provider.RecordedRequests()
	if len(reqs) != 1 {
		t.Fatalf("recorded requests = %d, want 1", len(reqs))
	}
	msgs := reqs[0].Messages
	// 历史 3 条（升序）+ 末尾指令 = 4 条；指令是最后一条 user 消息。
	if len(msgs) != 4 {
		t.Fatalf("outbound messages = %d, want 4: %+v", len(msgs), msgs)
	}
	if msgs[0].Role != "user" || msgs[0].Content != "我的订单还没发货" {
		t.Fatalf("first history message = %+v", msgs[0])
	}
	if msgs[1].Role != "assistant" || msgs[2].Role != "user" {
		t.Fatalf("history roles wrong: %+v", msgs[1:3])
	}
	if !strings.Contains(msgs[3].Content, "为坐席起草下一条回复") {
		t.Fatalf("trailing instruction missing: %q", msgs[3].Content)
	}
	if reqs[0].Model != "test-model" || reqs[0].MaxTokens != 256 || reqs[0].Options.TimeoutMs != 1500 {
		t.Fatalf("runtime params not passed through: %+v", reqs[0])
	}
}

func TestCopilotSuggestReplyValidation(t *testing.T) {
	svc := newCopilotService(&llmmock.Provider{}, &fakeHistoryLoader{})

	if _, err := svc.Copilot(context.Background(), CopilotRequest{Action: CopilotSuggestReply, SessionID: "  "}); !errors.Is(err, ErrCopilotSessionRequired) {
		t.Fatalf("empty session error = %v, want ErrCopilotSessionRequired", err)
	}
	// loader 加载失败：上抛而非静默降级（坐席工具明确失败）。
	failSvc := newCopilotService(&llmmock.Provider{}, &fakeHistoryLoader{err: errors.New("db down")})
	if _, err := failSvc.Copilot(context.Background(), CopilotRequest{Action: CopilotSuggestReply, SessionID: "s-1"}); err == nil || !strings.Contains(err.Error(), "load session history") {
		t.Fatalf("loader error = %v, want wrapped history error", err)
	}
	// 无历史消息：宁可明确失败也不凭空编上下文（suggest_reply 与 summarize 同规）。
	noHist := &AgentCopilotService{llm: &llmmock.Provider{}}
	if _, err := noHist.Copilot(context.Background(), CopilotRequest{Action: CopilotSuggestReply, SessionID: "s-2"}); !errors.Is(err, ErrCopilotEmptyHistory) {
		t.Fatalf("no-history error = %v, want ErrCopilotEmptyHistory", err)
	}
	if _, err := noHist.Copilot(context.Background(), CopilotRequest{Action: CopilotSummary, SessionID: "s-2"}); !errors.Is(err, ErrCopilotEmptyHistory) {
		t.Fatalf("summarize no-history error = %v, want ErrCopilotEmptyHistory", err)
	}
}

func TestCopilotRewrite(t *testing.T) {
	provider := &llmmock.Provider{ChatResponse: llm.ChatResponse{Content: "已为您加急处理。"}}
	svc := newCopilotService(provider, nil)

	for _, tc := range []struct {
		tone string
		want string
	}{
		{"", "专业"},
		{"professional", "专业"},
		{"friendly", "亲切友好"},
		{"concise", "简洁"},
		{"  playful  ", "playful"},
	} {
		resp, err := svc.Copilot(context.Background(), CopilotRequest{Action: CopilotRewrite, Draft: "  你的单子好了  ", Tone: tc.tone})
		if err != nil {
			t.Fatalf("rewrite(tone=%q) error = %v", tc.tone, err)
		}
		if resp.Action != CopilotRewrite || resp.Text != "已为您加急处理。" {
			t.Fatalf("rewrite(tone=%q) resp = %+v", tc.tone, resp)
		}
		reqs := provider.RecordedRequests()
		req := reqs[len(reqs)-1]
		if len(req.Messages) != 2 || req.Messages[0].Role != "system" {
			t.Fatalf("rewrite messages = %+v", req.Messages)
		}
		if !strings.Contains(req.Messages[0].Content, tc.want) {
			t.Fatalf("tone %q label %q not in prompt: %q", tc.tone, tc.want, req.Messages[0].Content)
		}
		if !strings.Contains(req.Messages[1].Content, "你的单子好了") {
			t.Fatalf("draft missing: %q", req.Messages[1].Content)
		}
	}

	if _, err := svc.Copilot(context.Background(), CopilotRequest{Action: CopilotRewrite, Draft: "   "}); !errors.Is(err, ErrCopilotDraftRequired) {
		t.Fatalf("blank draft error = %v, want ErrCopilotDraftRequired", err)
	}
}

func TestCopilotSummarize(t *testing.T) {
	provider := &llmmock.Provider{ChatResponse: llm.ChatResponse{Content: "1. 客户问题：发货\n2. 处理过程：核实\n3. 结论/待办：跟进"}}
	loader := &fakeHistoryLoader{msgs: descMessages(
		[2]string{"agent", "正在核实物流"},
		[2]string{"system", "会话已转接"}, // system 不进摘要
		[2]string{"customer", "订单还没发货"},
	)}
	svc := newCopilotService(provider, loader)

	resp, err := svc.Copilot(context.Background(), CopilotRequest{Action: CopilotSummary, SessionID: "s-9"})
	if err != nil {
		t.Fatalf("Copilot() error = %v", err)
	}
	if resp.Action != CopilotSummary || resp.SessionID != "s-9" {
		t.Fatalf("unexpected meta: %+v", resp)
	}
	req := provider.RecordedRequests()[0]
	if len(req.Messages) != 2 || req.Messages[0].Role != "system" {
		t.Fatalf("summary messages = %+v", req.Messages)
	}
	transcript := req.Messages[1].Content
	if !strings.Contains(transcript, "坐席：正在核实物流") || !strings.Contains(transcript, "客户：订单还没发货") {
		t.Fatalf("transcript wrong: %q", transcript)
	}
	if strings.Contains(transcript, "转接") {
		t.Fatalf("system message leaked into transcript: %q", transcript)
	}

	if _, err := svc.Copilot(context.Background(), CopilotRequest{Action: CopilotSummary, SessionID: " "}); !errors.Is(err, ErrCopilotSessionRequired) {
		t.Fatalf("empty session error = %v", err)
	}
}

func TestCopilotDispatchErrors(t *testing.T) {
	provider := &llmmock.Provider{ChatError: errors.New("upstream down")}
	svc := newCopilotService(provider, &fakeHistoryLoader{msgs: descMessages([2]string{"customer", "你好"})})

	if _, err := svc.Copilot(context.Background(), CopilotRequest{Action: "bogus"}); !errors.Is(err, ErrCopilotUnsupportedAction) {
		t.Fatalf("unknown action error = %v, want ErrCopilotUnsupportedAction", err)
	}
	if _, err := svc.Copilot(context.Background(), CopilotRequest{Action: CopilotSuggestReply, SessionID: "s-1"}); err == nil || !strings.Contains(err.Error(), "ai copilot chat") {
		t.Fatalf("chat error = %v, want wrapped chat error", err)
	}

	emptyProvider := &llmmock.Provider{}
	svc2 := newCopilotService(emptyProvider, &fakeHistoryLoader{msgs: descMessages([2]string{"customer", "你好"})})
	if _, err := svc2.Copilot(context.Background(), CopilotRequest{Action: CopilotSummary, SessionID: "s-1"}); err == nil || !strings.Contains(err.Error(), "empty content") {
		t.Fatalf("empty content error = %v", err)
	}

	// rewrite 的模型故障分支；summarize 的历史加载失败分支。
	if _, err := svc.Copilot(context.Background(), CopilotRequest{Action: CopilotRewrite, Draft: "草稿"}); err == nil || !strings.Contains(err.Error(), "ai copilot chat") {
		t.Fatalf("rewrite chat error = %v", err)
	}
	failSum := newCopilotService(&llmmock.Provider{}, &fakeHistoryLoader{err: errors.New("db down")})
	if _, err := failSum.Copilot(context.Background(), CopilotRequest{Action: CopilotSummary, SessionID: "s-1"}); err == nil || !strings.Contains(err.Error(), "load session history") {
		t.Fatalf("summarize history error = %v", err)
	}
}

func TestCopilotNilSafety(t *testing.T) {
	// nil 接收者与 nil provider 都降级为 ErrCopilotUnavailable（handler→503）。
	var nilSvc *AgentCopilotService
	if _, err := nilSvc.Copilot(context.Background(), CopilotRequest{Action: CopilotRewrite, Draft: "x"}); !errors.Is(err, ErrCopilotUnavailable) {
		t.Fatalf("nil receiver error = %v, want ErrCopilotUnavailable", err)
	}
	if _, err := NewAgentCopilotService(nil, AIRuntimeParams{}).Copilot(context.Background(), CopilotRequest{Action: CopilotRewrite, Draft: "x"}); !errors.Is(err, ErrCopilotUnavailable) {
		t.Fatalf("nil llm error = %v, want ErrCopilotUnavailable", err)
	}
	// nil 接收者链式回填安全。
	nilSvc.WithSessionHistory(&fakeHistoryLoader{})
}
