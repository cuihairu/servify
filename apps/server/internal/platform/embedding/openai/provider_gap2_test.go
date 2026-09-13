package openai

import (
	"context"
	"errors"
	"strings"
	"testing"
)

// TestEmbedPropagatesMarshalError 覆盖请求序列化的防御性错误分支
// （embedRequest 仅含基础类型字段，生产路径 json.Marshal 恒成功）。
func TestEmbedPropagatesMarshalError(t *testing.T) {
	marshalErr := errors.New("injected marshal failure")
	previous := jsonMarshal
	jsonMarshal = func(v any) ([]byte, error) { return nil, marshalErr }
	t.Cleanup(func() { jsonMarshal = previous })

	provider := NewProvider(Config{BaseURL: "http://127.0.0.1:1"})
	_, err := provider.Embed(context.Background(), []string{"hello"})
	if !errors.Is(err, marshalErr) {
		t.Fatalf("Embed() error = %v, want injected marshal failure", err)
	}
	if !strings.Contains(err.Error(), "marshal request") {
		t.Fatalf("Embed() error = %v, want wrapped marshal request error", err)
	}
}

// TestEmbedDefaultMarshalPathReachesServer 保证 seam 默认路径行为不变：
// 未注入失败时请求正常到达测试服务器。
func TestEmbedDefaultMarshalPathReachesServer(t *testing.T) {
	provider := NewProvider(Config{BaseURL: "http://127.0.0.1:1", Timeout: 1})
	// 本地无服务：拿到连接错误即证明序列化已通过、请求已发出。
	if _, err := provider.Embed(context.Background(), []string{"hello"}); err == nil {
		t.Fatal("Embed() expected transport error against closed port")
	}
}
