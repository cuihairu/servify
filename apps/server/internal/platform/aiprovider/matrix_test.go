package aiprovider

import (
	"testing"
	"time"
)

func TestMatrixEnabledSortsByPriority(t *testing.T) {
	matrix := Matrix{
		Providers: []ProviderDescriptor{
			{ID: "openai-secondary", Kind: KindLLM, Enabled: true, Fallback: FallbackPolicy{Priority: 2}},
			{ID: "weknora", Kind: KindKnowledge, Enabled: true, Fallback: FallbackPolicy{Priority: 1}},
			{ID: "openai-primary", Kind: KindLLM, Enabled: true, Fallback: FallbackPolicy{Priority: 1}},
			{ID: "disabled", Kind: KindLLM, Enabled: false, Fallback: FallbackPolicy{Priority: 0}},
		},
	}

	enabled := matrix.Enabled(KindLLM)
	if len(enabled) != 2 {
		t.Fatalf("expected 2 enabled llm providers, got %d", len(enabled))
	}
	if enabled[0].ID != "openai-primary" || enabled[1].ID != "openai-secondary" {
		t.Fatalf("unexpected provider order: %#v", enabled)
	}
}

func TestMatrixFindReturnsDescriptor(t *testing.T) {
	matrix := Matrix{
		Providers: []ProviderDescriptor{
			{
				ID:      "weknora",
				Kind:    KindKnowledge,
				Driver:  "weknora",
				Enabled: true,
				Fallback: FallbackPolicy{
					Priority: 1,
					CircuitBreaker: CircuitBreakerPolicy{
						Enabled:      true,
						MaxFailures:  3,
						ResetTimeout: time.Minute,
					},
				},
			},
		},
	}

	provider, ok := matrix.Find("weknora")
	if !ok {
		t.Fatal("expected provider to be found")
	}
	if provider.Fallback.CircuitBreaker.MaxFailures != 3 {
		t.Fatalf("expected circuit breaker policy to be preserved, got %#v", provider.Fallback)
	}
}

func TestMatrixEnabledSortsByIDOnEqualPriority(t *testing.T) {
	m := Matrix{Providers: []ProviderDescriptor{
		{ID: "zeta", Kind: KindKnowledge, Enabled: true, Fallback: FallbackPolicy{Priority: 1}},
		{ID: "alpha", Kind: KindKnowledge, Enabled: true, Fallback: FallbackPolicy{Priority: 1}},
		{ID: "mid", Kind: KindKnowledge, Enabled: false, Fallback: FallbackPolicy{Priority: 0}},
		{ID: "other-kind", Kind: KindLLM, Enabled: true},
	}}
	got := m.Enabled(KindKnowledge)
	if len(got) != 2 {
		t.Fatalf("expected 2 enabled knowledge providers, got %+v", got)
	}
	if got[0].ID != "alpha" || got[1].ID != "zeta" {
		t.Fatalf("unexpected order: %+v", got)
	}
}

func TestMatrixEnabledComparatorEdgeBranches(t *testing.T) {
	m := Matrix{Providers: []ProviderDescriptor{
		{ID: "same", Kind: KindLLM, Enabled: true, Fallback: FallbackPolicy{Priority: 2}},
		{ID: "same", Kind: KindLLM, Enabled: true, Fallback: FallbackPolicy{Priority: 2}},
		{ID: "high", Kind: KindLLM, Enabled: true, Fallback: FallbackPolicy{Priority: 9}},
		{ID: "low", Kind: KindLLM, Enabled: true, Fallback: FallbackPolicy{Priority: 1}},
	}}
	got := m.Enabled(KindLLM)
	if len(got) != 4 {
		t.Fatalf("expected 4 providers, got %d", len(got))
	}
	if got[0].ID != "low" || got[3].ID != "high" {
		t.Fatalf("unexpected order: %v", []string{got[0].ID, got[1].ID, got[2].ID, got[3].ID})
	}
}
