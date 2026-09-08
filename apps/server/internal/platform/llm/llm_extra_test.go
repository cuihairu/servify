package llm

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"testing"
	"time"

	"servify/apps/server/internal/platform/aiprovider"
)

func TestProviderDescriptors(t *testing.T) {
	openai := OpenAIDescriptor(false, "gpt-test")
	if openai.ID != "openai" || openai.Kind != aiprovider.KindLLM || openai.Driver != "openai" {
		t.Fatalf("unexpected openai descriptor: %+v", openai)
	}
	if openai.Enabled {
		t.Fatal("expected openai descriptor disabled")
	}
	if openai.Fallback.Priority != 1 {
		t.Fatalf("openai fallback priority = %d", openai.Fallback.Priority)
	}
	byName := map[aiprovider.CapabilityName]bool{}
	for _, cap := range openai.Capabilities {
		byName[cap.Name] = cap.Enabled
	}
	if !byName[aiprovider.CapabilityChat] || !byName[aiprovider.CapabilityToolCalling] || !byName[aiprovider.CapabilityHealthCheck] {
		t.Fatalf("expected chat/tool-calling/health-check capabilities enabled: %+v", openai.Capabilities)
	}
	if byName[aiprovider.CapabilityChatStream] || byName[aiprovider.CapabilityEmbeddings] {
		t.Fatalf("expected stream/embeddings capabilities disabled: %+v", openai.Capabilities)
	}
	for _, cap := range openai.Capabilities {
		if cap.Name == aiprovider.CapabilityChat {
			if got := cap.Metadata["default_model"]; got != "gpt-test" {
				t.Fatalf("default model metadata = %v", got)
			}
		}
	}

	anthropic := AnthropicDescriptor(true, "claude-test")
	if anthropic.ID != "anthropic" || !anthropic.Enabled {
		t.Fatalf("unexpected anthropic descriptor: %+v", anthropic)
	}
	if anthropic.Fallback.Priority != 2 || len(anthropic.Fallback.FallbackTo) != 1 || anthropic.Fallback.FallbackTo[0] != "openai" {
		t.Fatalf("anthropic fallback policy = %+v", anthropic.Fallback)
	}
}

func TestProviderErrorFormatting(t *testing.T) {
	var nilErr *ProviderError
	if nilErr.Error() != "" {
		t.Fatalf("nil ProviderError.Error() = %q", nilErr.Error())
	}
	if nilErr.Unwrap() != nil {
		t.Fatal("nil ProviderError.Unwrap() should be nil")
	}

	plain := &ProviderError{Provider: "openai", Code: ProviderErrorTimeout, Message: "timed out"}
	if plain.Error() != "openai provider error (timeout): timed out" {
		t.Fatalf("plain error = %q", plain.Error())
	}
	if plain.Unwrap() != nil {
		t.Fatal("expected nil cause without wrapping")
	}

	cause := errors.New("connection reset")
	wrapped := &ProviderError{Provider: "anthropic", Code: ProviderErrorUpstream, Message: "boom", StatusCode: 502, Cause: cause}
	if wrapped.Error() != "anthropic provider error (upstream_error:502): boom" {
		t.Fatalf("status error = %q", wrapped.Error())
	}
	if !errors.Is(wrapped, cause) {
		t.Fatal("expected Unwrap to expose cause")
	}
}

func TestHTTPErrorClassificationMatrix(t *testing.T) {
	cases := []struct {
		status    int
		code      ProviderErrorCode
		retryable bool
	}{
		{http.StatusUnauthorized, ProviderErrorAuthFailed, false},
		{http.StatusForbidden, ProviderErrorAuthFailed, false},
		{http.StatusTooManyRequests, ProviderErrorRateLimited, true},
		{http.StatusRequestTimeout, ProviderErrorTimeout, true},
		{http.StatusGatewayTimeout, ProviderErrorTimeout, true},
		{http.StatusInternalServerError, ProviderErrorUnavailable, true},
		{http.StatusNotImplemented, ProviderErrorUnavailable, true},
		{http.StatusBadGateway, ProviderErrorUnavailable, true},
		{http.StatusBadRequest, ProviderErrorInvalid, false},
		{http.StatusNotFound, ProviderErrorInvalid, false},
		{http.StatusConflict, ProviderErrorInvalid, false},
	}
	for _, tc := range cases {
		err := HTTPError("openai", tc.status, "msg")
		if err.Code != tc.code {
			t.Fatalf("status %d code = %s, want %s", tc.status, err.Code, tc.code)
		}
		if err.Retryable != tc.retryable {
			t.Fatalf("status %d retryable = %v, want %v", tc.status, err.Retryable, tc.retryable)
		}
		if err.StatusCode != tc.status || err.Provider != "openai" || err.Message != "msg" {
			t.Fatalf("unexpected error fields: %+v", err)
		}
	}
}

