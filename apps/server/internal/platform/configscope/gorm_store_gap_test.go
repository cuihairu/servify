package configscope

import (
	"strings"
	"testing"
)

// TestEncodeConfigPanicsOnUnserializable 是一个死分支论证测试:
// yaml.v3(v3.0.1) 的 Marshal 对不可序列化类型(如 chan)直接 panic
// (encoder.failf → handleErr repanic),从不向调用方返回非 nil error。
// 因此 encodeConfig 的 `if err != nil` 分支以及 UpsertTenantConfig /
// UpsertWorkspaceConfig 中 10 处 encodeConfig 错误分支均不可达:
// 五个 Scoped 配置结构体(Portal/OpenAI/Dify/WeKnora/SessionRisk 及其
// 嵌套子结构)只含 string/数值/布尔/time.Duration 字段,yaml.Marshal
// 对它们永远不会失败。该测试将此行为固化,若未来 yaml 升级改为返回
// error,此测试会先失败,提示这些分支重新变为可测。
func TestEncodeConfigPanicsOnUnserializable(t *testing.T) {
	defer func() {
		r := recover()
		if r == nil {
			t.Fatal("expected yaml.Marshal on chan to panic, got none")
		}
		msg, ok := r.(string)
		if !ok || !strings.Contains(msg, "cannot marshal type") {
			t.Fatalf("unexpected panic value: %v", r)
		}
	}()
	_, _ = encodeConfig(struct {
		C chan int
	}{C: make(chan int)})
}
