package memory

import (
	"testing"

	"servify/apps/server/internal/platform/knowledgeprovider"
)

// TestProviderName P1-1 外部映射一致性：provider_id 落库值与实际驱动一致。
func TestProviderName(t *testing.T) {
	var named knowledgeprovider.NamedProvider = NewProvider("tenant-a", "kb-a")
	if named.ProviderName() != "memory" {
		t.Fatalf("ProviderName() = %q", named.ProviderName())
	}
}