func TestNormalizeRetryPolicyDefaults(t *testing.T) {
	got := NormalizeRetryPolicy(RetryPolicy{})
	if got != (RetryPolicy{MaxAttempts: 1, BaseDelayMs: 100, BackoffFactor: 2}) {
		t.Fatalf("NormalizeRetryPolicy(zero) = %+v", got)
	}

	partial := NormalizeRetryPolicy(RetryPolicy{MaxAttempts: 4})
	if partial.BaseDelayMs != 100 || partial.BackoffFactor != 2 || partial.MaxAttempts != 4 {
		t.Fatalf("NormalizeRetryPolicy(partial) = %+v", partial)
	}

	preserved := NormalizeRetryPolicy(RetryPolicy{MaxAttempts: 3, BaseDelayMs: 7, BackoffFactor: 5})
	if preserved != (RetryPolicy{MaxAttempts: 3, BaseDelayMs: 7, BackoffFactor: 5}) {
		t.Fatalf("NormalizeRetryPolicy(valid) altered policy: %+v", preserved)
	}
}

func TestRetrySucceedsOnFirstAttempt(t *testing.T) {
	attempts := 0
	err := Retry(context.Background(), RetryPolicy{MaxAttempts: 3, BaseDelayMs: 1}, func(context.Context) error {
		attempts++
		return nil
	})
	if err != nil {
		t.Fatalf("expected success, got %v", err)
	}
	if attempts != 1 {
		t.Fatalf("attempts = %d, want 1", attempts)
	}
}

func TestRetryExhaustsRetryableProviderErrors(t *testing.T) {
	attempts := 0
	sentinel := &ProviderError{Provider: "openai", Code: ProviderErrorUnavailable, Retryable: true}
	err := Retry(context.Background(), RetryPolicy{MaxAttempts: 3, BaseDelayMs: 1, BackoffFactor: 1}, func(context.Context) error {
		attempts++
		return sentinel
	})
	if !errors.Is(err, sentinel) {
		t.Fatalf("err = %v, want sentinel after exhausted attempts", err)
	}
	if attempts != 3 {
		t.Fatalf("attempts = %d, want 3", attempts)
	}
}

func TestRetryStopsOnNonRetryableProviderError(t *testing.T) {
	attempts := 0
	permanent := &ProviderError{Provider: "openai", Code: ProviderErrorAuthFailed, Retryable: false}
	err := Retry(context.Background(), RetryPolicy{MaxAttempts: 4, BaseDelayMs: 1}, func(context.Context) error {
		attempts++
		return permanent
	})
	if !errors.Is(err, permanent) {
		t.Fatalf("err = %v, want permanent error immediately", err)
	}
	if attempts != 1 {
		t.Fatalf("attempts = %d, want 1", attempts)
	}
}

func TestRetryAppliesExponentialBackoff(t *testing.T) {
	policy := RetryPolicy{MaxAttempts: 3, BaseDelayMs: 20, BackoffFactor: 3}
	if got := NormalizeRetryPolicy(policy); got != policy {
		t.Fatalf("policy should stay normalized, got %+v", got)
	}

	start := time.Now()
	attempts := 0
	_ = Retry(context.Background(), policy, func(context.Context) error {
		attempts++
		return &ProviderError{Provider: "openai", Code: ProviderErrorUnavailable, Retryable: true}
	})
	elapsed := time.Since(start)
	// attempt 2 waits 20ms, attempt 3 waits 60ms → at least 80ms total.
	if elapsed < 80*time.Millisecond {
		t.Fatalf("expected backoff delays >= 80ms, got %v", elapsed)
	}
	if attempts != 3 {
		t.Fatalf("attempts = %d, want 3", attempts)
	}
}

func TestRetryRespectsContextCancellationDuringBackoff(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	attempts := 0
	err := Retry(ctx, RetryPolicy{MaxAttempts: 5, BaseDelayMs: 5000}, func(context.Context) error {
		attempts++
		cancel()
		return &ProviderError{Provider: "openai", Code: ProviderErrorUnavailable, Retryable: true}
	})
	if !errors.Is(err, context.Canceled) {
		t.Fatalf("err = %v, want context.Canceled", err)
	}
	if attempts != 1 {
		t.Fatalf("attempts = %d, want 1 (no retry after cancellation)", attempts)
	}
}

func TestWithRequestTimeoutFallsBackToDefault(t *testing.T) {
	ctx, cancel := WithRequestTimeout(context.Background(), RequestOptions{}, 30*time.Millisecond)
	defer cancel()
	select {
	case <-ctx.Done():
		t.Fatal("context expired too early for fallback timeout")
	case <-time.After(10 * time.Millisecond):
	}
	select {
	case <-ctx.Done():
	case <-time.After(200 * time.Millisecond):
		t.Fatal("fallback timeout did not fire")
	}
}

func TestProviderErrorCodeValues(t *testing.T) {
	codes := map[ProviderErrorCode]string{
		ProviderErrorTimeout:      "timeout",
		ProviderErrorUnavailable:  "unavailable",
		ProviderErrorRateLimited:  "rate_limited",
		ProviderErrorAuthFailed:   "auth_failed",
		ProviderErrorInvalid:      "invalid_request",
		ProviderErrorUpstream:     "upstream_error",
		ProviderErrorNotSupported: "not_supported",
	}
	for code, want := range codes {
		if string(code) != want {
			t.Fatalf("code %q != %q", code, want)
		}
	}
	err := HTTPError("p", http.StatusTeapot, fmtMsg("teapot"))
	if err.Code != ProviderErrorInvalid || err.Retryable {
		t.Fatalf("4xx fallback classification wrong: %+v", err)
	}
}

func fmtMsg(s string) string { return fmt.Sprintf("http %s", s) }
