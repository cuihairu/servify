package errors

import (
	"errors"
	"fmt"
	"testing"

	llm "servify/apps/server/internal/platform/llm"
)

func TestClassify_ProviderError(t *testing.T) {
	cases := []struct {
		code     llm.ProviderErrorCode
		severity Severity
		category Category
		status   int
	}{
		{llm.ProviderErrorTimeout, SeverityDependency, CategoryAI, 504},
		{llm.ProviderErrorRateLimited, SeverityDependency, CategoryAI, 429},
		{llm.ProviderErrorAuthFailed, SeverityConfig, CategoryAuth, 401},
		{llm.ProviderErrorUnavailable, SeverityDependency, CategoryAI, 502},
		{llm.ProviderErrorInvalid, SeverityUser, CategoryValidation, 400},
		{llm.ProviderErrorUpstream, SeverityDependency, CategoryNetwork, 502},
		{llm.ProviderErrorNotSupported, SeverityUser, CategoryValidation, 400},
		{llm.ProviderErrorCode("bizarre"), SeverityDependency, CategoryAI, 502},
	}

	for _, tc := range cases {
		t.Run(string(tc.code), func(t *testing.T) {
			pe := &llm.ProviderError{Provider: "openai", Code: tc.code, Message: "provider broke"}
			appErr := Classify(pe)
			if appErr == nil {
				t.Fatal("expected non-nil AppError")
			}
			if appErr.Severity != tc.severity {
				t.Fatalf("expected severity %s, got %s", tc.severity, appErr.Severity)
			}
			if appErr.Category != tc.category {
				t.Fatalf("expected category %s, got %s", tc.category, appErr.Category)
			}
			if appErr.HTTPStatus != tc.status {
				t.Fatalf("expected status %d, got %d", tc.status, appErr.HTTPStatus)
			}
			if appErr.Module != "ai" {
				t.Fatalf("expected module ai, got %s", appErr.Module)
			}
			if tc.code == llm.ProviderErrorCode("bizarre") {
				if appErr.Code != "" {
					t.Fatalf("expected empty code for unknown provider code, got %s", appErr.Code)
				}
			} else if appErr.Code != string(tc.code) {
				t.Fatalf("expected code %s, got %s", tc.code, appErr.Code)
			}
			if !errors.Is(appErr, pe) {
				t.Fatal("expected AppError to wrap provider error")
			}
		})
	}
}

func TestClassify_WrappedProviderError(t *testing.T) {
	pe := &llm.ProviderError{Provider: "anthropic", Code: llm.ProviderErrorTimeout, Message: "slow"}
	wrapped := fmt.Errorf("generate failed: %w", pe)

	appErr := Classify(wrapped)
	if appErr == nil {
		t.Fatal("expected non-nil AppError")
	}
	if appErr.Severity != SeverityDependency {
		t.Fatalf("expected dependency severity, got %s", appErr.Severity)
	}
	if appErr.Category != CategoryAI {
		t.Fatalf("expected ai category, got %s", appErr.Category)
	}
	if appErr.HTTPStatus != 504 {
		t.Fatalf("expected 504 status, got %d", appErr.HTTPStatus)
	}
	if appErr.Code != string(llm.ProviderErrorTimeout) {
		t.Fatalf("expected timeout code, got %s", appErr.Code)
	}
}

func TestClassify_WrappedAppError(t *testing.T) {
	original := New(errors.New("root"), SeverityUser, CategoryValidation)
	wrapped := fmt.Errorf("outer: %w", original)

	classified := Classify(wrapped)
	if classified != original {
		t.Fatal("expected original AppError instance to be returned")
	}
}

func TestAs_NilError(t *testing.T) {
	var appErr *AppError
	if As(nil, &appErr) {
		t.Fatal("expected As to fail for nil error")
	}
	if appErr != nil {
		t.Fatal("expected target to remain nil")
	}
}

func TestAs_UnrelatedTargetType(t *testing.T) {
	err := fmt.Errorf("wrapped: %w", errors.New("inner"))
	var n int
	if As(err, &n) {
		t.Fatal("expected As to fail for unrelated target type")
	}
}

func TestAs_ProviderErrorUnwrap(t *testing.T) {
	pe := &llm.ProviderError{Provider: "openai", Code: llm.ProviderErrorUpstream, Message: "m"}
	wrapped := fmt.Errorf("ctx: %w", pe)

	var target *llm.ProviderError
	if !As(wrapped, &target) {
		t.Fatal("expected As to find wrapped ProviderError")
	}
	if target != pe {
		t.Fatal("expected same ProviderError instance")
	}

	var notProvider *llm.ProviderError
	if As(errors.New("plain"), &notProvider) {
		t.Fatal("expected As to fail for plain error and provider target")
	}
}

func TestUnwrap_NonUnwrappingError(t *testing.T) {
	if Unwrap(errors.New("plain")) != nil {
		t.Fatal("expected nil for non-unwrapping error")
	}
	if Unwrap(nil) != nil {
		t.Fatal("expected nil for nil error")
	}
}

func TestWrapAs_AppErrorUnwrap(t *testing.T) {
	inner := New(errors.New("root"), SeveritySystem, CategoryInternal)
	wrapped := fmt.Errorf("outer: %w", inner)

	var target *AppError
	if !wrapAs(wrapped, &target) {
		t.Fatal("expected wrapAs to find wrapped AppError")
	}
	if target != inner {
		t.Fatal("expected same AppError instance")
	}
}
