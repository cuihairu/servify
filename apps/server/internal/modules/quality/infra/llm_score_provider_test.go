package infra

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"

	"servify/apps/server/internal/modules/quality/application"
	"servify/apps/server/internal/platform/llm"
)

type stubChatProvider struct {
	resp llm.ChatResponse
	err  error

	lastReq llm.ChatRequest
	calls   int
}

func (s *stubChatProvider) Chat(_ context.Context, req llm.ChatRequest) (llm.ChatResponse, error) {
	s.calls++
	s.lastReq = req
	return s.resp, s.err
}

func (s *stubChatProvider) ChatStream(context.Context, llm.ChatRequest) (<-chan llm.ChatChunk, error) {
	return nil, errors.New("not implemented")
}

func (s *stubChatProvider) Embed(context.Context, []string) ([][]float32, error) {
	return nil, errors.New("not implemented")
}

func (s *stubChatProvider) HealthCheck(context.Context) error { return nil }

var testDimensions = application.ScoringDimensions

func goodScoreJSON() string {
	return `{"dimensions":{"attitude":{"score":9,"reason":"礼貌"},"resolution":{"score":8,"reason":"解决"},"timeliness":{"score":9,"reason":"及时"}},"summary":"良好"}`
}

func TestLLMScoreProviderSuccess(t *testing.T) {
	provider := &stubChatProvider{resp: llm.ChatResponse{Content: goodScoreJSON(), Provider: "openai", Model: "gpt-test"}}
	p := NewLLMScoreProvider(provider, LLMRuntimeConfig{Temperature: 0.1, MaxTokens: 800})

	result, err := p.ScoreSession(context.Background(), application.ScoreRequest{
		SessionID: "s1",
		Turns: []application.TranscriptTurn{
			{Role: "user", At: time.Date(2026, 9, 12, 10, 0, 0, 0, time.UTC), Content: "你好"},
			{Role: "agent", At: time.Date(2026, 9, 12, 10, 0, 30, 0, time.UTC), Content: "您好"},
		},
		Dimensions: testDimensions,
	})
	if err != nil {
		t.Fatalf("score: %v", err)
	}
	// 加权总分：9*0.3 + 8*0.5 + 9*0.2 = 8.5（服务端自行计算，不信任 LLM 总分）
	if result.TotalScore < 8.499 || result.TotalScore > 8.501 {
		t.Fatalf("weighted total = %v, want 8.5", result.TotalScore)
	}
	if result.Provider != "openai" || result.Model != "gpt-test" || result.Summary != "良好" {
		t.Fatalf("metadata: %+v", result)
	}
	if len(result.Dimensions["attitude"].Reason) == 0 {
		t.Fatal("dimension reason must be kept")
	}
	// prompt 必须带维度定义与 JSON 约束
	sys := provider.lastReq.Messages[0].Content
	if !strings.Contains(sys, "attitude") || !strings.Contains(sys, "JSON") {
		t.Fatalf("system prompt missing constraints: %s", sys)
	}
	// transcript 行含 role 与内容
	user := provider.lastReq.Messages[1].Content
	if !strings.Contains(user, "user: 你好") || !strings.Contains(user, "agent: 您好") {
		t.Fatalf("transcript malformed: %s", user)
	}
}

func TestLLMScoreProviderStripsFences(t *testing.T) {
	content := "```json\n" + goodScoreJSON() + "\n```"
	provider := &stubChatProvider{resp: llm.ChatResponse{Content: content}}
	p := NewLLMScoreProvider(provider, LLMRuntimeConfig{})
	if _, err := p.ScoreSession(context.Background(), application.ScoreRequest{Dimensions: testDimensions}); err != nil {
		t.Fatalf("fenced output must parse: %v", err)
	}
}

func TestLLMScoreProviderInvalidOutputs(t *testing.T) {
	cases := map[string]llm.ChatResponse{
		"no json":      {Content: "我觉得挺好的"},
		"bad json":     {Content: "{\"dimensions\":"},
		"no dims":      {Content: `{"summary":"x"}`},
		"missing key":  {Content: `{"dimensions":{"attitude":{"score":8}},"summary":"x"}`},
		"out of range": {Content: `{"dimensions":{"attitude":{"score":11},"resolution":{"score":8},"timeliness":{"score":8}},"summary":"x"}`},
	}
	for name, resp := range cases {
		provider := &stubChatProvider{resp: resp}
		p := NewLLMScoreProvider(provider, LLMRuntimeConfig{})
		_, err := p.ScoreSession(context.Background(), application.ScoreRequest{Dimensions: testDimensions})
		if !errors.Is(err, application.ErrInvalidScoreOutput) {
			t.Fatalf("%s: want ErrInvalidScoreOutput, got %v", name, err)
		}
	}
}

func TestLLMScoreProviderChatErrorWrapped(t *testing.T) {
	provider := &stubChatProvider{err: errors.New("boom")}
	p := NewLLMScoreProvider(provider, LLMRuntimeConfig{})
	_, err := p.ScoreSession(context.Background(), application.ScoreRequest{Dimensions: testDimensions})
	if err == nil || errors.Is(err, application.ErrInvalidScoreOutput) {
		t.Fatalf("transport error must not be tagged invalid output: %v", err)
	}
}

func TestLLMScoreProviderTranscriptTrimming(t *testing.T) {
	provider := &stubChatProvider{resp: llm.ChatResponse{Content: goodScoreJSON()}}
	p := NewLLMScoreProvider(provider, LLMRuntimeConfig{MaxInputChars: 300, MaxTurnChars: 100})

	turns := make([]application.TranscriptTurn, 0, 20)
	base := time.Date(2026, 9, 12, 10, 0, 0, 0, time.UTC)
	for i := 0; i < 20; i++ {
		turns = append(turns, application.TranscriptTurn{
			Role: "user", At: base.Add(time.Duration(i) * time.Minute),
			Content: strings.Repeat("x", 60),
		})
	}
	if _, err := p.ScoreSession(context.Background(), application.ScoreRequest{Turns: turns, Dimensions: testDimensions}); err != nil {
		t.Fatalf("score: %v", err)
	}
	user := provider.lastReq.Messages[1].Content
	if len(user) > 300 {
		t.Fatalf("transcript exceeds budget: %d", len(user))
	}
	if !strings.Contains(user, "省略") {
		t.Fatalf("omission marker expected: %s", user)
	}
	if !strings.HasPrefix(user, "[") || !strings.HasSuffix(user, "x") {
		t.Fatalf("head and tail turns must be kept: %s", user)
	}
}

func TestLLMScoreProviderTurnTruncation(t *testing.T) {
	provider := &stubChatProvider{resp: llm.ChatResponse{Content: goodScoreJSON()}}
	p := NewLLMScoreProvider(provider, LLMRuntimeConfig{MaxTurnChars: 30})
	if _, err := p.ScoreSession(context.Background(), application.ScoreRequest{
		Turns:      []application.TranscriptTurn{{Role: "user", At: time.Now(), Content: strings.Repeat("长", 100)}},
		Dimensions: testDimensions,
	}); err != nil {
		t.Fatalf("score: %v", err)
	}
	line := provider.lastReq.Messages[1].Content
	if len(line) > 30 {
		t.Fatalf("turn must be truncated to 30 bytes, got %d", len(line))
	}
}
