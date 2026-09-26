package delivery

import (
	"context"
	"errors"
	"testing"

	translationapp "servify/apps/server/internal/modules/translation/application"
	"servify/apps/server/internal/platform/llm"
	mockllm "servify/apps/server/internal/platform/llm/mock"
)

// TestHandlerServiceAdapterTranslates 适配器透传成功链路（mock provider 注入，
// 与 golden 套件同款零网络口径）。
func TestHandlerServiceAdapterTranslates(t *testing.T) {
	provider := &mockllm.Provider{ChatResponse: llm.ChatResponse{Content: "Hello"}}
	adapter := NewTranslationHandlerService(provider, translationapp.RuntimeParams{Model: "m"})

	got, err := adapter.Translate(context.Background(), TranslateCommand{Text: "你好", TargetLang: "en"})
	if err != nil {
		t.Fatalf("Translate() error = %v", err)
	}
	if got.Text != "Hello" || got.TargetLang != "en" {
		t.Fatalf("unexpected result: %+v", got)
	}
}

// TestHandlerServiceAdapterErrorPaths 未配置降级与 provider 故障透传。
func TestHandlerServiceAdapterErrorPaths(t *testing.T) {
	ctx := context.Background()

	// llm 为 nil：端点降级 503 语义
	unconfigured := NewTranslationHandlerService(nil, translationapp.RuntimeParams{})
	if _, err := unconfigured.Translate(ctx, TranslateCommand{Text: "hi", TargetLang: "en"}); !errors.Is(err, ErrTranslationUnavailable) {
		t.Fatalf("nil llm error = %v, want ErrTranslationUnavailable", err)
	}

	// nil 适配器防御
	var nilAdapter *HandlerServiceAdapter
	if _, err := nilAdapter.Translate(ctx, TranslateCommand{Text: "hi", TargetLang: "en"}); !errors.Is(err, ErrTranslationUnavailable) {
		t.Fatalf("nil adapter error = %v, want ErrTranslationUnavailable", err)
	}

	// provider 故障原样上抛（handler 映射 502）
	boom := errors.New("upstream boom")
	failing := NewTranslationHandlerService(&mockllm.Provider{ChatError: boom}, translationapp.RuntimeParams{})
	if _, err := failing.Translate(ctx, TranslateCommand{Text: "hi", TargetLang: "en"}); !errors.Is(err, boom) {
		t.Fatalf("provider error = %v, want wrapped boom", err)
	}

	// 参数哨兵透传
	valid := NewTranslationHandlerService(&mockllm.Provider{}, translationapp.RuntimeParams{})
	if _, err := valid.Translate(ctx, TranslateCommand{TargetLang: "en"}); !errors.Is(err, ErrTranslationTextRequired) {
		t.Fatalf("empty text error = %v, want ErrTranslationTextRequired", err)
	}
}
