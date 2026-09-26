package application_test

import (
	"context"
	"errors"
	"strings"
	"testing"

	translationapp "servify/apps/server/internal/modules/translation/application"
	"servify/apps/server/internal/platform/llm"
	mockllm "servify/apps/server/internal/platform/llm/mock"
)

var testParams = translationapp.RuntimeParams{Model: "test-model", Temperature: 0.2, MaxTokens: 512, TimeoutMs: 8000}

func TestTranslateSuccess(t *testing.T) {
	provider := &mockllm.Provider{ChatResponse: llm.ChatResponse{Content: "When will my order ship?"}}
	svc := translationapp.NewService(provider, testParams)

	got, err := svc.Translate(context.Background(), translationapp.TranslateCommand{
		Text: "请问订单什么时候发货？", TargetLang: "en",
	})
	if err != nil {
		t.Fatalf("Translate() error = %v", err)
	}
	if got.Text != "When will my order ship?" || got.SourceLang != translationapp.SourceLangAuto || got.TargetLang != "en" {
		t.Fatalf("unexpected result: %+v", got)
	}

	requests := provider.RecordedRequests()
	if len(requests) != 1 {
		t.Fatalf("want 1 llm call, got %d", len(requests))
	}
	req := requests[0]
	if req.Model != testParams.Model || req.Temperature != testParams.Temperature ||
		req.MaxTokens != testParams.MaxTokens || req.Options.TimeoutMs != testParams.TimeoutMs {
		t.Fatalf("runtime params not forwarded: %+v", req)
	}
	if len(req.Messages) != 2 {
		t.Fatalf("want system+user messages, got %d", len(req.Messages))
	}
	if req.Messages[0].Role != "system" || !strings.Contains(req.Messages[0].Content, "翻译") {
		t.Fatalf("unexpected system prompt: %+v", req.Messages[0])
	}
	if !strings.Contains(req.Messages[1].Content, "en") || !strings.Contains(req.Messages[1].Content, "请问订单什么时候发货？") {
		t.Fatalf("user message must carry target lang and text: %+v", req.Messages[1])
	}
}

func TestTranslateWithSourceLang(t *testing.T) {
	provider := &mockllm.Provider{ChatResponse: llm.ChatResponse{Content: "Hello"}}
	svc := translationapp.NewService(provider, testParams)

	got, err := svc.Translate(context.Background(), translationapp.TranslateCommand{
		Text: "你好", SourceLang: "zh-CN", TargetLang: "en-US",
	})
	if err != nil {
		t.Fatalf("Translate() error = %v", err)
	}
	// 语言标签归一化为小写回显
	if got.SourceLang != "zh-cn" || got.TargetLang != "en-us" {
		t.Fatalf("langs not normalized: %+v", got)
	}
	userMsg := provider.RecordedRequests()[0].Messages[1].Content
	if !strings.Contains(userMsg, "从 zh-cn 翻译成 en-us") {
		t.Fatalf("source lang not in instruction: %s", userMsg)
	}
}

func TestTranslateContextCarriedInPrompt(t *testing.T) {
	provider := &mockllm.Provider{ChatResponse: llm.ChatResponse{Content: "Hello"}}
	svc := translationapp.NewService(provider, testParams)

	if _, err := svc.Translate(context.Background(), translationapp.TranslateCommand{
		Text: "你好", TargetLang: "en", Context: "上一句原文\n上一句译文",
	}); err != nil {
		t.Fatalf("Translate() error = %v", err)
	}
	userMsg := provider.RecordedRequests()[0].Messages[1].Content
	if !strings.Contains(userMsg, "对话上文") || !strings.Contains(userMsg, "上一句原文\n上一句译文") {
		t.Fatalf("context missing from prompt: %s", userMsg)
	}
	if !strings.Contains(userMsg, "仅帮助理解衔接") {
		t.Fatalf("context must be marked as non-output reference: %s", userMsg)
	}
}

func TestTranslateContextClampedToTailWindow(t *testing.T) {
	provider := &mockllm.Provider{ChatResponse: llm.ChatResponse{Content: "Hello"}}
	svc := translationapp.NewService(provider, testParams)

	long := strings.Repeat("a", translationapp.MaxContextRunes+100) + "TAIL"
	if _, err := svc.Translate(context.Background(), translationapp.TranslateCommand{
		Text: "你好", TargetLang: "en", Context: long,
	}); err != nil {
		t.Fatalf("Translate() error = %v", err)
	}
	userMsg := provider.RecordedRequests()[0].Messages[1].Content
	if !strings.Contains(userMsg, "TAIL") {
		t.Fatalf("tail window must keep the most recent text: %s", userMsg[len(userMsg)-200:])
	}
	if strings.Contains(userMsg, strings.Repeat("a", translationapp.MaxContextRunes+1)) {
		t.Fatalf("context must be clamped to %d runes", translationapp.MaxContextRunes)
	}
}

