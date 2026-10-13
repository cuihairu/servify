package delivery

import "testing"

// DecodeTranslationCache 分支覆盖（handlers 只走 delivery 面，不经
// conversation/application）：好 JSON 解析为按语言键表；空串/坏 JSON/非对象
// 一律 nil（译文是增强能力，静默降级不影响历史加载主链路）。
func TestDecodeTranslationCache(t *testing.T) {
	t.Run("valid json object", func(t *testing.T) {
		got := DecodeTranslationCache(`{"zh":"你好","en":"hello"}`)
		if len(got) != 2 || got["zh"] != "你好" || got["en"] != "hello" {
			t.Fatalf("decoded = %v, want {zh:你好 en:hello}", got)
		}
	})

	t.Run("empty string", func(t *testing.T) {
		if got := DecodeTranslationCache(""); got != nil {
			t.Fatalf("empty raw = %v, want nil", got)
		}
	})

	t.Run("malformed json", func(t *testing.T) {
		if got := DecodeTranslationCache("{not-json"); got != nil {
			t.Fatalf("malformed raw = %v, want nil", got)
		}
	})

	t.Run("non object json", func(t *testing.T) {
		if got := DecodeTranslationCache(`["zh","你好"]`); got != nil {
			t.Fatalf("array raw = %v, want nil", got)
		}
	})

	t.Run("empty object", func(t *testing.T) {
		if got := DecodeTranslationCache(`{}`); got != nil {
			t.Fatalf("empty object raw = %v, want nil", got)
		}
	})
}
