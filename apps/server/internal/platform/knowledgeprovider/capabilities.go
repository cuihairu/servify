package knowledgeprovider

import "servify/apps/server/internal/platform/aiprovider"

func WeKnoraDescriptor(enabled bool, knowledgeID string) aiprovider.ProviderDescriptor {
	return aiprovider.ProviderDescriptor{
		ID:      "weknora",
		Kind:    aiprovider.KindKnowledge,
		Driver:  "weknora",
		Enabled: enabled,
		Capabilities: []aiprovider.CapabilityDeclaration{
			{Name: aiprovider.CapabilityRetrieval, Enabled: true, Metadata: map[string]any{"knowledge_base_id": knowledgeID}},
			{Name: aiprovider.CapabilityIndexing, Enabled: true},
			{Name: aiprovider.CapabilityDeletion, Enabled: true},
			{Name: aiprovider.CapabilityHealthCheck, Enabled: true},
		},
		Fallback: aiprovider.FallbackPolicy{
			Priority: 1,
		},
	}
}

func DifyDescriptor(enabled bool, datasetID string) aiprovider.ProviderDescriptor {
	return aiprovider.ProviderDescriptor{
		ID:      "dify",
		Kind:    aiprovider.KindKnowledge,
		Driver:  "dify",
		Enabled: enabled,
		Capabilities: []aiprovider.CapabilityDeclaration{
			{Name: aiprovider.CapabilityRetrieval, Enabled: true, Metadata: map[string]any{"dataset_id": datasetID}},
			{Name: aiprovider.CapabilityIndexing, Enabled: true},
			{Name: aiprovider.CapabilityDeletion, Enabled: true},
			{Name: aiprovider.CapabilityHealthCheck, Enabled: true},
		},
		Fallback: aiprovider.FallbackPolicy{
			Priority: 0,
			FallbackTo: []string{
				"weknora",
			},
		},
	}
}
