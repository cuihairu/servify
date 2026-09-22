package infra

import (
	"context"
	"strings"
	"testing"
	"time"

	"servify/apps/server/internal/modules/quality/application"
	"servify/apps/server/internal/platform/llm"
)

// TestLLMScoreProviderTranscriptSingleTurnOverBudget 覆盖 head 收敛到可用轮数、
// 首部即超预算直接返回的分支。
func TestLLMScoreProviderTranscriptSingleTurnOverBudget(t *testing.T) {
	provider := &stubChatProvider{resp: llm.ChatResponse{Content: goodScoreJSON()}}
	p := NewLLMScoreProvider(provider, LLMRuntimeConfig{MaxInputChars: 50})

	if _, err := p.ScoreSession(t.Context(), application.ScoreRequest{
		Turns:      []application.TranscriptTurn{{Role: "user", At: time.Now(), Content: strings.Repeat("x", 80)}},
		Dimensions: testDimensions,
	}); err != nil {
		t.Fatalf("score: %v", err)
	}
	user := provider.lastReq.Messages[1].Content
	if !strings.Contains(user, "user: ") {
		t.Fatalf("the single head turn must be kept verbatim: %q", user)
	}
	if strings.Contains(user, "省略") {
		t.Fatalf("single-turn transcript must not carry an omission marker: %q", user)
	}
}

// TestLLMScoreProviderTranscriptTailDoesNotFit 覆盖尾部一轮都装不下时
// “其余 N 轮省略”的分支（kept == head）。
func TestLLMScoreProviderTranscriptTailDoesNotFit(t *testing.T) {
	provider := &stubChatProvider{resp: llm.ChatResponse{Content: goodScoreJSON()}}
	// 每轮渲染后 50 字节：头部两轮 102 字节装得下，尾部第三轮（51 字节）放不下。
	p := NewLLMScoreProvider(provider, LLMRuntimeConfig{MaxInputChars: 110})

	base := time.Date(2026, 9, 12, 10, 0, 0, 0, time.UTC)
	turns := make([]application.TranscriptTurn, 0, 3)
	for i := 0; i < 3; i++ {
		turns = append(turns, application.TranscriptTurn{
			Role: "user", At: base.Add(time.Duration(i) * time.Minute), Content: strings.Repeat("x", 30),
		})
	}
	if _, err := p.ScoreSession(t.Context(), application.ScoreRequest{Turns: turns, Dimensions: testDimensions}); err != nil {
		t.Fatalf("score: %v", err)
	}
	user := provider.lastReq.Messages[1].Content
	if !strings.Contains(user, "其余 1 轮省略") {
		t.Fatalf("expected tail-omission marker: %q", user)
	}
	if !strings.Contains(user, "[09-12 10:00]") || !strings.Contains(user, "[09-12 10:01]") {
		t.Fatalf("both head turns must be kept: %q", user)
	}
	if strings.Contains(user, "[09-12 10:02]") {
		t.Fatalf("the tail turn must be dropped: %q", user)
	}
}

// TestParseScoreOutputUnmarshalError 覆盖“有 JSON 对象但内容非法”的解析错误分支。
func TestParseScoreOutputUnmarshalError(t *testing.T) {
	_, err := parseScoreOutput("{not valid json}", testDimensions)
	if err == nil || !strings.Contains(err.Error(), "invalid llm score output") {
		t.Fatalf("malformed object must be tagged invalid output, got %v", err)
	}
}

// TestLLMScoreProviderZeroTimeoutKeepsCallerContext 覆盖 TimeoutSeconds<=0 的
// 防御分支（llm_score_provider.go:65）：绕过构造器归一，字面量构造零值超时，
// 证明调用方 context 原样透传（不会套 WithTimeout(0) 立即超时）。
func TestLLMScoreProviderZeroTimeoutKeepsCallerContext(t *testing.T) {
	provider := &stubChatProvider{resp: llm.ChatResponse{Content: goodScoreJSON()}}
	p := &LLMScoreProvider{provider: provider, cfg: LLMRuntimeConfig{TimeoutSeconds: 0}}

	result, err := p.ScoreSession(context.Background(), application.ScoreRequest{
		Turns:      []application.TranscriptTurn{{Role: "customer", Content: "你好", At: time.Now()}},
		Dimensions: testDimensions,
	})
	if err != nil {
		t.Fatalf("ScoreSession with zero timeout: %v", err)
	}
	if result.TotalScore <= 0 {
		t.Fatalf("unexpected total score: %+v", result)
	}
}
