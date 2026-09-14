package knowledgeprovider

import (
	"testing"

	"servify/apps/server/internal/platform/aiprovider"
)

func TestWeKnoraDescriptor(t *testing.T) {
	desc := WeKnoraDescriptor(true, "kb-42")
	if desc.ID != "weknora" || desc.Kind != aiprovider.KindKnowledge || desc.Driver != "weknora" {
		t.Fatalf("unexpected descriptor: %+v", desc)
	}
	if !desc.Enabled {
		t.Fatal("descriptor should be enabled")
	}
	found := map[aiprovider.CapabilityName]bool{}
	for _, cap := range desc.Capabilities {
		found[cap.Name] = cap.Enabled
	}
	if !found[aiprovider.CapabilityRetrieval] || !found[aiprovider.CapabilityIndexing] || !found[aiprovider.CapabilityDeletion] || !found[aiprovider.CapabilityHealthCheck] {
		t.Fatalf("expected all knowledge capabilities enabled, got %+v", desc.Capabilities)
	}
	if desc.Fallback.Priority != 1 {
		t.Fatalf("priority = %d", desc.Fallback.Priority)
	}
}

func TestDifyDescriptor(t *testing.T) {
	desc := DifyDescriptor(false, "ds-7")
	if desc.ID != "dify" || desc.Kind != aiprovider.KindKnowledge || desc.Driver != "dify" {
		t.Fatalf("unexpected descriptor: %+v", desc)
	}
	if desc.Enabled {
		t.Fatal("descriptor should be disabled")
	}
	found := map[aiprovider.CapabilityName]bool{}
	for _, cap := range desc.Capabilities {
		found[cap.Name] = cap.Enabled
	}
	if !found[aiprovider.CapabilityRetrieval] || !found[aiprovider.CapabilityIndexing] || !found[aiprovider.CapabilityDeletion] || !found[aiprovider.CapabilityHealthCheck] {
		t.Fatalf("expected all knowledge capabilities enabled, got %+v", desc.Capabilities)
	}
	if len(desc.Fallback.FallbackTo) != 1 || desc.Fallback.FallbackTo[0] != "weknora" {
		t.Fatalf("fallback = %+v", desc.Fallback)
	}
	if desc.Fallback.Priority != 0 {
		t.Fatalf("priority = %d", desc.Fallback.Priority)
	}
}
