package configscope

import (
	"context"
	"errors"
	"testing"

	"servify/apps/server/internal/config"
)

// stubYamlMarshalError 用返回错误的 Marshal 实现临时替换 yamlMarshal，
// 并在测试结束时恢复默认实现（同包测试串行执行，无并发冲突）。
func stubYamlMarshalError(t *testing.T, err error) {
	t.Helper()
	previous := yamlMarshal
	yamlMarshal = func(v any) ([]byte, error) { return nil, err }
	t.Cleanup(func() { yamlMarshal = previous })
}

var encodeErr = errors.New("injected marshal failure")

// TestEncodeConfigPropagatesMarshalError 覆盖 encodeConfig 的错误分支。
func TestEncodeConfigPropagatesMarshalError(t *testing.T) {
	stubYamlMarshalError(t, encodeErr)

	if _, err := encodeConfig(config.PortalConfig{}); !errors.Is(err, encodeErr) {
		t.Fatalf("encodeConfig() error = %v, want injected error", err)
	}
}

// TestUpsertTenantConfigEncodeErrorBranches 逐个覆盖 UpsertTenantConfig 中
// 五个配置段的 encodeConfig 错误分支。
func TestUpsertTenantConfigEncodeErrorBranches(t *testing.T) {
	cases := []struct {
		name    string
		payload ScopedConfigDocument
	}{
		{"portal", ScopedConfigDocument{Portal: &config.PortalConfig{}}},
		{"openai", ScopedConfigDocument{OpenAI: &config.OpenAIConfig{}}},
		{"dify", ScopedConfigDocument{Dify: &config.DifyConfig{}}},
		{"weknora", ScopedConfigDocument{WeKnora: &config.WeKnoraConfig{}}},
		{"session_risk", ScopedConfigDocument{SessionRisk: &config.SessionRiskPolicyConfig{}}},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			stubYamlMarshalError(t, encodeErr)
			store := NewGormConfigStore(newCov3DB(t))

			doc, err := store.UpsertTenantConfig(context.Background(), "tenant-enc-err", tc.payload)
			if !errors.Is(err, encodeErr) {
				t.Fatalf("UpsertTenantConfig() error = %v, want injected error", err)
			}
			if doc != nil {
				t.Fatalf("UpsertTenantConfig() doc = %+v, want nil on error", doc)
			}
		})
	}
}

// TestUpsertWorkspaceConfigEncodeErrorBranches 逐个覆盖 UpsertWorkspaceConfig
// 中五个配置段的 encodeConfig 错误分支。
func TestUpsertWorkspaceConfigEncodeErrorBranches(t *testing.T) {
	cases := []struct {
		name    string
		payload ScopedConfigDocument
	}{
		{"portal", ScopedConfigDocument{Portal: &config.PortalConfig{}}},
		{"openai", ScopedConfigDocument{OpenAI: &config.OpenAIConfig{}}},
		{"dify", ScopedConfigDocument{Dify: &config.DifyConfig{}}},
		{"weknora", ScopedConfigDocument{WeKnora: &config.WeKnoraConfig{}}},
		{"session_risk", ScopedConfigDocument{SessionRisk: &config.SessionRiskPolicyConfig{}}},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			stubYamlMarshalError(t, encodeErr)
			store := NewGormConfigStore(newCov3DB(t))

			doc, err := store.UpsertWorkspaceConfig(context.Background(), "tenant-enc-err", "ws-enc-err", tc.payload)
			if !errors.Is(err, encodeErr) {
				t.Fatalf("UpsertWorkspaceConfig() error = %v, want injected error", err)
			}
			if doc != nil {
				t.Fatalf("UpsertWorkspaceConfig() doc = %+v, want nil on error", doc)
			}
		})
	}
}

// TestUpsertWithDefaultYamlMarshalSucceeds 保证 seam 的默认路径行为不变：
// 未注入失败时 Upsert 仍然完整走通并持久化。
func TestUpsertWithDefaultYamlMarshalSucceeds(t *testing.T) {
	store := NewGormConfigStore(newCov3DB(t))

	doc, err := store.UpsertTenantConfig(context.Background(), "tenant-enc-ok", ScopedConfigDocument{
		OpenAI: &config.OpenAIConfig{APIKey: "k"},
	})
	if err != nil {
		t.Fatalf("UpsertTenantConfig() error = %v", err)
	}
	if doc == nil || doc.OpenAI == nil || doc.OpenAI.APIKey != "k" {
		t.Fatalf("UpsertTenantConfig() doc = %+v, want persisted openai section", doc)
	}
}
