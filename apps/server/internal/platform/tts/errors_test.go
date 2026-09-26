package tts

import (
	"errors"
	"testing"
)

// TestSentinelErrorsDistinct 契约哨兵彼此独立且可 errors.Is 寻址。
func TestSentinelErrorsDistinct(t *testing.T) {
	sentinels := []error{ErrNotConfigured, ErrInvalidRequest, ErrUpstream}
	for i, a := range sentinels {
		for j, b := range sentinels {
			if i != j && errors.Is(a, b) {
				t.Fatalf("sentinels %v and %v must be distinct", a, b)
			}
		}
	}
	for _, sentinel := range sentinels {
		if !errors.Is(sentinel, sentinel) {
			t.Fatalf("%v must match itself", sentinel)
		}
		if sentinel.Error() == "" {
			t.Fatal("sentinel messages must be non-empty")
		}
	}
}
