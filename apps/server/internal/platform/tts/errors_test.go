package tts

import (
	"errors"
	"testing"
)

// TestSentinelErrorsDistinct 契约哨兵彼此独立且可 errors.Is 寻址。
func TestSentinelErrorsDistinct(t *testing.T) {
	if errors.Is(ErrNotConfigured, ErrInvalidRequest) {
		t.Fatal("ErrNotConfigured and ErrInvalidRequest must be distinct")
	}
	for _, sentinel := range []error{ErrNotConfigured, ErrInvalidRequest} {
		if !errors.Is(sentinel, sentinel) {
			t.Fatalf("%v must match itself", sentinel)
		}
		if sentinel.Error() == "" {
			t.Fatal("sentinel messages must be non-empty")
		}
	}
}
