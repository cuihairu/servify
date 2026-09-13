package application

import (
	"context"
	"errors"
)

type StartRecordingCommand struct {
	CallID   string
	Provider string
}

type StopRecordingCommand struct {
	RecordingID string
}

type AppendTranscriptCommand struct {
	CallID    string
	Content   string
	Language  string
	Finalized bool
}

type RecordingProvider interface {
	StartRecording(ctx context.Context, cmd StartRecordingCommand) (string, error)
	StopRecording(ctx context.Context, cmd StopRecordingCommand) error
}

type TranscriptProvider interface {
	AppendTranscript(ctx context.Context, cmd AppendTranscriptCommand) error
}

type ProviderErrorCode string

const (
	ProviderErrorUnavailable ProviderErrorCode = "provider_unavailable"
	ProviderErrorTimeout     ProviderErrorCode = "provider_timeout"
	ProviderErrorRateLimited ProviderErrorCode = "provider_rate_limited"
	ProviderErrorInvalid     ProviderErrorCode = "provider_invalid_request"
)

type ProviderError struct {
	Code      ProviderErrorCode `json:"code"`
	Message   string            `json:"message"`
	Retryable bool              `json:"retryable"`
	Cause     error             `json:"-"`
}

func (e *ProviderError) Error() string {
	if e == nil {
		return ""
	}
	if e.Message != "" {
		return e.Message
	}
	if e.Cause != nil {
		return e.Cause.Error()
	}
	return string(e.Code)
}

func (e *ProviderError) Unwrap() error {
	if e == nil {
		return nil
	}
	return e.Cause
}

type RetryPolicy struct {
	MaxAttempts int
}

type AsyncCallbackSink interface {
	NotifyRecording(ctx context.Context, recording RecordingDTO) error
	NotifyTranscript(ctx context.Context, transcript TranscriptDTO) error
}

func shouldRetryProviderError(err error) bool {
	var providerErr *ProviderError
	if errors.As(err, &providerErr) {
		return providerErr.Retryable
	}
	return false
}

// applyRetry 最多尝试 attempts 次；循环体每轮要么成功返回 nil，要么在
// 不可重试或到达最后一次时返回错误，因此无限 for（无 break）等价于原先
// 的计数循环，且循环后不存在可达语句（原 lastErr 恒为 nil，循环后的
// 两个 return 均不可达，已删除）。
func applyRetry(attempts int, fn func() error) error {
	if attempts <= 0 {
		attempts = 1
	}
	for i := 0; ; i++ {
		if err := fn(); err != nil {
			if !shouldRetryProviderError(err) || i == attempts-1 {
				return err
			}
			continue
		}
		return nil
	}
}