func TestTranslateBlankContextOmitted(t *testing.T) {
	provider := &mockllm.Provider{ChatResponse: llm.ChatResponse{Content: "Hello"}}
	svc := translationapp.NewService(provider, testParams)

	if _, err := svc.Translate(context.Background(), translationapp.TranslateCommand{
		Text: "你好", TargetLang: "en", Context: "  ",
	}); err != nil {
		t.Fatalf("Translate() error = %v", err)
	}
	if strings.Contains(provider.RecordedRequests()[0].Messages[1].Content, "对话上文") {
		t.Fatalf("blank context must be omitted from prompt")
	}
}

func TestTranslateValidation(t *testing.T) {
	svc := translationapp.NewService(&mockllm.Provider{}, testParams)
	exactMax := strings.Repeat("字", translationapp.MaxTextRunes)

	cases := []struct {
		name string
		cmd  translationapp.TranslateCommand
		want error
	}{
		{"empty text", translationapp.TranslateCommand{TargetLang: "en"}, translationapp.ErrTranslationTextRequired},
		{"whitespace text", translationapp.TranslateCommand{Text: "   ", TargetLang: "en"}, translationapp.ErrTranslationTextRequired},
		{"text too long", translationapp.TranslateCommand{Text: exactMax + "x", TargetLang: "en"}, translationapp.ErrTranslationTextTooLong},
		{"missing target", translationapp.TranslateCommand{Text: "hi"}, translationapp.ErrTranslationTargetRequired},
		{"blank target", translationapp.TranslateCommand{Text: "hi", TargetLang: "  "}, translationapp.ErrTranslationTargetRequired},
		{"invalid target", translationapp.TranslateCommand{Text: "hi", TargetLang: "english!"}, translationapp.ErrTranslationLangInvalid},
		{"invalid source", translationapp.TranslateCommand{Text: "hi", SourceLang: "123", TargetLang: "en"}, translationapp.ErrTranslationLangInvalid},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if _, err := svc.Translate(context.Background(), tc.cmd); !errors.Is(err, tc.want) {
				t.Fatalf("want %v, got %v", tc.want, err)
			}
		})
	}

	// 恰好上限长度放行
	provider := &mockllm.Provider{ChatResponse: llm.ChatResponse{Content: "ok"}}
	if _, err := translationapp.NewService(provider, testParams).Translate(context.Background(), translationapp.TranslateCommand{Text: exactMax, TargetLang: "en"}); err != nil {
		t.Fatalf("max-length text should pass, got %v", err)
	}
}

func TestTranslateUnavailable(t *testing.T) {
	svc := translationapp.NewService(nil, testParams)
	if _, err := svc.Translate(context.Background(), translationapp.TranslateCommand{Text: "hi", TargetLang: "en"}); !errors.Is(err, translationapp.ErrTranslationUnavailable) {
		t.Fatalf("nil provider error = %v, want ErrTranslationUnavailable", err)
	}
	var nilSvc *translationapp.Service
	if _, err := nilSvc.Translate(context.Background(), translationapp.TranslateCommand{Text: "hi", TargetLang: "en"}); !errors.Is(err, translationapp.ErrTranslationUnavailable) {
		t.Fatalf("nil service error = %v, want ErrTranslationUnavailable", err)
	}
}

func TestTranslateProviderFailures(t *testing.T) {
	boom := errors.New("upstream boom")

	provider := &mockllm.Provider{ChatError: boom}
	_, err := translationapp.NewService(provider, testParams).Translate(context.Background(), translationapp.TranslateCommand{Text: "hi", TargetLang: "en"})
	if !errors.Is(err, boom) || !strings.Contains(err.Error(), "translation chat") {
		t.Fatalf("provider error must propagate wrapped, got %v", err)
	}

	empty := &mockllm.Provider{ChatResponse: llm.ChatResponse{Content: "   "}}
	if _, err := translationapp.NewService(empty, testParams).Translate(context.Background(), translationapp.TranslateCommand{Text: "hi", TargetLang: "en"}); !errors.Is(err, translationapp.ErrTranslationEmptyOutput) {
		t.Fatalf("empty output error = %v, want ErrTranslationEmptyOutput", err)
	}
}
