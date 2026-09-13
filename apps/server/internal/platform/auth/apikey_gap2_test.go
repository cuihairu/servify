package auth

import (
	"errors"
	"testing"
)

// TestGenerateAPIKeyPropagatesRandError 覆盖 rand.Read 的防御性错误分支
// （现代 Go 的 crypto/rand.Read 恒成功，错误仅能经 seam 注入）。
func TestGenerateAPIKeyPropagatesRandError(t *testing.T) {
	randErr := errors.New("injected rand failure")
	previous := cryptoRandRead
	cryptoRandRead = func(b []byte) (int, error) { return 0, randErr }
	t.Cleanup(func() { cryptoRandRead = previous })

	plaintext, prefix, hash, err := GenerateAPIKey()
	if !errors.Is(err, randErr) {
		t.Fatalf("GenerateAPIKey() error = %v, want injected error", err)
	}
	if plaintext != "" || prefix != "" || hash != "" {
		t.Fatalf("GenerateAPIKey() = (%q, %q, %q), want zero values on error", plaintext, prefix, hash)
	}
}

// TestGenerateAPIKeyDefaultPathSucceeds 保证 seam 默认路径行为不变。
func TestGenerateAPIKeyDefaultPathSucceeds(t *testing.T) {
	plaintext, prefix, hash, err := GenerateAPIKey()
	if err != nil {
		t.Fatalf("GenerateAPIKey() error = %v", err)
	}
	if len(plaintext) != len(APIKeyPrefix)+40 {
		t.Fatalf("plaintext length = %d, want %d", len(plaintext), len(APIKeyPrefix)+40)
	}
	if prefix != plaintext[:len(APIKeyPrefix)+8] {
		t.Fatalf("prefix = %q, want first 8 hex chars of plaintext", prefix)
	}
	if hash != APIKeyHash(plaintext) {
		t.Fatalf("hash mismatch for plaintext %q", plaintext)
	}
}
