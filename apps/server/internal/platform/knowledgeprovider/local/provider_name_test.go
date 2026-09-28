package local

import (
	"testing"

	"servify/apps/server/internal/platform/knowledgeprovider"
)

// TestProviderName P1-1 外部映射一致性：provider_id 落库值与实际驱动一致。
func TestProviderName(t *testing.T) {
	var named knowledgeprovider.NamedProvider = NewProvider(nil, nil, Config{})
	if named.ProviderName() != ProviderID {
		t.Fatalf("ProviderName() = %q, want %q", named.ProviderName(), ProviderID)
	}
}
