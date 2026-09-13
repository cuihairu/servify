package audit

import (
	"errors"
	"strings"
	"testing"
)

// TestRedactJSONTextMarshalFailureFallsBackToRaw 覆盖 redactJSONText 中
// 重序列化失败的回落分支：返回原始文本而不是半成品。
func TestRedactJSONTextMarshalFailureFallsBackToRaw(t *testing.T) {
	const raw = `{"token":"secret","plain":"value"}`

	previous := jsonMarshal
	jsonMarshal = func(v any) ([]byte, error) { return nil, errors.New("injected marshal failure") }
	t.Cleanup(func() { jsonMarshal = previous })

	if got := redactJSONText(raw); got != raw {
		t.Fatalf("redactJSONText() = %q, want original %q on marshal failure", got, raw)
	}
}

// TestRedactJSONTextDefaultMarshalSucceeds 保证 seam 默认路径行为不变。
func TestRedactJSONTextDefaultMarshalSucceeds(t *testing.T) {
	got := redactJSONText(`{"token":"secret","plain":"value"}`)
	if !strings.Contains(got, `"token":"[REDACTED]"`) || !strings.Contains(got, `"plain":"value"`) {
		t.Fatalf("redactJSONText() = %q, want redacted token with plain preserved", got)
	}
	if strings.Contains(got, "secret") {
		t.Fatalf("redactJSONText() = %q, want secret removed", got)
	}
}
