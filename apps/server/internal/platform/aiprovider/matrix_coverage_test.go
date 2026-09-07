package aiprovider

import "testing"

func TestMatrixEnabledEqualPrioritySortsByID(t *testing.T) {
	matrix := Matrix{
		Providers: []ProviderDescriptor{
			{ID: "zeta", Kind: KindLLM, Enabled: true, Fallback: FallbackPolicy{Priority: 1}},
			{ID: "alpha", Kind: KindLLM, Enabled: true, Fallback: FallbackPolicy{Priority: 1}},
			{ID: "beta", Kind: KindLLM, Enabled: true, Fallback: FallbackPolicy{Priority: 1}},
		},
	}

	enabled := matrix.Enabled(KindLLM)
	if len(enabled) != 3 {
		t.Fatalf("expected 3 providers, got %d", len(enabled))
	}
	if enabled[0].ID != "alpha" || enabled[1].ID != "beta" || enabled[2].ID != "zeta" {
		t.Fatalf("unexpected order for equal priorities: %s, %s, %s", enabled[0].ID, enabled[1].ID, enabled[2].ID)
	}
}

func TestMatrixEnabledHigherPriorityComesFirst(t *testing.T) {
	matrix := Matrix{
		Providers: []ProviderDescriptor{
			{ID: "low", Kind: KindKnowledge, Enabled: true, Fallback: FallbackPolicy{Priority: 9}},
			{ID: "high", Kind: KindKnowledge, Enabled: true, Fallback: FallbackPolicy{Priority: 1}},
		},
	}

	enabled := matrix.Enabled(KindKnowledge)
	if enabled[0].ID != "high" || enabled[1].ID != "low" {
		t.Fatalf("unexpected order: %s, %s", enabled[0].ID, enabled[1].ID)
	}
}

func TestMatrixEnabledEmpty(t *testing.T) {
	matrix := Matrix{}
	enabled := matrix.Enabled(KindLLM)
	if len(enabled) != 0 {
		t.Fatalf("expected 0 providers, got %d", len(enabled))
	}
}

func TestMatrixFindMissing(t *testing.T) {
	matrix := Matrix{Providers: []ProviderDescriptor{{ID: "openai"}}}
	if _, ok := matrix.Find("missing"); ok {
		t.Fatal("Find() expected ok=false for unknown id")
	}
}
