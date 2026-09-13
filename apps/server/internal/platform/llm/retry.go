package llm

import (
	"context"
	"time"
)

func NormalizeRetryPolicy(policy RetryPolicy) RetryPolicy {
	if policy.MaxAttempts <= 0 {
		policy.MaxAttempts = 1
	}
	if policy.BaseDelayMs <= 0 {
		policy.BaseDelayMs = 100
	}
	if policy.BackoffFactor <= 0 {
		policy.BackoffFactor = 2
	}
	return policy
}

func WithRequestTimeout(ctx context.Context, options RequestOptions, fallback time.Duration) (context.Context, context.CancelFunc) {
	timeout := fallback
	if options.TimeoutMs > 0 {
		timeout = time.Duration(options.TimeoutMs) * time.Millisecond
	}
	return context.WithTimeout(ctx, timeout)
}

func Retry(ctx context.Context, policy RetryPolicy, fn func(context.Context) error) error {
	policy = NormalizeRetryPolicy(policy)
	// NormalizeRetryPolicy 保证 MaxAttempts >= 1，且最后一次尝试必然在循环
	// 体内 return（成功、不可重试或已到最大次数），因此无条件循环即是
	// 终止语句，无需循环后的兜底 return。
	for attempt := 1; ; attempt++ {
		err := fn(ctx)
		if err == nil {
			return nil
		}

		providerErr, ok := err.(*ProviderError)
		if !ok || !providerErr.Retryable || attempt >= policy.MaxAttempts {
			return err
		}

		delay := time.Duration(policy.BaseDelayMs) * time.Millisecond
		for i := 1; i < attempt; i++ {
			delay *= time.Duration(policy.BackoffFactor)
		}

		timer := time.NewTimer(delay)
		select {
		case <-ctx.Done():
			timer.Stop()
			return ctx.Err()
		case <-timer.C:
		}
	}
}
