package application

import (
	"errors"
	"fmt"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestProviderErrorErrorRendering(t *testing.T) {
	var nilErr *ProviderError
	assert.Empty(t, nilErr.Error())

	assert.Equal(t, "boom", (&ProviderError{Message: "boom"}).Error())

	cause := errors.New("dial failed")
	assert.Equal(t, "dial failed", (&ProviderError{Cause: cause}).Error())

	assert.Equal(t, "provider_timeout", (&ProviderError{Code: ProviderErrorTimeout}).Error())
}

func TestProviderErrorUnwrap(t *testing.T) {
	var nilErr *ProviderError
	require.NoError(t, nilErr.Unwrap())

	cause := errors.New("root cause")
	providerErr := &ProviderError{Code: ProviderErrorUnavailable, Cause: cause}
	require.ErrorIs(t, providerErr, cause)
	assert.NoError(t, (&ProviderError{}).Unwrap())
}

func TestShouldRetryProviderError(t *testing.T) {
	assert.True(t, shouldRetryProviderError(&ProviderError{Retryable: true}))
	assert.False(t, shouldRetryProviderError(&ProviderError{Retryable: false}))
	assert.False(t, shouldRetryProviderError(errors.New("plain error")))
	assert.True(t, shouldRetryProviderError(fmt.Errorf("wrapped: %w", &ProviderError{Retryable: true})))
}

func TestApplyRetryClampsAttemptsToOne(t *testing.T) {
	for _, attempts := range []int{0, -3} {
		calls := 0
		err := applyRetry(attempts, func() error {
			calls++
			return nil
		})
		require.NoError(t, err)
		assert.Equal(t, 1, calls, "attempts=%d should be clamped to 1", attempts)
	}
}

func TestApplyRetryFailsFastOnNonRetryableError(t *testing.T) {
	boom := errors.New("boom")
	calls := 0
	err := applyRetry(3, func() error {
		calls++
		return &ProviderError{Code: ProviderErrorInvalid, Cause: boom}
	})
	require.ErrorIs(t, err, boom)
	assert.Equal(t, 1, calls)
}

func TestApplyRetryRetriesThenSucceeds(t *testing.T) {
	calls := 0
	err := applyRetry(3, func() error {
		calls++
		if calls <= 2 {
			return &ProviderError{Code: ProviderErrorUnavailable, Message: "temporarily down", Retryable: true}
		}
		return nil
	})
	require.NoError(t, err)
	assert.Equal(t, 3, calls)
}

func TestApplyRetryExhaustsAttempts(t *testing.T) {
	calls := 0
	err := applyRetry(2, func() error {
		calls++
		return &ProviderError{Code: ProviderErrorRateLimited, Message: "rate limited", Retryable: true}
	})
	require.Error(t, err)
	assert.Equal(t, "rate limited", err.Error())
	assert.Equal(t, 2, calls)
}

func TestApplyRetrySucceedsImmediately(t *testing.T) {
	calls := 0
	err := applyRetry(2, func() error {
		calls++
		return nil
	})
	require.NoError(t, err)
	assert.Equal(t, 1, calls)
}
